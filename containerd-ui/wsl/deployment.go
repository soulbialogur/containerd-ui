package wsl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"
)

var domainPattern = regexp.MustCompile(`^(?i:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)(?:\.(?i:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?))+$`)

const (
	dnsValidationCacheTTL     = 30 * time.Second
	dnsValidationCacheMaxSize = 100
)

var dnsValidationCache = struct {
	sync.RWMutex
	entries map[string]time.Time
}{entries: make(map[string]time.Time)}

func validateRoutePrefix(prefix string) error {
	if prefix == "" || prefix[0] != '/' || strings.ContainsAny(prefix, "` ?#\n\r;&|$()\\") {
		return fmt.Errorf("префикс backend должен начинаться с / и не содержать shell-метасимволы")
	}
	return nil
}

func ValidateACMEEmail(email string) error {
	email = strings.TrimSpace(email)
	if email == "" {
		return fmt.Errorf("email для Let's Encrypt не может быть пустым")
	}
	if strings.ContainsAny(email, "`$&;|()\\\n\r ") {
		return fmt.Errorf("email для Let's Encrypt содержит недопустимые символы")
	}
	if !strings.Contains(email, "@") || strings.Count(email, "@") != 1 {
		return fmt.Errorf("email для Let's Encrypt должен быть в формате user@example.com")
	}
	parts := strings.SplitN(email, "@", 2)
	if parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("email для Let's Encrypt должен содержать имя пользователя и домен")
	}
	return nil
}

func ValidateDomain(domain string) error {
	domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	if domain == "" || strings.ContainsAny(domain, "`$&;|()\\\n\r ") {
		return fmt.Errorf("некорректное имя домена")
	}
	if !domainPattern.MatchString(domain) {
		return fmt.Errorf("некорректное имя домена")
	}

	dnsValidationCache.RLock()
	validatedAt, ok := dnsValidationCache.entries[domain]
	dnsValidationCache.RUnlock()
	if ok && time.Since(validatedAt) < dnsValidationCacheTTL {
		return nil
	}

	hosts, err := net.LookupHost(domain)
	if err != nil || len(hosts) == 0 {
		return fmt.Errorf("для %s не найдена DNS-запись A/AAAA", domain)
	}

	dnsValidationCache.Lock()
	now := time.Now()
	for cachedDomain, cachedAt := range dnsValidationCache.entries {
		if now.Sub(cachedAt) >= dnsValidationCacheTTL {
			delete(dnsValidationCache.entries, cachedDomain)
		}
	}
	for len(dnsValidationCache.entries) >= dnsValidationCacheMaxSize {
		oldestDomain := ""
		var oldestAt time.Time
		for cachedDomain, cachedAt := range dnsValidationCache.entries {
			if oldestDomain == "" || cachedAt.Before(oldestAt) {
				oldestDomain = cachedDomain
				oldestAt = cachedAt
			}
		}
		if oldestDomain == "" {
			break
		}
		delete(dnsValidationCache.entries, oldestDomain)
	}
	dnsValidationCache.entries[domain] = now
	dnsValidationCache.Unlock()
	return nil
}

