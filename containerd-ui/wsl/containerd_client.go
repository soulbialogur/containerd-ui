package wsl

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
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

// Константы таймаутов
const (
	TimeoutFast   = 2 * time.Second
	TimeoutMedium = 5 * time.Second
	TimeoutSlow   = 15 * time.Second
)

// ---------------------------------------------------------------------------
// ТИПИЗИРОВАННЫЕ КЭШИ
// ---------------------------------------------------------------------------

// typedCache — типизированный кэш без interface{}
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

// boundedTypedCache — кэш с ограничением по размеру и количеству записей
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

// ---------------------------------------------------------------------------
// КЭШ СТРОК (для boundedStringCache)
// ---------------------------------------------------------------------------

type stringCacheEntry struct {
	value     string
	timestamp time.Time
}

// boundedStringCache — кэш строк с TTL и ограничением по количеству
type boundedStringCache struct {
	mu         sync.RWMutex
	data       map[string]stringCacheEntry
	defaultTTL time.Duration
	maxEntries int
}

func newBoundedStringCache(defaultTTL time.Duration, maxEntries int) *boundedStringCache {
	return &boundedStringCache{
		data:       make(map[string]stringCacheEntry, maxEntries),
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
	for k, entry := range c.data {
		if now.Sub(entry.timestamp) >= c.defaultTTL {
			delete(c.data, k)
		}
	}

	for len(c.data) >= c.maxEntries {
		var oldestKey string
		var oldestTime time.Time
		for k, entry := range c.data {
			if oldestKey == "" || entry.timestamp.Before(oldestTime) {
				oldestKey = k
				oldestTime = entry.timestamp
			}
		}
		if oldestKey != "" {
			delete(c.data, oldestKey)
		}
	}

	c.data[key] = stringCacheEntry{
		value:     value,
		timestamp: now,
	}
}

func (c *boundedStringCache) Invalidate() {
	c.mu.Lock()
	c.data = make(map[string]stringCacheEntry, c.maxEntries)
	c.mu.Unlock()
}

// ---------------------------------------------------------------------------
// КЭШ СТАТУСОВ КОНТЕЙНЕРОВ
// ---------------------------------------------------------------------------

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
	c.mu.Lock()
	if len(c.data) >= c.maxLen {
		keys := make([]string, 0, len(c.data))
		for k := range c.data {
			keys = append(keys, k)
		}
		for i := 0; i < len(keys)/2; i++ {
			delete(c.data, keys[i])
		}
	}
	c.data[id] = statusCacheEntry{status: status, timestamp: time.Now()}
	c.mu.Unlock()
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

const (
	statusCacheTTL = 15 * time.Second
)

// ---------------------------------------------------------------------------
// ГЛОБАЛЬНЫЕ КЭШИ
// ---------------------------------------------------------------------------

var (
	containersCache      = newTypedCache[[]Container](3 * time.Second)
	imagesCache          = newTypedCache[[]Image](5 * time.Second)
	volumesCache         = newTypedCache[[]Volume](10 * time.Second)
	statsCache           = newTypedCache[[]ContainerStat](5 * time.Second)
	containerStatusCache = newContainerStatusCache(statusCacheTTL)

	splitImageCache = newBoundedTypedCache[[2]string](30 * time.Second, 500)
	humanSizeCache  = newBoundedTypedCache[string](1 * time.Minute, 500)
)

// ---------------------------------------------------------------------------
// ПОДКЛЮЧЕНИЕ К CONTAINERD
// ---------------------------------------------------------------------------

var (
	cdClient    *cdclient.Client
	cdErr       error
	cdAvailable atomic.Bool
	cdIP        string
	cdBaseCtx   context.Context
	cdConn      *grpc.ClientConn
	appCtx      context.Context
	appCancel   context.CancelFunc

	cdMu      sync.Mutex
	cdIPValid atomic.Bool
)

func init() {
	cdBaseCtx = namespaces.WithNamespace(context.Background(), GetCdNamespace())
	appCtx, appCancel = context.WithCancel(context.Background())
}

// DetectWSLIP возвращает IP-адрес WSL2
func DetectWSLIP() string {
	return detectWSLIP(true)
}

func detectWSLIP(forceRefresh bool) string {
	if !forceRefresh && cdIP != "" && cdIPValid.Load() {
		return cdIP
	}
	cmd := exec.Command("wsl", "hostname", "-I")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		cdIPValid.Store(false)
		return ""
	}

	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) > 0 {
		newIP := fields[0]
		if newIP != cdIP {
			cdIP = newIP
			cdIPValid.Store(true)
			resetCDClient()
		}
		return cdIP
	}

	cdIPValid.Store(false)
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
}

