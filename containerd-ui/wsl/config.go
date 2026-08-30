package wsl

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// ConfigPath возвращает путь к файлу конфигурации
func ConfigPath() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), "config.json")
	}
	return "config.json"
}

// AppConfig хранит все настройки приложения
type AppConfig struct {
	// Путь к проекту с docker-compose.yml
	ProjectPath string `json:"project_path"`

	// Имя WSL-дистрибутива
	WslDistro string `json:"wsl_distro"`

	// Порт для gRPC-прокси containerd
	CdPort int `json:"cd_port"`

	// Пространство имён containerd (namespace)
	CdNamespace string `json:"cd_namespace"`

	// Путь к скрипту start-containerd.sh (относительный от проекта)
	ScriptsPath string `json:"scripts_path"`

	// Имя тома PostgreSQL
	DBVolumeName string `json:"db_volume_name"`

	// Имя systemd-сервиса containerd
	SystemdService string `json:"systemd_service"`

	// Путь к nerdctl (пустая строка = в PATH)
	NerdctlPath string `json:"nerdctl_path"`

	// Количество последних логов для отображения
	LogTail int `json:"log_tail"`

	// Время жизни кэша WSL в секундах
	WslCacheTTL int `json:"wsl_cache_ttl"`

	// Время жизни кэша контейнеров в секундах
	ContainersCacheTTL int `json:"containers_cache_ttl"`

	// Время жизни кэша образов в секундах
	ImagesCacheTTL int `json:"images_cache_ttl"`

	// Время жизни кэша томов в секундах
	VolumesCacheTTL int `json:"volumes_cache_ttl"`

	// Автообновление таблицы контейнеров (сек)
	AutoRefreshInterval int `json:"auto_refresh_interval"`

	// Режим экономии ресурсов UI
	EconomyMode bool `json:"economy_mode"`

	// Настройки сборки образов
	SquashLayers     bool   `json:"squash_layers"`
	Compression      string `json:"compression"`       // "gzip", "zstd", "none"
	CompressionLevel int    `json:"compression_level"` // 1-9

	// Максимальный размер кэша WSL (байты)
	MaxWSLCacheSize int64 `json:"max_wsl_cache_size"`

	// Порог очистки кэша WSL (количество записей)
	WSLCacheCleanupAt int `json:"wsl_cache_cleanup_at"`

	// TTL кэшей (сек)
	CacheContainer       int `json:"cache_container"`
	CacheImage           int `json:"cache_image"`
	CacheVolume          int `json:"cache_volume"`
	CacheStats           int `json:"cache_stats"`
	CacheContainerStatus int `json:"cache_container_status"`
	CacheSplitImage      int `json:"cache_split_image"`
	CacheHumanSize       int `json:"cache_human_size"`

	// Максимальное число записей в кэше
	MaxCacheEntries int `json:"max_cache_entries"`

	// Настройки retry для подключения
	RetryInitialDelay int `json:"retry_initial_delay"`
	RetryMaxDelay     int `json:"retry_max_delay"`
	RetryMultiplier   int `json:"retry_multiplier"`
	RetryMaxAttempts  int `json:"retry_max_attempts"`

	// Лимиты по умолчанию для новых контейнеров
	DefaultCPU    string `json:"default_cpu_limit"`    // например "0.5", "1.5", "2"
	DefaultMemory string `json:"default_memory_limit"` // например "512m", "1g", "2g"

	// Параллельная сборка
	MaxParallelism int `json:"max_parallelism"` // максимальное число параллельных сборок (0 = без ограничений)

	// Параллельные операции с контейнерами
	ContainerOperationConcurrency int `json:"container_operation_concurrency"` // число одновременных операций

	// Очистка кэша BuildKit
	BuildkitCacheTTL int    `json:"buildkit_cache_ttl"` // очищать кэш старше N часов (0 = отключено)
	BuildkitMaxSize  string `json:"buildkit_max_size"`  // максимальный размер кэша (например "5g", "10g")

	// Прокси для деплоя на домен: "traefik" или "cloudflare"
	DeploymentProxy string `json:"deployment_proxy"`

	// Email для Let's Encrypt / ACME
	DeployEmail string `json:"deploy_email"`

	// Имена сервисов для деплоя (должны совпадать с именами в docker-compose.yml)
	DeployServiceBackend string `json:"deploy_service_backend"`
	DeployServiceFrontend string `json:"deploy_service_frontend"`

	// Порты сервисов внутри docker-compose для маршрутизации через Traefik/Cloudflare
	DeployServiceBackendPort int `json:"deploy_service_backend_port"`
	DeployServiceFrontendPort int `json:"deploy_service_frontend_port"`
}

