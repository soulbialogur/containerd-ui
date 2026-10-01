package ui

import (
	"containerd-ui/i18n"
	"containerd-ui/wsl"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

var systemMetrics = struct {
	sync.RWMutex
	data      map[string]string
	timestamp time.Time
	ttl       time.Duration
}{
	ttl:  CacheStatus,
	data: make(map[string]string),
}

func getSystemMetrics() map[string]string {
	systemMetrics.RLock()
	if time.Since(systemMetrics.timestamp) < systemMetrics.ttl {
		result := make(map[string]string, len(systemMetrics.data))
		for k, v := range systemMetrics.data {
			result[k] = v
		}
		systemMetrics.RUnlock()
		return result
	}
	systemMetrics.RUnlock()

	script := "" +
		"printf 'CONTAINERS_TOTAL;'; nerdctl ps -a --format '{{.ID}}' 2>/dev/null | wc -l; " +
		"printf 'CONTAINERS_RUNNING;'; nerdctl ps --format '{{.ID}}' 2>/dev/null | wc -l; " +
		"printf 'IMAGES;'; nerdctl images --format '{{.ID}}' 2>/dev/null | wc -l; " +
		"printf 'VOLUMES;'; nerdctl volume ls --format '{{.Name}}' 2>/dev/null | grep -v '^$' | wc -l; " +
		"printf 'NETWORKS;'; nerdctl network ls --format '{{.Name}}' 2>/dev/null | grep -v '^$' | wc -l"

	out, err := wsl.RunWSLAsRootWithTimeout(script, 15*time.Second)
	if err != nil {
		result := map[string]string{
			"containers_total": "—", "containers_running": "—",
			"images": "—", "volumes": "—", "networks": "—",
		}
		systemMetrics.Lock()
		for k, v := range result {
			systemMetrics.data[k] = v
		}
		systemMetrics.timestamp = time.Now()
		systemMetrics.Unlock()
		return result
	}

	result := make(map[string]string)
	lines := strings.Split(out, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "CONTAINERS_TOTAL;"):
			result["containers_total"] = strings.TrimSpace(strings.TrimPrefix(line, "CONTAINERS_TOTAL;"))
		case strings.HasPrefix(line, "CONTAINERS_RUNNING;"):
			result["containers_running"] = strings.TrimSpace(strings.TrimPrefix(line, "CONTAINERS_RUNNING;"))
		case strings.HasPrefix(line, "IMAGES;"):
			result["images"] = strings.TrimSpace(strings.TrimPrefix(line, "IMAGES;"))
		case strings.HasPrefix(line, "VOLUMES;"):
			result["volumes"] = strings.TrimSpace(strings.TrimPrefix(line, "VOLUMES;"))
		case strings.HasPrefix(line, "NETWORKS;"):
			result["networks"] = strings.TrimSpace(strings.TrimPrefix(line, "NETWORKS;"))
		}
	}

	if result["containers_total"] == "" {
		result["containers_total"] = "—"
	}
	if result["containers_running"] == "" {
		result["containers_running"] = "—"
	}
	if result["images"] == "" {
		result["images"] = "—"
	}
	if result["volumes"] == "" {
		result["volumes"] = "—"
	}
	if result["networks"] == "" {
		result["networks"] = "—"
	}

	systemMetrics.Lock()
	for k, v := range result {
		systemMetrics.data[k] = v
	}
	systemMetrics.timestamp = time.Now()
	systemMetrics.Unlock()

	return result
}

type metricCard struct {
	label      *widget.Label
	value      *widget.Button
	title      string
	icon       string
	content    string
	navigateTo string
}

var statusMetricNavigation func(string)

func SetStatusMetricNavigation(navigate func(string)) {
	statusMetricNavigation = navigate
}

