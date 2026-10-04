package wsl

import (
	"container/list"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	cdclient "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/core/images"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	initialRetryDelay    = 1 * time.Second
	maxRetryDelay        = 30 * time.Second
	retryDelayMultiplier = 2
	maxRetries           = 5
)

const (
	TimeoutFast   = 2 * time.Second
	TimeoutMedium = 5 * time.Second
	TimeoutSlow   = 15 * time.Second
)

type typedCache[T any] struct {
	sync.RWMutex
	data      T
	valid     bool
	timestamp time.Time
	ttl       time.Duration
}

func newTypedCache[T any](ttl time.Duration) *typedCache[T] {
	return &typedCache[T]{ttl: ttl}
}

func (c *typedCache[T]) Get() (T, bool) {
	c.RLock()
	defer c.RUnlock()
	if c.valid && time.Since(c.timestamp) < c.ttl {
		return c.data, true
	}
	var zero T
	return zero, false
}

func (c *typedCache[T]) Set(data T) {
	c.Lock()
	c.data = data
	c.valid = true
	c.timestamp = time.Now()
	c.Unlock()
}

func (c *typedCache[T]) Invalidate() {
	c.Lock()
	c.valid = false
	c.Unlock()
}

const maxBoundedCacheBytes = 5 * 1024 * 1024

type boundedTypedCache[T any] struct {
	mu         sync.RWMutex
	data       map[string]T
	ttl        time.Duration
	valid      bool
	timestamp  time.Time
	maxEntries int
	totalSize  int64
	maxSize    int64
}

func newBoundedTypedCache[T any](ttl time.Duration, maxEntries int) *boundedTypedCache[T] {
	return &boundedTypedCache[T]{
		data:       make(map[string]T, maxEntries),
		ttl:        ttl,
		maxEntries: maxEntries,
		maxSize:    maxBoundedCacheBytes,
	}
}

func (c *boundedTypedCache[T]) GetWithKey(key string) (T, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.valid || time.Since(c.timestamp) >= c.ttl {
		var zero T
		return zero, false
	}
	if val, ok := c.data[key]; ok {
		return val, true
	}
	var zero T
	return zero, false
}

func (c *boundedTypedCache[T]) SetWithKey(key string, value T) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.valid || time.Since(c.timestamp) >= c.ttl {
		c.data = make(map[string]T, c.maxEntries)
		c.valid = true
		c.timestamp = time.Now()
	}
	valSize := int64(0)
	switch v := any(value).(type) {
	case string:
		valSize = int64(len(v))
	case [2]string:
		valSize = int64(len(v[0]) + len(v[1]))
	default:
		valSize = 64
	}
	c.data[key] = value
	c.totalSize += valSize
	for (len(c.data) > c.maxEntries || c.totalSize > c.maxSize) && len(c.data) > 0 {
		var oldestKey string
		for k := range c.data {
			oldestKey = k
			break
		}
		if oldestKey != "" {
			var delSize int64
			switch v := any(c.data[oldestKey]).(type) {
			case string:
				delSize = int64(len(v))
			case [2]string:
				delSize = int64(len(v[0]) + len(v[1]))
			default:
				delSize = 64
			}
			c.totalSize -= delSize
			delete(c.data, oldestKey)
		}
	}
}

func (c *boundedTypedCache[T]) Invalidate() {
	c.mu.Lock()
	c.valid = false
	c.mu.Unlock()
}

type stringCacheEntry struct {
	value     string
	timestamp time.Time
	orderNode *list.Element
}

type boundedStringCache struct {
	mu         sync.RWMutex
	data       map[string]stringCacheEntry
	order      *list.List
	defaultTTL time.Duration
	maxEntries int
}

func newBoundedStringCache(defaultTTL time.Duration, maxEntries int) *boundedStringCache {
	return &boundedStringCache{
		data:       make(map[string]stringCacheEntry, maxEntries),
		order:      list.New(),
		defaultTTL: defaultTTL,
		maxEntries: maxEntries,
	}
}

func (c *boundedStringCache) Get(key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.data[key]
	if !ok {
		return "", false
	}
	if time.Since(entry.timestamp) >= c.defaultTTL {
		return "", false
	}
	return entry.value, true
}

func (c *boundedStringCache) Set(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for node := c.order.Front(); node != nil; {
		next := node.Next()
		cachedKey := node.Value.(string)
		entry, ok := c.data[cachedKey]
		if !ok {
			c.order.Remove(node)
		} else if now.Sub(entry.timestamp) >= c.defaultTTL {
			delete(c.data, cachedKey)
			c.order.Remove(node)
		} else {
			break
		}
		node = next
	}
	if previous, ok := c.data[key]; ok {
		c.order.Remove(previous.orderNode)
		delete(c.data, key)
	}
	for len(c.data) >= c.maxEntries {
		oldest := c.order.Front()
		if oldest == nil {
			break
		}
		oldestKey := oldest.Value.(string)
		delete(c.data, oldestKey)
		c.order.Remove(oldest)
	}
	entry := stringCacheEntry{
		value:     value,
		timestamp: now,
	}
	entry.orderNode = c.order.PushBack(key)
	c.data[key] = entry
}

func (c *boundedStringCache) Invalidate() {
	c.mu.Lock()
	c.data = make(map[string]stringCacheEntry, c.maxEntries)
	c.order.Init()
	c.mu.Unlock()
}

type containerStatusStore struct {
	mu     sync.RWMutex
	data   map[string]statusCacheEntry
	ttl    time.Duration
	maxLen int
}

type statusCacheEntry struct {
	status    string
	timestamp time.Time
}

func newContainerStatusCache(ttl time.Duration) *containerStatusStore {
	return &containerStatusStore{
		data:   make(map[string]statusCacheEntry, 64),
		ttl:    ttl,
		maxLen: 100,
	}
}

func (c *containerStatusStore) get(id string) (string, bool) {
	c.mu.RLock()
	entry, ok := c.data[id]
	c.mu.RUnlock()
	if !ok || time.Since(entry.timestamp) > c.ttl {
		return "", false
	}
	return entry.status, true
}