func DeployDomain(ctx context.Context, domain, backendPrefix string, publishBackend, publishFrontend, https bool) (string, error) {
	domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	if err := ValidateDomain(domain); err != nil {
		return "", err
	}
	if https {
		cfg := GetConfig()
		acmeEmail := ""
		if cfg != nil {
			acmeEmail = strings.TrimSpace(cfg.DeployEmail)
		}
		if acmeEmail == "" {
			acmeEmail = "admin@" + domain
		}
		if err := ValidateACMEEmail(acmeEmail); err != nil {
			return "", fmt.Errorf("для Let's Encrypt нужен корректный email: %w", err)
		}
	}
	if !publishBackend && !publishFrontend {
		return "", fmt.Errorf("выберите хотя бы один сервис для публикации")
	}
	if publishBackend {
		if err := validateRoutePrefix(backendPrefix); err != nil {
			return "", err
		}
	}

	projectPath := GetProjectPath()
	if projectPath == "" {
		return "", fmt.Errorf("путь к проекту не настроен")
	}

	if err := CheckDeploymentPrerequisites(ctx); err != nil {
		return "", err
	}

	// Проверяем порты для Traefik
	proxy := GetDeploymentProxy()
	if proxy == "traefik" {
		port80, port443, err := CheckPorts(ctx)
		if err != nil {
			return "", fmt.Errorf("не удалось проверить порты: %w", err)
		}
		if !port80 || !port443 {
			var busy []string
			if !port80 {
				busy = append(busy, "80")
			}
			if !port443 {
				busy = append(busy, "443")
			}
			return "", fmt.Errorf("порты %s заняты. Освободите их перед деплоем Traefik.\n\n💡 Проверить: кнопка «Проверить порты 80/443»\n💡 Остановить: sudo systemctl stop nginx apache2", strings.Join(busy, ", "))
		}
	}

	if err := ensureDeploymentNetwork(ctx); err != nil {
		return "", err
	}

	var proxyOutput string

	switch proxy {
	case "cloudflare":
		output, err := deployWithCloudflare(ctx, projectPath, domain, backendPrefix, publishBackend, publishFrontend)
		if err != nil {
			return "", fmt.Errorf("ошибка запуска Cloudflare Tunnel: %w\nСервисы не запущены. Исправьте ошибку и попробуйте снова", err)
		}
		proxyOutput = output
	default:
		output, err := deployWithTraefik(ctx, projectPath, domain, backendPrefix, publishBackend, publishFrontend, https)
		if err != nil {
			return "", fmt.Errorf("ошибка запуска Traefik: %w\nСервисы не запущены. Исправьте ошибку и попробуйте снова", err)
		}
		proxyOutput = output
	}

	// Запускаем сервисы только после успешного запуска прокси
	serviceOutput, err := ensureDeploymentServices(ctx, projectPath, publishBackend, publishFrontend)
	if err != nil {
		// Если сервисы не запустились, останавливаем прокси
		_, rollbackErr := RollbackDomain(ctx)
		if rollbackErr != nil {
			return "", fmt.Errorf("ошибка запуска сервисов: %w\n(откат прокси также не удался: %v)", err, rollbackErr)
		}
		return "", fmt.Errorf("ошибка запуска сервисов: %w\nПрокси остановлен для предотвращения недоступности", err)
	}

	return strings.TrimSpace(proxyOutput + "\n" + serviceOutput), nil
}

func parseComposeServiceStatuses(output string) map[string]string {
	statuses := make(map[string]string)
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return statuses
	}

	var entries []map[string]any
	if err := json.Unmarshal([]byte(trimmed), &entries); err != nil {
		var single map[string]any
		if err := json.Unmarshal([]byte(trimmed), &single); err != nil {
			return statuses
		}
		entries = []map[string]any{single}
	}

	for _, item := range entries {
		service, _ := item["Service"].(string)
		if service == "" {
			service, _ = item["Name"].(string)
		}
		if service == "" {
			continue
		}

		status, _ := item["State"].(string)
		if status == "" {
			status, _ = item["Status"].(string)
		}
		if status == "" {
			continue
		}
		statuses[service] = status
	}
	return statuses
}

func proxyComposeProjectDir(projectPath string) string {
	return filepath.Join(projectPath, ".containerd-data")
}