func newMetricCard(title, icon, navigateTo string) *metricCard {
	lbl := widget.NewLabel("—")
	lbl.TextStyle = fyne.TextStyle{Bold: true, Monospace: true}
	value := widget.NewButton("—", func() {
		if statusMetricNavigation != nil {
			statusMetricNavigation(navigateTo)
		}
	})
	value.Importance = widget.LowImportance
	return &metricCard{label: lbl, value: value, title: title, icon: icon, navigateTo: navigateTo}
}

func (mc *metricCard) widget() fyne.CanvasObject {
	return container.NewBorder(
		nil, nil,
		widget.NewLabelWithStyle(mc.title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		nil,
		container.NewHBox(widget.NewLabel(mc.icon), mc.value),
	)
}

func (mc *metricCard) setValue(val string) {
	mc.content = val
	mc.label.SetText(val)
	mc.value.SetText(val)
}

func (mc *metricCard) setLoading(loading bool) {
	if loading {
		mc.label.SetText("...")
		mc.value.SetText("...")
		return
	}
	mc.label.SetText(mc.content)
	mc.value.SetText(mc.content)
}

type ComponentStatus struct {
	Name    string
	Version string
	Icon    string
	Active  bool
	Detail  string
}

var statusCache = struct {
	sync.RWMutex
	data      []ComponentStatus
	timestamp time.Time
	ttl       time.Duration
}{
	ttl: CacheStatus,
}

func runWSLWithTimeout(command string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, wsl.WslExecutable(), "-d", wsl.GetWslDistro(), "--exec", wsl.GetShell(), "-c", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(out), ctx.Err()
	}
	return string(out), err
}

func getAllComponentsStatus() []ComponentStatus {
	statusCache.RLock()
	if time.Since(statusCache.timestamp) < statusCache.ttl {
		result := make([]ComponentStatus, len(statusCache.data))
		copy(result, statusCache.data)
		statusCache.RUnlock()
		return result
	}
	statusCache.RUnlock()

	svcName := wsl.GetContainerdService()
	buildkitOK := wsl.CheckBuildkitd()
	cmd := "" +
		"echo 'WSL_CHECK_START'; " +
		"uname -r 2>/dev/null | grep -qi microsoft && echo 'WSL:OK' || echo 'WSL:NO'; " +
		"" + wsl.IsServiceActiveCommand(svcName) + " && echo 'CONTAINERD:OK' || echo 'CONTAINERD:NO'; " +
		"which nerdctl > /dev/null 2>&1 && echo 'NERDCTL:OK' || echo 'NERDCTL:NO'; " +
		"echo 'WSL_CHECK_END'"
	if buildkitOK {
		cmd = "echo 'BUILDKIT:OK'; " + cmd
	} else {
		cmd = "echo 'BUILDKIT:NO'; " + cmd
	}

	out, err := runWSLWithTimeout(cmd, 15*time.Second)

	versions := getComponentVersions()
	distro := wsl.GetWslDistro()
	if distro != "" {
		wslVersion := versions["WSL"]
		if wslVersion != "" && wslVersion != "—" {
			versions["WSL"] = distro + ", " + wslVersion
		} else {
			versions["WSL"] = distro
		}
	}

	var result []ComponentStatus
	if err != nil {
		result = []ComponentStatus{
			{Name: "WSL", Version: versions["WSL"], Icon: "❌", Active: false, Detail: i18n.T("status.detail_timeout")},
			{Name: "Containerd", Version: versions["Containerd"], Icon: "⚠️", Active: false, Detail: i18n.T("status.detail_unavailable")},
			{Name: "Buildkitd", Version: versions["Buildkitd"], Icon: "⚠️", Active: false, Detail: i18n.T("status.detail_unavailable")},
			{Name: "Nerdctl", Version: versions["Nerdctl"], Icon: "❌", Active: false, Detail: i18n.T("status.detail_unavailable")},
		}
	} else {
		lines := strings.Split(out, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)

			if strings.Contains(line, "WSL_CHECK_START") || strings.Contains(line, "WSL_CHECK_END") {
				continue
			}

			if strings.Contains(line, "WSL:OK") {
				result = append(result, ComponentStatus{Name: "WSL", Version: distro, Icon: "✅", Active: true, Detail: i18n.T("status.detail_active")})
			} else if strings.Contains(line, "WSL:NO") {
				result = append(result, ComponentStatus{Name: "WSL", Version: distro, Icon: "⚠️", Active: false, Detail: i18n.T("status.detail_stopped")})
			}

			if strings.Contains(line, "CONTAINERD:OK") {
				result = append(result, ComponentStatus{Name: "Containerd", Version: versions["Containerd"], Icon: "✅", Active: true, Detail: i18n.T("status.detail_grpc")})
			} else if strings.Contains(line, "CONTAINERD:NO") {
				result = append(result, ComponentStatus{Name: "Containerd", Version: versions["Containerd"], Icon: "⚠️", Active: false, Detail: i18n.T("status.detail_not_started")})
			}

			if strings.Contains(line, "BUILDKIT:OK") {
				result = append(result, ComponentStatus{Name: "Buildkitd", Version: versions["Buildkitd"], Icon: "✅", Active: true, Detail: i18n.T("status.detail_available")})
			} else if strings.Contains(line, "BUILDKIT:NO") {
				result = append(result, ComponentStatus{Name: "Buildkitd", Version: versions["Buildkitd"], Icon: "⚠️", Active: false, Detail: i18n.T("status.detail_stopped_cap")})
			}

			if strings.Contains(line, "NERDCTL:OK") {
				result = append(result, ComponentStatus{Name: "Nerdctl", Version: versions["Nerdctl"], Icon: "✅", Active: true, Detail: i18n.T("status.detail_available")})
			} else if strings.Contains(line, "NERDCTL:NO") {
				result = append(result, ComponentStatus{Name: "Nerdctl", Version: versions["Nerdctl"], Icon: "❌", Active: false, Detail: i18n.T("status.detail_not_found")})
			}
		}
	}

	statusCache.Lock()
	statusCache.data = result
	statusCache.timestamp = time.Now()
	statusCache.Unlock()

	return result
}