func (c *containerStatusStore) set(id, status string) {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.maxLen <= 0 {
		return
	}
	for key, entry := range c.data {
		if now.Sub(entry.timestamp) > c.ttl {
			delete(c.data, key)
		}
	}
	if _, exists := c.data[id]; !exists {
		for len(c.data) >= c.maxLen {
			var oldestID string
			var oldestTimestamp time.Time
			foundOldest := false
			for key, entry := range c.data {
				if !foundOldest || entry.timestamp.Before(oldestTimestamp) {
					oldestID = key
					oldestTimestamp = entry.timestamp
					foundOldest = true
				}
			}
			if !foundOldest {
				break
			}
			delete(c.data, oldestID)
		}
	}
	c.data[id] = statusCacheEntry{status: status, timestamp: now}
}

func (c *containerStatusStore) invalidate(id string) {
	c.mu.Lock()
	delete(c.data, id)
	c.mu.Unlock()
}

func (c *containerStatusStore) invalidateAll() {
	c.mu.Lock()
	for k := range c.data {
		delete(c.data, k)
	}
	c.mu.Unlock()
}

const statusCacheTTL = 15 * time.Second
const containerdHealthFailureLimit = 3

var (
	containersCache      = newTypedCache[[]Container](3 * time.Second)
	imagesCache          = newTypedCache[[]Image](5 * time.Second)
	volumesCache         = newTypedCache[[]Volume](10 * time.Second)
	statsCache           = newTypedCache[[]ContainerStat](5 * time.Second)
	containerStatusCache = newContainerStatusCache(statusCacheTTL)

	splitImageCache = newBoundedTypedCache[[2]string](30*time.Second, 500)
	humanSizeCache  = newBoundedTypedCache[string](1*time.Minute, 500)
)

var (
	cdClient    *cdclient.Client
	cdErr       error
	cdAvailable atomic.Bool
	cdIP        string
	cdBaseCtx   context.Context
	cdConn      *grpc.ClientConn
	appCtx      context.Context
	appCancel   context.CancelFunc

	cdMu             sync.Mutex
	cdIPValid        atomic.Bool
	cdHealthFailures atomic.Int32
	shutdownOnce     sync.Once
)

func init() {
	cdBaseCtx = namespaces.WithNamespace(context.Background(), GetCdNamespace())
	appCtx, appCancel = context.WithCancel(context.Background())
}

func DetectWSLIP() string {
	return detectWSLIP(true)
}

func detectWSLIP(forceRefresh bool) string {
	if !forceRefresh {
		cdMu.Lock()
		cachedIP := cdIP
		valid := cdIPValid.Load()
		cdMu.Unlock()
		if cachedIP != "" && valid {
			return cachedIP
		}
	}
	cmd := exec.Command(wslExecutable(), "-d", GetWslDistro(), "hostname", "-I")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		cdMu.Lock()
		cdIPValid.Store(false)
		cdMu.Unlock()
		return ""
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) > 0 {
		newIP := fields[0]
		cdMu.Lock()
		changed := newIP != cdIP
		cdIP = newIP
		cdIPValid.Store(true)
		cdMu.Unlock()
		if changed {
			resetCDClient()
		}
		return newIP
	}
	cdMu.Lock()
	cdIPValid.Store(false)
	cdMu.Unlock()
	return ""
}

func resetCDClient() {
	cdMu.Lock()
	defer cdMu.Unlock()
	if cdConn != nil {
		if !cdAvailable.Load() {
			cdConn.Close()
			cdConn = nil
		}
	}
	cdClient = nil
	cdErr = nil
	cdAvailable.Store(false)
	cdHealthFailures.Store(0)
}

func shouldResetCDClientAfterPing(pingErr error) bool {
	if pingErr == nil {
		cdHealthFailures.Store(0)
		return false
	}
	if cdHealthFailures.Add(1) < containerdHealthFailureLimit {
		return false
	}
	cdHealthFailures.Store(0)
	return true
}

func resetCDClientAfterHealthFailure(client *cdclient.Client) {
	cdMu.Lock()
	defer cdMu.Unlock()
	if cdClient != client {
		return
	}
	if cdConn != nil {
		cdConn.Close()
		cdConn = nil
	}
	cdClient = nil
	cdErr = nil
	cdAvailable.Store(false)
	cdHealthFailures.Store(0)
}

func getCDClient() (*cdclient.Client, error) {
	cdMu.Lock()
	cachedClient := cdClient
	clientAvailable := cdAvailable.Load()
	cdMu.Unlock()
	if cachedClient != nil && clientAvailable {
		return cachedClient, nil
	}
	ip := detectWSLIP(false)
	if ip == "" {
		ip = detectWSLIP(true)
	}
	if ip == "" {
		cdErr = fmt.Errorf("не удалось определить IP WSL2")
		return nil, cdErr
	}
	for attempt := 0; attempt < maxRetries; attempt++ {
		select {
		case <-appCtx.Done():
			return nil, appCtx.Err()
		default:
		}
		addr := fmt.Sprintf("%s:%d", ip, GetCdPort())
		var client *cdclient.Client
		conn, err := grpc.NewClient(addr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithUnaryInterceptor(func(ctx context.Context, method string, req, reply any, connection *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
				callErr := invoker(ctx, method, req, reply, connection, opts...)
				cdMu.Lock()
				activeClient := client
				cdMu.Unlock()
				if strings.HasSuffix(method, "/Version") {
					if shouldResetCDClientAfterPing(callErr) {
						resetCDClientAfterHealthFailure(activeClient)
					}
				} else if callErr != nil && activeClient != nil {
					pingCtx, cancel := context.WithTimeout(context.Background(), time.Second)
					_, pingErr := activeClient.Version(pingCtx)
					cancel()
					if shouldResetCDClientAfterPing(pingErr) {
						resetCDClientAfterHealthFailure(activeClient)
					}
				}
				return callErr
			}),
		)
		if err != nil {
			cdErr = err
			delay := initialRetryDelay * time.Duration(retryDelayMultiplier^attempt)
			if delay > maxRetryDelay {
				delay = maxRetryDelay
			}
			if attempt < maxRetries-1 {
				time.Sleep(delay)
			}
			continue
		}
		client, err = cdclient.NewWithConn(conn,
			cdclient.WithDefaultNamespace(GetCdNamespace()),
		)
		if err != nil {
			conn.Close()
			cdErr = err
			delay := initialRetryDelay * time.Duration(retryDelayMultiplier^attempt)
			if delay > maxRetryDelay {
				delay = maxRetryDelay
			}
			if attempt < maxRetries-1 {
				time.Sleep(delay)
			}
			continue
		}
		cdMu.Lock()
		if cdClient != nil && cdAvailable.Load() {
			cdMu.Unlock()
			client.Close()
			conn.Close()
			return cdClient, nil
		}
		if cdConn != nil && cdConn != conn {
			cdConn.Close()
		}
		cdConn = conn
		cdClient = client
		cdErr = nil
		cdAvailable.Store(true)
		cdHealthFailures.Store(0)
		cdMu.Unlock()
		return cdClient, nil
	}
	if cdErr == nil {
		cdErr = fmt.Errorf("не удалось подключиться к containerd после %d попыток", maxRetries)
	}
	return nil, cdErr
}