func verifyRunningContainers(ctx context.Context, projectPath, composePath string, serviceNames ...string) error {
	if len(serviceNames) == 0 {
		return nil
	}

	command := fmt.Sprintf(
		"nerdctl compose --project-directory %s -f %s ps --format json",
		strconv.Quote(ToWSLPath(proxyComposeProjectDir(projectPath))),
		shellQuote(ToWSLPath(composePath)),
	)
	output, err := RunWSLWithCancel(ctx, command)
	if err != nil {
		return fmt.Errorf("не удалось получить статус сервисов: %w", err)
	}

	statuses := parseComposeServiceStatuses(output)
	var failed []string
	for _, service := range serviceNames {
		if service == "" {
			continue
		}
		status, ok := statuses[service]
		if !ok {
			failed = append(failed, fmt.Sprintf("%s (не найден)", service))
			continue
		}
		lower := strings.ToLower(status)
		if !strings.HasPrefix(lower, "up") && !strings.HasPrefix(lower, "running") {
			failed = append(failed, fmt.Sprintf("%s (%s)", service, status))
		}
	}

	if len(failed) > 0 {
		return fmt.Errorf("сервисы не запустились: %s", strings.Join(failed, ", "))
	}
	return nil
}

func deployWithTraefik(ctx context.Context, projectPath, domain, backendPrefix string, publishBackend, publishFrontend, https bool) (string, error) {
	traefikDir := filepath.Join(projectPath, ".containerd-data", "traefik")
	if err := os.MkdirAll(traefikDir, 0700); err != nil {
		return "", fmt.Errorf("не удалось создать каталог Traefik: %w", err)
	}
	if err := ensureACMEStorage(filepath.Join(traefikDir, "acme.json")); err != nil {
		return "", fmt.Errorf("не удалось подготовить хранилище сертификатов: %w", err)
	}

	dynamic, err := buildTraefikDynamicConfig(domain, backendPrefix, publishBackend, publishFrontend, https)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(traefikDir, "dynamic.yml"), []byte(dynamic), 0644); err != nil {
		return "", fmt.Errorf("не удалось создать конфигурацию Traefik: %w", err)
	}
	cfg := GetConfig()
	acmeEmail := ""
	if cfg != nil {
		acmeEmail = strings.TrimSpace(cfg.DeployEmail)
	}
	if acmeEmail == "" {
		acmeEmail = "admin@" + domain
	}
	if err := ValidateACMEEmail(acmeEmail); err != nil {
		return "", fmt.Errorf("некорректный email для Let's Encrypt: %w", err)
	}
	composeConfig, err := renderTraefikCompose(acmeEmail)
	if err != nil {
		return "", fmt.Errorf("не удалось сформировать compose-конфигурацию Traefik: %w", err)
	}
	if err := os.WriteFile(filepath.Join(projectPath, ".containerd-data", "traefik-compose.yaml"), []byte(composeConfig), 0644); err != nil {
		return "", fmt.Errorf("не удалось создать compose-конфигурацию Traefik: %w", err)
	}

	composePath := filepath.Join(projectPath, ".containerd-data", "traefik-compose.yaml")
	wslProjectPath := ToWSLPath(proxyComposeProjectDir(projectPath))
	wslComposePath := ToWSLPath(composePath)
	command := fmt.Sprintf(
		"nerdctl compose --project-directory %s -f %s up -d traefik",
		strconv.Quote(wslProjectPath),
		shellQuote(wslComposePath),
	)
	traefikOutput, err := RunWSLWithCancel(ctx, command)
	if err != nil {
		return "", err
	}
	if err := verifyRunningContainers(ctx, projectPath, filepath.Join(projectPath, ".containerd-data", "traefik-compose.yaml"), "traefik"); err != nil {
		return strings.TrimSpace(traefikOutput), err
	}
	return strings.TrimSpace(traefikOutput), nil
}

