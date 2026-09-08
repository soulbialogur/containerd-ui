package wsl

import (
	"bytes"
	"containerd-ui/i18n"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Container struct {
	ID     string `json:"ID"`
	Name   string `json:"Names"`
	Image  string `json:"Image"`
	Status string `json:"Status"`
	Ports  string `json:"Ports"`
}

type Network struct {
	Name       string   `json:"Name"`
	Driver     string   `json:"Driver"`
	Scope      string   `json:"Scope"`
	Containers []string `json:"Containers"`
}

func ShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

type Image struct {
	ID         string `json:"ID"`
	Repository string `json:"Repository"`
	Tag        string `json:"Tag"`
	Size       string `json:"Size"`
	CreatedAt  string `json:"CreatedAt"`
	sizeBytes  int64
}

type Volume struct {
	Name       string `json:"Name"`
	Driver     string `json:"Driver"`
	Mountpoint string `json:"Mountpoint"`
}

type ContainerStat struct {
	ID     string `json:"ID"`
	Name   string `json:"Name"`
	CPU    string `json:"CPUPerc"`
	Memory string `json:"MemUsage"`
	NetIO  string `json:"NetIO"`
	PIDs   string `json:"PIDs"`
}

type BuildPhase struct {
	Icon     string
	Title    string
	Progress float32
}

type buildPhaseDefinition struct {
	keywords        []string
	icon            string
	title           string
	progressRange   [2]float32
	weight          int
	typicalDuration time.Duration
}

var buildPhaseMap = []buildPhaseDefinition{
	{
		[]string{"Preparing", "preparing"},
		"🔍", "Подготовка...",
		[2]float32{0.00, 0.05}, 1, 5 * time.Second,
	},
	{
		[]string{"Resolving", "resolving", "resolving dependencies"},
		"📦", "Разрешение зависимостей...",
		[2]float32{0.03, 0.08}, 2, 10 * time.Second,
	},
	{
		[]string{"Using cache", "Cached", "cache hit"},
		"⚡", "Используем кэш...",
		[2]float32{0.05, 0.12}, 3, 3 * time.Second,
	},
	{
		[]string{"Pulling", "pulling", "downloading", "download"},
		"🌐", "Загрузка образов...",
		[2]float32{0.10, 0.25}, 4, 30 * time.Second,
	},
	{
		[]string{"Verifying", "verifying", "verif"},
		"✅", "Проверка целостности...",
		[2]float32{0.20, 0.30}, 3, 10 * time.Second,
	},
	{
		[]string{"Expanding", "expanding", "unpacking"},
		"📂", "Распаковка слоя...",
		[2]float32{0.25, 0.35}, 3, 15 * time.Second,
	},
	{
		[]string{"Building", "building", "compile", "compiling", "gcc", "g++", "rustc", "npm run", "pip install"},
		"🔨", "Компиляция...",
		[2]float32{0.30, 0.65}, 5, 60 * time.Second,
	},
	{
		[]string{"Linking", "linking"},
		"🔗", "Линковка...",
		[2]float32{0.60, 0.70}, 4, 15 * time.Second,
	},
	{
		[]string{"Finalizing", "finalizing", "optimizing", "compressing"},
		"✨", "Оптимизация образа...",
		[2]float32{0.70, 0.85}, 4, 20 * time.Second,
	},
	{
		[]string{"Saving", "saving", "pushing", "uploading"},
		"💾", "Сохранение образа...",
		[2]float32{0.80, 0.95}, 4, 15 * time.Second,
	},
	{
		[]string{"Successfully", "success", "complete", "done", "Build complete"},
		"🎉", "Успешно!",
		[2]float32{1.0, 1.0}, 10, 0,
	},
	{
		[]string{"Error", "error", "failed", "fail", "panic"},
		"❌", "Ошибка сборки!",
		[2]float32{0.0, 0.0}, 10, 0,
	},
}

func detectProgressFromBar(line string) float32 {
	idx := strings.LastIndex(line, "%")
	if idx > 0 {
		start := idx - 1
		for start > 0 && ((line[start] >= '0' && line[start] <= '9') || line[start] == '.') {
			start--
		}
		start++
		if start <= idx {
			numStr := line[start:idx]
			var pct float32
			fmt.Sscanf(numStr, "%f", &pct)
			if pct >= 0 && pct <= 100 {
				return pct / 100.0
			}
		}
	}
	if slash := strings.Index(line, " / "); slash > 0 {
		var done, total float32
		n, _ := fmt.Sscanf(line[:slash], "%f", &done)
		if n == 1 {
			rest := line[slash+3:]
			end := strings.IndexAny(rest, " \t\n")
			if end < 0 {
				end = len(rest)
			}
			fmt.Sscanf(rest[:end], "%f", &total)
			if total > 0 {
				p := done / total
				if p >= 0 && p <= 1 {
					return p
				}
			}
		}
	}
	return -1
}

type buildProgressTracker struct {
	mu             sync.Mutex
	startTime      time.Time
	lastUpdateTime time.Time
	currentPhase   string
	phaseStartTime time.Time
	lastProgress   float32
}

var globalBuildTracker = &buildProgressTracker{}

func ResetBuildProgress() {
	globalBuildTracker.mu.Lock()
	defer globalBuildTracker.mu.Unlock()
	globalBuildTracker.startTime = time.Now()
	globalBuildTracker.lastUpdateTime = time.Now()
	globalBuildTracker.currentPhase = ""
	globalBuildTracker.phaseStartTime = time.Now()
	globalBuildTracker.lastProgress = 0
}

func (t *buildProgressTracker) getPhaseProgress(phase buildPhaseDefinition, elapsed time.Duration) float32 {
	if phase.typicalDuration == 0 {
		return phase.progressRange[1]
	}
	phaseProgress := float32(elapsed.Seconds() / phase.typicalDuration.Seconds())
	if phaseProgress > 1.0 {
		phaseProgress = 1.0
	}
	rangeSize := phase.progressRange[1] - phase.progressRange[0]
	return phase.progressRange[0] + phaseProgress*rangeSize
}

func DetermineBuildPhaseWithTime(output string) BuildPhase {
	if pct := detectMostRecentProgress(output); pct > 0 {
		return BuildPhase{"⏳", "Сборка...", pct}
	}
	phaseDef, _ := determinePhaseByKeywords(output)
	if phaseDef != nil {
		globalBuildTracker.mu.Lock()
		defer globalBuildTracker.mu.Unlock()
		now := time.Now()
		phaseName := phaseDef.title
		if globalBuildTracker.currentPhase != phaseName {
			globalBuildTracker.currentPhase = phaseName
			globalBuildTracker.phaseStartTime = now
		}
		elapsed := now.Sub(globalBuildTracker.phaseStartTime)
		progress := globalBuildTracker.getPhaseProgress(*phaseDef, elapsed)
		if globalBuildTracker.lastProgress > 0 {
			minAllowed := globalBuildTracker.lastProgress - 0.10
			if progress < minAllowed {
				progress = minAllowed
			}
		}
		globalBuildTracker.lastProgress = progress
		return BuildPhase{
			Icon:     phaseDef.icon,
			Title:    phaseDef.title,
			Progress: progress,
		}
	}
	return estimateProgressByOutputLength(output)
}

func detectMostRecentProgress(output string) float32 {
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		pct := detectProgressFromBar(line)
		if pct > 0 {
			return pct
		}
	}
	return -1
}