func Shutdown() {
	shutdownOnce.Do(func() {
		if appCancel != nil {
			appCancel()
		}
		cdMu.Lock()
		if cdConn != nil {
			cdConn.Close()
			cdConn = nil
		}
		cdClient = nil
		cdErr = nil
		cdAvailable.Store(false)
		cdMu.Unlock()
		cdIPValid.Store(false)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, WslExecutable(), "--shutdown")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		_ = cmd.Run()
	})
}

func AppContext() context.Context {
	return appCtx
}

func cdCtx(timeout time.Duration) (context.Context, context.CancelFunc) {
	cdMu.Lock()
	baseCtx := cdBaseCtx
	cdMu.Unlock()
	return context.WithTimeout(baseCtx, timeout)
}

func CDCheck() error {
	if cdAvailable.Load() {
		return nil
	}
	client, err := getCDClient()
	if err != nil {
		return err
	}
	ctx, cancel := cdCtx(TimeoutFast)
	defer cancel()
	_, err = client.ListImages(ctx)
	if err == nil {
		cdAvailable.Store(true)
	}
	return err
}

type ContainerConfig struct {
	ID      string
	Name    string
	Image   string
	Volumes []string
	Ports   string
	Labels  map[string]string
	Network string
	Env     []string
	Cmd     string
}

func CDGetContainerConfig(id string) (*ContainerConfig, error) {
	client, err := getCDClient()
	if err != nil {
		return nil, err
	}
	ctx, cancel := cdCtx(TimeoutSlow)
	defer cancel()
	container, err := client.LoadContainer(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("не удалось загрузить контейнер %s: %w", id, err)
	}
	info, err := container.Info(ctx)
	if err != nil {
		return nil, fmt.Errorf("не удалось получить информацию о контейнере: %w", err)
	}
	name := info.Labels["nerdctl/name"]
	if name == "" {
		name = id
	}
	config := &ContainerConfig{
		ID:     id,
		Name:   name,
		Image:  info.Image,
		Labels: make(map[string]string),
	}
	for k, v := range info.Labels {
		config.Labels[k] = v
	}
	if config.Image == "" {
		if img, ok := info.Labels["nerdctl/image"]; ok {
			config.Image = img
		}
	}
	if env, ok := info.Labels["nerdctl/env"]; ok {
		config.Env = strings.Split(env, "\n")
	}
	if cmd, ok := info.Labels["nerdctl/cmd"]; ok {
		config.Cmd = cmd
	}
	if network, ok := info.Labels["nerdctl/network"]; ok {
		config.Network = network
	}
	if ports, ok := info.Labels["nerdctl/ports"]; ok {
		config.Ports = ports
	}
	if volumes, ok := info.Labels["nerdctl/volumes"]; ok {
		config.Volumes = strings.Split(volumes, "\n")
	}
	return config, nil
}

type ContainerStatJSON struct {
	ID       string `json:"ID"`
	Name     string `json:"Name"`
	CPUPerc  string `json:"CPUPerc"`
	MemUsage string `json:"MemUsage"`
	NetIO    string `json:"NetIO"`
	PIDs     string `json:"PIDs"`
}

func CDGetStats() ([]ContainerStat, error) {
	if cached, ok := statsCache.Get(); ok {
		GlobalCacheManager.RecordHit("stats")
		return cached, nil
	}
	GlobalCacheManager.RecordMiss("stats")
	out, err := runRootNerdctl(context.Background(), "stats --no-stream --format '{{json .}}'")
	if err != nil {
		GlobalCacheManager.RecordError("stats")
		return nil, err
	}
	result := parseContainerStatsOutput(out)
	statsCache.Set(result)
	return result, nil
}

func parseContainerStatsOutput(out string) []ContainerStat {
	lines := strings.Split(out, "\n")
	var result []ContainerStat
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var stat ContainerStatJSON
		if err := json.Unmarshal([]byte(line), &stat); err != nil {
			continue
		}
		id := stat.ID
		if len(id) > 12 {
			id = id[:12]
		}
		pids := stat.PIDs
		if pids == "0" {
			pids = "—"
		}
		name := strings.TrimSpace(stat.Name)
		if name != "" {
			name = shortContainerName(name)
		}
		result = append(result, ContainerStat{
			ID:     id,
			Name:   name,
			CPU:    stat.CPUPerc,
			Memory: stat.MemUsage,
			NetIO:  stat.NetIO,
			PIDs:   pids,
		})
	}
	return result
}

func shortContainerName(name string) string {
	name = strings.TrimPrefix(name, "soul-dialogue-")
	name = strings.TrimPrefix(name, "containerd-")
	if name == "" {
		return "container"
	}
	return name
}

func CDGetContainerLogs(id string, tail int) (string, error) {
	tailArg := "all"
	if tail > 0 {
		tailArg = strconv.Itoa(tail)
	}
	logsArgs := fmt.Sprintf("logs --tail %s", tailArg)
	startedAt, inspectErr := runRootNerdctl(context.Background(), fmt.Sprintf("inspect --format '{{.State.StartedAt}}' %s", shellQuote(id)))
	if inspectErr == nil {
		if normalizedStart := normalizeContainerStartTime(startedAt); normalizedStart != "" {
			logsArgs += " --since " + shellQuote(normalizedStart)
		}
	}
	logsArgs += " " + shellQuote(id)
	out, err := runRootNerdctl(context.Background(), logsArgs)
	if err != nil {
		if detail := strings.TrimSpace(out); detail != "" {
			return "", fmt.Errorf("не удалось получить логи контейнера %s: %s", id, detail)
		}
		return "", err
	}
	if strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("логи для контейнера %s не найдены", id)
	}
	return out, nil
}

