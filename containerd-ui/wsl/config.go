package wsl

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

// ConfigPath возвращает путь к файлу конфигурации в %APPDATA%.
func ConfigPath() string {
	appData, err := os.UserConfigDir()
	if err == nil {
		appDir := filepath.Join(appData, "ContainerdUI")
		os.MkdirAll(appDir, 0755)
		return filepath.Join(appDir, "config.json")
	}
	if home, err := os.UserHomeDir(); err == nil {
		appDir := filepath.Join(home, ".containerd-ui")
		os.MkdirAll(appDir, 0755)
		return filepath.Join(appDir, "config.json")
	}
	return "config.json"
}

type ProjectInfo struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	Added string `json:"added"`
}

func (p *ProjectInfo) NameWithFallback() string {
	if p.Name != "" {
		return p.Name
	}
	_, name := filepath.Split(p.Path)
	if name != "" {
		return name
	}
	return p.Path
}

type AppConfig struct {
	Language                  string        `json:"language"`
	Projects                  []ProjectInfo `json:"projects"`
	ActiveProjectPath         string        `json:"active_project_path"`
	ProjectPath               string        `json:"project_path"`
	WslDistro                 string        `json:"wsl_distro"`
	CdPort                    int           `json:"cd_port"`
	CdNamespace               string        `json:"cd_namespace"`
	ScriptsPath               string        `json:"scripts_path"`
	DBVolumeName              string        `json:"db_volume_name"`
	SystemdService            string        `json:"systemd_service"`
	NerdctlPath               string        `json:"nerdctl_path"`
	LogTail                   int           `json:"log_tail"`
	WslCacheTTL               int           `json:"wsl_cache_ttl"`
	ContainersCacheTTL        int           `json:"containers_cache_ttl"`
	ImagesCacheTTL            int           `json:"images_cache_ttl"`
	VolumesCacheTTL           int           `json:"volumes_cache_ttl"`
	AutoRefreshInterval       int           `json:"auto_refresh_interval"`
	EconomyMode               bool          `json:"economy_mode"`
	IdleDaemonStopMinutes     int           `json:"idle_daemon_stop_minutes"`
	SquashLayers              bool          `json:"squash_layers"`
	Compression               string        `json:"compression"`
	CompressionLevel          int           `json:"compression_level"`
	MaxWSLCacheSize           int64         `json:"max_wsl_cache_size"`
	WSLCacheCleanupAt         int           `json:"wsl_cache_cleanup_at"`
	CacheContainer            int           `json:"cache_container"`
	CacheImage                int           `json:"cache_image"`
	CacheVolume               int           `json:"cache_volume"`
	CacheStats                int           `json:"cache_stats"`
	CacheContainerStatus      int           `json:"cache_container_status"`
	CacheSplitImage           int           `json:"cache_split_image"`
	CacheHumanSize            int           `json:"cache_human_size"`
	MaxCacheEntries           int           `json:"max_cache_entries"`
	RetryInitialDelay         int           `json:"retry_initial_delay"`
	RetryMaxDelay             int           `json:"retry_max_delay"`
	RetryMultiplier           int           `json:"retry_multiplier"`
	RetryMaxAttempts          int           `json:"retry_max_attempts"`
	DefaultCPU                string        `json:"default_cpu_limit"`
	DefaultMemory             string        `json:"default_memory_limit"`
	MaxParallelism            int           `json:"max_parallelism"`
	ContainerOperationConcurrency int      `json:"container_operation_concurrency"`
	BuildkitCacheTTL          int           `json:"buildkit_cache_ttl"`
	BuildkitMaxSize           string        `json:"buildkit_max_size"`
	DeploymentProxy           string        `json:"deployment_proxy"`
	DeployNetwork             string        `json:"deploy_network"`
	DeployEmail               string        `json:"deploy_email"`
	DeployServiceBackend      string        `json:"deploy_service_backend"`
	DeployServiceFrontend     string        `json:"deploy_service_frontend"`
	DeployServiceBackendPort  int           `json:"deploy_service_backend_port"`
	DeployServiceFrontendPort int           `json:"deploy_service_frontend_port"`
}

