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
	"fyne.io/fyne/v2/layout"
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
		"echo 'CONTAINERS_TOTAL'; nerdctl ps -a --format '{{.ID}}' 2>/dev/null | wc -l; " +
		"echo 'CONTAINERS_RUNNING'; nerdctl ps --format '{{.ID}}' 2>/dev/null | wc -l; " +
		"echo 'IMAGES'; nerdctl images --format '{{.ID}}' 2>/dev/null | wc -l; " +
		"echo 'VOLUMES'; nerdctl volume ls --format '{{.Name}}' 2>/dev/null | grep -v '^$' | wc -l; " +
		"echo 'NETWORKS'; nerdctl network ls --format '{{.Name}}' 2>/dev/null | grep -v '^$' | wc -l"

	out, err := runWSLWithTimeout(script, 15*time.Second)
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
		if idx := strings.Index(line, "; "); idx >= 0 {
			continue
		}
		switch {
		case strings.HasPrefix(line, "CONTAINERS_TOTAL; "):
			result["containers_total"] = strings.TrimSpace(strings.TrimPrefix(line, "CONTAINERS_TOTAL; "))
		case strings.HasPrefix(line, "CONTAINERS_RUNNING; "):
			result["containers_running"] = strings.TrimSpace(strings.TrimPrefix(line, "CONTAINERS_RUNNING; "))
		case strings.HasPrefix(line, "IMAGES; "):
			result["images"] = strings.TrimSpace(strings.TrimPrefix(line, "IMAGES; "))
		case strings.HasPrefix(line, "VOLUMES; "):
			result["volumes"] = strings.TrimSpace(strings.TrimPrefix(line, "VOLUMES; "))
		case strings.HasPrefix(line, "NETWORKS; "):
			result["networks"] = strings.TrimSpace(strings.TrimPrefix(line, "NETWORKS; "))
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
	label   *widget.Label
	title   string
	icon    string
	content string
}

func newMetricCard(title, icon string) *metricCard {
	lbl := widget.NewLabel("—")
	lbl.TextStyle = fyne.TextStyle{Bold: true, Monospace: true}
	return &metricCard{label: lbl, title: title, icon: icon}
}