func CDGetContainerLogsWithTimestamp(id string, tail int) (string, string, error) {
	tailArg := "all"
	if tail > 0 {
		tailArg = strconv.Itoa(tail)
	}
	logsArgs := fmt.Sprintf("logs --timestamps --tail %s", tailArg)
	startedAt, inspectErr := runRootNerdctl(context.Background(), fmt.Sprintf("inspect --format '{{.State.StartedAt}}' %s", shellQuote(id)))
	if inspectErr == nil {
		if normalizedStart := normalizeContainerStartTime(startedAt); normalizedStart != "" {
			logsArgs += " --since " + shellQuote(normalizedStart)
		}
	}
	out, err := runRootNerdctl(context.Background(), logsArgs+" "+shellQuote(id))
	if err != nil {
		if detail := strings.TrimSpace(out); detail != "" {
			return "", "", fmt.Errorf("не удалось получить логи контейнера %s: %s", id, detail)
		}
		return "", "", err
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	lastTimestamp := ""
	for index, line := range lines {
		timestamp, message, ok := stripContainerLogTimestamp(line)
		if !ok {
			continue
		}
		lines[index] = message
		lastTimestamp = timestamp
	}
	logs := strings.TrimSpace(strings.Join(lines, "\n"))
	if _, err := time.Parse(time.RFC3339Nano, lastTimestamp); err != nil {
		lastTimestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	return logs, lastTimestamp, nil
}

type ContainerLogEntry struct {
	Timestamp string
	Message   string
}

func CDGetContainerLogEntriesSince(ctx context.Context, id, since string) ([]ContainerLogEntry, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("ID контейнера не может быть пустым")
	}
	since = normalizeContainerStartTime(since)
	if since == "" {
		return nil, fmt.Errorf("некорректная временная метка для обновления логов")
	}

	command := fmt.Sprintf("logs --timestamps --since %s %s", shellQuote(since), shellQuote(id))
	out, err := runRootNerdctl(ctx, command)
	if err != nil {
		if detail := strings.TrimSpace(out); detail != "" {
			return nil, fmt.Errorf("не удалось обновить логи контейнера %s: %s", id, detail)
		}
		return nil, err
	}
	return parseContainerLogEntries(out), nil
}

func parseContainerLogEntries(output string) []ContainerLogEntry {
	var entries []ContainerLogEntry
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		timestamp, message, ok := splitContainerLogTimestamp(line)
		if !ok {
			entries = append(entries, ContainerLogEntry{Message: line})
			continue
		}
		entries = append(entries, ContainerLogEntry{Timestamp: timestamp, Message: message})
	}
	return entries
}

func StreamContainerLogs(ctx context.Context, id, since string, onLine func(timestamp, message string)) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("ID контейнера не может быть пустым")
	}
	if since = normalizeContainerStartTime(since); since == "" {
		return fmt.Errorf("некорректная временная метка для продолжения логов")
	}

	command := fmt.Sprintf("logs --follow --timestamps --since %s %s", shellQuote(since), shellQuote(id))
	_, err := runWSLAsRootWithCancelStream(ctx, rootNerdctlCommand(command), func(line string) {
		timestamp, message, ok := splitContainerLogTimestamp(line)
		if !ok {
			if onLine != nil {
				onLine("", line)
			}
			return
		}
		if onLine != nil {
			onLine(timestamp, message)
		}
	})
	return err
}

func splitContainerLogTimestamp(line string) (string, string, bool) {
	timestamp, message, ok := stripContainerLogTimestamp(line)
	if !ok {
		return "", strings.TrimSpace(line), false
	}
	if _, err := time.Parse(time.RFC3339Nano, timestamp); err != nil {
		return "", strings.TrimSpace(line), false
	}
	return timestamp, message, true
}

func stripContainerLogTimestamp(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	separator := strings.IndexByte(line, ' ')
	if separator <= 0 {
		return "", line, false
	}
	timestamp := line[:separator]
	return timestamp, strings.TrimLeft(line[separator+1:], " "), true
}

func normalizeContainerStartTime(raw string) string {
	raw = strings.ReplaceAll(raw, `\x0a`, "")
	raw = strings.ReplaceAll(raw, `\n`, "")
	raw = strings.ReplaceAll(raw, `\r`, "")
	raw = strings.Trim(raw, " \t\r\n\\\"'")
	if _, err := time.Parse(time.RFC3339Nano, raw); err != nil {
		return ""
	}
	return raw
}

func CDGetContainerStartupLogs(id string, tail int) (string, error) {
	inspect, err := runRootNerdctl(context.Background(), fmt.Sprintf(
		"inspect --format '{{.Name}}\nstatus={{.State.Status}}\nexit_code={{.State.ExitCode}}\nerror={{.State.Error}}\nrestart_count={{.RestartCount}}\nlog_path={{.LogPath}}' %s",
		shellQuote(id),
	))
	if err != nil {
		return "", err
	}

	result := strings.TrimSpace(inspect)
	status := ""
	for _, line := range strings.Split(inspect, "\n") {
		if strings.HasPrefix(line, "status=") {
			status = strings.TrimSpace(strings.TrimPrefix(line, "status="))
		}
	}
	if status == "running" {
		lines := strings.Split(result, "\n")
		filtered := lines[:0]
		for _, line := range lines {
			if strings.HasPrefix(line, "error=") {
				continue
			}
			filtered = append(filtered, line)
		}
		result = strings.Join(filtered, "\n")
	}
	for _, line := range strings.Split(inspect, "\n") {
		if !strings.HasPrefix(line, "log_path=") {
			continue
		}
		logPath := strings.TrimSpace(strings.TrimPrefix(line, "log_path="))
		if logPath == "" {
			continue
		}
		logOutput, logErr := runWSLAsRootWithTimeout(
			"tail -n "+strconv.Itoa(tail)+" "+shellQuote(logPath)+" 2>/dev/null || true",
			10*time.Second,
		)
		if logErr == nil && strings.TrimSpace(logOutput) != "" {
			result += "\n\n--- runtime log ---\n" + strings.TrimSpace(logOutput)
		}
		break
	}
	return result, nil
}