func determinePhaseByKeywords(output string) (*buildPhaseDefinition, float32) {
	lower := strings.ToLower(output)
	var bestPhase *buildPhaseDefinition
	bestScore := 0
	for i := range buildPhaseMap {
		def := &buildPhaseMap[i]
		if def.title == "Ошибка сборки!" || def.title == "Успешно!" {
			continue
		}
		score := 0
		for _, kw := range def.keywords {
			if strings.Contains(lower, strings.ToLower(kw)) {
				score += def.weight
			}
		}
		if score > bestScore {
			bestScore = score
			bestPhase = def
		}
	}
	if bestPhase == nil {
		return nil, 0
	}
	return bestPhase, float32(bestScore)
}

func estimateProgressByOutputLength(output string) BuildPhase {
	outputLen := len(output)
	switch {
	case outputLen > 10000:
		return BuildPhase{"🔨", "Сборка...", 0.85}
	case outputLen > 5000:
		return BuildPhase{"🔨", "Компиляция...", 0.65}
	case outputLen > 2000:
		return BuildPhase{"🔨", "Компиляция...", 0.40}
	case outputLen > 500:
		return BuildPhase{"📦", "Подготовка...", 0.20}
	default:
		return BuildPhase{"⏳", "Подготовка...", 0.05}
	}
}

func DetectBuildPhase(output string) BuildPhase {
	lower := strings.ToLower(output)
	for _, kw := range []string{"Error", "error", "failed", "fail", "panic"} {
		if strings.Contains(lower, kw) {
			ResetBuildProgress()
			return BuildPhase{"❌", "Ошибка сборки!", 0.0}
		}
	}
	for _, kw := range []string{"Successfully", "success", "complete", "done", "Build complete"} {
		if strings.Contains(lower, kw) {
			ResetBuildProgress()
			return BuildPhase{"🎉", "Успешно!", 1.0}
		}
	}
	return DetermineBuildPhaseWithTime(output)
}

func FormatBuildStatus(phase BuildPhase) string {
	return fmt.Sprintf("%s %s", phase.Icon, phase.Title)
}

var buildkitdState = struct {
	sync.RWMutex
	running bool
	pid     int
}{}

func CheckBuildkitd() bool {
	buildkitdState.RLock()
	if buildkitdState.running && buildkitdState.pid > 0 {
		buildkitdState.RUnlock()
		return true
	}
	buildkitdState.RUnlock()
	_, err := RunWSL("sudo buildctl --addr unix:///run/buildkit/buildkitd.sock debug workers 2>/dev/null")
	return err == nil
}

func StartBuildkitd() error {
	if CheckBuildkitd() {
		return nil
	}
	_, err := RunWSL("sudo mkdir -p /run/buildkit && sudo chmod 777 /run/buildkit && sudo nohup /usr/local/bin/buildkitd --addr unix:///run/buildkit/buildkitd.sock > /tmp/buildkitd.log 2>&1 & chmod 666 /run/buildkit/buildkitd.sock")
	if err != nil {
		return fmt.Errorf("не удалось запустить buildkitd: %w", err)
	}
	time.Sleep(2 * time.Second)
	if CheckBuildkitd() {
		buildkitdState.Lock()
		buildkitdState.running = true
		buildkitdState.Unlock()
		return nil
	}
	return fmt.Errorf("buildkitd запустился, но не отвечает на запросы")
}

