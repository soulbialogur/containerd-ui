package ui

import (
	"containerd-ui/i18n"
	"containerd-ui/wsl"
	"context"
	"errors"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
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

func systemMetricsSnapshot() map[string]string {
	systemMetrics.RLock()
	defer systemMetrics.RUnlock()
	result := make(map[string]string, len(systemMetrics.data))
	for key, value := range systemMetrics.data {
		result[key] = value
	}
	return result
}

func storeSystemMetrics(ctx context.Context, result map[string]string) bool {
	if ctx.Err() != nil {
		return false
	}
	systemMetrics.Lock()
	defer systemMetrics.Unlock()
	if ctx.Err() != nil {
		return false
	}
	for key, value := range result {
		systemMetrics.data[key] = value
	}
	systemMetrics.timestamp = time.Now()
	return true
}

func getSystemMetrics() map[string]string {
	ctx := wsl.AppContext()
	if ctx.Err() != nil {
		return systemMetricsSnapshot()
	}

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
	if ctx.Err() != nil {
		return systemMetricsSnapshot()
	}

	script := "" +
		"printf 'CONTAINERS_TOTAL;'; nerdctl ps -a --format '{{.ID}}' 2>/dev/null | wc -l; " +
		"printf 'CONTAINERS_RUNNING;'; nerdctl ps --format '{{.ID}}' 2>/dev/null | wc -l; " +
		"printf 'IMAGES;'; nerdctl images --format '{{.ID}}' 2>/dev/null | wc -l; " +
		"printf 'VOLUMES;'; nerdctl volume ls --format '{{.Name}}' 2>/dev/null | grep -v '^$' | wc -l; " +
		"printf 'NETWORKS;'; nerdctl network ls --format '{{.Name}}' 2>/dev/null | grep -v '^$' | wc -l"

	if ctx.Err() != nil {
		return systemMetricsSnapshot()
	}
	out, err := wsl.RunWSLAsRootWithTimeout(script, 15*time.Second)
	if ctx.Err() != nil {
		return systemMetricsSnapshot()
	}
	if err != nil {
		result := emptySystemMetrics()
		if !storeSystemMetrics(ctx, result) {
			return systemMetricsSnapshot()
		}
		return result
	}

	result := parseSystemMetricsOutput(out)

	if !storeSystemMetrics(ctx, result) {
		return systemMetricsSnapshot()
	}

	return result
}

func emptySystemMetrics() map[string]string {
	return map[string]string{
		"containers_total": "—", "containers_running": "—",
		"images": "—", "volumes": "—", "networks": "—",
	}
}

func parseSystemMetricsOutput(output string) map[string]string {
	result := emptySystemMetrics()
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		for _, metric := range []string{"CONTAINERS_TOTAL", "CONTAINERS_RUNNING", "IMAGES", "VOLUMES", "NETWORKS"} {
			prefix := metric + ";"
			if strings.HasPrefix(line, prefix) {
				key := strings.ToLower(metric)
				result[key] = strings.TrimSpace(strings.TrimPrefix(line, prefix))
				break
			}
		}
	}
	return result
}

type metricCard struct {
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
	value := widget.NewButton("—", func() {
		if statusMetricNavigation != nil {
			statusMetricNavigation(navigateTo)
		}
	})
	value.Importance = widget.LowImportance
	return &metricCard{value: value, title: title, icon: icon, navigateTo: navigateTo}
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
	mc.value.SetText(val)
}