type versionEntry struct {
	version string
	ok      bool
}

func getComponentVersions() map[string]string {
	versions := map[string]string{"WSL": getWindowsWslVersion()}

	script := `export PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:${PATH}"
echo "CONTAINERD_VERSION=$(containerd --version 2>/dev/null || /usr/local/bin/containerd --version 2>/dev/null || /usr/bin/containerd --version 2>/dev/null || true)"
echo "BUILDKIT_VERSION=$(buildctl --version 2>/dev/null || /usr/local/bin/buildctl --version 2>/dev/null || /usr/bin/buildctl --version 2>/dev/null || true)"
echo "NERDCTL_VERSION=$(nerdctl --version 2>/dev/null || /usr/local/bin/nerdctl --version 2>/dev/null || /usr/bin/nerdctl --version 2>/dev/null || true)"`

	out, err := wsl.RunWSLAsRootWithTimeout(script, 30*time.Second)
	if err != nil {
		// Скрипт завершается с exit 0 благодаря финальному `true`,
		// но на всякий случай не теряем частичный вывод.
		if strings.TrimSpace(out) == "" {
			versions["Containerd"] = "—"
			versions["Buildkitd"] = "—"
			versions["Nerdctl"] = "—"
			return versions
		}
	}

	for key, version := range parseComponentVersions(out) {
		versions[key] = version
	}

	for _, key := range []string{"WSL", "Containerd", "Buildkitd", "Nerdctl"} {
		if versions[key] == "" {
			versions[key] = "—"
		}
	}

	return versions
}

func parseComponentVersions(output string) map[string]string {
	versions := make(map[string]string, 3)
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(line, "\x00\uFEFF"))
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		switch key {
		case "CONTAINERD_VERSION":
			versions["Containerd"] = shortVersion(value)
		case "BUILDKIT_VERSION":
			versions["Buildkitd"] = shortVersion(value)
		case "NERDCTL_VERSION":
			versions["Nerdctl"] = shortVersion(value)
		}
	}
	return versions
}