func StopBuildkitd() {
	_, err := RunWSL("sudo pkill -f buildkitd 2>/dev/null; echo 'ok'")
	if err == nil {
		buildkitdState.Lock()
		buildkitdState.running = false
		buildkitdState.pid = 0
		buildkitdState.Unlock()
	}
}

const maxWSLCacheSize = 10 * 1024 * 1024

var wslCache = struct {
	sync.RWMutex
	m         map[string]wslCacheEntry
	totalSize int64
	maxSize   int64
	cleanupAt int
}{m: make(map[string]wslCacheEntry), maxSize: maxWSLCacheSize, cleanupAt: 25}

type wslCacheEntry struct {
	output    string
	err       error
	timestamp time.Time
	size      int64
}

func RunWSL(command string) (string, error) {
	wslCache.RLock()
	ttl := time.Duration(wslCacheTTL.Load()) * time.Second
	if entry, ok := wslCache.m[command]; ok && time.Since(entry.timestamp) < ttl {
		wslCache.RUnlock()
		return entry.output, entry.err
	}
	wslCache.RUnlock()

	cmd := exec.Command("wsl", "-d", GetWslDistro(), "bash", "-c", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := strings.TrimSpace(out.String())
	resultSize := int64(len(result))

	wslCache.Lock()
	for k, v := range wslCache.m {
		if time.Since(v.timestamp) > ttl {
			wslCache.totalSize -= v.size
			delete(wslCache.m, k)
		}
	}
	for (wslCache.totalSize+resultSize > wslCache.maxSize ||
		(wslCache.cleanupAt > 0 && len(wslCache.m) >= wslCache.cleanupAt)) && len(wslCache.m) > 0 {
		var oldestKey string
		var oldestTime time.Time
		for k, v := range wslCache.m {
			if oldestKey == "" || v.timestamp.Before(oldestTime) {
				oldestKey = k
				oldestTime = v.timestamp
			}
		}
		if oldestKey != "" {
			wslCache.totalSize -= wslCache.m[oldestKey].size
			delete(wslCache.m, oldestKey)
		}
	}
	wslCache.m[command] = wslCacheEntry{
		output:    result,
		err:       err,
		timestamp: time.Now(),
		size:      resultSize,
	}
	wslCache.totalSize += resultSize
	wslCache.Unlock()

	return result, err
}

func RunWSLWithCancel(ctx context.Context, command string) (string, error) {
	if isBuildCommand(command) {
		return executeWSLCommand(ctx, command, true)
	}
	wslCache.RLock()
	ttl := time.Duration(wslCacheTTL.Load()) * time.Second
	if entry, ok := wslCache.m[command]; ok && time.Since(entry.timestamp) < ttl {
		wslCache.RUnlock()
		return entry.output, entry.err
	}
	wslCache.RUnlock()
	return executeWSLCommand(ctx, command, false)
}

func executeWSLCommand(ctx context.Context, command string, skipCache bool) (string, error) {
	cmd := exec.CommandContext(ctx, "wsl", "-d", GetWslDistro(), "bash", "-c", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := strings.TrimSpace(out.String())
	errOutput := strings.TrimSpace(stderr.String())

	if ctx.Err() == nil && !skipCache {
		resultSize := len(result)
		if resultSize < 1024*1024 {
			wslCache.Lock()
			wslCache.m[command] = wslCacheEntry{
				output:    result,
				err:       err,
				timestamp: time.Now(),
			}
			if len(wslCache.m) > 100 {
				var oldest string
				var oldestTime time.Time
				for k, v := range wslCache.m {
					if oldestTime.IsZero() || v.timestamp.Before(oldestTime) {
						oldest = k
						oldestTime = v.timestamp
					}
				}
				delete(wslCache.m, oldest)
			}
			wslCache.Unlock()
		}
	}

	if err != nil && errOutput != "" {
		fullOutput := result
		if fullOutput != "" && errOutput != "" {
			fullOutput += "\n" + errOutput
		} else if errOutput != "" {
			fullOutput = errOutput
		}
		return fullOutput, err
	}
	return result, err
}

func isBuildCommand(command string) bool {
	lower := strings.ToLower(command)
	return strings.Contains(lower, "compose build") ||
		strings.Contains(lower, "compose up") ||
		strings.Contains(lower, "nerdctl build") ||
		strings.Contains(lower, "nerdctl push") ||
		strings.Contains(lower, "start-containerd.sh build") ||
		strings.Contains(lower, "start-containerd.sh rebuild") ||
		strings.Contains(lower, "docker_buildkit=0")
}

func InvalidateWSLCache() {
	wslCache.Lock()
	for k := range wslCache.m {
		delete(wslCache.m, k)
	}
	wslCache.Unlock()
}

func CheckService() map[string]interface{} {
	status := map[string]interface{}{"wsl": false, "nerdctl": false, "containerd": false, "error": ""}
	_, err := RunWSL("echo ok")
	if err != nil {
		status["error"] = fmt.Sprintf("WSL '%s' не найден", GetWslDistro())
		return status
	}
	status["wsl"] = true
	if cdAvailable.Load() || CDCheck() == nil {
		status["containerd"] = true
		status["nerdctl"] = true
		return status
	}
	out, _ := RunWSL("systemctl is-active containerd 2>/dev/null; echo '---'; which nerdctl 2>/dev/null")
	parts := strings.Split(out, "---")
	if len(parts) > 0 && strings.TrimSpace(parts[0]) == "active" {
		status["containerd"] = true
	}
	if len(parts) > 1 && strings.TrimSpace(parts[1]) != "" {
		status["nerdctl"] = true
	}
	return status
}

func ListContainers(all bool) ([]Container, error) {
	return CDListContainers(all)
}

func ListNetworks(ctx context.Context) ([]Network, error) {
	out, err := RunWSLWithCancel(ctx, "nerdctl network ls --format '{{json .}}' 2>/dev/null")
	if err != nil {
		return nil, err
	}
	var networks []Network
	for _, line := range strings.Split(out, "\n") {
		var network Network
		if json.Unmarshal([]byte(strings.TrimSpace(line)), &network) == nil && network.Name != "" {
			networks = append(networks, network)
		}
	}
	return networks, nil
}

func GetNetworkContainers(ctx context.Context, name string) ([]string, error) {
	command := fmt.Sprintf("nerdctl network inspect %s --format '{{json .Containers}}' 2>/dev/null", shellQuote(name))
	out, err := RunWSLWithCancel(ctx, command)
	if err != nil {
		return nil, err
	}
	var entries map[string]struct {
		Name string `json:"Name"`
	}
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		return nil, err
	}
	containers := make([]string, 0, len(entries))
	for id, entry := range entries {
		if entry.Name != "" {
			containers = append(containers, entry.Name)
		} else {
			containers = append(containers, id)
		}
	}
	sort.Strings(containers)
	return containers, nil
}

func CreateNetwork(ctx context.Context, name, driver string) error {
	_, err := RunWSLWithCancel(ctx, fmt.Sprintf("nerdctl network create --driver %s %s", shellQuote(driver), shellQuote(name)))
	return err
}

func RemoveNetwork(ctx context.Context, name string) error {
	_, err := RunWSLWithCancel(ctx, fmt.Sprintf("nerdctl network rm %s", shellQuote(name)))
	return err
}

func StartContainer(id string) error {
	err := CDStartContainer(id)
	if err == nil {
		CDInvalidateContainersCache()
	}
	return err
}

func StopContainer(id string) error {
	err := CDStopContainer(id)
	if err == nil {
		CDInvalidateContainersCache()
	}
	return err
}

func ForceStopContainer(id string) error {
	_, err := RunWSL("nerdctl stop -t 2 " + shellQuote(id) + " 2>/dev/null; nerdctl kill " + shellQuote(id))
	if err == nil {
		CDInvalidateContainersCache()
	}
	return err
}

func RestartContainer(id string) error {
	err := CDRestartContainer(id)
	if err == nil {
		CDInvalidateContainersCache()
	}
	return err
}

func RemoveContainer(id string) error {
	err := CDRemoveContainer(id)
	if err == nil {
		CDInvalidateContainersCache()
	}
	return err
}

func CleanupAfterBuild() error {
	ctx, cancel := context.WithTimeout(context.Background(), TimeoutMedium)
	defer cancel()
	var cleanupErrors []string
	for _, command := range []string{
		"nerdctl container prune --force",
		"nerdctl image prune --force",
	} {
		if _, err := RunWSLWithCancel(ctx, command); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Sprintf("%s: %v", command, err))
		}
	}
	if len(cleanupErrors) > 0 {
		return fmt.Errorf("очистка после сборки не выполнена: %s", strings.Join(cleanupErrors, "; "))
	}
	return nil
}