func DefaultConfig() *AppConfig {
	return &AppConfig{
		Language:                      "ru",
		Projects:                      nil,
		ActiveProjectPath:             "",
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
		IdleDaemonStopMinutes:         2,
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
		DeployNetwork:                 "soul-dialogue",
		DeployEmail:                   "",
		DeployServiceBackend:          "backend",
		DeployServiceFrontend:         "frontend",
		DeployServiceBackendPort:      8000,
		DeployServiceFrontendPort:     80,
	}
}

func LoadConfig() (*AppConfig, error) {
	config := DefaultConfig()
	path := ConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return config, nil
	}
	if err := json.Unmarshal(data, config); err != nil {
		return nil, err
	}
	EnsureActiveProject(config)

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
	if config.IdleDaemonStopMinutes <= 0 {
		config.IdleDaemonStopMinutes = DefaultConfig().IdleDaemonStopMinutes
	}
	if config.ContainerOperationConcurrency <= 0 {
		config.ContainerOperationConcurrency = DefaultConfig().ContainerOperationConcurrency
	}
	if config.DeployNetwork == "" {
		config.DeployNetwork = DefaultConfig().DeployNetwork
	}
	if config.Language != "en" {
		config.Language = "ru"
	}
	return config, nil
}

func SaveConfig(config *AppConfig) error {
	path := ConfigPath()
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "" {
		os.MkdirAll(dir, 0755)
	}
	return os.WriteFile(path, data, 0644)
}

func migrateLegacyProjectPath(config *AppConfig) {
	if config.ProjectPath != "" {
		exists := false
		for _, p := range config.Projects {
			if p.Path == config.ProjectPath {
				exists = true
				break
			}
		}
		if !exists {
			config.Projects = append(config.Projects, ProjectInfo{
				Path:  config.ProjectPath,
				Name:  "",
				Added: "",
			})
		}
		if config.ActiveProjectPath == "" {
			config.ActiveProjectPath = config.ProjectPath
		}
		config.ProjectPath = ""
	}
}

func EnsureActiveProject(config *AppConfig) {
	migrateLegacyProjectPath(config)
	if len(config.Projects) == 0 {
		config.ActiveProjectPath = ""
		return
	}
	found := false
	for _, p := range config.Projects {
		if p.Path == config.ActiveProjectPath {
			found = true
			break
		}
	}
	if !found {
		config.ActiveProjectPath = config.Projects[0].Path
	}
}

func GetProjects() []ProjectInfo {
	configCache.RLock()
	config := configCache.config
	configCache.RUnlock()
	if config == nil {
		return nil
	}
	EnsureActiveProject(config)
	result := make([]ProjectInfo, len(config.Projects))
	copy(result, config.Projects)
	return result
}

func GetActiveProjectPath() string {
	configCache.RLock()
	path := configCache.config.ActiveProjectPath
	configCache.RUnlock()
	return path
}

func AddProject(path string) error {
	path = filepath.Clean(path)
	if err := ValidatePath(path); err != nil {
		return fmt.Errorf("недопустимый путь к проекту: %w", err)
	}
	config, err := LoadConfig()
	if err != nil {
		return err
	}
	EnsureActiveProject(config)
	for _, p := range config.Projects {
		if p.Path == path {
			return fmt.Errorf("проект с таким путём уже существует: %s", path)
		}
	}
	config.Projects = append(config.Projects, ProjectInfo{
		Path:  path,
		Name:  "",
		Added: "",
	})
	config.ActiveProjectPath = path
	return SaveConfig(config)
}