func CDClearContainerLogs(id string) error {
	inspect, err := runRootNerdctl(context.Background(), fmt.Sprintf("inspect --format '{{.LogPath}}' %s", shellQuote(id)))
	if err != nil {
		return err
	}
	logPath := cleanContainerLogPath(inspect)
	if logPath == "" {
		return fmt.Errorf("путь к логам контейнера %s не найден", id)
	}
	_, err = runWSLAsRootWithTimeout("truncate -s 0 "+shellQuote(logPath), 10*time.Second)
	return err
}

func cleanContainerLogPath(raw string) string {
	cleaned := strings.TrimSpace(strings.ReplaceAll(raw, "\x00", ""))
	if strings.ContainsAny(cleaned, "\r\n") || !strings.HasPrefix(cleaned, "/") {
		return ""
	}
	return cleaned
}

func CDGetDBInfo(volumeName string) (string, []string, error) {
	mountpoint := volumeDataPath(volumeName)
	lookup, lookupErr := runRootNerdctl(context.Background(), "volume ls --format '{{json .}}'")
	if lookupErr == nil {
		for _, line := range strings.Split(lookup, "\n") {
			var volume struct {
				Name       string `json:"Name"`
				Directory  string `json:"Directory"`
				Mountpoint string `json:"Mountpoint"`
			}
			if json.Unmarshal([]byte(strings.TrimSpace(line)), &volume) == nil {
				if volume.Mountpoint == "" {
					volume.Mountpoint = volume.Directory
				}
			}
			if volume.Name != "" &&
				(strings.HasSuffix(volume.Name, volumeName) || volume.Name == volumeName) &&
				volume.Mountpoint != "" {
				mountpoint = volume.Mountpoint
				break
			}
		}
	}
	if mountpoint == "" {
		return "", nil, fmt.Errorf("путь к тому базы данных %s не найден", volumeName)
	}
	dbPath := mountpoint
	pgdataPath := mountpoint + "/pgdata"
	if out, err := runWSLAsRootWithTimeout("test -d "+shellQuote(pgdataPath), TimeoutFast); err == nil && strings.TrimSpace(out) == "" {
		dbPath = pgdataPath
	}
	quotedDBPath := shellQuote(dbPath)
	out, err := runWSLAsRootWithTimeout(fmt.Sprintf(
		"if [ ! -d %s ]; then echo '===ERROR==='; echo \"Том не найден: %s\"; exit 1; fi; "+
			"%s; echo '===FILES==='; "+
			"find %s -mindepth 1 -maxdepth 3 -printf '%%y\\t%%s\\t%%P\\n' | sort -k3 | head -n 500",
		quotedDBPath, dbPath, duSizeCommand(dbPath), quotedDBPath,
	), TimeoutMedium)
	if err != nil {
		details := strings.TrimSpace(out)
		if details != "" {
			return "", nil, fmt.Errorf("не удалось прочитать том %s: %w: %s", volumeName, err, details)
		}
		return "", nil, fmt.Errorf("не удалось прочитать том %s: %w", volumeName, err)
	}
	parts := strings.SplitN(out, "===FILES===", 2)
	size := "—"
	if len(parts) > 0 {
		fields := strings.Fields(strings.TrimSpace(parts[0]))
		if len(fields) > 0 {
			size = fields[0]
		}
	}
	var files []string
	if len(parts) > 1 {
		for _, line := range strings.Split(parts[1], "\n") {
			line = strings.TrimSpace(line)
			fields := strings.SplitN(line, "\t", 3)
			if len(fields) == 3 && fields[2] != "" {
				kind := "FILE"
				switch fields[0] {
				case "d":
					kind = "DIR"
				case "l":
					kind = "LINK"
				}
				files = append(files, fmt.Sprintf("%-4s %8s  %s", kind, humanBytes(fields[1]), fields[2]))
			}
		}
	}
	return size, files, nil
}

func humanBytes(value string) string {
	bytes, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return value
	}
	units := []string{"B", "K", "M", "G", "T"}
	unit := 0
	amount := float64(bytes)
	for amount >= 1024 && unit < len(units)-1 {
		amount /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%d%s", bytes, units[unit])
	}
	return fmt.Sprintf("%.1f%s", amount, units[unit])
}

func volumeDataPath(volumeName string) string {
	if volumeName == GetDBVolumeName() {
		if projectPath := GetProjectPathWSL(); projectPath != "" {
			return projectPath + "/.containerd-data/postgres"
		}
	}
	ns := GetCdNamespace()
	return "/var/lib/nerdctl/" + ns + "/volumes/" + volumeName + "/_data"
}

func CDListContainers(all bool) ([]Container, error) {
	if cached, ok := containersCache.Get(); ok {
		if all {
			return cached, nil
		}
		return filterRunningContainers(cached), nil
	}

	containers, err := listContainersFallback()
	if err != nil {
		return nil, err
	}
	if all {
		return containers, nil
	}
	return filterRunningContainers(containers), nil
}

func listContainersFallback() ([]Container, error) {
	out, err := runRootNerdctl(context.Background(), "ps -a --format '{{json .}}' 2>/dev/null")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return []Container{}, nil
	}
	var result []Container
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var c struct {
			ID     string `json:"ID"`
			Name   string `json:"Names"`
			Image  string `json:"Image"`
			Status string `json:"Status"`
			Ports  string `json:"Ports"`
		}
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			continue
		}
		result = append(result, normalizeContainer(c.ID, c.Name, c.Image, c.Status, c.Ports))
	}
	containersCache.Set(result)
	return result, nil
}

func filterRunningContainers(containers []Container) []Container {
	running := make([]Container, 0, len(containers))
	for _, container := range containers {
		if isContainerRunning(container.Status) {
			running = append(running, container)
		}
	}
	return running
}

func normalizeContainer(id, name, image, status, ports string) Container {
	if name == "" {
		name = id
	}
	if len(id) > 12 {
		id = id[:12]
	}
	status, uptime, health := splitContainerStatus(status)
	return Container{ID: id, Name: name, Image: image, Status: status, Uptime: uptime, Health: health, Ports: ports}
}

