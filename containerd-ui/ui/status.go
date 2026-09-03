package ui

import (
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

// ============================================================================
// Модуль статуса компонентов (Полностью переписанный Dashboard)
// ============================================================================

// ComponentStatus описывает статус одного компонента
type ComponentStatus struct {
	Name    string
	Version string
	Icon    string
	Active  bool
	Detail  string
}

// statusCache кэширует результат проверки всех компонентов
var statusCache = struct {
	sync.RWMutex
	data      []ComponentStatus
	timestamp time.Time
	ttl       time.Duration
}{
	ttl: CacheStatus, // Кэш на 5 секунд
}

// runWSLWithTimeout — выполняет команду WSL с таймаутом (без кэширования)
// Возвращает stdout + stderr (объединённые) и ошибку (включая таймаут)
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

// getAllComponentsStatus — единый вызов WSL для проверки ВСЕХ компонентов
func getAllComponentsStatus() []ComponentStatus {
	// Проверяем кэш
	statusCache.RLock()
	if time.Since(statusCache.timestamp) < statusCache.ttl {
		result := make([]ComponentStatus, len(statusCache.data))
		copy(result, statusCache.data)
		statusCache.RUnlock()
		return result
	}
	statusCache.RUnlock()

	// Единый вызов WSL — все проверки в одном процессе (добавлен sudo для systemctl)
	svcName := wsl.GetSystemdService()
	cmd := "" +
		"echo 'WSL_CHECK_START'; " +
		"which wsl.exe > /dev/null 2>&1 && echo 'WSL:OK' || echo 'WSL:NO'; " +
		"sudo systemctl is-active " + svcName + " > /dev/null 2>&1 && echo 'CONTAINERD:OK' || echo 'CONTAINERD:NO'; " +
		"sudo buildctl --addr unix:///run/buildkit/buildkitd.sock debug workers > /dev/null 2>&1 && echo 'BUILDKIT:OK' || echo 'BUILDKIT:NO'; " +
		"which nerdctl > /dev/null 2>&1 && echo 'NERDCTL:OK' || echo 'NERDCTL:NO'; " +
		"echo 'WSL_CHECK_END'"

	out, err := runWSLWithTimeout(cmd, 5*time.Second) // Таймаут 5 секунд

	// Получаем версии компонентов
	versions := getComponentVersions()
	distro := wsl.GetWslDistro()
	versions["WSL"] = distro

	var result []ComponentStatus
	if err != nil {
		// Если таймаут или другая ошибка — помечаем все компоненты как недоступные
		result = []ComponentStatus{
			{Name: "WSL", Version: versions["WSL"], Icon: "❌", Active: false, Detail: "Таймаут/ошибка проверки"},
			{Name: "Containerd", Version: versions["Containerd"], Icon: "⚠️", Active: false, Detail: "Недоступен"},
			{Name: "Buildkitd", Version: versions["Buildkitd"], Icon: "⚠️", Active: false, Detail: "Недоступен"},
			{Name: "Nerdctl", Version: versions["Nerdctl"], Icon: "❌", Active: false, Detail: "Недоступен"},
		}
	} else {
		lines := strings.Split(out, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)

			if strings.Contains(line, "WSL_CHECK_START") || strings.Contains(line, "WSL_CHECK_END") {
				continue
			}

			if strings.Contains(line, "WSL:OK") {
				result = append(result, ComponentStatus{Name: "WSL", Version: distro, Icon: "✅", Active: true, Detail: "активен"})
			} else if strings.Contains(line, "WSL:NO") {
				result = append(result, ComponentStatus{Name: "WSL", Version: distro, Icon: "⚠️", Active: false, Detail: "остановлен"})
			}

			if strings.Contains(line, "CONTAINERD:OK") {
				result = append(result, ComponentStatus{Name: "Containerd", Version: versions["Containerd"], Icon: "✅", Active: true, Detail: "gRPC активен"})
			} else if strings.Contains(line, "CONTAINERD:NO") {
				result = append(result, ComponentStatus{Name: "Containerd", Version: versions["Containerd"], Icon: "⚠️", Active: false, Detail: "Не запущен"})
			}

			if strings.Contains(line, "BUILDKIT:OK") {
				result = append(result, ComponentStatus{Name: "Buildkitd", Version: versions["Buildkitd"], Icon: "✅", Active: true, Detail: "Доступен"})
			} else if strings.Contains(line, "BUILDKIT:NO") {
				result = append(result, ComponentStatus{Name: "Buildkitd", Version: versions["Buildkitd"], Icon: "⚠️", Active: false, Detail: "Остановлен"})
			}

			if strings.Contains(line, "NERDCTL:OK") {
				result = append(result, ComponentStatus{Name: "Nerdctl", Version: versions["Nerdctl"], Icon: "✅", Active: true, Detail: "Доступен"})
			} else if strings.Contains(line, "NERDCTL:NO") {
				result = append(result, ComponentStatus{Name: "Nerdctl", Version: versions["Nerdctl"], Icon: "❌", Active: false, Detail: "Не найден"})
			}
		}
	}

	// Сохраняем в кэш
	statusCache.Lock()
	statusCache.data = result
	statusCache.timestamp = time.Now()
	statusCache.Unlock()

	return result
}

// versionEntry хранит версию одного компонента
type versionEntry struct {
	version string
	ok      bool
}