func BuildProject(ctx context.Context, tag string) (string, error) {
	defer func() {
		if err := CleanupAfterBuild(); err != nil {
			fmt.Printf("⚠️ %v\n", err)
		}
	}()
	return RunWSLWithCancel(ctx, BuildComposeCommand("build"))
}

func RunProject(ctx context.Context) (string, error) {
	projectPath := GetProjectPathWSL()
	if projectPath == "" {
		return "", fmt.Errorf("⚠️ Путь к проекту не настроен!\n\nПерейдите в Настройки и укажите путь к папке с docker-compose.yml")
	}
	return RunWSLWithCancel(ctx, BuildComposeCommand("up", "-d"))
}

func BuildAndRunProject(ctx context.Context) (string, error) {
	projectPath := GetProjectPathWSL()
	if projectPath == "" {
		return "", fmt.Errorf("⚠️ Путь к проекту не настроен!\n\nПерейдите в Настройки и укажите путь к папке с docker-compose.yml")
	}
	defer func() {
		if err := CleanupAfterBuild(); err != nil {
			fmt.Printf("⚠️ %v\n", err)
		}
	}()

	fmt.Println("🔨 Сборка и запуск напрямую через nerdctl...")
	fmt.Println("🔍 Проверка buildkitd...")
	startedByUs := false
	if !CheckBuildkitd() {
		fmt.Println("⚙️ Запуск buildkitd...")
		if err := StartBuildkitd(); err != nil {
			errorMsg := fmt.Sprintf("❌ Не удалось запустить buildkitd: %v\n\n", err)
			errorMsg += "Попробуйте запустить вручную:\n"
			errorMsg += "  wsl bash -c \"sudo /usr/local/bin/buildkitd --addr unix:///run/buildkit/buildkitd.sock &\"\n"
			return "", fmt.Errorf("%s", errorMsg)
		}
		fmt.Println("✅ buildkitd запущен")
		startedByUs = true
	} else {
		fmt.Println("✅ buildkitd уже запущен")
	}
	defer func() {
		if startedByUs {
			StopBuildkitd()
		}
	}()
	defer StopBuildkitd()

	out, err := buildProjectImages(ctx, projectPath)
	if err != nil {
		if ctx.Err() != nil {
			return out, fmt.Errorf("⛔ Сборка отменена пользователем")
		}
		errorMsg := "❌ Ошибка сборки\n\n"
		if out != "" {
			lines := strings.Split(out, "\n")
			start := 0
			if len(lines) > 30 {
				start = len(lines) - 30
			}
			errorMsg += "Вывод (последние 30 строк):\n" + strings.Join(lines[start:], "\n") + "\n\n"
		}
		errorMsg += "Статус: " + err.Error()
		return out, fmt.Errorf("%s", errorMsg)
	}

	fmt.Println("🚀 Запуск стека через nerdctl compose...")
	scriptsPath := GetScriptsPath()
	startCmd := fmt.Sprintf(
		"cd %s && if [[ -f backend/config/.env ]]; then set -a; source backend/config/.env; set +a; fi; nerdctl compose -f %s/compose.yaml up -d",
		shellQuote(projectPath),
		shellQuote(scriptsPath),
	)
	result, err := RunWSLWithCancel(ctx, startCmd)
	if err != nil {
		if ctx.Err() != nil {
			return result, fmt.Errorf("⛔ Запуск отменён пользователем")
		}
		return result, fmt.Errorf("❌ Ошибка запуска:\n\n%v", err)
	}
	CDInvalidateContainersCache()
	CDInvalidateImagesCache()
	return result, nil
}