func splitContainerStatus(status string) (normalizedStatus, uptime, health string) {
	normalizedStatus = strings.TrimSpace(status)
	lowerStatus := strings.ToLower(normalizedStatus)
	for _, state := range []string{"unhealthy", "healthy", "health: starting"} {
		marker := "(" + state + ")"
		if index := strings.LastIndex(lowerStatus, marker); index >= 0 {
			health = state
			normalizedStatus = strings.TrimSpace(normalizedStatus[:index] + normalizedStatus[index+len(marker):])
			lowerStatus = strings.ToLower(normalizedStatus)
			break
		}
	}
	if strings.HasPrefix(lowerStatus, "up ") {
		uptime = strings.TrimSpace(normalizedStatus[len("up "):])
	}
	return normalizedStatus, uptime, health
}

func isContainerRunning(status string) bool {
	status = strings.ToLower(status)
	return status == "running" || strings.Contains(status, "up")
}

func determineContainerStatus(client *cdclient.Client, ctx context.Context, id string) string {
	container, err := client.LoadContainer(ctx, id)
	if err != nil {
		return "exited"
	}
	task, err := container.Task(ctx, nil)
	if err != nil {
		return "exited"
	}
	ts, err := task.Status(ctx)
	if err != nil {
		return "exited"
	}
	switch ts.Status {
	case cdclient.Running:
		return "running"
	case cdclient.Created:
		return "created"
	case cdclient.Paused:
		return "paused"
	case cdclient.Stopped:
		return "exited"
	default:
		return string(ts.Status)
	}
}

func getContainerStatusesBatch() (map[string]string, error) {
	out, err := RunWSLCacheable("nerdctl ps -a --format '{{.ID}}\t{{.Status}}' 2>/dev/null")
	if err != nil || out == "" {
		return nil, err
	}
	statuses := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		id := strings.TrimSpace(parts[0])
		status := strings.TrimSpace(parts[1])
		if strings.Contains(status, "Up") || strings.Contains(status, "running") {
			status = "running"
		} else if strings.Contains(status, "Exited") {
			status = "exited"
		} else if strings.Contains(status, "Created") {
			status = "created"
		} else if strings.Contains(status, "Paused") {
			status = "paused"
		}
		if id != "" {
			statuses[id] = status
		}
	}
	return statuses, nil
}

func CDListImages() ([]Image, error) {
	if cached, ok := imagesCache.Get(); ok {
		return cached, nil
	}
	return listImagesFallback()
}

func listImagesFallback() ([]Image, error) {
	out, err := runRootNerdctl(context.Background(), "images --format '{{json .}}' 2>/dev/null")
	if err != nil || out == "" {
		return []Image{}, nil
	}
	var result []Image
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		img, err := parseImageLine(line)
		if err != nil {
			continue
		}
		result = append(result, img)
	}
	imagesCache.Set(result)
	return result, nil
}

func parseImageLine(line string) (Image, error) {
	var img Image
	if err := json.Unmarshal([]byte(line), &img); err != nil {
		return Image{}, err
	}
	img.CreatedAt = normalizeImageCreatedAt(img.CreatedAt)
	img.Size = normalizeImageSize(img.Size)
	return img, nil
}

var imageSizePattern = regexp.MustCompile(`(?i)^([0-9]+(?:\.[0-9]+)?)\s*([kmgtpe])((?:i?b)?)$`)

func normalizeImageSize(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "—"
	}
	if bytes, err := strconv.ParseInt(raw, 10, 64); err == nil && bytes >= 0 {
		return humanImageSize(float64(bytes))
	}
	parts := imageSizePattern.FindStringSubmatch(raw)
	if parts == nil {
		return raw
	}
	value, err := strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return raw
	}
	unit := strings.ToUpper(parts[2])
	if strings.HasPrefix(strings.ToLower(parts[3]), "i") {
		unit += "iB"
	} else {
		unit += "B"
	}
	return fmt.Sprintf("%s %s", trimImageSizeNumber(value), unit)
}

func humanImageSize(bytes float64) string {
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	unit := 0
	for bytes >= 1024 && unit < len(units)-1 {
		bytes /= 1024
		unit++
	}
	return fmt.Sprintf("%s %s", trimImageSizeNumber(bytes), units[unit])
}

func trimImageSizeNumber(value float64) string {
	formatted := strconv.FormatFloat(value, 'f', 2, 64)
	return strings.TrimRight(strings.TrimRight(formatted, "0"), ".")
}

func normalizeImageCreatedAt(raw string) string {
	raw = strings.TrimSpace(raw)
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05 -0700 MST",
		"2006-01-02 15:04:05 -0700",
		"2006-01-02 15:04:05 MST",
		"2006-01-02 15:04:05",
	} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed.Format(time.RFC3339Nano)
		}
	}
	return raw
}

func getImageSizes(ctx context.Context, imgs []images.Image) map[string]int64 {
	defer func() { recover() }()
	sizeMap := make(map[string]int64, len(imgs))
	imageSizeCache.RLock()
	for _, img := range imgs {
		digest := img.Target.Digest.String()
		if size, cached := imageSizeCache.m[digest]; cached {
			sizeMap[digest] = size
		}
	}
	imageSizeCache.RUnlock()
	for _, img := range imgs {
		digest := img.Target.Digest.String()
		if _, ok := sizeMap[digest]; ok {
			continue
		}
		size := int64(0)
		func() {
			defer func() { recover() }()
			size, _ = img.Size(ctx, nil, nil)
		}()
		sizeMap[digest] = size
		addImageSizeWithCleanup(digest, size)
	}
	return sizeMap
}

const maxImageSizeCacheBytes = 10 * 1024 * 1024

var imageSizeCache = struct {
	sync.RWMutex
	m         map[string]int64
	count     int
	maxLen    int
	totalSize int64
	maxSize   int64
}{
	m:       make(map[string]int64),
	maxLen:  200,
	maxSize: maxImageSizeCacheBytes,
}

func ClearImageSizeCache() {
	imageSizeCache.Lock()
	imageSizeCache.m = make(map[string]int64)
	imageSizeCache.count = 0
	imageSizeCache.totalSize = 0
	imageSizeCache.Unlock()
}

func addImageSizeWithCleanup(digest string, size int64) {
	imageSizeCache.Lock()
	for (len(imageSizeCache.m) >= imageSizeCache.maxLen || imageSizeCache.totalSize > imageSizeCache.maxSize) && len(imageSizeCache.m) > 0 {
		var oldestKey string
		for k := range imageSizeCache.m {
			oldestKey = k
			break
		}
		if oldestKey != "" {
			delete(imageSizeCache.m, oldestKey)
		}
	}
	imageSizeCache.m[digest] = size
	imageSizeCache.count++
	imageSizeCache.Unlock()
}