func getCDClient() (*cdclient.Client, error) {
	if cdClient != nil && cdAvailable.Load() {
		return cdClient, nil
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
		conn, err := grpc.NewClient(addr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
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

		client, err := cdclient.NewWithConn(conn,
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
		cdMu.Unlock()

		return cdClient, nil
	}

	if cdErr == nil {
		cdErr = fmt.Errorf("не удалось подключиться к containerd после %d попыток", maxRetries)
	}
	return nil, cdErr
}

// Shutdown закрывает соединение
func Shutdown() {
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
}

// AppContext возвращает глобальный контекст
func AppContext() context.Context {
	return appCtx
}

func cdCtx(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(cdBaseCtx, timeout)
}

// CDCheck проверяет доступность containerd
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

// ---------------------------------------------------------------------------
// ПОЛУЧЕНИЕ КОНФИГУРАЦИИ КОНТЕЙНЕРА (gRPC)
// ---------------------------------------------------------------------------

// ContainerConfig хранит конфигурацию контейнера для пересоздания
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

// CDGetContainerConfig получает конфигурацию через gRPC
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

// ---------------------------------------------------------------------------
// СТАТИСТИКА КОНТЕЙНЕРОВ (JSON формат — надёжно)
// ---------------------------------------------------------------------------

// ContainerStatJSON структура для JSON-парсинга nerdctl stats
type ContainerStatJSON struct {
	ID        string `json:"ID"`
	Name      string `json:"Name"`
	CPUPerc   string `json:"CPUPerc"`
	MemUsage  string `json:"MemUsage"`
	NetIO     string `json:"NetIO"`
	PIDs      string `json:"PIDs"`
}

// CDGetStats получает статистику через nerdctl stats с JSON форматом
func CDGetStats() ([]ContainerStat, error) {
	if cached, ok := statsCache.Get(); ok {
		GlobalCacheManager.RecordHit("stats")
		return cached, nil
	}
	GlobalCacheManager.RecordMiss("stats")

	// Используем --format '{{json .}}' вместо текстового парсинга
	out, err := RunWSL("nerdctl stats --no-stream --format '{{json .}}' 2>/dev/null")
	if err != nil || out == "" {
		GlobalCacheManager.RecordError("stats")
		return []ContainerStat{}, nil
	}

	lines := strings.Split(out, "\n")
	var result []ContainerStat

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Парсим JSON — надёжно и не зависит от формата вывода
		var stat ContainerStatJSON
		if err := json.Unmarshal([]byte(line), &stat); err != nil {
			continue // пропускаем ошибочные строки
		}

		// Обрабатываем ID
		id := stat.ID
		if len(id) > 12 {
			id = id[:12]
		}

		// Обрабатываем PIDs
		pids := stat.PIDs
		if pids == "0" {
			pids = "—"
		}

		result = append(result, ContainerStat{
			ID:     id,
			Name:   stat.Name,
			CPU:    stat.CPUPerc,
			Memory: stat.MemUsage,
			NetIO:  stat.NetIO,
			PIDs:   pids,
		})
	}

	statsCache.Set(result)
	return result, nil
}

// ---------------------------------------------------------------------------
// ЛОГИ КОНТЕЙНЕРА (чтение из файловой системы)
// ---------------------------------------------------------------------------

// CDGetContainerLogs читает логи через find + tail (WSL)
func CDGetContainerLogs(id string, tail int) (string, error) {
	ns := GetCdNamespace()
	pattern := fmt.Sprintf("/var/lib/nerdctl/%s/containers/%s*", ns, id)
	out, err := RunWSL(fmt.Sprintf(
		"logfile=$(find %s -name '*.log' 2>/dev/null | head -1); if [ -n \"$logfile\" ]; then tail -n %d \"$logfile\" 2>&1; else echo 'NO_LOGS_FOUND'; fi",
		pattern, tail,
	))
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(out) == "NO_LOGS_FOUND" {
		return "", fmt.Errorf("логи для контейнера %s не найдены", id)
	}
	return out, nil
}

// CDClearContainerLogs очищает лог-файл
func CDClearContainerLogs(id string) error {
	ns := GetCdNamespace()
	pattern := fmt.Sprintf("/var/lib/nerdctl/%s/containers/%s*", ns, id)
	_, err := RunWSL(fmt.Sprintf(
		"logfile=$(find %s -name '*.log' 2>/dev/null | head -1); [ -n \"$logfile\" ] && truncate -s 0 \"$logfile\" 2>/dev/null",
		pattern,
	))
	return err
}

// ---------------------------------------------------------------------------
// ИНФОРМАЦИЯ О ТОМЕ (gRPC для поиска + WSL для размера)
// ---------------------------------------------------------------------------

// CDGetDBInfo получает размер и список файлов тома
func CDGetDBInfo(volumeName string) (string, []string, error) {
	client, err := getCDClient()
	if err != nil {
		return cdGetDBInfoFallback(volumeName)
	}
	ctx, cancel := cdCtx(TimeoutMedium)
	defer cancel()

	store := client.ContainerService()
	containers, err := store.List(ctx)
	if err != nil {
		return cdGetDBInfoFallback(volumeName)
	}

	var mountpoint string
	for _, c := range containers {
		if _, ok := c.Labels["nerdctl/volume."+volumeName]; ok {
			ns := GetCdNamespace()
			mountpoint = "/var/lib/nerdctl/" + ns + "/volumes/" + volumeName + "/_data"
			break
		}
	}
	if mountpoint == "" {
		ns := GetCdNamespace()
		mountpoint = "/var/lib/nerdctl/" + ns + "/volumes/" + volumeName + "/_data"
	}

	out, err := RunWSL(fmt.Sprintf(
		"du -sh %s 2>/dev/null; echo '===FILES==='; find %s -type f 2>/dev/null | head -50",
		mountpoint, mountpoint,
	))
	if err != nil {
		return "", nil, err
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
			if line != "" {
				files = append(files, strings.TrimPrefix(line, mountpoint))
			}
		}
	}

	return size, files, nil
}