// getComponentVersions получает версии всех компонентов за ОДИН вызов WSL
func getComponentVersions() map[string]string {
	versions := make(map[string]string)

	// Один вызов WSL — все версии в одном процессе
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

		// Определяем, для какого компонента эта строка
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

	// Fallback: если версия не найдена
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

// shortVersion обрезает вывод до короткого вида (имя + версия)
// Пример: "containerd github.com/containerd/containerd/v2 v2.1.4-0.20250430162418-..." → "v2.1.4"
// Пример: "buildctl github.com/moby/buildkit v0.15.1" → "v0.15.1"
func shortVersion(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "—"
	}
	// Ищем последовательность цифр, разделённых точками (три группы)
	re := regexp.MustCompile(`v?(\d+\.\d+\.\d+)`)
	match := re.FindStringSubmatch(raw)
	if len(match) > 0 {
		// match[0] — полное совпадение (например, "v2.1.4" или "2.1.4")
		if strings.HasPrefix(match[0], "v") {
			return match[0]
		}
		return "v" + match[1]
	}
	// fallback: если ничего не найдено, возвращаем первые два слова
	parts := strings.Fields(raw)
	if len(parts) >= 2 {
		return parts[0] + " " + parts[1]
	}
	return raw
}

// BuildStatusTab — главная функция построения вкладки статуса (Панель приборов)
func BuildStatusTab() fyne.CanvasObject {
	// Создаём компонентные карточки, которые скрывают версии при сжатии.
	wslCard := newResponsiveStatusCard("WSL")
	containerdCard := newResponsiveStatusCard("Containerd")
	buildkitdCard := newResponsiveStatusCard("Buildkitd")
	nerdctlCard := newResponsiveStatusCard("Nerdctl")

	// Идеально ровная таблица (без HBox внутри ячеек)
	table := widget.NewTable(
		func() (int, int) {
			statusCache.RLock()
			defer statusCache.RUnlock()
			return len(statusCache.data) + 1, 3
		},
		func() fyne.CanvasObject {
			label := widget.NewLabel("")
			label.Wrapping = fyne.TextTruncate
			return label
		},
		func(id widget.TableCellID, o fyne.CanvasObject) {
			label := o.(*widget.Label)
			statusCache.RLock()
			defer statusCache.RUnlock()

			if id.Row == 0 {
				label.TextStyle = fyne.TextStyle{Bold: true}
				switch id.Col {
				case 0:
					label.SetText("Компонент")
				case 1:
					label.SetText("Версия")
				case 2:
					label.SetText("Статус")
				}
				return
			}

			idx := id.Row - 1
			if idx >= len(statusCache.data) {
				return
			}

			cs := statusCache.data[idx]
			switch id.Col {
			case 0:
				label.SetText(cs.Name)
			case 1:
				if cs.Version != "" {
					label.SetText(cs.Version)
				} else {
					label.SetText("—")
				}
			case 2:
				label.SetText(cs.Icon + " " + wsl.TranslateStatus(cs.Detail))
			}
		},
	)
	table.SetColumnWidth(0, 110)
	table.SetColumnWidth(1, 130)
	table.SetColumnWidth(2, 190)

	// Метка последней проверки
	lastCheckLabel := widget.NewLabel("Последняя проверка: —")

	// Функция обновления статуса (атомарное обновление)
	updateUI := func() {
		statusCache.Lock()
		statusCache.timestamp = time.Time{} // Сбрасываем кэш
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
		lastCheckLabel.SetText("Последняя проверка: " + time.Now().Format("15:04:05"))
		table.Refresh()
	}

	// Кнопки управления
	btnRefresh := widget.NewButton("🔄 Обновить", func() { go updateUI() })

	// Управляем активностью вкладки: тикер останавливается при скрытии
	tab := newTabActive(true, TickerAutoRefresh, func() {
		updateUI()
	})

	// Регистрируем в глобальной карте по имени вкладки (см. строку 257)

	autoRefresh := widget.NewCheck("Автообновление (30с)", func(checked bool) {
		tab.SetActive(checked)
	})
	autoRefresh.Checked = true

	btnStartBuildkitd := widget.NewButton("▶ Запустить Buildkitd", func() {
		go func() {
			if err := wsl.StartBuildkitd(); err != nil {
				lastCheckLabel.SetText("Ошибка: " + err.Error())
			} else {
				updateUI()
			}
		}()
	})

	btnStopBuildkitd := widget.NewButton("⏹ Остановить Buildkitd", func() {
		go func() {
			wsl.StopBuildkitd()
			updateUI()
		}()
	})

	// Первый запуск при открытии вкладки
	updateUI()

	// Регистрируем tabActive в глобальной карте по имени вкладки
	registerTabNamed("Статус", tab)

	return withVerticalScroll(container.NewBorder(
		container.NewVBox(
			// Панель инструментов
			container.NewHBox(btnRefresh, autoRefresh, layout.NewSpacer(), lastCheckLabel),
			widget.NewSeparator(),
			// Дашборд из 4 карточек
			container.NewAdaptiveGrid(4, wslCard.CanvasObject(), containerdCard.CanvasObject(), buildkitdCard.CanvasObject(), nerdctlCard.CanvasObject()),
			// Панель управления Buildkitd
			container.NewHBox(
				widget.NewLabelWithStyle("Управление Buildkitd:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
				btnStartBuildkitd, btnStopBuildkitd,
			),
			widget.NewSeparator(),
			// Заголовок таблицы
			widget.NewLabelWithStyle("Детали компонентов:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		),
		nil, nil, nil,
		// Сама таблица
		table,
	))
}