func deployWithCloudflare(ctx context.Context, projectPath, domain, backendPrefix string, publishBackend, publishFrontend bool) (string, error) {
	cfDir := filepath.Join(projectPath, ".containerd-data", "cloudflare")
	if err := os.MkdirAll(cfDir, 0700); err != nil {
		return "", fmt.Errorf("не удалось создать каталог Cloudflare: %w", err)
	}

	credentialsPath := filepath.Join(cfDir, "credentials.json")
	if _, err := os.Stat(credentialsPath); os.IsNotExist(err) {
		return "", fmt.Errorf("файл credentials.json не найден в %s\nПолучите JSON-токен: Cloudflare Dashboard → Zero Trust → Networks → Tunnels → Save or manage → JSON token", cfDir)
	}

	cfConfig, err := renderCloudflareConfig(domain, backendPrefix, publishBackend, publishFrontend)
	if err != nil {
		return "", fmt.Errorf("не удалось создать конфигурацию Cloudflare: %w", err)
	}
	if err := os.WriteFile(filepath.Join(cfDir, "config.json"), []byte(cfConfig), 0644); err != nil {
		return "", fmt.Errorf("не удалось записать config.json: %w", err)
	}

	composeConfig, err := renderCloudflareCompose()
	if err != nil {
		return "", fmt.Errorf("не удалось сформировать compose-конфигурацию Cloudflare: %w", err)
	}
	if err := os.WriteFile(filepath.Join(projectPath, ".containerd-data", "cloudflare-compose.yaml"), []byte(composeConfig), 0644); err != nil {
		return "", fmt.Errorf("не удалось создать compose-конфигурацию Cloudflare: %w", err)
	}

	composePath := filepath.Join(projectPath, ".containerd-data", "cloudflare-compose.yaml")
	wslProjectPath := ToWSLPath(proxyComposeProjectDir(projectPath))
	wslComposePath := ToWSLPath(composePath)
	command := fmt.Sprintf(
		"nerdctl compose --project-directory %s -f %s up -d cloudflared",
		strconv.Quote(wslProjectPath),
		shellQuote(wslComposePath),
	)
	composeOutput, err := RunWSLWithCancel(ctx, command)
	if err != nil {
		return "", fmt.Errorf("не удалось запустить контейнер cloudflared: %w\nПроверьте JSON-токен и DNS-домен. Команда tunnel create не используется, потому что токен должен быть создан заранее в Cloudflare Dashboard", err)
	}
	if err := verifyRunningContainers(ctx, projectPath, filepath.Join(projectPath, ".containerd-data", "cloudflare-compose.yaml"), "cloudflared"); err != nil {
		return strings.TrimSpace(composeOutput), err
	}

	return strings.TrimSpace(composeOutput), nil
}

// CheckPorts проверяет, свободны ли порты 80 и 443 в WSL.
// Дополнительно делаем TCP dial на localhost:port, потому что ss внутри WSL
// не видит сервисы, которые слушают порты на уровне Windows (например, IIS),
// а Traefik всё равно не сможет стартовать на таких портах.
func CheckPorts(ctx context.Context) (port80, port443 bool, err error) {
	command := "ss -tlnp 2>/dev/null | grep -E ':(80|443) ' || echo 'PORTS_FREE'"
	output, err := RunWSLWithCancel(ctx, command)
	if err != nil {
		return false, false, err
	}

	port80 = true
	port443 = true
	if strings.TrimSpace(output) != "PORTS_FREE" {
		lines := strings.Split(strings.TrimSpace(output), "\n")
		for _, line := range lines {
			if strings.Contains(line, ":80 ") || strings.HasSuffix(line, ":80") {
				port80 = false
			}
			if strings.Contains(line, ":443 ") || strings.HasSuffix(line, ":443") {
				port443 = false
			}
		}
	}

	if port80 && isLocalPortOccupied("127.0.0.1", 80) {
		port80 = false
	}
	if port443 && isLocalPortOccupied("127.0.0.1", 443) {
		port443 = false
	}
	if port80 && isLocalPortOccupied("::1", 80) {
		port80 = false
	}
	if port443 && isLocalPortOccupied("::1", 443) {
		port443 = false
	}

	return port80, port443, nil
}

func isLocalPortOccupied(host string, port int) bool {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, 1*time.Second)
	if err != nil {
		return false
	}
	defer conn.Close()
	return true
}