func buildProjectImages(ctx context.Context, projectPath string) (string, error) {
	commands := []string{
		"nerdctl build --progress=plain --tag soul-dialogue/postgres:latest --file postgres/Dockerfile ./postgres",
		"nerdctl build --progress=plain --tag soul-dialogue/backend:latest --file backend/Dockerfile ./backend",
		"nerdctl build --progress=plain --tag soul-dialogue/frontend:latest --file frontend/Dockerfile .",
	}
	var output strings.Builder
	for _, command := range commands {
		buildCommand := fmt.Sprintf(
			"cd %s && export BUILDKIT_STEP_LOG_MAX_SIZE=10000000 BUILDKIT_STEP_LOG_MAX_SPEED=1000000 && %s",
			shellQuote(projectPath),
			command,
		)
		result, err := RunWSLWithCancel(ctx, buildCommand)
		if output.Len() > 0 && result != "" {
			output.WriteString("\n")
		}
		output.WriteString(result)
		if err != nil {
			return output.String(), err
		}
	}
	return output.String(), nil
}

func GetDBInfo(volumeName string) (string, []string, error) {
	return CDGetDBInfo(volumeName)
}

func ListImages() ([]Image, error) {
	return CDListImages()
}

func RemoveImage(id string) error {
	err := CDRemoveImage(id)
	if err == nil {
		CDInvalidateImagesCache()
		ClearImageSizeCache()
	}
	return err
}

func ListVolumes() ([]Volume, error) {
	return CDListVolumes()
}

func RemoveVolume(name string) error {
	return CDRemoveVolume(name)
}

func GetContainerLogs(id string, tail int) (string, error) {
	return CDGetContainerLogs(id, tail)
}

var statusCache = newBoundedStringCache(1*time.Hour, 500)

func TranslateStatus(status string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	if translated, ok := statusCache.Get(status); ok {
		return translated
	}
	var result string
	switch {
	case strings.Contains(status, "healthy"):
		result = i18n.T("container_status.healthy")
	case strings.Contains(status, "unhealthy"):
		result = i18n.T("container_status.unhealthy")
	case strings.Contains(status, "running") || strings.Contains(status, "up"):
		result = i18n.T("container_status.running")
	case strings.Contains(status, "created"):
		result = i18n.T("container_status.created")
	case strings.Contains(status, "restarting"):
		result = i18n.T("container_status.restarting")
	case strings.Contains(status, "removing"):
		result = i18n.T("container_status.removing")
	case strings.Contains(status, "paused"):
		result = i18n.T("container_status.paused")
	case strings.Contains(status, "exited"):
		result = i18n.T("container_status.exited")
	case strings.Contains(status, "dead"):
		result = i18n.T("container_status.dead")
	default:
		result = status
	}
	statusCache.Set(status, result)
	return result
}

func ClearContainerLogs(id string) error {
	return CDClearContainerLogs(id)
}

func CleanContainerdLogs() (string, error) {
	out, err := RunWSL("sudo find /var/log -name '*.log' -mtime +7 -delete 2>/dev/null; echo '---'; sudo journalctl --vacuum-time=7d 2>/dev/null")
	return out, err
}

var cleanCache = struct {
	sync.RWMutex
	data      string
	err       error
	timestamp time.Time
}{
	timestamp: time.Now().Add(-20 * time.Second),
}

