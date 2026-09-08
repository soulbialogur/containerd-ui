package ui

import (
	"containerd-ui/i18n"
	"containerd-ui/wsl"
	"context"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
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

	out, err := runWSLWithTimeout(script, 5*time.Second)
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

	cmd := exec.CommandContext(ctx, "wsl", "-d", wsl.GetWslDistro(), "bash", "-c", command)
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
		"which wsl.exe > /dev/null 2>&1 && echo 'WSL:OK' || echo 'WSL:NO'; " +
		"sudo systemctl is-active " + svcName + " > /dev/null 2>&1 && echo 'CONTAINERD:OK' || echo 'CONTAINERD:NO'; " +
		"sudo buildctl --addr unix:///run/buildkit/buildkitd.sock debug workers > /dev/null 2>&1 && echo 'BUILDKIT:OK' || echo 'BUILDKIT:NO'; " +
		"which nerdctl > /dev/null 2>&1 && echo 'NERDCTL:OK' || echo 'NERDCTL:NO'; " +
		"echo 'WSL_CHECK_END'"

	out, err := runWSLWithTimeout(cmd, 5*time.Second)

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
		"echo 'CONTAINERD_VERSION'; sudo containerd --version 2>/dev/null; " +
		"echo 'BUILDKIT_VERSION'; buildctl --version 2>/dev/null; " +
		"echo 'NERDCTL_VERSION'; nerdctl --version 2>/dev/null"

	out, err := runWSLWithTimeout(script, 10*time.Second)
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

func BuildStatusTab() fyne.CanvasObject {
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

	updateMetrics := func() {
		m := getSystemMetrics()
		for k, mc := range metrics {
			if v, ok := m[k]; ok {
				mc.setValue(v)
			}
		}
	}

	lastCheckLabel := widget.NewLabel(i18n.T("status.last_check_never"))

	updateUI := func() {
		statusCache.Lock()
		statusCache.timestamp = time.Time{}
		statusCache.Unlock()

		statuses := getAllComponentsStatus()
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
		lastCheckLabel.SetText(i18n.T("status.last_check", time.Now().Format("15:04:05")))
		updateMetrics()
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
				lastCheckLabel.SetText(i18n.T("common.error") + ": " + err.Error())
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

	updateUI()

	registerTabNamed(i18n.T("tabs.status"), tab)

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
	))
}