func RemoveProject(path string) error {
	config, err := LoadConfig()
	if err != nil {
		return err
	}
	EnsureActiveProject(config)
	found := false
	var remaining []ProjectInfo
	for _, p := range config.Projects {
		if p.Path == path {
			found = true
			continue
		}
		remaining = append(remaining, p)
	}
	if !found {
		return fmt.Errorf("проект не найден: %s", path)
	}
	config.Projects = remaining
	if config.ActiveProjectPath == path {
		if len(config.Projects) > 0 {
			config.ActiveProjectPath = config.Projects[0].Path
		} else {
			config.ActiveProjectPath = ""
		}
	}
	return SaveConfig(config)
}

func SetActiveProject(path string) error {
	// Если путь пустой — просто сбрасываем активный проект
	if path == "" {
		config, err := LoadConfig()
		if err != nil {
			return err
		}
		config.ActiveProjectPath = ""
		return SaveConfig(config)
	}

	config, err := LoadConfig()
	if err != nil {
		return err
	}
	EnsureActiveProject(config)
	found := false
	for _, p := range config.Projects {
		if p.Path == path {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("проект не найден в списке: %s", path)
	}
	config.ActiveProjectPath = path
	return SaveConfig(config)
}

func ActiveProject() *ProjectInfo {
	configCache.RLock()
	config := configCache.config
	configCache.RUnlock()
	if config == nil || len(config.Projects) == 0 {
		return nil
	}
	EnsureActiveProject(config)
	for _, p := range config.Projects {
		if p.Path == config.ActiveProjectPath {
			return &p
		}
	}
	return nil
}

func RenameProject(oldPath, newName string) error {
	config, err := LoadConfig()
	if err != nil {
		return err
	}
	EnsureActiveProject(config)
	for i, p := range config.Projects {
		if p.Path == oldPath {
			config.Projects[i].Name = newName
			return SaveConfig(config)
		}
	}
	return fmt.Errorf("проект не найден: %s", oldPath)
}

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

// GetProjectPath возвращает путь к активному проекту с fallback-логикой.
func GetProjectPath() string {
	var config *AppConfig

	configCache.RLock()
	if configCache.config != nil && configCache.config.ActiveProjectPath != "" {
		activePath := configCache.config.ActiveProjectPath
		found := false
		for _, p := range configCache.config.Projects {
			if p.Path == activePath {
				found = true
				break
			}
		}
		if found {
			configCache.RUnlock()
			return activePath
		}
		// Активный проект не найден — сбрасываем и перезагружаем
		configCache.RUnlock()
		config, err := LoadConfig()
		if err == nil {
			config.ActiveProjectPath = ""
			SaveConfig(config)
			configCache.Lock()
			configCache.config = config
			configCache.Unlock()
		}
	} else {
		configCache.RUnlock()
	}

	config, err := LoadConfig()
	if err != nil {
		config = nil
	}
	if config != nil {
		EnsureActiveProject(config)
		configCache.Lock()
		configCache.config = config
		configCache.Unlock()
		if config.ActiveProjectPath != "" {
			return config.ActiveProjectPath
		}
		if len(config.Projects) > 0 {
			return config.Projects[0].Path
		}
	}
	if config != nil && config.ProjectPath != "" {
		return config.ProjectPath
	}
	detected := DetectProjectPath()
	if detected != "" {
		if config == nil {
			config, _ = LoadConfig()
		}
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

func SetProjectPath(path string) error {
	config, err := LoadConfig()
	if err != nil {
		return err
	}
	EnsureActiveProject(config)
	exists := false
	for _, p := range config.Projects {
		if p.Path == path {
			exists = true
			break
		}
	}
	if !exists {
		config.Projects = append(config.Projects, ProjectInfo{
			Path:  path,
			Name:  "",
			Added: "",
		})
	}
	config.ActiveProjectPath = path
	config.ProjectPath = ""
	return SaveConfig(config)
}

func GetProjectPathWSL() string {
	return ToWSLPath(GetProjectPath())
}

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

func GetWslCacheTTL() int {
	return int(wslCacheTTL.Load())
}

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

// DetectProjectPath ищет docker-compose.yml в стандартных местах.
func DetectProjectPath() string {
	const maxDepth = 4
	if wd, err := os.Getwd(); err == nil {
		if found := walkForCompose(filepath.Clean(wd), maxDepth); found != "" {
			return found
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
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

func walkForCompose(root string, maxDepth int) string {
	root = filepath.Clean(root)
	rootDepth := countPathDepth(root)
	composeFiles := map[string]bool{
		"docker-compose.yml": true,
		"compose.yaml":       true,
	}
	skipDirs := map[string]bool{
		".git": true, ".svn": true, ".hg": true, ".bzr": true,
		"node_modules": true, "__pycache__": true, ".venv": true, "venv": true,
		".gradle": true, ".m2": true, "vendor": true,
		".vscode": true, ".idea": true, ".eclipse": true,
		"System Volume Information": true, "Recovery": true,
		"$RECYCLE.BIN": true, "recycled": true,
		"proc": true, "sys": true, "dev": true, "tmp": true,
		".cache": true, "cache": true, ".npm": true,
	}
	var found string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if countPathDepth(path) > rootDepth+maxDepth {
			return filepath.SkipDir
		}
		if d.IsDir() {
			base := d.Name()
			if skipDirs[base] {
				return filepath.SkipDir
			}
			if len(base) > 0 && base[0] == '.' {
				return filepath.SkipDir
			}
			return nil
		}
		if composeFiles[d.Name()] {
			found = filepath.Dir(path)
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

func countPathDepth(path string) int {
	path = filepath.Clean(path)
	return strings.Count(path, string(filepath.Separator))
}

// BuildComposeCommand формирует команду nerdctl compose с --project-directory.
func BuildComposeCommand(args ...string) string {
	projectPath := GetProjectPathWSL()
	if projectPath == "" {
		return ""
	}
	cmd := "nerdctl compose --project-directory " + shellQuote(projectPath) + " "
	cmd += strings.Join(args, " ")
	if len(args) > 0 && args[0] == "build" {
		buildFlags := BuildFlags()
		if buildFlags != "" {
			cmd += " " + buildFlags
		}
	}
	return cmd
}

func BuildFlags() string {
	var flags []string
	config := GetConfig()
	if config != nil {
		if config.SquashLayers {
			flags = append(flags, "--squash")
		}
		if config.Compression != "" && config.Compression != "none" {
			flags = append(flags, "--compression="+config.Compression)
			if config.CompressionLevel > 0 && config.CompressionLevel <= 9 {
				flags = append(flags, "--compression-level="+fmt.Sprintf("%d", config.CompressionLevel))
			}
		}
	}
	flags = append(flags, "--parallel")
	if n := GetMaxParallelism(); n > 0 {
		flags = append(flags, fmt.Sprintf("--max-parallelism=%d", n))
	}
	return strings.Join(flags, " ")
}

func GetConfig() *AppConfig {
	configCache.RLock()
	defer configCache.RUnlock()
	return configCache.config
}

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

func GetDeployNetwork() string {
	configCache.RLock()
	if configCache.config != nil {
		network := strings.TrimSpace(configCache.config.DeployNetwork)
		configCache.RUnlock()
		if network != "" {
			return network
		}
		return DefaultConfig().DeployNetwork
	}
	configCache.RUnlock()
	return DefaultConfig().DeployNetwork
}

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

func InitConfigCache(config *AppConfig) {
	configCache.Lock()
	configCache.config = config
	configCache.Unlock()
	ApplyConfigToCaches(config)
}

// ApplyConfigToCaches применяет настройки кэшей из конфига.
func ApplyConfigToCaches(config *AppConfig) {
	if config == nil {
		return
	}
	wslCacheTTL.Store(int64(config.WslCacheTTL))
	if config.IdleDaemonStopMinutes > 0 {
		SetIdleDaemonThresholdForRuntime(config.IdleDaemonStopMinutes)
	}
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
}

var configCache = struct {
	sync.RWMutex
	config *AppConfig
}{
	config: nil,
}

var wslCacheTTL atomic.Int64

func init() {
	wslCacheTTL.Store(2)
}