// DefaultConfig возвращает конфигурацию по умолчанию
func DefaultConfig() *AppConfig {
	return &AppConfig{
		ProjectPath:                   "",
		WslDistro:                     "Ubuntu-24.04",
		CdPort:                        50051,
		CdNamespace:                   "default",
		ScriptsPath:                   "scripts/containerd",
		DBVolumeName:                  "soul-dialogue-postgres-data",
		SystemdService:                "containerd",
		NerdctlPath:                   "",
		LogTail:                       100,
		WslCacheTTL:                   2,
		ContainersCacheTTL:            3,
		ImagesCacheTTL:                5,
		VolumesCacheTTL:               5,
		AutoRefreshInterval:           3,
		EconomyMode:                   false,
		SquashLayers:                  false,
		Compression:                   "zstd",
		CompressionLevel:              6,
		DefaultCPU:                    "",
		DefaultMemory:                 "",
		MaxParallelism:                0,
		ContainerOperationConcurrency: 4,
		MaxWSLCacheSize:               10 * 1024 * 1024,
		WSLCacheCleanupAt:             25,
		BuildkitCacheTTL:              24,
		BuildkitMaxSize:               "5g",
		DeploymentProxy:               "traefik",
		DeployEmail:                   "",
		DeployServiceBackend:          "backend",
		DeployServiceFrontend:         "frontend",
		DeployServiceBackendPort:      8000,
		DeployServiceFrontendPort:     80,
	}
}

// LoadConfig загружает конфигурацию из файла
func LoadConfig() (*AppConfig, error) {
	config := DefaultConfig()

	path := ConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		// Если файла нет, возвращаем конфиг по умолчанию
		return config, nil
	}

	if err := json.Unmarshal(data, config); err != nil {
		return nil, err
	}

	// Устанавливаем значения по умолчанию для пустых полей
	if config.WslDistro == "" {
		config.WslDistro = DefaultConfig().WslDistro
	}
	if config.CdPort == 0 {
		config.CdPort = DefaultConfig().CdPort
	}
	if config.CdNamespace == "" {
		config.CdNamespace = DefaultConfig().CdNamespace
	}
	if config.ScriptsPath == "" {
		config.ScriptsPath = DefaultConfig().ScriptsPath
	}
	if config.DBVolumeName == "" {
		config.DBVolumeName = DefaultConfig().DBVolumeName
	}
	if config.SystemdService == "" {
		config.SystemdService = DefaultConfig().SystemdService
	}
	if config.LogTail == 0 {
		config.LogTail = DefaultConfig().LogTail
	}
	if config.WslCacheTTL == 0 {
		config.WslCacheTTL = DefaultConfig().WslCacheTTL
	}
	if config.ContainerOperationConcurrency <= 0 {
		config.ContainerOperationConcurrency = DefaultConfig().ContainerOperationConcurrency
	}

	return config, nil
}

// SaveConfig сохраняет конфигурацию в файл
func SaveConfig(config *AppConfig) error {
	path := ConfigPath()
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	// Создаём директорию, если не существует
	if dir := filepath.Dir(path); dir != "" {
		os.MkdirAll(dir, 0755)
	}

	return os.WriteFile(path, data, 0644)
}

// ToWSLPath преобразует Windows-путь в формат, понятный WSL.
// Пример: C:\Users\Name\Project -> /mnt/c/Users/Name/Project
func ToWSLPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}

	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, "\\\\wsl$") {
		return filepath.ToSlash(path)
	}

	if strings.HasPrefix(path, "~") {
		return filepath.ToSlash(path)
	}

	path = strings.ReplaceAll(path, "\\", "/")
	if len(path) >= 2 && path[1] == ':' {
		drive := strings.ToLower(path[:1])
		rest := strings.TrimPrefix(path[2:], "/")
		rest = strings.TrimPrefix(rest, "")
		if rest == "" {
			return "/mnt/" + drive
		}
		return "/mnt/" + drive + "/" + rest
	}

	if strings.HasPrefix(path, "//") {
		return "/" + strings.TrimPrefix(path, "/")
	}

	return filepath.ToSlash(path)
}

