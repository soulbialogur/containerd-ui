package wsl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ============================================================================
// Структуры данных
// ============================================================================

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

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

type Image struct {
	ID         string `json:"ID"`
	Repository string `json:"Repository"`
	Tag        string `json:"Tag"`
	Size       string `json:"Size"`
	CreatedAt  string `json:"CreatedAt"`
	sizeBytes  int64  // внутреннее поле, не сериализуется (кэш размера в байтах)
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

// ============================================================================
// Парсер вывода сборки — определяет фазу по ключевым словам
// ============================================================================

// BuildPhase описывает текущую фазу сборки
type BuildPhase struct {
	Icon     string  // Иконка-эмодзи
	Title    string  // Название фазы
	Progress float32 // Прогресс (0.0-1.0)
}

// buildPhaseDefinition описывает фазу сборки с весами ключевых слов
type buildPhaseDefinition struct {
	// Ключевые слова для определения фазы
	keywords []string
	// Эмодзи-иконка
	icon string
	// Название фазы
	title string
	// Диапазон прогресса [min, max] — фаза обычно занимает этот диапазон
	progressRange [2]float32
	// Вес фазы — чем выше, тем выше приоритет при определении
	weight int
	// Сколько времени (в секундах) обычно длится фаза для оценки прогресса
	typicalDuration time.Duration
}

// buildPhaseMap — полная карта фаз сборки с весами и диапазонами
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

// detectProgressFromBar извлекает числовой прогресс из buildkit-формата:
// "[===                   ]  25%" или "25 / 100"
func detectProgressFromBar(line string) float32 {
	// Ищем паттерн "[===...] XX%" — buildkit progress bar
	// Формат: "[====================]  50%"
	idx := strings.LastIndex(line, "%")
	if idx > 0 {
		// Ищем число перед %
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

	// Ищем паттерн "N / M" (например "2 / 5")
	if slash := strings.Index(line, " / "); slash > 0 {
		var done, total float32
		n, _ := fmt.Sscanf(line[:slash], "%f", &done)
		if n == 1 {
			rest := line[slash+3:]
			// Ищем число в rest
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

	return -1 // Не удалось определить
}

// buildProgressTracker отслеживает прогресс сборки во времени
type buildProgressTracker struct {
	mu             sync.Mutex
	startTime      time.Time
	lastUpdateTime time.Time
	currentPhase   string
	phaseStartTime time.Time
	lastProgress   float32
}

// globalBuildTracker — глобальный трекер для отслеживания прогресса сборки
var globalBuildTracker = &buildProgressTracker{}

// ResetBuildProgress сбрасывает трекер при начале новой сборки
func ResetBuildProgress() {
	globalBuildTracker.mu.Lock()
	defer globalBuildTracker.mu.Unlock()

	globalBuildTracker.startTime = time.Now()
	globalBuildTracker.lastUpdateTime = time.Now()
	globalBuildTracker.currentPhase = ""
	globalBuildTracker.phaseStartTime = time.Now()
	globalBuildTracker.lastProgress = 0
}

// getPhaseProgress вычисляет прогресс внутри фазы на основе времени
func (t *buildProgressTracker) getPhaseProgress(phase buildPhaseDefinition, elapsed time.Duration) float32 {
	if phase.typicalDuration == 0 {
		// Для фаз без типичной длительности (успех/ошибка) возвращаем границы
		return phase.progressRange[1]
	}

	// Прогресс внутри фазы — линейная интерполяция по времени
	phaseProgress := float32(elapsed.Seconds() / phase.typicalDuration.Seconds())
	if phaseProgress > 1.0 {
		phaseProgress = 1.0
	}

	// Интерполируем прогресс в диапазоне фазы
	rangeSize := phase.progressRange[1] - phase.progressRange[0]
	return phase.progressRange[0] + phaseProgress*rangeSize
}

// DetermineBuildPhaseWithTime определяет фазу сборки с учётом временной динамики
func DetermineBuildPhaseWithTime(output string) BuildPhase {
	// Сначала проверяем buildkit progress bar — это самый точный источник
	if pct := detectMostRecentProgress(output); pct > 0 {
		return BuildPhase{"⏳", "Сборка...", pct}
	}

	// Определяем фазу по ключевым словам
	phaseDef, _ := determinePhaseByKeywords(output)

	// Если фаза найдена — вычисляем прогресс с учётом времени
	if phaseDef != nil {
		globalBuildTracker.mu.Lock()
		defer globalBuildTracker.mu.Unlock()

		now := time.Now()

		// Проверяем, сменилась ли фаза
		phaseName := phaseDef.title
		if globalBuildTracker.currentPhase != phaseName {
			// Фаза сменилась — обновляем таймеры
			globalBuildTracker.currentPhase = phaseName
			globalBuildTracker.phaseStartTime = now
		}

		elapsed := now.Sub(globalBuildTracker.phaseStartTime)
		progress := globalBuildTracker.getPhaseProgress(*phaseDef, elapsed)

		// Плавная интерполяция — прогресс не прыгает резко
		if globalBuildTracker.lastProgress > 0 {
			// Сглаживаем скачки: новый прогресс не может упасть более чем на 10%
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

	// Если фаза не определена — используем грубую оценку по длине вывода
	return estimateProgressByOutputLength(output)
}

// detectMostRecentProgress ищет самый свежий прогресс-бар в выводе
func detectMostRecentProgress(output string) float32 {
	lines := strings.Split(output, "\n")
	// Проходим с конца — последние строки имеют приоритет
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

// determinePhaseByKeywords определяет фазу по ключевым словам с весами
func determinePhaseByKeywords(output string) (*buildPhaseDefinition, float32) {
	lower := strings.ToLower(output)

	var bestPhase *buildPhaseDefinition
	bestScore := 0

	for i := range buildPhaseMap {
		def := &buildPhaseMap[i]

		// Пропускаем error/success — обрабатываем отдельно
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

	// Если ничего не найдено — возвращаем nil
	if bestPhase == nil {
		return nil, 0
	}

	return bestPhase, float32(bestScore)
}

// estimateProgressByOutputLength оценивает прогресс по длине вывода
// Это fallback, если не удалось определить фазу другими способами
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

// DetectBuildPhase определяет фазу сборки по выводу.
//
// Алгоритм:
//  1. Сначала ищем числовой прогресс (buildkit progress bar) — самый точный источник
//  2. Затем определяем фазу по ключевым словам с весами
//  3. Вычисляем прогресс с учётом временной динамики фазы
//  4. Если ничего не найдено — оцениваем по длине вывода
func DetectBuildPhase(output string) BuildPhase {
	lower := strings.ToLower(output)

	// ── Шаг 1: Проверяем ошибки (высший приоритет) ──
	for _, kw := range []string{"Error", "error", "failed", "fail", "panic"} {
		if strings.Contains(lower, kw) {
			// Сбрасываем трекер при ошибке
			ResetBuildProgress()
			return BuildPhase{"❌", "Ошибка сборки!", 0.0}
		}
	}

	// ── Шаг 2: Проверяем успех ──
	for _, kw := range []string{"Successfully", "success", "complete", "done", "Build complete"} {
		if strings.Contains(lower, kw) {
			// Сбрасываем трекер при успехе
			ResetBuildProgress()
			return BuildPhase{"🎉", "Успешно!", 1.0}
		}
	}

	// ── Шаг 3: Определяем фазу с учётом временной динамики ──
	return DetermineBuildPhaseWithTime(output)
}

// FormatBuildStatus форматирует красивый статус для отображения
func FormatBuildStatus(phase BuildPhase) string {
	return fmt.Sprintf("%s %s", phase.Icon, phase.Title)
}

// ============================================================================
// Buildkitd — автозапуск при сборке
// ============================================================================

// BuildkitConfig хранит состояние buildkitd
var buildkitdState = struct {
	sync.RWMutex
	running bool
	pid     int
}{}

// CheckBuildkitd проверяет, запущен ли buildkitd
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

// StartBuildkitd запускает buildkitd в фоне
func StartBuildkitd() error {
	// Проверяем, запущен ли уже
	if CheckBuildkitd() {
		return nil
	}

	// Запускаем buildkitd
	_, err := RunWSL("sudo mkdir -p /run/buildkit && sudo chmod 777 /run/buildkit && sudo nohup /usr/local/bin/buildkitd --addr unix:///run/buildkit/buildkitd.sock > /tmp/buildkitd.log 2>&1 & chmod 666 /run/buildkit/buildkitd.sock")
	if err != nil {
		return fmt.Errorf("не удалось запустить buildkitd: %w", err)
	}

	// Ждём запуска
	time.Sleep(2 * time.Second)

	// Проверяем, что запустился
	if CheckBuildkitd() {
		buildkitdState.Lock()
		buildkitdState.running = true
		buildkitdState.Unlock()
		return nil
	}

	return fmt.Errorf("buildkitd запустился, но не отвечает на запросы")
}

// StopBuildkitd останавливает buildkitd
func StopBuildkitd() {
	_, err := RunWSL("sudo pkill -f buildkitd 2>/dev/null; echo 'ok'")
	if err == nil {
		buildkitdState.Lock()
		buildkitdState.running = false
		buildkitdState.pid = 0
		buildkitdState.Unlock()
	}
}

// RunWSL с кэшированием результатов (избегает повторных вызовов для одинаковых команд)
const maxWSLCacheSize = 10 * 1024 * 1024 // 10MB максимальный размер кэша

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
	size      int64 // размер в байтах
}

func RunWSL(command string) (string, error) {
	// Быстрый путь из кэша для повторяющихся команд
	wslCache.RLock()
	ttl := time.Duration(wslCacheTTL.Load()) * time.Second
	if entry, ok := wslCache.m[command]; ok && time.Since(entry.timestamp) < ttl {
		wslCache.RUnlock()
		return entry.output, entry.err
	}
	wslCache.RUnlock()

	// Выполняем команду
	cmd := exec.Command("wsl", "-d", GetWslDistro(), "bash", "-c", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := strings.TrimSpace(out.String())
	resultSize := int64(len(result))

	// Сохраняем в кэш
	wslCache.Lock()

	// Удаляем просроченные записи
	for k, v := range wslCache.m {
		if time.Since(v.timestamp) > ttl {
			wslCache.totalSize -= v.size
			delete(wslCache.m, k)
		}
	}

	// Если кэш переполнен по размеру или количеству записей — удаляем самые старые.
	for (wslCache.totalSize+resultSize > wslCache.maxSize ||
		(wslCache.cleanupAt > 0 && len(wslCache.m) >= wslCache.cleanupAt)) && len(wslCache.m) > 0 {
		// Находим самую старую запись
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

// RunWSLWithCancel — версия RunWSL с поддержкой отмены через context
func RunWSLWithCancel(ctx context.Context, command string) (string, error) {
	// Отключаем кэширование для команд сборки и запуска
	// Эти команды всегда должны выполняться заново
	if isBuildCommand(command) {
		// Пропускаем кэш полностью — даже запись
		return executeWSLCommand(ctx, command, true)
	}

	// Быстрый путь из кэша для повторяющихся команд
	wslCache.RLock()
	ttl := time.Duration(wslCacheTTL.Load()) * time.Second
	if entry, ok := wslCache.m[command]; ok && time.Since(entry.timestamp) < ttl {
		wslCache.RUnlock()
		return entry.output, entry.err
	}
	wslCache.RUnlock()

	// Выполняем команду с контекстом
	return executeWSLCommand(ctx, command, false)
}

// executeWSLCommand выполняет команду WSL с контекстом
// skipCache — если true, результат НЕ сохраняется в кэш (для команд сборки)
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

	// Сохраняем в кэш ТОЛЬКО если не skipCache и команда не отменена
	// Защита: результат > 1МБ не кэшируется (защита от утечки памяти)
	if ctx.Err() == nil && !skipCache {
		resultSize := len(result)
		if resultSize < 1024*1024 { // < 1MB
			wslCache.Lock()
			wslCache.m[command] = wslCacheEntry{
				output:    result,
				err:       err,
				timestamp: time.Now(),
			}
			// Ограничиваем размер кэша
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

	// ВАЖНО: Если есть ошибка и stderr содержит вывод — возвращаем его как часть результата
	// Потому что nerdctl compose build выводит ошибки именно в stderr
	if err != nil && errOutput != "" {
		// Объединяем stdout и stderr для полной картины
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

// isBuildCommand проверяет, является ли команда командой сборки
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

// InvalidateWSLCache инвалидирует кэш WSL (оптимизировано)
func InvalidateWSLCache() {
	wslCache.Lock()
	// Оптимизация: вместо create новой мапы — очищаем существующую
	// Это сохраняет allocated capacity и avoids reallocation
	for k := range wslCache.m {
		delete(wslCache.m, k)
	}
	wslCache.Unlock()
}

// Проверка работы WSL и containerd (через прямой gRPC API)
// Оптимизация: fallback-команды WSL объединены в один вызов
func CheckService() map[string]interface{} {
	status := map[string]interface{}{"wsl": false, "nerdctl": false, "containerd": false, "error": ""}

	_, err := RunWSL("echo ok")
	if err != nil {
		status["error"] = fmt.Sprintf("WSL '%s' не найден", GetWslDistro())
		return status
	}
	status["wsl"] = true

	// Проверяем containerd через кэшированный gRPC API
	if cdAvailable.Load() || CDCheck() == nil {
		status["containerd"] = true
		status["nerdctl"] = true
		return status
	}

	// Fallback: обе проверки в одном вызове wsl.exe
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

// Список контейнеров (прямой containerd API)
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

// Действия с контейнерами
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

// ForceStopContainer — принудительное убийство контейнера (SIGKILL)
func ForceStopContainer(id string) error {
	// Сначала пробуем stop (SIGTERM), если не помогло — kill (SIGKILL)
	_, err := RunWSL("nerdctl stop -t 2 " + id + " 2>/dev/null; nerdctl kill " + id)
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

// CleanupAfterBuild удаляет временные ресурсы, оставшиеся после сборки.
// Ошибка очистки не должна скрывать результат самой сборки.
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

// BuildProject собирает все образы из docker-compose.yml
func BuildProject(ctx context.Context, tag string) (string, error) {
	defer func() {
		if err := CleanupAfterBuild(); err != nil {
			fmt.Printf("⚠️ %v\n", err)
		}
	}()

	return RunWSLWithCancel(ctx, BuildComposeCommand("build"))
}

// RunProject запускает весь проект (только существующие образы)
// Автоматически создаёт тома и контейнеры, если их нет
func RunProject(ctx context.Context) (string, error) {
	projectPath := GetProjectPathWSL()
	if projectPath == "" {
		return "", fmt.Errorf("⚠️ Путь к проекту не настроен!\n\nПерейдите в Настройки и укажите путь к папке с docker-compose.yml")
	}
	return RunWSLWithCancel(ctx, BuildComposeCommand("up", "-d"))
}

// BuildAndRunProject собирает и запускает проект прямыми nerdctl-командами.
// Автоматически управляет buildkitd: запускает перед сборкой, останавливает после
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

	// АВТОМАТИЧЕСКИЙ ЗАПУСК BUILDKITD
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

	// Останавливаем только если запустили сами
	defer func() {
		if startedByUs {
			StopBuildkitd()
		}
	}()

	// ГАРАНТИРОВАННАЯ ОЧИСТКА — buildkitd остановится даже при panic
	defer StopBuildkitd()

	// Запускаем сборку без промежуточного shell-скрипта.
	out, err := buildProjectImages(ctx, projectPath)

	if err != nil {
		if ctx.Err() != nil {
			return out, fmt.Errorf("⛔ Сборка отменена пользователем")
		}

		// Формируем понятное сообщение с выводом
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

	// Запускаем проект напрямую через compose.
	fmt.Println("🚀 Запуск стека через nerdctl compose...")
	scriptsPath := GetScriptsPath()
	startCmd := fmt.Sprintf(
		"cd %s && if [[ -f backend/config/.env ]]; then set -a; source backend/config/.env; set +a; fi; nerdctl compose -f %s/compose.yaml up -d",
		strconv.Quote(projectPath),
		strconv.Quote(scriptsPath),
	)
	result, err := RunWSLWithCancel(ctx, startCmd)
	if err != nil {
		if ctx.Err() != nil {
			return result, fmt.Errorf("⛔ Запуск отменён пользователем")
		}
		return result, fmt.Errorf("❌ Ошибка запуска:\n\n%v", err)
	}

	// Инвалидируем кэши
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
			strconv.Quote(projectPath),
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

// База данных: размер и файлы одним запросом (оптимизация)
func GetDBInfo(volumeName string) (string, []string, error) {
	return CDGetDBInfo(volumeName)
}

// Список образов (прямой containerd API)
func ListImages() ([]Image, error) {
	return CDListImages()
}

// Удаление образа
func RemoveImage(id string) error {
	err := CDRemoveImage(id)
	if err == nil {
		CDInvalidateImagesCache()
		ClearImageSizeCache()
	}
	return err
}

// Список томов (напрямую из файловой системы, без nerdctl)
func ListVolumes() ([]Volume, error) {
	return CDListVolumes()
}

func RemoveVolume(name string) error {
	return CDRemoveVolume(name)
}

// Логи контейнера (напрямую из файлов, без nerdctl)
func GetContainerLogs(id string, tail int) (string, error) {
	return CDGetContainerLogs(id, tail)
}

// statusCache кэширует результат перевода статусов (ограничен 500 записями, TTL 1 час)
var statusCache = newBoundedStringCache(1*time.Hour, 500)

// TranslateStatus переводит статус контейнера на русский (с кэшированием).
func TranslateStatus(status string) string {
	status = strings.ToLower(strings.TrimSpace(status))

	if translated, ok := statusCache.Get(status); ok {
		return translated
	}

	var result string
	switch {
	case strings.Contains(status, "healthy"):
		result = "[OK] Запущен (здоров)"
	case strings.Contains(status, "unhealthy"):
		result = "[!] Запущен (болен)"
	case strings.Contains(status, "running") || strings.Contains(status, "up"):
		result = "[OK] Запущен"
	case strings.Contains(status, "created"):
		result = "Создан"
	case strings.Contains(status, "restarting"):
		result = "Перезапуск..."
	case strings.Contains(status, "removing"):
		result = "Удаление..."
	case strings.Contains(status, "paused"):
		result = "Приостановлен"
	case strings.Contains(status, "exited"):
		result = "Остановлен"
	case strings.Contains(status, "dead"):
		result = "Мёртв"
	default:
		result = status
	}

	statusCache.Set(status, result)
	return result
}

// Очистка логов контейнера (напрямую truncate, без nerdctl)
func ClearContainerLogs(id string) error {
	return CDClearContainerLogs(id)
}

// Очистка логов системы (одним вызовом wsl.exe)
func CleanContainerdLogs() (string, error) {
	out, err := RunWSL("sudo find /var/log -name '*.log' -mtime +7 -delete 2>/dev/null; echo '---'; sudo journalctl --vacuum-time=7d 2>/dev/null")
	return out, err
}

// Очистка кэша и dangling-образов через wsl.exe и nerdctl.
// Кэширует результат на 10 секунд
var cleanCache = struct {
	sync.RWMutex
	data      string
	err       error
	timestamp time.Time
}{
	timestamp: time.Now().Add(-20 * time.Second),
}

func CleanNerdctlCache() (string, error) {
	// Быстрый путь из кэша
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

// CleanUnusedVolumes удаляет все неиспользуемые тома через wsl.exe и nerdctl.
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

// CleanUnusedNetworks удаляет все сети, которые не используются контейнерами
func CleanUnusedNetworks(ctx context.Context) (string, error) {
	netOut, err := RunWSLWithCancel(ctx, "nerdctl network ls --format '{{.Name}}' 2>/dev/null")
	if err != nil {
		return "", err
	}

	allNetworks := strings.Split(netOut, "\n")
	if len(allNetworks) == 0 {
		return "Нет сетей для очистки", nil
	}

	psOut, err := RunWSLWithCancel(ctx, "nerdctl ps -a --format '{{json .}}' 2>/dev/null")
	if err != nil {
		return "Не удалось получить список контейнеров", nil
	}

	usedNetworks := make(map[string]bool)
	lines := strings.Split(psOut, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var container struct {
			NetworkMode     string `json:"NetworkMode"`
			NetworkSettings struct {
				Networks map[string]struct{} `json:"Networks"`
			} `json:"NetworkSettings"`
		}
		if err := json.Unmarshal([]byte(line), &container); err != nil {
			continue
		}
		if container.NetworkMode != "" && container.NetworkMode != "default" {
			usedNetworks[container.NetworkMode] = true
		}
		for netName := range container.NetworkSettings.Networks {
			if netName != "" && netName != "default" {
				usedNetworks[netName] = true
			}
		}
	}

	var unusedNets []string
	for _, net := range allNetworks {
		net = strings.TrimSpace(net)
		if net == "" || usedNetworks[net] {
			continue
		}
		if net == "bridge" || net == "host" || net == "none" {
			continue
		}
		unusedNets = append(unusedNets, net)
	}

	if len(unusedNets) == 0 {
		return "Неиспользуемые сети не найдены", nil
	}

	var removedNet int
	var results []string

	netCmd := fmt.Sprintf("nerdctl network rm %s 2>/dev/null", strings.Join(unusedNets, " "))
	_, err = RunWSLWithCancel(ctx, netCmd)
	if err != nil {
		for _, net := range unusedNets {
			if _, err := RunWSLWithCancel(ctx, fmt.Sprintf("nerdctl network rm %s 2>/dev/null", net)); err == nil {
				removedNet++
				results = append(results, "Удалена сеть: "+net)
			}
		}
		if removedNet > 0 {
			CDInvalidateContainersCache()
			return fmt.Sprintf("Удалено неиспользуемых сетей: %d\n%s", removedNet, strings.Join(results, "\n")), nil
		}
		return "", err
	}

	for _, net := range unusedNets {
		removedNet++
		results = append(results, "Удалена сеть: "+net)
	}

	CDInvalidateContainersCache()
	return fmt.Sprintf("Удалено неиспользуемых сетей: %d\n%s", removedNet, strings.Join(results, "\n")), nil
}

// CleanUntaggedImages удаляет все образы без тегов через wsl.exe и nerdctl.
func CleanUntaggedImages(ctx context.Context) (string, error) {
	out, err := RunWSLWithCancel(ctx, "nerdctl images --format '{{.ID}}\t{{.Repository}}\t{{.Tag}}' 2>&1")
	if err != nil {
		return "", fmt.Errorf("не удалось получить список образов через wsl.exe: %w", err)
	}

	var untaggedIDs []string
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(strings.TrimSpace(line), "\t")
		if len(fields) < 3 || fields[0] == "" {
			continue
		}
		if fields[1] == "<none>" || fields[2] == "<none>" {
			untaggedIDs = append(untaggedIDs, fields[0])
		}
	}

	if len(untaggedIDs) == 0 {
		return "Образы без тегов не найдены", nil
	}

	var removedImg int
	var results []string

	for _, imageID := range untaggedIDs {
		if _, err := RunWSLWithCancel(ctx, fmt.Sprintf("nerdctl rmi -f %s 2>&1", shellQuote(imageID))); err == nil {
			removedImg++
			results = append(results, "Удалён образ: "+imageID)
		}
	}

	if removedImg > 0 {
		CDInvalidateImagesCache()
		ClearImageSizeCache()
		return fmt.Sprintf("Удалено образов без тегов: %d\n%s", removedImg, strings.Join(results, "\n")), nil
	}
	return "", fmt.Errorf("не удалось удалить ни одного образа через wsl.exe")
}

// Мониторинг ресурсов (через WSL procfs)
func GetHostResources() (string, error) {
	return RunWSL("free -h | grep Mem && echo '---' && cat /proc/loadavg")
}

// Статистика контейнеров (через containerd metrics API)
func GetStats() ([]ContainerStat, error) {
	return CDGetStats()
}

// ---------------------------------------------------------------------------
// Системные ресурсы (RAM, CPU, Disk)
// ---------------------------------------------------------------------------

// SystemResources содержит информацию о системных ресурсах
type SystemResources struct {
	RAMTotal  string // Всего RAM
	RAMUsed   string // Использовано RAM
	RAMFree   string // Свободно RAM
	CPUCores  string // Ядра CPU
	CPULoad   string // Нагрузка CPU
	DiskTotal string // Всего диск
	DiskUsed  string // Использовано диск
	DiskFree  string // Свободно диск
}

// GetSystemResources получает информацию о системных ресурсах
// Кэширует результат на 5 секунд
var sysResCache = struct {
	sync.RWMutex
	data      *SystemResources
	timestamp time.Time
}{
	timestamp: time.Now().Add(-10 * time.Second), // Изначально просрочен
}

// GetSystemResources получает информацию о системных ресурсах
func GetSystemResources() (*SystemResources, error) {
	// Быстрый путь из кэша
	sysResCache.RLock()
	if sysResCache.data != nil && time.Since(sysResCache.timestamp) < 5*time.Second {
		result := *sysResCache.data
		sysResCache.RUnlock()
		return &result, nil
	}
	sysResCache.RUnlock()

	// Все команды в одном вызове wsl.exe
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

	// Сохраняем в кэш
	sysResCache.Lock()
	sysResCache.data = result
	sysResCache.timestamp = time.Now()
	sysResCache.Unlock()

	return result, nil
}

// ---------------------------------------------------------------------------
// Кэширование форматирования дат
// ---------------------------------------------------------------------------

// dateCache кэширует отформатированные даты (ограничен 500 записями, TTL 1 час)
var dateCache = newBoundedStringCache(1*time.Hour, 500)

// FormatDateShort форматирует дату коротко (с кэшированием).
func FormatDateShort(dateStr string) string {
	if formatted, ok := dateCache.Get(dateStr); ok {
		return formatted
	}

	formatted := formatDateShort(dateStr)
	dateCache.Set(dateStr, formatted)
	return formatted
}

// formatDateShort форматирует строку даты в формат "YYYY-MM-DD HH:MM".
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

// ---------------------------------------------------------------------------
// Обновление образа (CI/CD workflow)
// ---------------------------------------------------------------------------

// GetContainerConfig получает конфигурацию контейнера для обновления
// Использует gRPC API containerd (1 вызов вместо 7 WSL)
func GetContainerConfig(id string) (*ContainerConfig, error) {
	return CDGetContainerConfig(id)
}

// UpdateContainerImage выполняет обновление образа (CI/CD).
// Оптимизация: 13 WSL-вызовов → 2 WSL-вызова (pull + run).
// Остальные шаги (config, stop, rm, rmi) через gRPC.
func UpdateContainerImage(id string, newImage string) (string, error) {
	var logs []string

	// ========================================================================
	// ШАГ 1: Получение конфигурации (gRPC — 1 вызов вместо 8 nerdctl inspect)
	// ========================================================================
	logs = append(logs, "📋 Получение конфигурации контейнера...")
	config, err := GetContainerConfig(id)
	if err != nil {
		return strings.Join(logs, "\n"), fmt.Errorf("не удалось получить конфигурацию: %w", err)
	}
	logs = append(logs, "✅ Конфигурация получена: "+config.Name)

	// ========================================================================
	// ШАГ 2: Остановка контейнера (gRPC — CDStopContainer)
	// ========================================================================
	logs = append(logs, "🛑 Остановка контейнера...")
	if err := CDStopContainer(id); err != nil {
		logs = append(logs, "⚠️ Не удалось остановить через gRPC, пробуем принудительно...")
		// Fallback: kill через WSL (только если gRPC не справился)
		_, _ = RunWSL(fmt.Sprintf("nerdctl kill %s 2>/dev/null", id))
		time.Sleep(1 * time.Second)
	}

	// ========================================================================
	// ШАГ 3: Удаление контейнера (gRPC — CDRemoveContainer)
	// ========================================================================
	logs = append(logs, "🗑️ Удаление старого контейнера...")
	if err := CDRemoveContainer(id); err != nil {
		// Fallback: WSL
		_, err = RunWSL(fmt.Sprintf("nerdctl rm -f %s 2>/dev/null", id))
		if err != nil {
			return strings.Join(logs, "\n"), fmt.Errorf("не удалось удалить контейнер: %w", err)
		}
	}

	// ========================================================================
	// ШАГ 4: Удаление старого образа (gRPC — CDRemoveImage)
	// ========================================================================
	if config.Image != "" && config.Image != newImage {
		logs = append(logs, "🗑️ Удаление старого образа...")
		if err := CDRemoveImage(config.Image); err != nil {
			logs = append(logs, "⚠️ Не удалось удалить старый образ — продолжим")
		}
	}

	// Инвалидация кэшей после изменений
	CDInvalidateContainersCache()
	CDInvalidateImagesCache()

	// ========================================================================
	// ШАГ 5: Загрузка нового образа (WSL — nerdctl pull)
	// ========================================================================
	logs = append(logs, "⬇️  Загрузка нового образа: "+newImage)
	pullOut, err := RunWSL(fmt.Sprintf("nerdctl pull %s", newImage))
	if err != nil {
		return strings.Join(logs, "\n"), fmt.Errorf("не удалось загрузить образ: %w", err)
	}
	// Показываем прогресс загрузки
	if pullOut != "" {
		lines := strings.Split(pullOut, "\n")
		for _, line := range lines {
			if strings.Contains(line, "Pulling") || strings.Contains(line, "Downloading") {
				logs = append(logs, "  "+line)
			}
		}
	}
	logs = append(logs, "✅ Образ загружен")

	// ========================================================================
	// ШАГ 6: Пересоздание контейнера (WSL — nerdctl run)
	// ========================================================================
	logs = append(logs, "🔄 Пересоздание контейнера...")

	// Формируем команду для пересоздания
	runCmd := fmt.Sprintf("nerdctl run -d --name %s", config.Name)

	// Добавляем тома
	for _, vol := range config.Volumes {
		runCmd += fmt.Sprintf(" -v %s", vol)
	}

	// Добавляем порты
	if config.Ports != "" {
		runCmd += fmt.Sprintf(" -p %s", config.Ports)
	}

	// Добавляем переменные окружения
	for _, env := range config.Env {
		runCmd += fmt.Sprintf(" -e %s", env)
	}

	// Добавляем метки
	for k, v := range config.Labels {
		runCmd += fmt.Sprintf(" --label %s=%s", k, v)
	}

	// Добавляем сеть
	if config.Network != "" && config.Network != "default" {
		runCmd += fmt.Sprintf(" --network %s", config.Network)
	}

	// Добавляем лимиты CPU и памяти из настроек
	if cpu := GetDefaultCPU(); cpu != "" {
		runCmd += fmt.Sprintf(" --cpus=%s", cpu)
	}
	if mem := GetDefaultMemory(); mem != "" {
		runCmd += fmt.Sprintf(" --memory=%s", mem)
	}

	// Добавляем образ
	runCmd += " " + newImage

	logs = append(logs, "🚀 Выполняю: "+runCmd)

	_, err = RunWSL(runCmd)
	if err != nil {
		return strings.Join(logs, "\n"), fmt.Errorf("не удалось пересоздать контейнер: %w", err)
	}

	logs = append(logs, "✅ Контейнер пересоздан успешно!")
	return strings.Join(logs, "\n"), nil
}

// ---------------------------------------------------------------------------
// Оптимизации производительности и памяти
// ---------------------------------------------------------------------------

// syncPoolString кэш для часто используемых строк (избегаем аллокаций)
var stringPool = sync.Pool{
	New: func() interface{} {
		b := make([]byte, 0, 256)
		return &b
	},
}

// getBuffer получает буфер из пула
func getBuffer() *[]byte {
	return stringPool.Get().(*[]byte)
}

// putBuffer возвращает буфер в пул
func putBuffer(b *[]byte) {
	*b = (*b)[:0] // сбрасываем длину, сохраняем capacity
	stringPool.Put(b)
}

// buildStringConcat объединяет строки с минимумом аллокаций
func buildStringConcat(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0]
	}

	buf := getBuffer()

	// Вычисляем общую длину
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

	// Сначала создаём строку, потом возвращаем буфер в пул
	result := string(*buf)
	putBuffer(buf)

	return result
}

// formatStatusFast — оптимизированная версия TranslateStatus без аллокаций
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

// ---------------------------------------------------------------------------
// Очистка кэша BuildKit
// ---------------------------------------------------------------------------

// CleanBuildkitCache очищает кэш BuildKit:
// 1. Удаляет кэш старше BuildkitCacheTTL часов (по умолчанию 24)
// 2. Удаляет неиспользуемые ресурсы, если кэш превышает BuildkitMaxSize (по умолчанию 5g)
func CleanBuildkitCache(ctx context.Context) (string, error) {
	var results []string
	ttl := GetBuildkitCacheTTL()
	maxSize := GetBuildkitMaxSize()

	// Проверяем, включена ли очистка
	if ttl == 0 && maxSize == "" {
		return "Очистка кэша BuildKit отключена в настройках", nil
	}

	if !CheckBuildkitd() {
		return "⚠️ Кэш BuildKit не очищен\n\n"+
			"Демон buildkitd не запущен или недоступен.\n"+
			"Запустите buildkitd и повторите операцию.", nil
	}

	// ШАГ 1: Очистка кэша старше N часов
	if ttl > 0 {
		results = append(results, "🧹 Очистка кэша старше "+strconv.Itoa(ttl)+" часов...")
		cmd := fmt.Sprintf("buildctl --addr unix:///run/buildkit/buildkitd.sock prune --filter=until=%dh --all 2>/dev/null", ttl)
		out, err := RunWSLWithCancel(ctx, cmd)
		if err != nil {
			results = append(results, "⚠️ Ошибка очистки по времени: "+err.Error())
		} else if out != "" {
			results = append(results, "  "+out)
		} else {
			results = append(results, "  ✅ Кэш очищен")
		}
	}

	// ШАГ 2: Очистка неиспользуемых ресурсов
	results = append(results, "🧹 Очистка неиспользуемых ресурсов...")
	cmd := "buildctl --addr unix:///run/buildkit/buildkitd.sock prune --all 2>/dev/null"
	out, err := RunWSLWithCancel(ctx, cmd)
	if err != nil {
		results = append(results, "⚠️ Ошибка очистки: "+err.Error())
	} else if out != "" {
		results = append(results, "  "+out)
	} else {
		results = append(results, "  ✅ Нечего очищать")
	}

	// ШАГ 3: Проверка размера кэша
	results = append(results, "📊 Размер кэша BuildKit:")
	sizeCmd := "du -sh /var/lib/buildkit 2>/dev/null || echo 'Кэш не найден'"
	sizeOut, _ := RunWSL(sizeCmd)
	results = append(results, "  "+sizeOut)

	// ШАГ 4: Ограничение размера кэша (если задано)
	if maxSize != "" {
		results = append(results, "⚙️ Ограничение кэша: "+maxSize)

		// Получаем текущий размер в байтах
		sizeCmdBytes := "du -sb /var/lib/buildkit 2>/dev/null | awk '{print $1}' || echo 0"
		sizeBytesStr, _ := RunWSL(sizeCmdBytes)

		// Парсим размер (упрощённо)
		var sizeBytes int64
		fmt.Sscanf(sizeBytesStr, "%d", &sizeBytes)

		// Парсим лимит (упрощённо: g=GB, m=MB, k=KB)
		var limitBytes int64
		sizeStr := strings.ToLower(maxSize)
		if strings.HasSuffix(sizeStr, "g") {
			fmt.Sscanf(sizeStr[:len(sizeStr)-1], "%d", &limitBytes)
			limitBytes *= 1024 * 1024 * 1024
		} else if strings.HasSuffix(sizeStr, "m") {
			fmt.Sscanf(sizeStr[:len(sizeStr)-1], "%d", &limitBytes)
			limitBytes *= 1024 * 1024
		} else if strings.HasSuffix(sizeStr, "k") {
			fmt.Sscanf(sizeStr[:len(sizeStr)-1], "%d", &limitBytes)
			limitBytes *= 1024
		} else {
			fmt.Sscanf(sizeStr, "%d", &limitBytes)
		}

		if sizeBytes > limitBytes && limitBytes > 0 {
			results = append(results, "  ⚠️ Кэш превышает лимит! Принудительная очистка...")
			forceCmd := "buildctl --addr unix:///run/buildkit/buildkitd.sock prune --all --keep-storage=" + maxSize + " 2>/dev/null"
			forceOut, _ := RunWSLWithCancel(ctx, forceCmd)
			if forceOut != "" {
				results = append(results, "  "+forceOut)
			}
		} else {
			results = append(results, "  ✅ Кэш в пределах лимита")
		}
	}

	return strings.Join(results, "\n"), nil
}