func CleanNerdctlCache() (string, error) {
	cleanCache.RLock()
	if cleanCache.data != "" && time.Since(cleanCache.timestamp) < 10*time.Second {
		result := cleanCache.data
		err := cleanCache.err
		cleanCache.RUnlock()
		return result, err
	}
	cleanCache.RUnlock()

	res, err := RunWSL("nerdctl system prune --force 2>&1")
	if err == nil && strings.TrimSpace(res) == "" {
		res = "Система чиста — нечего удалять"
	}
	cleanCache.Lock()
	cleanCache.data = res
	cleanCache.err = err
	cleanCache.timestamp = time.Now()
	cleanCache.Unlock()
	return res, err
}

func CleanUnusedVolumes(ctx context.Context) (string, error) {
	result, err := RunWSLWithCancel(ctx, "nerdctl volume prune --force 2>&1")
	if err != nil {
		return result, fmt.Errorf("не удалось очистить неиспользуемые тома через wsl.exe: %w", err)
	}
	if strings.TrimSpace(result) == "" {
		result = "Неиспользуемые тома не найдены"
	}
	CDInvalidateVolumesCache()
	CDInvalidateContainersCache()
	return result, nil
}

func CleanUnusedNetworks(ctx context.Context) (string, error) {
	script := `
mapfile -t all_nets < <(nerdctl network ls --format '{{.Name}}' 2>/dev/null)
if [ ${#all_nets[@]} -eq 0 ]; then
	echo "Нет сетей для очистки"
	exit 0
fi
used_nets=$(nerdctl ps -a --format '{{json .}}' 2>/dev/null | \
	awk -F'"NetworkMode":' '{if(NF>1){gsub(/[^a-zA-Z0-9_.-]/,"",$2);print $2}}' | \
	awk -F'"Networks":' '{if(NF>1){gsub(/[{}\[\]" ]/,"",$2);print $2}}' | \
	tr ',' '\n' | sort -u)
skip_nets="bridge host none default"
removed=0
for net in "${all_nets[@]}"; do
	net=$(echo "$net" | xargs)
	[ -z "$net" ] && continue
	skip=0
	for s in $skip_nets; do
		[ "$net" = "$s" ] && skip=1
	done
	[ $skip -eq 1 ] && continue
	if echo "$used_nets" | grep -qx "$net"; then
		continue
	fi
	if nerdctl network rm "$net" >/dev/null 2>&1; then
		echo "Удалена сеть: $net"
		((removed++))
	else
		echo "Не удалось удалить сеть: $net"
	fi
done
if [ $removed -eq 0 ]; then
	echo "Неиспользуемые сети не найдены"
else
	echo "Удалено неиспользуемых сетей: $removed"
fi
`
	result, err := RunWSLWithCancel(ctx, script)
	if err != nil {
		return result, err
	}
	CDInvalidateContainersCache()
	return result, nil
}

func CleanUntaggedImages(ctx context.Context) (string, error) {
	script := `
mapfile -t untagged < <(nerdctl images --format '{{.ID}}\t{{.Repository}}\t{{.Tag}}' 2>&1 | \
	awk -F'\t' '($2 == "<none>" || $3 == "<none>") && $1 != "" {print $1}')
if [ ${#untagged[@]} -eq 0 ]; then
	echo "Образы без тегов не найдены"
	exit 0
fi
removed=0
for img_id in "${untagged[@]}"; do
	if nerdctl rmi -f "$img_id" >/dev/null 2>&1; then
		echo "Удалён образ: $img_id"
		((removed++))
	else
		echo "Не удалось удалить образ: $img_id"
	fi
done
if [ $removed -gt 0 ]; then
	echo "Удалено образов без тегов: $removed"
else
	echo "Не удалось удалить ни одного образа"
fi
`
	result, err := RunWSLWithCancel(ctx, script)
	if err != nil {
		return result, err
	}
	if strings.Contains(result, "Удалено образов") {
		CDInvalidateImagesCache()
		ClearImageSizeCache()
	}
	return result, nil
}

func GetHostResources() (string, error) {
	return RunWSL("free -h | grep Mem && echo '---' && cat /proc/loadavg")
}

func GetStats() ([]ContainerStat, error) {
	return CDGetStats()
}

type SystemResources struct {
	RAMTotal  string
	RAMUsed   string
	RAMFree   string
	CPUCores  string
	CPULoad   string
	DiskTotal string
	DiskUsed  string
	DiskFree  string
}

var sysResCache = struct {
	sync.RWMutex
	data      *SystemResources
	timestamp time.Time
}{
	timestamp: time.Now().Add(-10 * time.Second),
}