// GetProjectPath возвращает путь к проекту из конфига или пытается обнаружить
func GetProjectPathWSL() string {
	return ToWSLPath(GetProjectPath())
}

func GetProjectPath() string {
	configCache.RLock()
	if configCache.config != nil && configCache.config.ProjectPath != "" {
		path := configCache.config.ProjectPath
		configCache.RUnlock()
		return path
	}
	configCache.RUnlock()

	// Загружаем конфиг
	config, err := LoadConfig()
	if err == nil && config.ProjectPath != "" {
		configCache.Lock()
		configCache.config = config
		configCache.Unlock()
		// Инициализируем атомарную переменную при первой загрузке
		wslCacheTTL.Store(int64(config.WslCacheTTL))
		return config.ProjectPath
	}

	// Пробуем обнаружить
	detected := DetectProjectPath()
	if detected != "" {
		config.ProjectPath = detected
		SaveConfig(config)
		configCache.Lock()
		configCache.config = config
		configCache.Unlock()
		wslCacheTTL.Store(int64(config.WslCacheTTL))
		return detected
	}

	return ""
}

// SetProjectPath устанавливает путь к проекту и сохраняет
func SetProjectPath(path string) error {
	config, err := LoadConfig()
	if err != nil {
		return err
	}

	config.ProjectPath = path
	return SaveConfig(config)
}

// GetWslDistro возвращает имя WSL-дистрибутива из конфига
func GetWslDistro() string {
	configCache.RLock()
	if configCache.config != nil && configCache.config.WslDistro != "" {
		distro := configCache.config.WslDistro
		configCache.RUnlock()
		return distro
	}
	configCache.RUnlock()

	config, err := LoadConfig()
	if err == nil && config.WslDistro != "" {
		configCache.Lock()
		configCache.config = config
		configCache.Unlock()
		return config.WslDistro
	}

	configCache.Lock()
	if configCache.config == nil {
		configCache.config = DefaultConfig()
	}
	configCache.Unlock()

	return DefaultConfig().WslDistro
}

// GetCdPort возвращает порт gRPC-прокси из конфига
func GetCdPort() int {
	configCache.RLock()
	if configCache.config != nil {
		port := configCache.config.CdPort
		configCache.RUnlock()
		return port
	}
	configCache.RUnlock()

	return DefaultConfig().CdPort
}

// GetCdNamespace возвращает namespace containerd из конфига
func GetCdNamespace() string {
	configCache.RLock()
	if configCache.config != nil {
		ns := configCache.config.CdNamespace
		configCache.RUnlock()
		return ns
	}
	configCache.RUnlock()

	return DefaultConfig().CdNamespace
}

// GetScriptsPath возвращает путь к скриптам из конфига
func GetScriptsPath() string {
	configCache.RLock()
	if configCache.config != nil {
		path := configCache.config.ScriptsPath
		configCache.RUnlock()
		return path
	}
	configCache.RUnlock()

	return DefaultConfig().ScriptsPath
}

// GetDBVolumeName возвращает имя тома БД из конфига
func GetDBVolumeName() string {
	configCache.RLock()
	if configCache.config != nil {
		name := configCache.config.DBVolumeName
		configCache.RUnlock()
		return name
	}
	configCache.RUnlock()

	return DefaultConfig().DBVolumeName
}

// GetSystemdService возвращает имя systemd-сервиса из конфига
func GetSystemdService() string {
	configCache.RLock()
	if configCache.config != nil {
		svc := configCache.config.SystemdService
		configCache.RUnlock()
		return svc
	}
	configCache.RUnlock()

	return DefaultConfig().SystemdService
}

// GetNerdctlPath возвращает путь к nerdctl (пустая строка = в PATH)
func GetNerdctlPath() string {
	configCache.RLock()
	if configCache.config != nil {
		path := configCache.config.NerdctlPath
		configCache.RUnlock()
		return path
	}
	configCache.RUnlock()

	return DefaultConfig().NerdctlPath
}