// ValidateCloudflareCredentials проверяет, что JSON-токен соответствует реальному Account/Tunnel credential.
func ValidateCloudflareCredentials(projectPath string) error {
	credentialsPath := filepath.Join(projectPath, ".containerd-data", "cloudflare", "credentials.json")
	if _, err := os.Stat(credentialsPath); os.IsNotExist(err) {
		return fmt.Errorf("токен Tunnel не найден. Вставьте токен в поле ниже")
	}

	cmd := fmt.Sprintf(
		"cloudflared tunnel list --credentials-file %s --loglevel fatal 2>&1",
		shellQuote(ToWSLPath(credentialsPath)),
	)
	output, err := RunWSLWithCancel(context.Background(), cmd)
	if err != nil {
		_ = os.Remove(credentialsPath)
		msg := strings.TrimSpace(output)
		if msg == "" {
			msg = "не удалось проверить токен Cloudflare Tunnel"
		}
		return fmt.Errorf("токен Cloudflare Tunnel недействителен или истёк: %s", msg)
	}
	return nil
}

// SaveCloudflareToken записывает токен Cloudflare Tunnel в credentials.json
func SaveCloudflareToken(projectPath, tokenJSON string) error {
	cfDir := filepath.Join(projectPath, ".containerd-data", "cloudflare")
	if err := os.MkdirAll(cfDir, 0700); err != nil {
		return fmt.Errorf("не удалось создать каталог Cloudflare: %w", err)
	}

	// Валидируем JSON
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(tokenJSON), &parsed); err != nil {
		return fmt.Errorf("некорректный JSON токен: %w", err)
	}

	credentialsPath := filepath.Join(cfDir, "credentials.json")
	if err := os.WriteFile(credentialsPath, []byte(tokenJSON), 0600); err != nil {
		return fmt.Errorf("не удалось сохранить токен: %w", err)
	}
	if err := ValidateCloudflareCredentials(projectPath); err != nil {
		_ = os.Remove(credentialsPath)
		return err
	}
	return nil
}

// CheckCloudflareToken проверяет наличие credentials.json
func CheckCloudflareToken(projectPath string) error {
	return ValidateCloudflareCredentials(projectPath)
}

// CheckDeploymentPrerequisites проверяет наличие необходимых инструментов в WSL
func CheckDeploymentPrerequisites(ctx context.Context) error {
	var missing []string

	tools := []string{"nerdctl", "containerd", "buildctl"}
	for _, tool := range tools {
		cmd := fmt.Sprintf("which %s >/dev/null 2>&1 && echo OK || echo MISSING", tool)
		output, err := RunWSLWithCancel(ctx, cmd)
		if err != nil || strings.TrimSpace(output) != "OK" {
			missing = append(missing, tool)
		}
	}

	proxy := GetDeploymentProxy()
	if proxy == "cloudflare" {
		cmd := "which cloudflared >/dev/null 2>&1 && echo OK || echo MISSING"
		output, err := RunWSLWithCancel(ctx, cmd)
		if err != nil || strings.TrimSpace(output) != "OK" {
			missing = append(missing, "cloudflared")
		}
	}

	if len(missing) > 0 {
		var msg string
		switch proxy {
		case "cloudflare":
			msg = "В WSL не найдены необходимые инструменты:\n\n"
			for _, m := range missing {
				msg += fmt.Sprintf("  ❌ %s\n", m)
			}
			msg += "\n📦 Установите через:\n"
			msg += "  sudo apt update && sudo apt install -y nerdctl containerd build-essential\n"
			msg += "  cloudflared: https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/\n"
		default:
			msg = "В WSL не найдены необходимые инструменты:\n\n"
			for _, m := range missing {
				msg += fmt.Sprintf("  ❌ %s\n", m)
			}
			msg += "\n📦 Установите через:\n"
			msg += "  sudo apt update && sudo apt install -y nerdctl containerd build-essential\n"
		}
		return fmt.Errorf("%s", msg)
	}

	return nil
}

func ensureACMEStorage(path string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Chmod(path, 0600)
}