func GetSystemResources() (*SystemResources, error) {
	sysResCache.RLock()
	if sysResCache.data != nil && time.Since(sysResCache.timestamp) < 5*time.Second {
		result := *sysResCache.data
		sysResCache.RUnlock()
		return &result, nil
	}
	sysResCache.RUnlock()

	out, err := RunWSL(
		"free -h | grep Mem && echo '---CPU---' && nproc && cat /proc/loadavg | awk '{print $1, $2, $3}' && echo '---DISK---' && df -h / | tail -1",
	)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(out, "---CPU---")
	if len(parts) < 2 {
		return nil, fmt.Errorf("не удалось распарсить системные ресурсы")
	}
	ramFields := strings.Fields(strings.TrimSpace(parts[0]))
	var ramTotal, ramUsed, ramFree string
	if len(ramFields) >= 3 {
		ramTotal = ramFields[1]
		ramUsed = ramFields[2]
		ramFree = ramFields[3]
	}
	cpuParts := strings.Split(parts[1], "---DISK---")
	var cpuCores, cpuLoad string
	if len(cpuParts) >= 1 {
		cpuLines := strings.Split(strings.TrimSpace(cpuParts[0]), "\n")
		if len(cpuLines) >= 2 {
			cpuCores = strings.TrimSpace(cpuLines[0])
			loadFields := strings.Fields(cpuLines[1])
			if len(loadFields) >= 3 {
				cpuLoad = loadFields[0] + " " + loadFields[1] + " " + loadFields[2]
			}
		}
	}
	diskFields := strings.Fields(strings.TrimSpace(cpuParts[1]))
	var diskTotal, diskUsed, diskFree string
	if len(diskFields) >= 5 {
		diskTotal = diskFields[1]
		diskUsed = diskFields[2]
		diskFree = diskFields[3]
	}
	result := &SystemResources{
		RAMTotal:  ramTotal,
		RAMUsed:   ramUsed,
		RAMFree:   ramFree,
		CPUCores:  cpuCores,
		CPULoad:   cpuLoad,
		DiskTotal: diskTotal,
		DiskUsed:  diskUsed,
		DiskFree:  diskFree,
	}
	sysResCache.Lock()
	sysResCache.data = result
	sysResCache.timestamp = time.Now()
	sysResCache.Unlock()
	return result, nil
}

var dateCache = newBoundedStringCache(1*time.Hour, 500)

func FormatDateShort(dateStr string) string {
	if formatted, ok := dateCache.Get(dateStr); ok {
		return formatted
	}
	formatted := formatDateShort(dateStr)
	dateCache.Set(dateStr, formatted)
	return formatted
}

func formatDateShort(dateStr string) string {
	t, err := time.Parse("2006-01-02 15:04:05 -0700", dateStr)
	if err == nil {
		return t.Format("2006-01-02 15:04")
	}
	t, err = time.Parse(time.RFC3339, dateStr)
	if err == nil {
		return t.Format("2006-01-02 15:04")
	}
	if len(dateStr) > 16 {
		return dateStr[:16]
	}
	return dateStr
}

func GetContainerConfig(id string) (*ContainerConfig, error) {
	return CDGetContainerConfig(id)
}

func UpdateContainerImage(id string, newImage string) (string, error) {
	var logs []string
	logs = append(logs, "📋 Получение конфигурации контейнера...")
	config, err := GetContainerConfig(id)
	if err != nil {
		return strings.Join(logs, "\n"), fmt.Errorf("не удалось получить конфигурацию: %w", err)
	}
	logs = append(logs, "✅ Конфигурация получена: "+config.Name)

	logs = append(logs, "🛑 Остановка контейнера...")
	if err := CDStopContainer(id); err != nil {
		logs = append(logs, "⚠️ Не удалось остановить через gRPC, пробуем принудительно...")
		_, _ = RunWSL(fmt.Sprintf("nerdctl kill %s 2>/dev/null", shellQuote(id)))
		time.Sleep(1 * time.Second)
	}

	logs = append(logs, "🗑️ Удаление старого контейнера...")
	if err := CDRemoveContainer(id); err != nil {
		_, err = RunWSL(fmt.Sprintf("nerdctl rm -f %s 2>/dev/null", shellQuote(id)))
		if err != nil {
			return strings.Join(logs, "\n"), fmt.Errorf("не удалось удалить контейнер: %w", err)
		}
	}

	if config.Image != "" && config.Image != newImage {
		logs = append(logs, "🗑️ Удаление старого образа...")
		if err := CDRemoveImage(config.Image); err != nil {
			logs = append(logs, "⚠️ Не удалось удалить старый образ — продолжим")
		}
	}
	CDInvalidateContainersCache()
	CDInvalidateImagesCache()

	logs = append(logs, "⬇️  Загрузка нового образа: "+newImage)
	pullOut, err := RunWSL(fmt.Sprintf("nerdctl pull %s", newImage))
	if err != nil {
		return strings.Join(logs, "\n"), fmt.Errorf("не удалось загрузить образ: %w", err)
	}
	if pullOut != "" {
		lines := strings.Split(pullOut, "\n")
		for _, line := range lines {
			if strings.Contains(line, "Pulling") || strings.Contains(line, "Downloading") {
				logs = append(logs, "  "+line)
			}
		}
	}
	logs = append(logs, "✅ Образ загружен")

	logs = append(logs, "🔄 Пересоздание контейнера...")
	runCmd := fmt.Sprintf("nerdctl run -d --name %s", config.Name)
	for _, vol := range config.Volumes {
		runCmd += fmt.Sprintf(" -v %s", vol)
	}
	if config.Ports != "" {
		runCmd += fmt.Sprintf(" -p %s", config.Ports)
	}
	for _, env := range config.Env {
		runCmd += fmt.Sprintf(" -e %s", env)
	}
	for k, v := range config.Labels {
		runCmd += fmt.Sprintf(" --label %s=%s", k, v)
	}
	if config.Network != "" && config.Network != "default" {
		runCmd += fmt.Sprintf(" --network %s", config.Network)
	}
	if cpu := GetDefaultCPU(); cpu != "" {
		runCmd += fmt.Sprintf(" --cpus=%s", cpu)
	}
	if mem := GetDefaultMemory(); mem != "" {
		runCmd += fmt.Sprintf(" --memory=%s", mem)
	}
	runCmd += " " + newImage
	logs = append(logs, "🚀 Выполняю: "+runCmd)
	_, err = RunWSL(runCmd)
	if err != nil {
		return strings.Join(logs, "\n"), fmt.Errorf("не удалось пересоздать контейнер: %w", err)
	}
	logs = append(logs, "✅ Контейнер пересоздан успешно!")
	return strings.Join(logs, "\n"), nil
}