func (mc *metricCard) setLoading(loading bool) {
	if loading {
		mc.value.SetText("...")
		return
	}
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

	out, err := wsl.RunWSLAsRootWithTimeout(statusSnapshotScript(wsl.GetContainerdService()), 30*time.Second)
	metrics := emptySystemMetrics()
	if err == nil {
		metrics = parseSystemMetricsOutput(out)
	}
	storeSystemMetrics(wsl.AppContext(), metrics)

	versions := getComponentVersions(out)
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
		result = append(result, ComponentStatus{Name: "WSL", Version: distro, Icon: "✅", Active: true, Detail: i18n.T("status.detail_active")})
		lines := strings.Split(out, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)

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

func compactComponentStatus(status ComponentStatus) string {
	if status.Name == "WSL" && status.Version != "" {
		return status.Icon + " " + status.Version
	}
	return status.Icon + " " + status.Detail
}

func fullComponentStatus(status ComponentStatus) string {
	text := status.Icon
	if status.Version != "" {
		text += " " + status.Version
	}
	if status.Detail != "" && status.Name != "WSL" {
		text += " (" + status.Detail + ")"
	}
	return text
}

func statusSnapshotScript(service string) string {
	return strings.Join([]string{
		`export PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:${PATH}"`,
		wsl.IsServiceActiveCommand(service) + ` && echo 'CONTAINERD:OK' || echo 'CONTAINERD:NO'`,
		`if command -v buildctl >/dev/null 2>&1 && [ -S /run/buildkit/buildkitd.sock ] && buildctl --addr ` + wsl.BuildkitHostAddr() + ` debug workers </dev/null >/dev/null 2>&1; then echo 'BUILDKIT:OK'; else echo 'BUILDKIT:NO'; fi`,
		`command -v nerdctl >/dev/null 2>&1 && echo 'NERDCTL:OK' || echo 'NERDCTL:NO'`,
		`echo 'WSL_CHECK_END'`,
		`echo "CONTAINERD_VERSION=$(containerd --version 2>/dev/null || /usr/local/bin/containerd --version 2>/dev/null || /usr/bin/containerd --version 2>/dev/null || true)"`,
		`echo "BUILDKIT_VERSION=$(buildctl --version 2>/dev/null || /usr/local/bin/buildctl --version 2>/dev/null || /usr/bin/buildctl --version 2>/dev/null || true)"`,
		`echo "NERDCTL_VERSION=$(nerdctl --version 2>/dev/null || /usr/local/bin/nerdctl --version 2>/dev/null || /usr/bin/nerdctl --version 2>/dev/null || true)"`,
		`printf 'CONTAINERS_TOTAL;'; nerdctl ps -a --format '{{.ID}}' 2>/dev/null | wc -l`,
		`printf 'CONTAINERS_RUNNING;'; nerdctl ps --format '{{.ID}}' 2>/dev/null | wc -l`,
		`printf 'IMAGES;'; nerdctl images --format '{{.ID}}' 2>/dev/null | wc -l`,
		`printf 'VOLUMES;'; nerdctl volume ls --format '{{.Name}}' 2>/dev/null | grep -v '^$' | wc -l`,
		`printf 'NETWORKS;'; nerdctl network ls --format '{{.Name}}' 2>/dev/null | grep -v '^$' | wc -l`,
	}, "\n")
}

type versionEntry struct {
	version string
	ok      bool
}

func getComponentVersions(output string) map[string]string {
	versions := map[string]string{"WSL": getWindowsWslVersion()}
	for key, version := range parseComponentVersions(output) {
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

var windowsWslVersionCache = struct {
	sync.Mutex
	value   string
	expires time.Time
}{}

func getWindowsWslVersion() string {
	windowsWslVersionCache.Lock()
	defer windowsWslVersionCache.Unlock()
	if time.Now().Before(windowsWslVersionCache.expires) {
		return windowsWslVersionCache.value
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, wsl.WslExecutable(), "--version")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := cmd.Output()
	if err != nil {
		windowsWslVersionCache.value = "—"
		windowsWslVersionCache.expires = time.Now().Add(15 * time.Second)
		return windowsWslVersionCache.value
	}
	windowsWslVersionCache.value = parseWindowsWslVersion(string(output))
	windowsWslVersionCache.expires = time.Now().Add(time.Hour)
	return windowsWslVersionCache.value
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

var installInProgress atomic.Bool
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
	if installInProgress.Load() {
		return
	}

	selectedDistro := strings.TrimSpace(wsl.GetWslDistro())
	if selectedDistro == "" {
		showAppInfo(win, i18n.T("status.install_all_title"), i18n.T("status.install_bundled_distro_required", wsl.GetDefaultWslDistroName()))
		return
	}

	// Проверяем, установлены ли уже все компоненты
	if areAllComponentsInstalled() {
		showAppInfo(win, i18n.T("status.install_all_title"), i18n.T("status.install_all_already_installed"))
		return
	}

	// Проверяем что уже есть, чтобы не устанавливать заново
	distroName := selectedDistro
	containersInstalled := false

	out, _ := runWSLWithTimeout(runtimeComponentsProbeScript(), 10*time.Second)
	containersInstalled = runtimeComponentsAreInstalled(out, wsl.CurrentEnvironment())

	// Показываем диалог подтверждения с информацией о том что будет установлено
	confirmMsg := i18n.T("status.install_all_confirm_intro")
	confirmMsg += i18n.T("status.distro_installed", distroName)
	if !containersInstalled {
		confirmMsg += i18n.T("status.runtime_components_will_install")
	} else {
		confirmMsg += i18n.T("status.runtime_components_installed")
	}
	confirmMsg += i18n.T("status.install_all_confirm_continue")

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
				if !installInProgress.CompareAndSwap(false, true) {
					return
				}
				defer func() {
					installInProgress.Store(false)
				}()

				var logs []string
				wslSuccess := false
				installSuccess := false

				// The bundled distro is installed by the offline setup executable, not the Store.
				logs = append(logs, i18n.T("status.install_wsl_already_installed", distroName))
				wslSuccess = true

				// Шаг 2: Проверяем сеть перед установкой пакетов
				if !containersInstalled {
					progressLabel.SetText(i18n.T("status.install_containerd"))
					logs = append(logs, "\n📦 "+i18n.T("status.install_containerd"))

					// Проверяем сеть
					if !checkNetwork() {
						cancel()
						logs = append(logs, i18n.T("status.install_network_unavailable"))

						fyne.Do(func() {
							dlg.Hide()
							showAppError(win, errors.New(logs[len(logs)-1]))
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
							logs = append(logs, i18n.T("status.install_cancelled"))
						} else if installCtx.Err() == context.DeadlineExceeded {
							logs = append(logs, i18n.T("status.install_package_timeout"))
						} else if err != nil {
							if strings.Contains(strings.ToLower(output), "could not resolve") ||
								strings.Contains(strings.ToLower(output), "temporary failure") ||
								strings.Contains(strings.ToLower(output), "network is unreachable") {
								logs = append(logs, i18n.T("status.install_network_error"))
							} else {
								logs = append(logs, i18n.T("status.install_package_error", err))
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
							logs = append(logs, i18n.T("status.install_runtime_success"))
							installSuccess = true
						}
					}
				} else {
					logs = append(logs, "\n"+i18n.T("status.install_runtime_already_installed"))
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
						showAppInfo(win, i18n.T("status.install_all_success"), resultMsg)
					} else if wslSuccess || installSuccess {
						// Частичный успех
						showAppInfo(win, i18n.T("status.install_all_partial"), resultMsg)
					} else {
						// Полная ошибка — показываем ошибку, а не скрываем
						showAppError(win, errors.New(resultMsg))
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
	if distro := wsl.GetWslDistro(); distro != "" {
		wslCard.SetStatus(
			"⏳ "+distro+" ("+i18n.T("common.loading")+")",
			"⏳ "+distro,
		)
	}
	containerdCard := newResponsiveStatusCard("Containerd")
	containerdCard.SetStatus("⏳ "+i18n.T("common.loading"), "⏳ "+i18n.T("common.loading"))
	buildkitdCard := newResponsiveStatusCard("Buildkitd")
	buildkitdCard.SetStatus("⏳ "+i18n.T("common.loading"), "⏳ "+i18n.T("common.loading"))
	nerdctlCard := newResponsiveStatusCard("Nerdctl")
	nerdctlCard.SetStatus("⏳ "+i18n.T("common.loading"), "⏳ "+i18n.T("common.loading"))

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
				statusText := fullComponentStatus(cs)
				compactStatus := compactComponentStatus(cs)

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
	tab.SetAutoRefreshEnabled(false)

	autoRefresh := widget.NewCheck(i18n.T("status.auto_refresh"), func(checked bool) {
		tab.SetAutoRefreshEnabled(checked)
	})

	var btnStartBuildkitd *widget.Button
	var btnStopBuildkitd *widget.Button

	btnStartBuildkitd = widget.NewButton(i18n.T("status.start_buildkitd"), func() {
		btnStartBuildkitd.Disable()
		btnStopBuildkitd.Disable()
		buildkitdCard.SetStatus(i18n.T("status.buildkit_starting"), i18n.T("status.buildkit_starting_compact"))
		lastCheckLabel.SetText(i18n.T("status.buildkit_starting"))
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
					buildkitdCard.SetStatus(i18n.T("status.buildkit_start_error", err.Error()), i18n.T("status.buildkit_start_error_compact"))
					lastCheckLabel.SetText(i18n.T("common.error") + ": " + err.Error())
					btnStartBuildkitd.Enable()
					btnStopBuildkitd.Enable()
				})
				// Ошибка не должна висеть вечно: через 15 секунд
				// возвращаем штатную подпись.
				time.AfterFunc(15*time.Second, func() {
					safeUI(func() {
						buildkitdCard.SetStatus(i18n.T("status.buildkit_stopped"), i18n.T("status.buildkit_stopped_compact"))
						lastCheckLabel.SetText(i18n.T("status.last_check", time.Now().Format("15:04:05")))
					})
				})
			} else {
				safeUI(func() {
					buildkitdCard.SetStatus(i18n.T("status.buildkit_started"), i18n.T("status.buildkit_started_compact"))
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
		buildkitdCard.SetStatus(i18n.T("status.buildkit_stopping"), i18n.T("status.buildkit_stopping_compact"))
		lastCheckLabel.SetText(i18n.T("status.buildkit_stopping"))
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
				buildkitdCard.SetStatus(i18n.T("status.buildkit_stopped"), i18n.T("status.buildkit_stopped_compact"))
				lastCheckLabel.SetText(i18n.T("status.buildkit_stopped"))
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