func cachedSplitImageRef(ref string) (string, string) {
	if val, ok := splitImageCache.GetWithKey(ref); ok {
		return val[0], val[1]
	}
	repo, tag := splitImageRef(ref)
	splitImageCache.SetWithKey(ref, [2]string{repo, tag})
	return repo, tag
}

func cachedHumanSize(size int64) string {
	key := fmt.Sprintf("%d", size)
	if s, ok := humanSizeCache.GetWithKey(key); ok {
		return s
	}
	s := humanSize(size)
	humanSizeCache.SetWithKey(key, s)
	return s
}

func CDListVolumes() ([]Volume, error) {
	if cached, ok := volumesCache.Get(); ok {
		return cached, nil
	}
	out, err := runRootNerdctl(context.Background(), "volume ls --format '{{json .}}' 2>/dev/null")
	if err != nil {
		return nil, err
	}
	result := parseVolumeLines(out)
	volumesCache.Set(result)
	return result, nil
}

func CDGetVolumeSizes(ctx context.Context, volumes []Volume) map[string]string {
	sizes := make(map[string]string, len(volumes))
	if ctx.Err() != nil || len(volumes) == 0 {
		return sizes
	}

	var script strings.Builder
	for _, volume := range volumes {
		mountpoint := strings.TrimSpace(volume.Mountpoint)
		if volume.Name == "" || mountpoint == "" {
			continue
		}
		fmt.Fprintf(&script, "printf '%%s\\t' %s; %s\n", shellQuote(volume.Name), duSizeCommand(mountpoint))
	}
	if script.Len() == 0 || ctx.Err() != nil {
		return sizes
	}

	output, err := runWSLAsRootWithCancelStream(ctx, script.String(), nil)
	if err != nil || ctx.Err() != nil {
		return sizes
	}
	for _, line := range strings.Split(output, "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "\t", 2)
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			sizes[parts[0]] = parts[1]
		}
	}
	return sizes
}

func duSizeCommand(path string) string {
	return "du -sh -- " + shellQuote(path) + " 2>/dev/null | cut -f1"
}

// parseVolumeLines разбирает вывод "nerdctl volume ls --format '{{json .}}'",
// пропуская пустые строки и мусор (диагностику WSL, предупреждения nerdctl).
func parseVolumeLines(out string) []Volume {
	var result []Volume
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		volume, err := parseVolumeLine(line)
		if err != nil || volume.Name == "" {
			continue
		}
		result = append(result, volume)
	}
	return result
}

func parseVolumeLine(line string) (Volume, error) {
	var volume struct {
		Name       string `json:"Name"`
		Driver     string `json:"Driver"`
		Mountpoint string `json:"Mountpoint"`
		Directory  string `json:"Directory"`
	}
	if err := json.Unmarshal([]byte(line), &volume); err != nil {
		return Volume{}, err
	}
	volume.Name = strings.TrimSpace(volume.Name)
	volume.Driver = strings.TrimSpace(volume.Driver)
	volume.Mountpoint = strings.TrimSpace(volume.Mountpoint)
	volume.Directory = strings.TrimSpace(volume.Directory)
	if volume.Mountpoint == "" {
		volume.Mountpoint = volume.Directory
	}
	if volume.Driver == "" {
		volume.Driver = "local"
	}
	return Volume{
		Name:       volume.Name,
		Driver:     volume.Driver,
		Mountpoint: volume.Mountpoint,
	}, nil
}

func CDGetUsedVolumes(ctx context.Context) (map[string]bool, error) {
	client, err := getCDClient()
	if err == nil {
		cdCtx2, cancel := cdCtx(TimeoutMedium)
		defer cancel()
		store := client.ContainerService()
		containers, err := store.List(cdCtx2)
		if err == nil {
			used := make(map[string]bool)
			for _, c := range containers {
				container, err := client.LoadContainer(cdCtx2, c.ID)
				if err != nil {
					continue
				}
				spec, err := container.Spec(cdCtx2)
				if err != nil {
					continue
				}
				for _, mount := range spec.Mounts {
					if mount.Type == "volume" && mount.Source != "" {
						used[mount.Source] = true
					}
				}
			}
			return used, nil
		}
	}
	return getUsedVolumesFallback()
}

func getUsedVolumesFallback() (map[string]bool, error) {
	used := make(map[string]bool)
	out, err := RunWSL("nerdctl ps -a --format '{{.ID}}\t{{.Mounts}}' 2>/dev/null")
	if err != nil || out == "" {
		return used, nil
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		mounts := parts[1]
		for _, m := range strings.Split(mounts, ",") {
			m = strings.TrimSpace(m)
			if idx := strings.Index(m, " -> "); idx > 0 {
				volName := strings.TrimSpace(m[:idx])
				if volName != "" {
					used[volName] = true
				}
			}
		}
	}
	return used, nil
}