var stringPool = sync.Pool{
	New: func() interface{} {
		b := make([]byte, 0, 256)
		return &b
	},
}

func getBuffer() *[]byte {
	return stringPool.Get().(*[]byte)
}

func putBuffer(b *[]byte) {
	*b = (*b)[:0]
	stringPool.Put(b)
}

func buildStringConcat(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0]
	}
	buf := getBuffer()
	totalLen := 0
	for _, p := range parts {
		totalLen += len(p) + len(sep)
	}
	*buf = make([]byte, totalLen)
	pos := 0
	for i, p := range parts {
		copy((*buf)[pos:], p)
		pos += len(p)
		if i < len(parts)-1 {
			copy((*buf)[pos:], sep)
			pos += len(sep)
		}
	}
	result := string(*buf)
	putBuffer(buf)
	return result
}

func formatStatusFast(status string) string {
	switch {
	case strings.Contains(status, "healthy"):
		return "[OK] Запущен (здоров)"
	case strings.Contains(status, "unhealthy"):
		return "[!] Запущен (болен)"
	case strings.Contains(status, "running") || strings.Contains(status, "up"):
		return "[OK] Запущен"
	case strings.Contains(status, "created"):
		return "Создан"
	case strings.Contains(status, "restarting"):
		return "Перезапуск..."
	case strings.Contains(status, "removing"):
		return "Удаление..."
	case strings.Contains(status, "paused"):
		return "Приостановлен"
	case strings.Contains(status, "exited"):
		return "Остановлен"
	case strings.Contains(status, "dead"):
		return "Мёртв"
	default:
		return status
	}
}

func CleanBuildkitCache(ctx context.Context) (string, error) {
	ttl := GetBuildkitCacheTTL()
	maxSize := GetBuildkitMaxSize()
	if ttl == 0 && maxSize == "" {
		return "Очистка кэша BuildKit отключена в настройках", nil
	}
	if !CheckBuildkitd() {
		return "⚠️ Кэш BuildKit не очищен\n\n" +
			"Демон buildkitd не запущен или недоступен.\n" +
			"Запустите buildkitd и повторите операцию.", nil
	}
	script := fmt.Sprintf(`
echo "🔨 Очистка кэша BuildKit"
echo "========================"
addr="unix:///run/buildkit/buildkitd.sock"
%s
echo ""
echo "🧹 Очистка неиспользуемых ресурсов..."
out=$(buildctl --addr $addr prune --all 2>/dev/null)
if [ -n "$out" ]; then
	echo "  $out"
else
	echo "  ✅ Нечего очищать"
fi
echo ""
echo "📊 Размер кэша BuildKit:"
du -sh /var/lib/buildkit 2>/dev/null || echo "  Кэш не найден"
%s
`,
		func() string {
			if ttl > 0 {
				return fmt.Sprintf(`
echo ""
echo "🧹 Очистка кэша старше %d часов..."
out=$(buildctl --addr $addr prune --filter=until=%dh --all 2>/dev/null)
if [ -n "$out" ]; then
	echo "  $out"
else
	echo "  ✅ Кэш очищен"
fi`, ttl, ttl)
			}
			return "echo \"Пропуск очистки по времени (отключено)\""
		}(),
		func() string {
			if maxSize == "" {
				return "echo \"Пропуск ограничения размера (не задано)\""
			}
			return fmt.Sprintf(`
echo ""
echo "⚙️ Ограничение кэша: %s"
size_bytes=$(du -sb /var/lib/buildkit 2>/dev/null | awk '{print $1}' || echo 0)
limit_str="%s"
limit_bytes=0
if [[ "$limit_str" =~ ^([0-9]+)g$ ]]; then
	limit_bytes=$(( ${BASH_REMATCH[1]} * 1024 * 1024 * 1024 ))
elif [[ "$limit_str" =~ ^([0-9]+)m$ ]]; then
	limit_bytes=$(( ${BASH_REMATCH[1]} * 1024 * 1024 ))
elif [[ "$limit_str" =~ ^([0-9]+)k$ ]]; then
	limit_bytes=$(( ${BASH_REMATCH[1]} * 1024 ))
fi
if [ "$size_bytes" -gt "$limit_bytes" ] && [ "$limit_bytes" -gt 0 ]; then
	echo "  ⚠️ Кэш превышает лимит! Принудительная очистка..."
	buildctl --addr $addr prune --all --keep-storage=%s 2>/dev/null
else
	echo "  ✅ Кэш в пределах лимита"
fi`, maxSize, maxSize, maxSize)
		}(),
	)
	results, err := RunWSLWithCancel(ctx, script)
	if err != nil {
		return results, err
	}
	return results, nil
}