func getWindowsWslVersion() string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, wsl.WslExecutable(), "--version")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := cmd.Output()
	if err != nil {
		return "—"
	}
	return parseWindowsWslVersion(string(output))
}

func parseWindowsWslVersion(output string) string {
	return shortVersion(output)
}

func shortVersion(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "—"
	}
	re := regexp.MustCompile(`v?(\d+\.\d+\.\d+)`)
	match := re.FindStringSubmatch(raw)
	if len(match) > 0 {
		if strings.HasPrefix(match[0], "v") {
			return match[0]
		}
		return "v" + match[1]
	}
	parts := strings.Fields(raw)
	if len(parts) >= 2 {
		return parts[0] + " " + parts[1]
	}
	return raw
}

func areAllComponentsInstalled() bool {
	// Проверяем наличие бинарников в WSL, а не их статус.
	// CNI-плагины необходимы для корректной работы bridge и сетей в Alpine.
	out, err := runWSLWithTimeout(runtimeComponentsProbeScript(), 10*time.Second)
	return err == nil && runtimeComponentsAreInstalled(out, wsl.CurrentEnvironment())
}

func runtimeComponentsProbeScript() string {
	return "if command -v containerd >/dev/null 2>&1 && " +
		"command -v nerdctl >/dev/null 2>&1 && " +
		"command -v buildctl >/dev/null 2>&1 && " +
		"(test -x /opt/cni/bin/bridge || test -x /usr/lib/cni/bridge || test -x /usr/libexec/cni/bridge); then " +
		"printf 'NERDCTL_VERSION='; nerdctl --version; " +
		"printf 'BUILDKIT_VERSION='; buildctl --version; echo ALL_OK; fi"
}

func runtimeComponentsAreInstalled(output string, environment wsl.Environment) bool {
	if !strings.Contains(output, "ALL_OK") {
		return false
	}
	if environment.PkgManager != wsl.PkgApk {
		return true
	}

	var nerdctlVersion, buildkitVersion string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(line, "\x00\uFEFF"))
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		switch key {
		case "NERDCTL_VERSION":
			nerdctlVersion = value
		case "BUILDKIT_VERSION":
			buildkitVersion = value
		}
	}
	return wsl.AlpineToolchainVersionsSupported(nerdctlVersion, buildkitVersion)
}

var installInProgress = false
var installCancel context.CancelFunc

// checkNetwork проверяет доступность интернета
func checkNetwork() bool {
	// Пробуем пингануть Google DNS — быстро и надёжно
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, wsl.WslExecutable(), "-d", wsl.GetWslDistro(), "--exec", wsl.GetShell(), "-c",
		"ping -c 1 -W 2 8.8.8.8 >/dev/null 2>&1 && echo OK || echo FAIL")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()

	return err == nil && strings.Contains(string(out), "OK")
}