func ensureDeploymentNetwork(ctx context.Context) error {
	networkName := GetDeployNetwork()
	command := fmt.Sprintf(
		"nerdctl network inspect %s >/dev/null 2>&1 || nerdctl network create --driver bridge %s",
		shellQuote(networkName),
		shellQuote(networkName),
	)
	if _, err := RunWSLWithCancel(ctx, command); err != nil {
		return fmt.Errorf("не удалось подготовить сеть %s: %w", networkName, err)
	}
	return nil
}

func validateProjectComposeNetworkFromText(text string) error {
	networkName := GetDeployNetwork()

	// Это эвристическая проверка, а не полноценный YAML-парсер.
	// Для этого достаточно стандартных сервисных блоков, но YAML-якоря/многострочные эквиваленты
	// могут не проходить строковый поиск даже при корректной конфигурации.
	if !strings.Contains(text, networkName) {
		return fmt.Errorf("Compose-файл проекта не подключён к сети %q. Добавьте сеть %q и подключите backend/frontend к ней, иначе Traefik/Cloudflare не смогут обращаться к сервисам по имени.", networkName, networkName)
	}
	if !strings.Contains(text, "external: true") || !strings.Contains(text, "name: "+networkName) {
		return fmt.Errorf("Compose-файл проекта должен объявлять сеть %q как external: true с name: %q. В противном случае Traefik/Cloudflare не увидят сервисы в общей сети.", networkName, networkName)
	}
	if !strings.Contains(text, "- "+networkName) {
		return fmt.Errorf("Compose-файл проекта должен подключать backend/frontend к сети %q. Например: networks: [\n  - %s\n]", networkName, networkName)
	}
	return nil
}

func validateProjectComposeNetwork(composePath string) error {
	content, err := os.ReadFile(composePath)
	if err != nil {
		return fmt.Errorf("не удалось прочитать Compose-файл проекта: %w", err)
	}
	return validateProjectComposeNetworkFromText(string(content))
}

func ensureDeploymentServices(ctx context.Context, projectPath string, backend, frontend bool) (string, error) {
	services := make([]string, 0, 2)
	if backend {
		services = append(services, GetDeployServiceBackend())
	}
	if frontend {
		services = append(services, GetDeployServiceFrontend())
	}

	composePath, err := findDeploymentComposeFile(projectPath)
	if err != nil {
		return "", err
	}
	if err := validateProjectComposeNetwork(composePath); err != nil {
		return "", err
	}
	command := fmt.Sprintf(
		"nerdctl compose --project-directory %s -f %s up -d %s",
		strconv.Quote(ToWSLPath(projectPath)),
		shellQuote(ToWSLPath(composePath)),
		strings.Join(services, " "),
	)
	output, err := RunWSLWithCancel(ctx, command)
	if err != nil {
		return output, fmt.Errorf("не удалось запустить выбранные сервисы (%s): %w", strings.Join(services, ", "), err)
	}
	if err := verifyRunningContainers(ctx, projectPath, composePath, services...); err != nil {
		return output, fmt.Errorf("сервисы запущены не в стабильном состоянии (%s): %w", strings.Join(services, ", "), err)
	}
	return output, nil
}

func findDeploymentComposeFile(projectPath string) (string, error) {
	candidates := []string{
		filepath.Join(projectPath, "compose.yaml"),
		filepath.Join(projectPath, "docker-compose.yml"),
		filepath.Join(projectPath, GetScriptsPath(), "compose.yaml"),
	}

	for _, path := range candidates {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}

	return "", fmt.Errorf("compose-файл не найден в корне проекта или в %s", GetScriptsPath())
}

func renderTraefikCompose(acmeEmail string) (string, error) {
	composeTemplate, err := template.New("traefik-compose").Parse(traefikCompose)
	if err != nil {
		return "", err
	}

	var rendered bytes.Buffer
	if err := composeTemplate.Execute(&rendered, struct {
		ACMEEmail string
		Network   string
	}{ACMEEmail: acmeEmail, Network: GetDeployNetwork()}); err != nil {
		return "", err
	}
	return rendered.String(), nil
}