func cdGetDBInfoFallback(volumeName string) (string, []string, error) {
	ns := GetCdNamespace()
	mp := "/var/lib/nerdctl/" + ns + "/volumes/" + volumeName + "/_data"
	out, err := RunWSL(fmt.Sprintf(
		"du -sh %s 2>/dev/null; echo '===FILES==='; find %s -type f 2>/dev/null | head -50",
		mp, mp,
	))
	if err != nil {
		return "", nil, err
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
			if line != "" {
				files = append(files, strings.TrimPrefix(line, mp))
			}
		}
	}
	return size, files, nil
}

// ---------------------------------------------------------------------------
// СПИСКИ КОНТЕЙНЕРОВ, ОБРАЗОВ, ТОМОВ
// ---------------------------------------------------------------------------

// CDListContainers с кэшем и batch-режимом
func CDListContainers(all bool) ([]Container, error) {
	if cached, ok := containersCache.Get(); ok {
		if all {
			return cached, nil
		}
		var running []Container
		for _, c := range cached {
			if isContainerRunning(c.Status) {
				running = append(running, c)
			}
		}
		return running, nil
	}

	client, err := getCDClient()
	if err == nil {
		ctx, cancel := cdCtx(TimeoutMedium)
		defer cancel()

		store := client.ContainerService()
		list, err := store.List(ctx)
		if err == nil {
			result := make([]Container, 0, len(list))
			for _, c := range list {
				status, ok := containerStatusCache.get(c.ID)
				if !ok {
					status = determineContainerStatus(client, ctx, c.ID)
					containerStatusCache.set(c.ID, status)
				}

				if !all && !isContainerRunning(status) {
					continue
				}

				result = append(result, normalizeContainer(
					c.ID,
					c.Labels["nerdctl/name"],
					c.Image,
					status,
					c.Labels["nerdctl/ports"],
				))
			}

			containersCache.Set(result)
			return result, nil
		}
	}

	// Fallback: используем WSL (nerdctl ps)
	return listContainersFallback(all)
}