func installAllComponents(win fyne.Window) {
	// Если установка уже идёт — ничего не делаем
	if installInProgress {
		return
	}

	// Проверяем, установлены ли уже все компоненты
	if areAllComponentsInstalled() {
		dialog.ShowInformation(
			i18n.T("status.install_all_title"),
			i18n.T("status.install_all_already_installed"),
			win,
		)
		return
	}

	// Проверяем что уже есть, чтобы не устанавливать заново
	selectedDistro := strings.TrimSpace(wsl.GetWslDistro())
	wslInstalled := selectedDistro != ""
	distroName := selectedDistro
	containersInstalled := false
	if distroName == "" {
		distroName = wsl.GetDefaultWslDistroName()
	}

	out, _ := runWSLWithTimeout(runtimeComponentsProbeScript(), 10*time.Second)
	containersInstalled = runtimeComponentsAreInstalled(out, wsl.CurrentEnvironment())

	// Показываем диалог подтверждения с информацией о том что будет установлено
	confirmMsg := "Вы уверены, что хотите установить компоненты?\n\n"
	if !wslInstalled {
		confirmMsg += i18n.T("status.distro_will_install", distroName)
	} else {
		confirmMsg += i18n.T("status.distro_installed", distroName)
	}
	if !containersInstalled {
		confirmMsg += "• containerd, nerdctl, BuildKit — будут установлены\n"
	} else {
		confirmMsg += "• containerd, nerdctl, BuildKit — уже установлены ✓\n"
	}
	confirmMsg += "\nЭто займёт несколько минут. Продолжить?"

	dialog.ShowCustomConfirm(
		i18n.T("status.install_all_confirm_title"),
		i18n.T("dialogs.ok"),
		i18n.T("dialogs.cancel"),
		widget.NewLabel(confirmMsg),
		func(confirmed bool) {
			if !confirmed {
				return
			}

			// Создаём канал для отмены
			cancelCtx, cancel := context.WithCancel(context.Background())
			installCancel = cancel
			defer func() {
				installCancel = nil
			}()

			// Показываем диалог прогресса с кнопкой отмены
			progressLabel := widget.NewLabel(i18n.T("status.install_all_progress"))
			progressBar := widget.NewProgressBarInfinite()
			cancelBtn := widget.NewButton(i18n.T("dialogs.cancel"), func() {
				cancel()
			})

			dlgContent := container.NewVBox(
				progressLabel,
				progressBar,
				cancelBtn,
			)
			dlg := dialog.NewCustomConfirm(
				i18n.T("status.install_all_progress"),
				"",
				i18n.T("dialogs.cancel"),
				dlgContent,
				func(_ bool) {
					// Кнопка отмены — отменяет установку
				},
				win,
			)
			dlg.Show()

			// Запускаем установку в фоне
			go func() {
				installInProgress = true
				defer func() {
					installInProgress = false
				}()

				var logs []string
				wslSuccess := false
				installSuccess := false

				// Шаг 1: Alpine WSL — только если не установлен
				if !wslInstalled {
					installDistroMessage := i18n.T("status.install_wsl_distro", distroName)
					progressLabel.SetText(installDistroMessage)
					logs = append(logs, "📦 "+installDistroMessage)

					// Быстрый таймаут для WSL установки
					wslCtx, wslCancel := context.WithTimeout(context.Background(), 2*time.Minute)
					defer wslCancel()

					wslCmd := exec.CommandContext(wslCtx, wsl.WslExecutable(), "--install", "--distribution", distroName)
					wslCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
					wslOut, wslErr := wslCmd.CombinedOutput()

					if wslCtx.Err() == context.DeadlineExceeded {
						logs = append(logs, "  ❌ Таймаут: WSL установка не отвечает более 2 минут")
					} else if wslErr != nil {
						logs = append(logs, fmt.Sprintf("  ⚠️ WSL/%s: %s", distroName, strings.TrimSpace(string(wslOut))))
					} else {
						wsl.InvalidateEnvironmentCache()
						logs = append(logs, fmt.Sprintf("  ✅ WSL/%s установлен", distroName))
						wslSuccess = true
					}
				} else {
					logs = append(logs, "📦 WSL2 + "+distroName+" — уже установлен, пропускаем")
					wslSuccess = true
				}

				// Шаг 2: Проверяем сеть перед установкой пакетов
				if !containersInstalled {
					progressLabel.SetText(i18n.T("status.install_containerd"))
					logs = append(logs, "\n📦 "+i18n.T("status.install_containerd"))

					// Проверяем сеть
					if !checkNetwork() {
						cancel()
						logs = append(logs, "  ❌ Нет подключения к интернету\n\n"+
							"Установка невозможна без доступа к репозиториям.\n"+
							"Проверьте сетевое подключение и попробуйте снова.")

						fyne.Do(func() {
							dlg.Hide()
							dialog.ShowError(errors.New(logs[len(logs)-1]), win)
						})
						return
					}

					environment := wsl.CurrentEnvironment()
					installEnvironment := environment
					installEnvironment.PrivilegeCmd = ""
					installScript, commandErr := wsl.BuildRuntimeInstallCommand(installEnvironment)
					if commandErr != nil {
						logs = append(logs, "  ❌ "+commandErr.Error())
					} else {
						installCtx, installCancel := context.WithCancel(cancelCtx)
						defer installCancel()

						cmd := exec.CommandContext(installCtx, wsl.WslExecutable(), "-d", distroName, "-u", "root", "--exec", wsl.ShellSh, "-c", installScript)
						cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
						out, err := cmd.CombinedOutput()
						output := strings.TrimSpace(string(out))

						if installCtx.Err() == context.Canceled {
							logs = append(logs, "  ⏹ Установка отменена пользователем")
						} else if installCtx.Err() == context.DeadlineExceeded {
							logs = append(logs, "  ❌ Таймаут: установка пакетов не завершилась вовремя")
						} else if err != nil {
							if strings.Contains(strings.ToLower(output), "could not resolve") ||
								strings.Contains(strings.ToLower(output), "temporary failure") ||
								strings.Contains(strings.ToLower(output), "network is unreachable") {
								logs = append(logs, "  ❌ Ошибка сети: не удалось подключиться к репозиториям\n\n"+
									"Проверьте подключение к интернету и попробуйте снова.")
							} else {
								logs = append(logs, fmt.Sprintf("  ❌ Ошибка установки: %v", err))
							}
							if output != "" {
								lines := strings.Split(output, "\n")
								start := 0
								if len(lines) > 10 {
									start = len(lines) - 10
								}
								logs = append(logs, "  "+strings.Join(lines[start:], "\n  "))
							}
						} else {
							wsl.InvalidateEnvironmentCache()
							logs = append(logs, "  ✅ containerd, nerdctl, BuildKit установлены")
							installSuccess = true
						}
					}
				} else {
					logs = append(logs, "\n📦 containerd, nerdctl, BuildKit — уже установлены, пропускаем")
					installSuccess = true
				}

				// Показываем результат
				resultMsg := strings.Join(logs, "\n")

				fyne.Do(func() {
					dlg.Hide()

					// Проверяем, была ли отмена
					if cancelCtx.Err() == context.Canceled {
						// Отмена — не показываем ошибку
						return
					}

					if wslSuccess && installSuccess {
						// Всё успешно — показываем сообщение об успехе
						dialog.ShowInformation(i18n.T("status.install_all_success"), resultMsg, win)
					} else if wslSuccess || installSuccess {
						// Частичный успех
						dialog.ShowInformation(i18n.T("status.install_all_partial"), resultMsg, win)
					} else {
						// Полная ошибка — показываем ошибку, а не скрываем
						dialog.ShowError(errors.New(resultMsg), win)
					}
				})
			}()
		},
		win,
	)
}