// GetLogTail возвращает количество строк логов из конфига
func GetLogTail() int {
	configCache.RLock()
	if configCache.config != nil {
		tail := configCache.config.LogTail
		configCache.RUnlock()
		return tail
	}
	configCache.RUnlock()

	return DefaultConfig().LogTail
}

// GetWslCacheTTL возвращает TTL кэша WSL (из атомарной переменной)
func GetWslCacheTTL() int {
	return int(wslCacheTTL.Load())
}

// GetAutoRefreshInterval возвращает интервал автообновления из конфига
func GetAutoRefreshInterval() int {
	configCache.RLock()
	if configCache.config != nil {
		interval := configCache.config.AutoRefreshInterval
		configCache.RUnlock()
		return interval
	}
	configCache.RUnlock()

	return DefaultConfig().AutoRefreshInterval
}

// DetectProjectPath пытается найти docker-compose.yml в стандартных местах.
// Использует filepath.WalkDir для обхода файловой системы — работает быстро,
// поддерживает кириллицу, пробелы и любые UTF-8 символы.
func DetectProjectPath() string {
	// Максимальная глубина рекурсии
	const maxDepth = 4

	// 1. Текущая рабочая директория
	if wd, err := os.Getwd(); err == nil {
		if found := walkForCompose(filepath.Clean(wd), maxDepth); found != "" {
			return found
		}
	}

	// 2. Домашняя директория пользователя
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	// 3. Типичные папки с проектами
	projectDirs := []string{
		filepath.Join(home, "projects"),
		filepath.Join(home, "workspace"),
		filepath.Join(home, "code"),
		filepath.Join(home, "src"),
		filepath.Join(home, "Documents"),
		filepath.Join(home, "Desktop"),
		filepath.Join(home, "OneDrive", "Рабочий стол"),
	}

	for _, dir := range projectDirs {
		if _, err := os.Stat(dir); err == nil {
			if found := walkForCompose(filepath.Clean(dir), maxDepth); found != "" {
				return found
			}
		}
	}

	return ""
}

// walkForCompose рекурсивно ищет compose-файлы в директории с ограничением глубины.
// Возвращает путь к директории, где найден ПЕРВЫЙ compose-файл.
// maxDepth — максимальная вложенность ОТ root (не абсолютная глубина).
func walkForCompose(root string, maxDepth int) string {
	root = filepath.Clean(root)
	rootDepth := countPathDepth(root)

	composeFiles := map[string]bool{
		"docker-compose.yml": true,
		"compose.yaml":       true,
	}

	// Чёрный список директорий для пропуска — системные, кэш, зависимости.
	// Это значительно ускоряет сканирование больших папок (C:\Users и т.п.).
	skipDirs := map[string]bool{
		// VCS
		".git": true, ".svn": true, ".hg": true, ".bzr": true,
		// Зависимости
		"node_modules": true, "__pycache__": true, ".venv": true, "venv": true,
		".gradle": true, ".m2": true, "vendor": true,
		// IDE и редакторы
		".vscode": true, ".idea": true, ".eclipse": true,
		// Системные папки Windows
		"System Volume Information": true, "Recovery": true,
		"$RECYCLE.BIN": true, "recycled": true,
		// Системные папки Linux/WSL
		"proc": true, "sys": true, "dev": true, "tmp": true,
		// Кэш и временные
		".cache": true, "cache": true, ".npm": true,
	}

	var found string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // пропускаем ошибки доступа
		}

		// Ограничиваем глубину: rootDepth + maxDepth
		if countPathDepth(path) > rootDepth+maxDepth {
			return filepath.SkipDir
		}

		// Пропускаем системные и скрытые директории
		if d.IsDir() {
			base := d.Name()
			if skipDirs[base] {
				return filepath.SkipDir
			}
			// Пропускаем все скрытые папки (начинаются с .)
			if len(base) > 0 && base[0] == '.' {
				return filepath.SkipDir
			}
			return nil
		}

		// Проверяем имя файла
		if composeFiles[d.Name()] {
			found = filepath.Dir(path)
			return filepath.SkipAll // полная остановка обхода
		}

		return nil
	})

	// SkipAll возвращает nil, так что проверяем только результат
	return found
}