// listContainersFallback — fallback через WSL, если gRPC недоступен
func listContainersFallback(all bool) ([]Container, error) {
	flag := ""
	if all {
		flag = "-a "
	}
	out, err := RunWSL("nerdctl ps " + flag + "--format '{{json .}}' 2>/dev/null")
	if err != nil || out == "" {
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
		if !all && !isContainerRunning(c.Status) {
			continue
		}
		result = append(result, normalizeContainer(c.ID, c.Name, c.Image, c.Status, c.Ports))
	}

	containersCache.Set(result)
	return result, nil
}

func normalizeContainer(id, name, image, status, ports string) Container {
	if name == "" {
		name = id
	}
	if len(id) > 12 {
		id = id[:12]
	}
	return Container{ID: id, Name: name, Image: image, Status: status, Ports: ports}
}

func isContainerRunning(status string) bool {
	status = strings.ToLower(status)
	return status == "running" || strings.Contains(status, "up")
}

// determineContainerStatus через gRPC
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

// getContainerStatusesBatch – один вызов nerdctl ps
func getContainerStatusesBatch() (map[string]string, error) {
	out, err := RunWSL("nerdctl ps -a --format '{{.ID}}\t{{.Status}}' 2>/dev/null")
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

// CDListImages – список образов через gRPC с кэшированием размеров
func CDListImages() ([]Image, error) {
	if cached, ok := imagesCache.Get(); ok {
		return cached, nil
	}

	client, err := getCDClient()
	if err == nil {
		ctx, cancel := cdCtx(TimeoutMedium)
		defer cancel()

		store := client.ImageService()
		imgs, err := store.List(ctx)
		if err == nil {
			result := make([]Image, 0, len(imgs))

			sizeMap := func() map[string]int64 {
				defer func() { recover() }()
				return getImageSizes(ctx, imgs)
			}()
			if sizeMap == nil {
				sizeMap = make(map[string]int64)
			}

			for _, img := range imgs {
				repo, tag := cachedSplitImageRef(img.Name)
				digest := img.Target.Digest.String()
				id := digest
				if len(id) > 12 {
					id = id[len(id)-12:]
				}
				size := sizeMap[digest]
				sizeStr := cachedHumanSize(size)
				if size == 0 {
					sizeStr = "—"
				}
				createdStr := ""
				if !img.CreatedAt.IsZero() {
					createdStr = img.CreatedAt.Format("2006-01-02 15:04")
				}
				result = append(result, Image{
					ID:         id,
					Repository: repo,
					Tag:        tag,
					Size:       sizeStr,
					CreatedAt:  createdStr,
					sizeBytes:  size,
				})
			}

			imagesCache.Set(result)
			return result, nil
		}
	}

	// Fallback: используем WSL (nerdctl images)
	return listImagesFallback()
}

// listImagesFallback — fallback через WSL, если gRPC недоступен
func listImagesFallback() ([]Image, error) {
	out, err := RunWSL("nerdctl images --format '{{json .}}' 2>/dev/null")
	if err != nil || out == "" {
		return []Image{}, nil
	}

	var result []Image
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var img struct {
			Repository string `json:"Repository"`
			Tag        string `json:"Tag"`
			ID         string `json:"ID"`
			Size       string `json:"Size"`
		}
		if err := json.Unmarshal([]byte(line), &img); err != nil {
			continue
		}
		result = append(result, Image{
			ID:         img.ID,
			Repository: img.Repository,
			Tag:        img.Tag,
			Size:       img.Size,
		})
	}

	imagesCache.Set(result)
	return result, nil
}