func buildTraefikDynamicConfig(domain, backendPrefix string, backend, frontend, https bool) (string, error) {
	serviceBackend := GetDeployServiceBackend()
	serviceFrontend := GetDeployServiceFrontend()

	entryPoint := "web"
	tls := ""
	if https {
		entryPoint = "websecure"
		tls = "\n    tls:\n      certResolver: letsencrypt"
	}

	var middlewares []string
	if backend && backendPrefix != "/" {
		// Создаём middleware для удаления префикса /api, /backend и т.п.
		// Убираем ведущий слэш для имени middleware
		name := strings.TrimPrefix(backendPrefix, "/")
		if name == "" {
			name = "root"
		}
		middlewareName := "strip" + name
		middlewares = append(middlewares, middlewareName)
	}

	var routes []string
	if backend {
		route := fmt.Sprintf("  backend:\n    rule: Host(`%s`) && PathPrefix(`%s`)\n    service: backend\n    entryPoints:\n      - %s\n    priority: 100%s", domain, backendPrefix, entryPoint, tls)
		if len(middlewares) > 0 {
			route += "\n    middlewares:\n      - " + strings.Join(middlewares, "\n      - ")
		}
		routes = append(routes, route)
	}
	if frontend {
		routes = append(routes, fmt.Sprintf("  frontend:\n    rule: Host(`%s`)\n    service: frontend\n    entryPoints:\n      - %s%s", domain, entryPoint, tls))
	}

	backendPort := GetDeployServiceBackendPort()
	frontendPort := GetDeployServiceFrontendPort()

	var services []string
	if backend {
		services = append(services, fmt.Sprintf("  backend:\n    loadBalancer:\n      servers:\n        - url: http://%s:%d", serviceBackend, backendPort))
	}
	if frontend {
		services = append(services, fmt.Sprintf("  frontend:\n    loadBalancer:\n      servers:\n        - url: http://%s:%d", serviceFrontend, frontendPort))
	}

	var middlewareBlock string
	if len(middlewares) > 0 {
		middlewareBlock = fmt.Sprintf("  middlewares:\n    %s:\n      stripPrefix:\n        prefixes:\n          - \"%s\"\n", middlewares[0], backendPrefix)
	}

	return fmt.Sprintf("http:\n  routers:\n%s\n  services:\n%s%s", strings.Join(routes, "\n"), strings.Join(services, "\n"), middlewareBlock), nil
}

const traefikCompose = `services:
  traefik:
    image: traefik:v3.0
    container_name: soul-dialogue-traefik
    user: "0:0"
    restart: unless-stopped
    command:
      - --providers.file.filename=/etc/traefik/dynamic.yml
      - --providers.file.watch=true
      - --entrypoints.web.address=:80
      - --entrypoints.websecure.address=:443
      - --api.dashboard=false
      - --log.level=INFO
      - --accesslog=true
      - --certificatesresolvers.letsencrypt.acme.email={{ .ACMEEmail }}
      - --certificatesresolvers.letsencrypt.acme.storage=/etc/traefik/acme.json
      - --certificatesresolvers.letsencrypt.acme.httpchallenge.entrypoint=web
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./traefik/dynamic.yml:/etc/traefik/dynamic.yml:ro
      - ./traefik/acme.json:/etc/traefik/acme.json
		networks:
			- {{ .Network }}
networks:
	{{ .Network }}:
    external: true
		name: {{ .Network }}
`