func (mc *metricCard) widget() fyne.CanvasObject {
	return container.NewBorder(
		nil, nil,
		widget.NewLabelWithStyle(mc.title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		nil,
		container.NewHBox(widget.NewLabel(mc.icon), mc.label),
	)
}

func (mc *metricCard) setValue(val string) {
	mc.content = val
	mc.label.SetText(val)
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

	cmd := exec.CommandContext(ctx, wsl.WslExecutable(), "-d", wsl.GetWslDistro(), wsl.GetShell(), "-c", command)
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

	svcName := wsl.GetSystemdService()
	cmd := "" +
		"echo 'WSL_CHECK_START'; " +
		"uname -r 2>/dev/null | grep -qi microsoft && echo 'WSL:OK' || echo 'WSL:NO'; " +
		"" + wsl.IsServiceActiveCommand(svcName) + " && echo 'CONTAINERD:OK' || echo 'CONTAINERD:NO'; " +
		"buildctl --addr unix:///run/buildkit/buildkitd.sock debug workers > /dev/null 2>&1 && echo 'BUILDKIT:OK' || echo 'BUILDKIT:NO'; " +
		"which nerdctl > /dev/null 2>&1 && echo 'NERDCTL:OK' || echo 'NERDCTL:NO'; " +
		"echo 'WSL_CHECK_END'"

	out, err := runWSLWithTimeout(cmd, 15*time.Second)

	versions := getComponentVersions()
	distro := wsl.GetWslDistro()
	versions["WSL"] = distro

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
	versions := make(map[string]string)

	script := "" +
		"echo 'WSL_VERSION'; wsl --version 2>/dev/null | head -1; " +
		"echo 'CONTAINERD_VERSION'; containerd --version 2>/dev/null; " +
		"echo 'BUILDKIT_VERSION'; buildctl --version 2>/dev/null; " +
		"echo 'NERDCTL_VERSION'; nerdctl --version 2>/dev/null"

	out, err := runWSLWithTimeout(script, 30*time.Second)
	if err != nil {
		versions["WSL"] = "—"
		versions["Containerd"] = "—"
		versions["Buildkitd"] = "—"
		versions["Nerdctl"] = "—"
		return versions
	}

	lines := strings.Split(out, "\n")
	var currentKey string

	for _, line := range lines {
		line = strings.TrimSpace(line)

		switch {
		case strings.HasPrefix(line, "WSL_VERSION"):
			currentKey = "WSL"
		case strings.HasPrefix(line, "CONTAINERD_VERSION"):
			currentKey = "Containerd"
		case strings.HasPrefix(line, "BUILDKIT_VERSION"):
			currentKey = "Buildkitd"
		case strings.HasPrefix(line, "NERDCTL_VERSION"):
			currentKey = "Nerdctl"
		case currentKey != "" && line != "":
			versions[currentKey] = shortVersion(line)
			currentKey = ""
		}
	}

	if versions["WSL"] == "" {
		versions["WSL"] = "—"
	}
	if versions["Containerd"] == "" {
		versions["Containerd"] = "—"
	}
	if versions["Buildkitd"] == "" {
		versions["Buildkitd"] = "—"
	}
	if versions["Nerdctl"] == "" {
		versions["Nerdctl"] = "—"
	}

	return versions
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
	// Проверяем наличие бинарников в WSL, а не их статус
	script := "" +
		"command -v containerd >/dev/null 2>&1 && echo 'CD:OK' || echo 'CD:NO'; " +
		"command -v nerdctl >/dev/null 2>&1 && echo 'NC:OK' || echo 'NC:NO'; " +
		"command -v buildctl >/dev/null 2>&1 && echo 'BK:OK' || echo 'BK:NO'; " +
		"wsl --version >/dev/null 2>&1 && echo 'WSL:OK' || echo 'WSL:NO'"

	out, err := runWSLWithTimeout(script, 10*time.Second)
	if err != nil {
		return false
	}

	installed := false
	lines := strings.Split(out, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, ":OK") {
			installed = true
		} else if strings.Contains(line, ":NO") {
			return false
		}
	}
	return installed
}

var installInProgress = false
var installCancel context.CancelFunc