// getImageSizes – batch-загрузка размеров
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

// Кэш размеров образов
const maxImageSizeCacheBytes = 10 * 1024 * 1024

var imageSizeCache = struct {
	sync.RWMutex
	m         map[string]int64
	count     int
	maxLen    int
	totalSize int64
	maxSize   int64
}{
	m:      make(map[string]int64),
	maxLen: 200,
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

// cachedSplitImageRef и cachedHumanSize
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

// CDListVolumes – список томов (WSL)
func CDListVolumes() ([]Volume, error) {
	if cached, ok := volumesCache.Get(); ok {
		return cached, nil
	}

	ns := GetCdNamespace()
	base := "/var/lib/nerdctl/" + ns + "/volumes"
	out, err := RunWSL("ls -1 " + base + " 2>/dev/null")
	if err != nil {
		return nil, nil
	}

	var result []Volume
	for _, name := range strings.Split(out, "\n") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		result = append(result, Volume{
			Name:       name,
			Driver:     "local",
			Mountpoint: base + "/" + name + "/_data",
		})
	}
	volumesCache.Set(result)
	return result, nil
}

// CDGetUsedVolumes возвращает множество имён томов, используемых хотя бы одним контейнером.
// Использует gRPC для получения спецификаций контейнеров.
// Если gRPC недоступен — использует fallback через WSL (nerdctl).
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
					continue // пропускаем проблемные контейнеры
				}
				spec, err := container.Spec(cdCtx2)
				if err != nil {
					continue
				}
				// Проходим по всем монтированиям
				for _, mount := range spec.Mounts {
					// Именованные тома имеют тип "volume", а Source содержит имя тома
					if mount.Type == "volume" && mount.Source != "" {
						used[mount.Source] = true
					}
				}
			}
			return used, nil
		}
	}

	// Fallback: используем WSL (nerdctl ps —v)
	return getUsedVolumesFallback()
}

// getUsedVolumesFallback — fallback через WSL, если gRPC недоступен
func getUsedVolumesFallback() (map[string]bool, error) {
	used := make(map[string]bool)

	// nerdctl ps --format '{{.ID}}\t{{.Mounts}}'
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
		// Парсим список монтирований — разделитель запятая
		for _, m := range strings.Split(mounts, ",") {
			m = strings.TrimSpace(m)
			// Формат: volume_name -> /path/to/_data
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

// ---------------------------------------------------------------------------
// ДЕЙСТВИЯ С КОНТЕЙНЕРАМИ И ОБРАЗАМИ (gRPC)
// ---------------------------------------------------------------------------

func CDStopContainer(id string) error {
	// ---- Шаг 1: gRPC ----
	client, err := getCDClient()
	if err == nil {
		ctx, cancel := cdCtx(TimeoutSlow)
		defer cancel()

		container, err := client.LoadContainer(ctx, id)
		if err == nil {
			task, err := container.Task(ctx, nil)
			if err == nil {
				// Отправляем SIGTERM
				_ = task.Kill(ctx, syscall.SIGTERM)
				waitCh, err := task.Wait(ctx)
				if err == nil {
					select {
					case <-waitCh:
						// завершился штатно
					case <-time.After(TimeoutMedium):
						// не успел – убиваем принудительно
						_ = task.Kill(ctx, syscall.SIGKILL)
						<-waitCh
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				_, err = task.Delete(ctx)
				if err == nil {
					return nil // успешно через gRPC
				}
			}
		}
		// если дошли сюда – gRPC не помог, идём в fallback
	}

	// ---- Шаг 2: Fallback через WSL (nerdctl) ----
	_, err = RunWSL("nerdctl stop " + id + " 2>/dev/null")
	return err
}

func CDStartContainer(id string) error {
	// ---- Шаг 1: gRPC ----
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
					return nil // уже запущен
				}
				// удаляем старую задачу, если есть
				_, _ = task.Delete(ctx)
			}

			// создаём новую задачу
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
					return nil // успешно запущен через gRPC
				}
			}
		}
		// gRPC не удался – fallback
	}

	// ---- Шаг 2: Fallback через WSL (nerdctl) ----
	_, err = RunWSL("nerdctl start " + id + " 2>/dev/null")
	return err
}