// renderCloudflareConfig генерирует config.json для cloudflared
func renderCloudflareConfig(domain, backendPrefix string, backend, frontend bool) (string, error) {
	configTemplate, err := template.New("cloudflare-config").Parse(cloudflareConfigTemplate)
	if err != nil {
		return "", err
	}

	var rendered bytes.Buffer
	if err := configTemplate.Execute(&rendered, struct {
		Domain            string
		BackendPrefix     string
		Backend           bool
		Frontend          bool
		BackendService    string
		FrontendService   string
		BackendPort       int
		FrontendPort      int
	}{
		Domain:           domain,
		BackendPrefix:    backendPrefix,
		Backend:          backend,
		Frontend:         frontend,
		BackendService:   GetDeployServiceBackend(),
		FrontendService:  GetDeployServiceFrontend(),
		BackendPort:      GetDeployServiceBackendPort(),
		FrontendPort:     GetDeployServiceFrontendPort(),
	}); err != nil {
		return "", err
	}
	return rendered.String(), nil
}

// renderCloudflareCompose возвращает compose-файл для cloudflared
func renderCloudflareCompose() (string, error) {
	composeTemplate, err := template.New("cloudflare-compose").Parse(cloudflareComposeTemplate)
	if err != nil {
		return "", err
	}

	var rendered bytes.Buffer
	if err := composeTemplate.Execute(&rendered, struct {
		Network string
	}{Network: GetDeployNetwork()}); err != nil {
		return "", err
	}
	return rendered.String(), nil
}

func DomainProxyLogs(ctx context.Context) (string, error) {
	proxy := GetDeploymentProxy()
	switch proxy {
	case "cloudflare":
		return RunWSLWithCancel(ctx, "nerdctl logs --tail 200 soul-dialogue-cloudflared")
	default:
		return RunWSLWithCancel(ctx, "nerdctl logs --tail 200 soul-dialogue-traefik")
	}
}

func RollbackDomain(ctx context.Context) (string, error) {
	projectPath := GetProjectPath()
	if projectPath == "" {
		return "", fmt.Errorf("путь к проекту не настроен")
	}

	proxy := GetDeploymentProxy()
	switch proxy {
	case "cloudflare":
		composePath := filepath.Join(projectPath, ".containerd-data", "cloudflare-compose.yaml")
		return RunWSLWithCancel(ctx, fmt.Sprintf(
			"nerdctl compose --project-directory %s -f %s down",
			strconv.Quote(ToWSLPath(proxyComposeProjectDir(projectPath))),
			shellQuote(ToWSLPath(composePath)),
		))
	default:
		composePath := filepath.Join(projectPath, ".containerd-data", "traefik-compose.yaml")
		return RunWSLWithCancel(ctx, fmt.Sprintf(
			"nerdctl compose --project-directory %s -f %s down",
			strconv.Quote(ToWSLPath(proxyComposeProjectDir(projectPath))),
			shellQuote(ToWSLPath(composePath)),
		))
	}
}

const cloudflareConfigTemplate = `{
  "ingress": [
    {{ if .Frontend }}{
      "hostname": "{{ .Domain }}",
      "service": "http://{{ .FrontendService }}:{{ .FrontendPort }}"
    },{{ end }}
    {{ if .Backend }}{
      "hostname": "{{ .Domain }}",
      "path": "{{ .BackendPrefix }}*",
      "service": "http://{{ .BackendService }}:{{ .BackendPort }}"
    },{{ end }}
    {
      "service": "http_status:404"
    }
  ]
}`

// Note: Cloudflare Tunnel не поддерживает stripPrefix в ingress.
// Бэкенд должен обрабатывать пути с префиксом. Для автоматического удаления используйте Traefik.

const cloudflareComposeTemplate = `services:
  cloudflared:
    image: cloudflare/cloudflared:latest
    container_name: soul-dialogue-cloudflared
    restart: unless-stopped
    command:
      - tunnel
      - --config
      - /etc/cloudflare/config.json
      - --credentials-file
      - /etc/cloudflare/credentials.json
      - run
    volumes:
      - ./cloudflare/config.json:/etc/cloudflare/config.json:ro
      - ./cloudflare/credentials.json:/etc/cloudflare/credentials.json:ro
		networks:
			- {{ .Network }}
networks:
	{{ .Network }}:
    external: true
		name: {{ .Network }}
`