// countPathDepth считает количество разделителей в пути.
// "C:\Users\Name\project" → 3, "C:\Users" → 1, "C:\" → 1.
// Это позволяет корректно считать вложенность ОТ root, а не абсолютную глубину.
func countPathDepth(path string) int {
	path = filepath.Clean(path)
	return strings.Count(path, string(filepath.Separator))
}

// BuildComposeCommand собирает команду nerdctl compose для проекта.
// Использует --project-directory вместо cd + кавычек — надёжно работает
// с пробелами, кириллицей, апострофами и любыми другими символами.
func BuildComposeCommand(args ...string) string {
	projectPath := GetProjectPathWSL()
	if projectPath == "" {
		return ""
	}

	cmd := "nerdctl compose --project-directory " + strconv.Quote(projectPath) + " "
	cmd += strings.Join(args, " ")

	// Добавляем флаги сборки, если это build-команда
	if len(args) > 0 && args[0] == "build" {
		buildFlags := BuildFlags()
		if buildFlags != "" {
			cmd += " " + buildFlags
		}
	}

	return cmd
}

// BuildFlags возвращает строку флагов для nerdctl build
func BuildFlags() string {
	var flags []string

	config := GetConfig()
	if config != nil {
		// --squash объединяет все слои в один
		if config.SquashLayers {
			flags = append(flags, "--squash")
		}

		// --compression=zstd (более эффективный, чем gzip)
		if config.Compression != "" && config.Compression != "none" {
			flags = append(flags, "--compression="+config.Compression)

			// --compression-level (1-9)
			if config.CompressionLevel > 0 && config.CompressionLevel <= 9 {
				flags = append(flags, "--compression-level="+fmt.Sprintf("%d", config.CompressionLevel))
			}
		}
	}

	// --parallel включает параллельную сборку сервисов
	flags = append(flags, "--parallel")

	// --max-parallelism ограничивает число параллельных сборок
	if n := GetMaxParallelism(); n > 0 {
		flags = append(flags, fmt.Sprintf("--max-parallelism=%d", n))
	}

	return strings.Join(flags, " ")
}

// GetConfig возвращает текущую конфигурацию из кэша
func GetConfig() *AppConfig {
	configCache.RLock()
	defer configCache.RUnlock()
	return configCache.config
}

// GetDefaultCPU возвращает лимит CPU по умолчанию (пустая строка = без лимита)
func GetDefaultCPU() string {
	configCache.RLock()
	if configCache.config != nil {
		cpu := configCache.config.DefaultCPU
		configCache.RUnlock()
		return cpu
	}
	configCache.RUnlock()
	return DefaultConfig().DefaultCPU
}

// GetDefaultMemory возвращает лимит памяти по умолчанию (пустая строка = без лимита)
func GetDefaultMemory() string {
	configCache.RLock()
	if configCache.config != nil {
		mem := configCache.config.DefaultMemory
		configCache.RUnlock()
		return mem
	}
	configCache.RUnlock()
	return DefaultConfig().DefaultMemory
}

// GetMaxParallelism возвращает максимальное число параллельных сборок (0 = без ограничений)
func GetMaxParallelism() int {
	configCache.RLock()
	if configCache.config != nil {
		n := configCache.config.MaxParallelism
		configCache.RUnlock()
		return n
	}
	configCache.RUnlock()
	return DefaultConfig().MaxParallelism
}

// GetContainerOperationConcurrency возвращает лимит параллельных операций с контейнерами.
func GetContainerOperationConcurrency() int {
	configCache.RLock()
	if configCache.config != nil {
		concurrency := configCache.config.ContainerOperationConcurrency
		configCache.RUnlock()
		if concurrency > 0 {
			return concurrency
		}
		return DefaultConfig().ContainerOperationConcurrency
	}
	configCache.RUnlock()
	return DefaultConfig().ContainerOperationConcurrency
}

// GetBuildkitCacheTTL возвращает TTL кэша BuildKit в часах (0 = отключено)
func GetBuildkitCacheTTL() int {
	configCache.RLock()
	if configCache.config != nil {
		ttl := configCache.config.BuildkitCacheTTL
		configCache.RUnlock()
		return ttl
	}
	configCache.RUnlock()
	return DefaultConfig().BuildkitCacheTTL
}