func BuildStatusTab(win fyne.Window) fyne.CanvasObject {
	var updateMu sync.Mutex

	wslCard := newResponsiveStatusCard("WSL")
	containerdCard := newResponsiveStatusCard("Containerd")
	buildkitdCard := newResponsiveStatusCard("Buildkitd")
	nerdctlCard := newResponsiveStatusCard("Nerdctl")

	metrics := map[string]*metricCard{
		"containers_running": newMetricCard(i18n.T("status.metric_containers"), "📦", i18n.T("tabs.containers")),
		"images":             newMetricCard(i18n.T("status.metric_images"), "🖼️", i18n.T("tabs.images")),
		"volumes":            newMetricCard(i18n.T("status.metric_volumes"), "💾", i18n.T("tabs.volumes")),
		"networks":           newMetricCard(i18n.T("status.metric_networks"), "🌐", i18n.T("tabs.networks")),
	}

	lastCheckLabel := widget.NewLabel(i18n.T("status.last_check_never"))
	var btnRefresh *widget.Button
	setLoading := func(loading bool) {
		wslCard.SetLoading(loading)
		containerdCard.SetLoading(loading)
		buildkitdCard.SetLoading(loading)
		nerdctlCard.SetLoading(loading)
		for _, mc := range metrics {
			mc.setLoading(loading)
		}
	}

	updateUI := func() {
		if !updateMu.TryLock() {
			return
		}
		defer updateMu.Unlock()
		safeUI(func() {
			setLoading(true)
			lastCheckLabel.SetText(i18n.T("status.loading"))
			btnRefresh.Disable()
		})

		statusCache.Lock()
		statusCache.timestamp = time.Time{}
		statusCache.Unlock()

		// WSL-запросы выполняются вне UI goroutine.
		statuses := getAllComponentsStatus()
		metricsSnapshot := getSystemMetrics()

		// Все изменения Fyne-виджетов — только через fyne.Do.
		safeUI(func() {
			setLoading(false)
			for _, cs := range statuses {
				statusText := cs.Icon
				if cs.Version != "" {
					statusText += " " + cs.Version
				}
				if cs.Detail != "" {
					statusText += " (" + wsl.TranslateStatus(cs.Detail) + ")"
				}
				compactStatus := cs.Icon + " " + wsl.TranslateStatus(cs.Detail)

				switch cs.Name {
				case "WSL":
					wslCard.SetStatus(statusText, compactStatus)
				case "Containerd":
					containerdCard.SetStatus(statusText, compactStatus)
				case "Buildkitd":
					buildkitdCard.SetStatus(statusText, compactStatus)
				case "Nerdctl":
					nerdctlCard.SetStatus(statusText, compactStatus)
				}
			}

			for k, mc := range metrics {
				if v, ok := metricsSnapshot[k]; ok {
					mc.setValue(v)
				}
			}
			lastCheckLabel.SetText(i18n.T("status.last_check", time.Now().Format("15:04:05")))
			btnRefresh.Enable()
		})
	}

	btnRefresh = widget.NewButton(i18n.T("status.refresh"), func() { go updateUI() })

	tab := newTabActive(false, time.Duration(wsl.GetAutoRefreshInterval())*time.Second, func() {
		updateUI()
	})

	autoRefresh := widget.NewCheck(i18n.T("status.auto_refresh"), func(checked bool) {
		tab.SetActive(checked)
	})

	var btnStartBuildkitd *widget.Button
	var btnStopBuildkitd *widget.Button

	btnStartBuildkitd = widget.NewButton(i18n.T("status.start_buildkitd"), func() {
		btnStartBuildkitd.Disable()
		btnStopBuildkitd.Disable()
		buildkitdCard.SetStatus("⏳ Запуск Buildkitd...", "⏳ Запуск...")
		lastCheckLabel.SetText("⏳ Запуск Buildkitd...")
		go func() {
			select {
			case <-wsl.AppContext().Done():
				safeUI(func() {
					btnStartBuildkitd.Enable()
					btnStopBuildkitd.Enable()
				})
				return
			default:
			}

			if err := wsl.StartBuildkitd(); err != nil {
				safeUI(func() {
					buildkitdCard.SetStatus("❌ Ошибка запуска: "+err.Error(), "❌ Ошибка запуска")
					lastCheckLabel.SetText(i18n.T("common.error") + ": " + err.Error())
					btnStartBuildkitd.Enable()
					btnStopBuildkitd.Enable()
				})
				// Ошибка не должна висеть вечно: через 15 секунд
				// возвращаем штатную подпись.
				time.AfterFunc(15*time.Second, func() {
					safeUI(func() {
						buildkitdCard.SetStatus("⚠️ Buildkitd остановлен", "⚠️ Остановлен")
						lastCheckLabel.SetText(i18n.T("status.last_check", time.Now().Format("15:04:05")))
					})
				})
			} else {
				safeUI(func() {
					buildkitdCard.SetStatus("✅ Buildkitd запущен", "✅ Активен")
					btnStartBuildkitd.Enable()
					btnStopBuildkitd.Enable()
				})
				updateUI()
			}
		}()
	})
	btnStartBuildkitd.Importance = widget.MediumImportance

	btnStopBuildkitd = widget.NewButton(i18n.T("status.stop_buildkitd"), func() {
		btnStartBuildkitd.Disable()
		btnStopBuildkitd.Disable()
		buildkitdCard.SetStatus("⏳ Остановка Buildkitd...", "⏳ Остановка...")
		lastCheckLabel.SetText("⏳ Остановка Buildkitd...")
		go func() {
			select {
			case <-wsl.AppContext().Done():
				safeUI(func() {
					btnStartBuildkitd.Enable()
					btnStopBuildkitd.Enable()
				})
				return
			default:
			}

			wsl.StopBuildkitd()
			safeUI(func() {
				buildkitdCard.SetStatus("⚠️ Buildkitd остановлен", "⚠️ Остановлен")
				lastCheckLabel.SetText("Buildkitd остановлен")
				btnStartBuildkitd.Enable()
				btnStopBuildkitd.Enable()
			})
			updateUI()
		}()
	})
	btnStopBuildkitd.Importance = widget.MediumImportance

	// --- Ссылки на скачивание компонентов и кнопка установки ---
	installBtn := widget.NewButton(i18n.T("status.install_all_title"), func() {
		installAllComponents(win)
	})
	installBtn.Importance = widget.HighImportance
	installBtn.SetText(i18n.T("status.install_all_checking"))
	installBtn.Disable()

	downloads := newResponsiveFlow(
		widget.NewHyperlink(i18n.T("status.download_wsl"), mustParseURL("https://learn.microsoft.com/ru-ru/windows/wsl/install")),
		widget.NewHyperlink(i18n.T("status.download_containerd"), mustParseURL("https://containerd.io/downloads/")),
		widget.NewHyperlink(i18n.T("status.download_nerdctl"), mustParseURL("https://github.com/containerd/nerdctl/releases")),
		widget.NewHyperlink(i18n.T("status.download_buildkit"), mustParseURL("https://github.com/moby/buildkit/releases")),
		widget.NewHyperlink(i18n.T("status.download_cloudflared"), mustParseURL("https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/")),
		installBtn,
	)
	downloadsCard := widget.NewCard(i18n.T("status.downloads_title"), i18n.T("status.downloads_hint"), downloads)

	registerTabNamed(i18n.T("tabs.status"), tab)

	// Первичная проверка WSL запускается после построения UI.
	go func() {
		updateUI()
		installed := areAllComponentsInstalled()
		safeUI(func() {
			if installed {
				installBtn.SetText(i18n.T("status.install_all_done"))
				installBtn.Disable()
				return
			}
			installBtn.SetText(i18n.T("status.install_all_title"))
			installBtn.Enable()
		})
	}()

	return withVerticalScroll(container.NewVBox(
		newResponsiveFlow(btnRefresh, autoRefresh, lastCheckLabel),
		widget.NewSeparator(),
		newResponsiveGrid(4, wslCard.CanvasObject(), containerdCard.CanvasObject(), buildkitdCard.CanvasObject(), nerdctlCard.CanvasObject()),
		widget.NewSeparator(),
		widget.NewLabelWithStyle(i18n.T("status.overview"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		newResponsiveGrid(4,
			metrics["containers_running"].widget(),
			metrics["images"].widget(),
			metrics["volumes"].widget(),
			metrics["networks"].widget(),
		),
		widget.NewSeparator(),
		newResponsiveFlow(
			widget.NewLabelWithStyle(i18n.T("status.buildkitd_control"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			btnStartBuildkitd, btnStopBuildkitd,
		),
		widget.NewSeparator(),
		downloadsCard,
	))
}

func mustParseURL(raw string) *url.URL {
	u, _ := url.Parse(raw)
	return u
}