func CDRestartContainer(id string) error {
	if err := CDStopContainer(id); err != nil {
		return err
	}
	return CDStartContainer(id)
}

func CDRemoveContainer(id string) error {
	client, err := getCDClient()
	if err != nil {
		return err
	}
	ctx, cancel := cdCtx(TimeoutSlow)
	defer cancel()

	container, err := client.LoadContainer(ctx, id)
	if err != nil {
		return err
	}
	if task, err := container.Task(ctx, nil); err == nil {
		_ = task.Kill(ctx, syscall.SIGKILL)
		_, _ = task.Delete(ctx)
	}
	return container.Delete(ctx)
}

func CDRemoveImage(ref string) error {
	client, err := getCDClient()
	if err == nil {
		ctx, cancel := cdCtx(TimeoutSlow)
		defer cancel()

		store := client.ImageService()
		img, err := store.Get(ctx, ref)
		if err == nil {
			return store.Delete(ctx, img.Name)
		}
		imgs, err := store.List(ctx)
		if err == nil {
			for _, img := range imgs {
				digest := img.Target.Digest.String()
				if strings.HasSuffix(digest, ref) || img.Name == ref {
					return store.Delete(ctx, img.Name)
				}
			}
		}
	}

	// Fallback: используем WSL (nerdctl rmi)
	_, err = RunWSL("nerdctl rmi -f " + ref + " 2>/dev/null")
	return err
}

func CDRemoveVolume(name string) error {
	ns := GetCdNamespace()
	base := "/var/lib/nerdctl/" + ns + "/volumes/" + name
	_, err := RunWSL("rm -rf " + base)
	return err
}

// ---------------------------------------------------------------------------
// ОЧИСТКА СИСТЕМЫ (gRPC + WSL)
// ---------------------------------------------------------------------------

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

	out, _ := RunWSL(fmt.Sprintf(
		"rm -rf /var/lib/nerdctl/%s/cache/* 2>/dev/null; "+
			"rm -rf /var/lib/containerd/tmp/* 2>/dev/null; "+
			"sudo find /var/log -name '*.log' -mtime +7 -delete 2>/dev/null; "+
			"sudo journalctl --vacuum-time=7d 2>/dev/null; "+
			"echo 'WSL_CLEANUP_DONE'",
		GetCdNamespace(),
	))
	if strings.Contains(out, "WSL_CLEANUP_DONE") {
		results = append(results, "Кэш, временные файлы и логи очищены")
	}

	if len(results) == 0 {
		return "Система чиста — нечего удалять", nil
	}
	return strings.Join(results, "\n"), nil
}

// ---------------------------------------------------------------------------
// ИНВАЛИДАЦИЯ КЭШЕЙ
// ---------------------------------------------------------------------------

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

// CDInvalidateAllCaches инвалидирует все кэши
func CDInvalidateAllCaches() {
	CDInvalidateContainersCache()
	CDInvalidateImagesCache()
	CDInvalidateVolumesCache()
	CDInvalidateStatsCache()
}

// ---------------------------------------------------------------------------
// ВСПОМОГАТЕЛЬНЫЕ ФУНКЦИИ
// ---------------------------------------------------------------------------

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