func CDStopContainer(id string) error {
	client, err := getCDClient()
	if err == nil {
		ctx, cancel := cdCtx(TimeoutSlow)
		defer cancel()
		container, err := client.LoadContainer(ctx, id)
		if err == nil {
			task, err := container.Task(ctx, nil)
			if err == nil {
				_ = task.Kill(ctx, syscall.SIGTERM)
				waitCh, err := task.Wait(ctx)
				if err == nil {
					select {
					case <-waitCh:
					case <-time.After(TimeoutMedium):
						_ = task.Kill(ctx, syscall.SIGKILL)
						<-waitCh
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				_, err = task.Delete(ctx)
				if err == nil {
					return nil
				}
			}
		}
	}
	_, err = runRootNerdctl(context.Background(), "stop "+shellQuote(id))
	return err
}

func CDStartContainer(id string) error {
	client, err := getCDClient()
	if err == nil {
		ctx, cancel := cdCtx(TimeoutSlow)
		defer cancel()
		container, err := client.LoadContainer(ctx, id)
		if err == nil {
			task, err := container.Task(ctx, nil)
			if err == nil {
				ts, err := task.Status(ctx)
				if err == nil && ts.Status == cdclient.Running {
					return nil
				}
				_, _ = task.Delete(ctx)
			}
			labels, _ := container.Labels(ctx)
			logURI := labels["containerd.io/restart.loguri"]
			var ioCreator cio.Creator
			if logURI != "" {
				uri, err := cio.LogURIGenerator("binary", logURI, nil)
				if err == nil {
					ioCreator = cio.LogURI(uri)
				} else {
					ioCreator = cio.NewCreator()
				}
			} else {
				ioCreator = cio.NewCreator()
			}
			task, err = container.NewTask(ctx, ioCreator)
			if err == nil {
				if err := task.Start(ctx); err == nil {
					return nil
				}
			}
		}
	}
	_, err = runRootNerdctl(context.Background(), "start "+shellQuote(id))
	return err
}

func CDRestartContainer(id string) error {
	if err := CDStopContainer(id); err != nil {
		return fmt.Errorf("остановка через containerd: %w", err)
	}
	if err := CDStartContainer(id); err != nil {
		return fmt.Errorf("запуск через containerd: %w", err)
	}
	return nil
}

func CDRemoveContainer(id string) error {
	_, err := runRootNerdctl(context.Background(), "rm -f "+shellQuote(id))
	return err
}

func CDRemoveImage(ref string) error {
	_, err := runRootNerdctl(context.Background(), "rmi -f "+shellQuote(ref))
	return err
}

func lookupVolumeMountpoint(name string) string {
	out, err := runRootNerdctl(context.Background(), "volume ls --format '{{json .}}'")
	if err != nil {
		return ""
	}
	for _, volume := range parseVolumeLines(out) {
		if volume.Name == name {
			return volume.Mountpoint
		}
	}
	return ""
}

func CDRemoveVolume(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("имя тома не может быть пустым")
	}

	mountpoint := lookupVolumeMountpoint(name)
	output, removalErr := runRootNerdctl(context.Background(), volumeRemoveCommand(name))
	if removalErr != nil {
		// Для мёртвых orphan-томов containerd может держать dead snapshot/лейбл,
		// даже когда сам контейнер удалён. Сначала убираем stale snapshot, затем
		// повторяем ручное удаление — это явно пользовательский сценарий.
		if cleanupDeadVolumeSnapshot(name) {
			output, removalErr = runRootNerdctl(context.Background(), volumeRemoveCommand(name))
		}
		if removalErr != nil {
			retryOutput, retryErr := runRootNerdctl(context.Background(), "volume rm "+shellQuote(name))
			if retryErr != nil {
				detail := strings.TrimSpace(output)
				if detail == "" {
					detail = strings.TrimSpace(retryOutput)
				}
				if detail == "" {
					detail = retryErr.Error()
				}
				return fmt.Errorf("не удалось удалить том %s: %s", name, detail)
			}
		}
	}

	var cleanupErr error
	for _, path := range volumeRemovalTargets(name, mountpoint) {
		if _, err := runWSLAsRootWithTimeout(
			"if [ -e "+shellQuote(path)+" ]; then rm -rf "+shellQuote(path)+"; fi; true",
			TimeoutMedium,
		); err != nil && cleanupErr == nil {
			cleanupErr = err
		}
	}

	InvalidateWSLCache()
	CDInvalidateVolumesCache()
	GlobalCacheManager.Invalidate(CacheEventVolumes, "after-volume-remove")
	if cleanupErr != nil {
		return cleanupErr
	}
	return nil
}

func CDCleanSystem() (string, error) {
	client, err := getCDClient()
	if err != nil {
		return "", err
	}
	ctx, cancel := cdCtx(TimeoutSlow)
	defer cancel()
	var results []string
	store := client.ImageService()
	imgs, err := store.List(ctx)
	if err == nil {
		var dangling []string
		for _, img := range imgs {
			if strings.HasPrefix(img.Name, "sha256:") {
				dangling = append(dangling, img.Name)
			}
		}
		removedImg := 0
		for _, name := range dangling {
			if err := store.Delete(ctx, name); err == nil {
				removedImg++
			}
		}
		if removedImg > 0 {
			results = append(results, fmt.Sprintf("Удалено dangling-образов: %d", removedImg))
		}
	}
	privilegePrefix := PrivilegePrefix()
	out, _ := RunWSL(fmt.Sprintf(
		"rm -rf /var/lib/nerdctl/%s/cache/* 2>/dev/null; "+
			"rm -rf /var/lib/containerd/tmp/* 2>/dev/null; "+
			"%sfind /var/log -name '*.log' -mtime +7 -delete 2>/dev/null; "+
			"if command -v logrotate >/dev/null 2>&1 && [ -f /etc/logrotate.conf ]; then %slogrotate -s /run/logrotate.status /etc/logrotate.conf 2>/dev/null || true; fi; "+
			"echo 'WSL_CLEANUP_DONE'",
		GetCdNamespace(),
		privilegePrefix,
		privilegePrefix,
	))
	if strings.Contains(out, "WSL_CLEANUP_DONE") {
		results = append(results, "Кэш, временные файлы и логи очищены")
	}
	if len(results) == 0 {
		return "Система чиста — нечего удалять", nil
	}
	return strings.Join(results, "\n"), nil
}

func CDInvalidateContainersCache() {
	containersCache.Invalidate()
	containerStatusCache.invalidateAll()
	GlobalCacheManager.Invalidate(CacheEventContainers, "manual")
}

func CDInvalidateImagesCache() {
	imagesCache.Invalidate()
	GlobalCacheManager.Invalidate(CacheEventImages, "manual")
}

func CDInvalidateVolumesCache() {
	volumesCache.Invalidate()
	GlobalCacheManager.Invalidate(CacheEventVolumes, "manual")
}

func CDInvalidateStatsCache() {
	statsCache.Invalidate()
	GlobalCacheManager.Invalidate(CacheEventStats, "manual")
}

func CDInvalidateAllCaches() {
	CDInvalidateContainersCache()
	CDInvalidateImagesCache()
	CDInvalidateVolumesCache()
	CDInvalidateStatsCache()
}

func splitImageRef(ref string) (repo, tag string) {
	if idx := strings.LastIndex(ref, ":"); idx > 0 && !strings.Contains(ref[idx:], "/") {
		return ref[:idx], ref[idx+1:]
	}
	return ref, "latest"
}

func humanSize(size int64) string {
	switch {
	case size >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(size)/(1<<30))
	case size >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(size)/(1<<20))
	case size >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(size)/(1<<10))
	default:
		return fmt.Sprintf("%d B", size)
	}
}