// GetBuildkitMaxSize возвращает максимальный размер кэша BuildKit
func GetBuildkitMaxSize() string {
	configCache.RLock()
	if configCache.config != nil {
		size := configCache.config.BuildkitMaxSize
		configCache.RUnlock()
		return size
	}
	configCache.RUnlock()
	return DefaultConfig().BuildkitMaxSize
}

// GetDeploymentProxy возвращает выбранный прокси для деплоя
func GetDeploymentProxy() string {
	configCache.RLock()
	if configCache.config != nil {
		proxy := configCache.config.DeploymentProxy
		configCache.RUnlock()
		if proxy == "" || (proxy != "traefik" && proxy != "cloudflare") {
			return "traefik"
		}
		return proxy
	}
	configCache.RUnlock()
	return "traefik"
}

// SetDeploymentProxy устанавливает прокси для деплоя и сохраняет
func SetDeploymentProxy(proxy string) error {
	config, err := LoadConfig()
	if err != nil {
		return err
	}
	if proxy != "traefik" && proxy != "cloudflare" {
		proxy = "traefik"
	}
	config.DeploymentProxy = proxy
	return SaveConfig(config)
}

// GetDeployServiceBackend возвращает имя сервиса backend для деплоя
func GetDeployServiceBackend() string {
	configCache.RLock()
	if configCache.config != nil {
		svc := configCache.config.DeployServiceBackend
		configCache.RUnlock()
		if svc == "" {
			return "backend"
		}
		return svc
	}
	configCache.RUnlock()
	return "backend"
}

// GetDeployServiceBackendPort возвращает порт backend для деплоя
func GetDeployServiceBackendPort() int {
	configCache.RLock()
	if configCache.config != nil {
		port := configCache.config.DeployServiceBackendPort
		configCache.RUnlock()
		if port <= 0 {
			return 8000
		}
		return port
	}
	configCache.RUnlock()
	return 8000
}

// GetDeployServiceFrontend возвращает имя сервиса frontend для деплоя
func GetDeployServiceFrontend() string {
	configCache.RLock()
	if configCache.config != nil {
		svc := configCache.config.DeployServiceFrontend
		configCache.RUnlock()
		if svc == "" {
			return "frontend"
		}
		return svc
	}
	configCache.RUnlock()
	return "frontend"
}

// GetDeployServiceFrontendPort возвращает порт frontend для деплоя
func GetDeployServiceFrontendPort() int {
	configCache.RLock()
	if configCache.config != nil {
		port := configCache.config.DeployServiceFrontendPort
		configCache.RUnlock()
		if port <= 0 {
			return 80
		}
		return port
	}
	configCache.RUnlock()
	return 80
}

// InitConfigCache инициализирует кэш конфигурации
func InitConfigCache(config *AppConfig) {
	configCache.Lock()
	configCache.config = config
	configCache.Unlock()

	// Применяем TTL из конфига к глобальным кэшам
	ApplyConfigToCaches(config)
}

// ApplyConfigToCaches применяет настройки конфига к глобальным кэшам
func ApplyConfigToCaches(config *AppConfig) {
	if config == nil {
		return
	}

	// Обновляем TTL кэша WSL
	wslCacheTTL.Store(int64(config.WslCacheTTL))

	if config.MaxWSLCacheSize > 0 || config.WSLCacheCleanupAt > 0 {
		wslCache.Lock()
		if config.MaxWSLCacheSize > 0 {
			wslCache.maxSize = config.MaxWSLCacheSize
		}
		if config.WSLCacheCleanupAt > 0 {
			wslCache.cleanupAt = config.WSLCacheCleanupAt
		}
		wslCache.Unlock()
	}

	// Примечание: остальные кэши (containers, images, volumes)
	// используют фиксированные TTL для стабильности.
	// Конфигурируемые значения можно добавить при необходимости.
}

// configCache кэширует загруженную конфигурацию
var configCache = struct {
	sync.RWMutex
	config *AppConfig
}{
	config: nil,
}

// wslCacheTTL атомарно хранит TTL кэша WSL из конфига
var wslCacheTTL atomic.Int64

func init() {
	// Устанавливаем значение по умолчанию
	wslCacheTTL.Store(2)
}