// checkNetwork проверяет доступность интернета
func checkNetwork() bool {
	// Пробуем пингануть Google DNS — быстро и надёжно
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	cmd := exec.CommandContext(ctx, wsl.WslExecutable(), "-d", wsl.GetWslDistro(), wsl.GetShell(), "-c", 
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
	wslInstalled := false
	containersInstalled := false
	
	script := "" +
		"wsl --version >/dev/null 2>&1 && echo 'WSL:OK' || echo 'WSL:NO'; " +
		"command -v containerd >/dev/null 2>&1 && echo 'CD:OK' || echo 'CD:NO'; " +
		"command -v nerdctl >/dev/null 2>&1 && echo 'NC:OK' || echo 'NC:NO'; " +
		"command -v buildctl >/dev/null 2>&1 && echo 'BK:OK' || echo 'BK:NO'"
	
	out, _ := runWSLWithTimeout(script, 10*time.Second)
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "WSL:OK") {
			wslInstalled = true
		}
		if strings.Contains(line, "CD:OK") && strings.Contains(line, "NC:OK") && strings.Contains(line, "BK:OK") {
			containersInstalled = true
		}
	}
	
	// Показываем диалог подтверждения с информацией о том что будет установлено
	confirmMsg := "Вы уверены, что хотите установить компоненты?\n\n"
	if !wslInstalled {
		confirmMsg += "• WSL2 + Debian — будет установлено\n"
	} else {
		confirmMsg += "• WSL2 + Debian — уже установлено ✓\n"
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
				aptSuccess := false

				// Шаг 1: WSL2 + Debian — только если не установлен
				if !wslInstalled {
					progressLabel.SetText(i18n.T("status.install_wsl_debian"))
					logs = append(logs, "📦 "+i18n.T("status.install_wsl_debian"))

					// Быстрый таймаут для WSL установки
					wslCtx, wslCancel := context.WithTimeout(context.Background(), 2*time.Minute)
					defer wslCancel()
					
					wslCmd := exec.CommandContext(wslCtx, "powershell.exe", "-Command", "wsl --install Debian")
					wslCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
					wslOut, wslErr := wslCmd.CombinedOutput()
					
					if wslCtx.Err() == context.DeadlineExceeded {
						logs = append(logs, "  ❌ Таймаут: WSL установка не отвечает более 2 минут")
					} else if wslErr != nil {
						logs = append(logs, fmt.Sprintf("  ⚠️ WSL/Debian: %s", strings.TrimSpace(string(wslOut))))
					} else {
						logs = append(logs, "  ✅ WSL/Debian установлен")
						wslSuccess = true
					}
				} else {
					logs = append(logs, "📦 WSL2 + Debian — уже установлен, пропускаем")
					wslSuccess = true
				}

				// Шаг 2: Проверяем сеть перед apt install
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

					// Длинный таймаут для apt — но с возможностью отмены
					aptCtx, aptCancel := context.WithCancel(cancelCtx)
					defer aptCancel()
					
					installScript := "" +
						"sudo apt update && " +
						"sudo apt install -y containerd nerdctl buildkit && " +
						"sudo systemctl enable --now containerd && " +
						"sudo systemctl enable --now buildkit"

					cmd := exec.CommandContext(aptCtx, wsl.WslExecutable(), "-d", wsl.GetWslDistro(), wsl.GetShell(), "-c", installScript)
					cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
					out, err := cmd.CombinedOutput()
					output := strings.TrimSpace(string(out))

					if aptCtx.Err() == context.Canceled {
						logs = append(logs, "  ⏹ Установка отменена пользователем")
					} else if aptCtx.Err() == context.DeadlineExceeded {
						logs = append(logs, "  ❌ Таймаут: apt не отвечает более 5 минут")
					} else if err != nil {
						// Проверяем конкретную ошибку сети
						if strings.Contains(strings.ToLower(output), "could not resolve") ||
							strings.Contains(strings.ToLower(output), "temporary failure") ||
							strings.Contains(strings.ToLower(output), "network is unreachable") {
							logs = append(logs, "  ❌ Ошибка сети: не удалось подключиться к репозиториям\n\n"+
								"Проверьте подключение к интернету и попробуйте снова.")
						} else {
							logs = append(logs, fmt.Sprintf("  ❌ Ошибка установки: %v", err))
						}
						if output != "" {
							// Показываем только последние строки вывода
							lines := strings.Split(output, "\n")
							start := 0
							if len(lines) > 10 {
								start = len(lines) - 10
							}
							logs = append(logs, "  "+strings.Join(lines[start:], "\n  "))
						}
					} else {
						logs = append(logs, "  ✅ containerd, nerdctl, BuildKit установлены")
						aptSuccess = true
					}
				} else {
					logs = append(logs, "\n📦 containerd, nerdctl, BuildKit — уже установлены, пропускаем")
					aptSuccess = true
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

					if wslSuccess && aptSuccess {
						// Всё успешно — показываем сообщение об успехе
						dialog.ShowInformation(i18n.T("status.install_all_success"), resultMsg, win)
					} else if wslSuccess || aptSuccess {
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
		"containers_running": newMetricCard(i18n.T("status.metric_containers"), "📦"),
		"images":             newMetricCard(i18n.T("status.metric_images"), "🖼️"),
		"volumes":            newMetricCard(i18n.T("status.metric_volumes"), "💾"),
		"networks":           newMetricCard(i18n.T("status.metric_networks"), "🌐"),
	}

	lastCheckLabel := widget.NewLabel(i18n.T("status.last_check_never"))

	updateUI := func() {
		if !updateMu.TryLock() {
			return
		}
		defer updateMu.Unlock()

		statusCache.Lock()
		statusCache.timestamp = time.Time{}
		statusCache.Unlock()

		// WSL-запросы выполняются вне UI goroutine.
		statuses := getAllComponentsStatus()
		metricsSnapshot := getSystemMetrics()

		// Все изменения Fyne-виджетов — только через fyne.Do.
		safeUI(func() {
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
		})
	}

	btnRefresh := widget.NewButton(i18n.T("status.refresh"), func() { go updateUI() })

	tab := newTabActive(true, TickerAutoRefresh, func() {
		updateUI()
	})

	autoRefresh := widget.NewCheck(i18n.T("status.auto_refresh"), func(checked bool) {
		tab.SetActive(checked)
	})
	autoRefresh.Checked = true

	btnStartBuildkitd := widget.NewButton(i18n.T("status.start_buildkitd"), func() {
		go func() {
			select {
			case <-wsl.AppContext().Done():
				return
			default:
			}

			if err := wsl.StartBuildkitd(); err != nil {
				safeUI(func() { lastCheckLabel.SetText(i18n.T("common.error") + ": " + err.Error()) })
			} else {
				updateUI()
			}
		}()
	})

	btnStopBuildkitd := widget.NewButton(i18n.T("status.stop_buildkitd"), func() {
		go func() {
			select {
			case <-wsl.AppContext().Done():
				return
			default:
			}

			wsl.StopBuildkitd()
			updateUI()
		}()
	})

	// --- Ссылки на скачивание компонентов и кнопка установки ---
	installBtn := widget.NewButton(i18n.T("status.install_all_title"), func() {
		installAllComponents(win)
	})
	installBtn.Importance = widget.HighImportance
	
	// Если все компоненты уже установлены — делаем кнопку неактивной
	if areAllComponentsInstalled() {
		installBtn.Disable()
	}

	downloads := container.NewVBox(
		container.NewHBox(
			widget.NewHyperlink(i18n.T("status.download_wsl"), mustParseURL("https://learn.microsoft.com/ru-ru/windows/wsl/install")),
			widget.NewHyperlink(i18n.T("status.download_containerd"), mustParseURL("https://containerd.io/downloads/")),
			widget.NewHyperlink(i18n.T("status.download_nerdctl"), mustParseURL("https://github.com/containerd/nerdctl/releases")),
		),
		container.NewHBox(
			widget.NewHyperlink(i18n.T("status.download_buildkit"), mustParseURL("https://github.com/moby/buildkit/releases")),
			widget.NewHyperlink(i18n.T("status.download_cloudflared"), mustParseURL("https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/")),
		),
	)
	downloadsCard := widget.NewCard(i18n.T("status.downloads_title"), i18n.T("status.downloads_hint"), container.NewBorder(nil, nil, nil, installBtn, downloads))

	registerTabNamed(i18n.T("tabs.status"), tab)

	// Первичная проверка WSL запускается после построения UI.
	go updateUI()

	return withVerticalScroll(container.NewVBox(
		container.NewHBox(btnRefresh, autoRefresh, layout.NewSpacer(), lastCheckLabel),
		widget.NewSeparator(),
		container.NewAdaptiveGrid(4, wslCard.CanvasObject(), containerdCard.CanvasObject(), buildkitdCard.CanvasObject(), nerdctlCard.CanvasObject()),
		widget.NewSeparator(),
		widget.NewLabelWithStyle(i18n.T("status.overview"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		container.NewAdaptiveGrid(4,
			metrics["containers_running"].widget(),
			metrics["images"].widget(),
			metrics["volumes"].widget(),
			metrics["networks"].widget(),
		),
		widget.NewSeparator(),
		container.NewHBox(
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
