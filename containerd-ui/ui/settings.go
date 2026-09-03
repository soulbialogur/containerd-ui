package ui

import (
	"containerd-ui/wsl"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// ScrollableEntry — кастомное поле ввода, которое не блокирует прокрутку.
type ScrollableEntry struct {
	widget.Entry
}

func NewScrollableEntry() *ScrollableEntry {
	entry := &ScrollableEntry{}
	entry.Entry.ExtendBaseWidget(entry)
	return entry
}

func (e *ScrollableEntry) Scrolled(_ *fyne.ScrollEvent) {
	// Пустой — прокрутка уходит к родителю
}

func makeSettingEntry(placeHolder string) *ScrollableEntry {
	entry := NewScrollableEntry()
	entry.SetPlaceHolder(placeHolder)
	return entry
}

func makeSettingRow(entry *ScrollableEntry) fyne.CanvasObject {
	return container.NewMax(entry)
}

func deploymentProxyUIValue(configValue string) string {
	switch configValue {
	case "cloudflare":
		return "Cloudflare Tunnel"
	default:
		return "Traefik + Let's Encrypt"
	}
}

func deploymentProxyConfigValue(uiValue string) string {
	switch uiValue {
	case "Cloudflare Tunnel":
		return "cloudflare"
	default:
		return "traefik"
	}
}

// BuildSettingsTab — переработанная вкладка настроек с карточками.
func BuildSettingsTab(win fyne.Window) fyne.CanvasObject {
	config, _ := wsl.LoadConfig()

	// Поля ввода
	entryPath := makeSettingEntry("Путь к папке с docker-compose.yml")
	entryPath.SetText(config.ProjectPath)

	entryDistro := makeSettingEntry("Имя WSL-дистрибутива")
	entryDistro.SetText(config.WslDistro)

	entryCdPort := makeSettingEntry("Порт gRPC-прокси")
	entryCdPort.SetText(strconv.Itoa(config.CdPort))

	entryCdNamespace := makeSettingEntry("Namespace containerd")
	entryCdNamespace.SetText(config.CdNamespace)

	entryLogTail := makeSettingEntry("Количество строк логов")
	entryLogTail.SetText(strconv.Itoa(config.LogTail))

	entryCacheTTL := makeSettingEntry("TTL кэша WSL (сек)")
	entryCacheTTL.SetText(strconv.Itoa(config.WslCacheTTL))

	entryMaxWSLCacheSize := makeSettingEntry("Максимальный размер кэша WSL (байт)")
	entryMaxWSLCacheSize.SetText(strconv.FormatInt(config.MaxWSLCacheSize, 10))

	entryWSLCacheCleanupAt := makeSettingEntry("Порог очистки кэша WSL (записей)")
	entryWSLCacheCleanupAt.SetText(strconv.Itoa(config.WSLCacheCleanupAt))

	entryRefreshInterval := makeSettingEntry("Интервал обновления (сек)")
	entryRefreshInterval.SetText(strconv.Itoa(config.AutoRefreshInterval))

	entryIdleStopMinutes := makeSettingEntry("Автоостановка демона после простоя (мин)")
	entryIdleStopMinutes.SetText(strconv.Itoa(config.IdleDaemonStopMinutes))

	checkEconomyMode := widget.NewCheck("Режим экономии ресурсов", nil)
	checkEconomyMode.SetChecked(config.EconomyMode)

	// Лимиты по умолчанию для контейнеров
	entryCPU := makeSettingEntry("Лимит CPU (например: 0.5, 1.5, 2)")
	entryCPU.SetText(config.DefaultCPU)

	entryMemory := makeSettingEntry("Лимит памяти (например: 512m, 1g, 2g)")
	entryMemory.SetText(config.DefaultMemory)

	// Параллельная сборка
	entryMaxParallel := makeSettingEntry("Параллельные сборки (0 = без ограничений)")
	entryMaxParallel.SetText(strconv.Itoa(config.MaxParallelism))

	entryContainerConcurrency := makeSettingEntry("Параллельные операции контейнеров")
	entryContainerConcurrency.SetText(strconv.Itoa(config.ContainerOperationConcurrency))

	// BuildKit кэш
	entryBuildkitTTL := makeSettingEntry("Очистка кэша старше (часов, 0 = отключено)")
	entryBuildkitTTL.SetText(strconv.Itoa(config.BuildkitCacheTTL))

	entryBuildkitSize := makeSettingEntry("Макс. размер кэша (например: 5g, 10g)")
	entryBuildkitSize.SetText(config.BuildkitMaxSize)

	// Прокси для деплоя
	proxyRadio := widget.NewRadioGroup([]string{"Traefik + Let's Encrypt", "Cloudflare Tunnel"}, nil)
	proxyRadio.Horizontal = true
	proxyRadio.SetSelected(deploymentProxyUIValue(config.DeploymentProxy))
	if config.DeploymentProxy == "" || (config.DeploymentProxy != "traefik" && config.DeploymentProxy != "cloudflare") {
		proxyRadio.SetSelected("Traefik + Let's Encrypt")
	}
	proxyHint := widget.NewLabel("💡 Traefik — бесплатный SSL через Let's Encrypt; Cloudflare — через Tunnel, без открытых портов")
	proxyHint.TextStyle = fyne.TextStyle{Italic: true}

	// Имена сервисов и порты для деплоя
	entryBackendService := makeSettingEntry("Имя сервиса backend (в docker-compose.yml)")
	entryBackendService.SetText(config.DeployServiceBackend)
	if config.DeployServiceBackend == "" {
		entryBackendService.SetText("backend")
	}

	entryBackendPort := makeSettingEntry("Порт backend для маршрутизации")
	entryBackendPort.SetText(strconv.Itoa(config.DeployServiceBackendPort))
	if config.DeployServiceBackendPort == 0 {
		entryBackendPort.SetText("8000")
	}

	entryDeployEmail := makeSettingEntry("Email для Let's Encrypt")
	entryDeployEmail.SetPlaceHolder("admin@your-domain.com")
	if config.DeployEmail != "" {
		entryDeployEmail.SetText(config.DeployEmail)
	}

	entryDeployNetwork := makeSettingEntry("Имя внешней сети для деплоя")
	entryDeployNetwork.SetText(config.DeployNetwork)
	if config.DeployNetwork == "" {
		entryDeployNetwork.SetText("soul-dialogue")
	}

	entryFrontendService := makeSettingEntry("Имя сервиса frontend (в docker-compose.yml)")
	entryFrontendService.SetText(config.DeployServiceFrontend)
	if config.DeployServiceFrontend == "" {
		entryFrontendService.SetText("frontend")
	}

	entryFrontendPort := makeSettingEntry("Порт frontend для маршрутизации")
	entryFrontendPort.SetText(strconv.Itoa(config.DeployServiceFrontendPort))
	if config.DeployServiceFrontendPort == 0 {
		entryFrontendPort.SetText("80")
	}
	serviceHint := widget.NewLabel("💡 Должны совпадать с именами сервисов и портами внутри docker-compose.yml")
	serviceHint.TextStyle = fyne.TextStyle{Italic: true}

	// Настройки сборки
	checkSquash := widget.NewCheck("Объединить слои (--squash)", nil)
	checkSquash.SetChecked(config.SquashLayers)

	compressionRadio := widget.NewRadioGroup([]string{"gzip", "zstd", "none"}, nil)
	compressionRadio.Horizontal = true
	compressionRadio.SetSelected(config.Compression)

	compressionLevel := widget.NewSlider(1, 9)
	compressionLevel.SetValue(float64(config.CompressionLevel))
	compressionLevel.Step = 1
	// Устанавливаем начальное состояние в зависимости от выбранного алгоритма
	if config.Compression == "gzip" || config.Compression == "zstd" {
		compressionLevel.Enable()
	} else {
		compressionLevel.Disable()
	}
	compressionLevelLabel := widget.NewLabel(fmt.Sprintf("Уровень сжатия: %.0f", float64(config.CompressionLevel)))

	compressionLevel.OnChanged = func(value float64) {
		compressionLevelLabel.SetText(fmt.Sprintf("Уровень сжатия: %.0f", value))
	}

	compressionRadio.OnChanged = func(value string) {
		if value == "gzip" || value == "zstd" {
			compressionLevel.Enable()
		} else {
			compressionLevel.Disable()
		}
	}

	// Кнопки
	btnOpenExplorer := widget.NewButton("📁 Открыть проводник", nil)
	btnCheckPath := widget.NewButton("✅ Проверить путь", nil)
	btnDetect := widget.NewButton("🔍 Автоопределение", nil)
	btnSave := widget.NewButton("💾 Сохранить", nil)
	btnReset := widget.NewButton("🔄 Сбросить", nil)

	// Обновление UI после загрузки или сброса
	updateUI := func() {
		cfg, _ := wsl.LoadConfig()
		entryPath.SetText(cfg.ProjectPath)
		entryDistro.SetText(cfg.WslDistro)
		entryCdPort.SetText(strconv.Itoa(cfg.CdPort))
		entryCdNamespace.SetText(cfg.CdNamespace)
		entryLogTail.SetText(strconv.Itoa(cfg.LogTail))
		entryCacheTTL.SetText(strconv.Itoa(cfg.WslCacheTTL))
		entryMaxWSLCacheSize.SetText(strconv.FormatInt(cfg.MaxWSLCacheSize, 10))
		entryWSLCacheCleanupAt.SetText(strconv.Itoa(cfg.WSLCacheCleanupAt))
		entryRefreshInterval.SetText(strconv.Itoa(cfg.AutoRefreshInterval))
		entryIdleStopMinutes.SetText(strconv.Itoa(cfg.IdleDaemonStopMinutes))
		checkEconomyMode.SetChecked(cfg.EconomyMode)
		entryCPU.SetText(cfg.DefaultCPU)
		entryMemory.SetText(cfg.DefaultMemory)
		entryMaxParallel.SetText(strconv.Itoa(cfg.MaxParallelism))
		entryContainerConcurrency.SetText(strconv.Itoa(cfg.ContainerOperationConcurrency))
		entryBuildkitTTL.SetText(strconv.Itoa(cfg.BuildkitCacheTTL))
		entryBuildkitSize.SetText(cfg.BuildkitMaxSize)
		checkSquash.SetChecked(cfg.SquashLayers)
		compressionRadio.SetSelected(cfg.Compression)
		compressionLevel.SetValue(float64(cfg.CompressionLevel))
		compressionLevelLabel.SetText(fmt.Sprintf("Уровень сжатия: %.0f", float64(cfg.CompressionLevel)))
		if cfg.Compression == "gzip" || cfg.Compression == "zstd" {
			compressionLevel.Enable()
		} else {
			compressionLevel.Disable()
		}
		proxyRadio.SetSelected(deploymentProxyUIValue(cfg.DeploymentProxy))
		if cfg.DeployEmail != "" {
			entryDeployEmail.SetText(cfg.DeployEmail)
		} else {
			entryDeployEmail.SetText("")
		}
		entryBackendService.SetText(cfg.DeployServiceBackend)
		entryBackendPort.SetText(strconv.Itoa(cfg.DeployServiceBackendPort))
		entryFrontendService.SetText(cfg.DeployServiceFrontend)
		entryFrontendPort.SetText(strconv.Itoa(cfg.DeployServiceFrontendPort))
		entryDeployNetwork.SetText(cfg.DeployNetwork)
	}

	// Обработчики кнопок
	btnOpenExplorer.OnTapped = func() {
		go func() {
			select {
			case <-wsl.AppContext().Done():
				return
			default:
			}
			// Открываем домашнюю директорию пользователя — она переносима
			cmd := exec.Command("explorer.exe")
			cmd.Start()
		}()
	}

	btnCheckPath.OnTapped = func() {
		path := entryPath.Text
		if path == "" {
			dialog.ShowInformation("Нет пути", "Сначала введите путь в поле выше.", win)
			return
		}

		// Нормализуем путь
		path = filepath.Clean(path)

		// Проверяем существование папки через os.Stat (работает с UTF-8, пробелами, кириллицей)
		dirInfo, err := os.Stat(path)
		if err != nil || !dirInfo.IsDir() {
			dialog.ShowCustom("Ошибка", "ОК", widget.NewLabel(fmt.Sprintf("Папка не найдена: %s\n%s", path, err)), win)
			return
		}

		// Проверяем наличие docker-compose.yml или compose.yaml
		composeExists := false
		composeFiles := []string{"compose.yaml", "docker-compose.yml"}
		for _, name := range composeFiles {
			filePath := filepath.Join(path, name)
			if _, err := os.Stat(filePath); err == nil {
				composeExists = true
				break
			}
		}

		if composeExists {
			dialog.ShowCustom("Путь проверен", "ОК",
				widget.NewLabel(fmt.Sprintf("✅ Путь корректен:\n%s\n\nНайден файл docker-compose.yml или compose.yaml", path)),
				win)
		} else {
			dialog.ShowCustom("Путь проверен", "ОК",
				widget.NewLabel(fmt.Sprintf("⚠️ Папка найдена:\n%s\n\nНО не найден docker-compose.yml или compose.yaml\n\nПроект может быть настроен иначе.", path)),
				win)
		}
	}

	btnDetect.OnTapped = func() {
		btnDetect.Disable()
		btnDetect.SetText("🔍 Поиск...")
		btnDetect.Refresh()

		go func() {
			select {
			case <-wsl.AppContext().Done():
				return
			default:
			}

			path := wsl.DetectProjectPath()
			if path != "" {
				err := wsl.SetProjectPath(path)
				if err != nil {
					dialog.ShowError(err, win)
				} else {
					updateUI()
					dialog.ShowCustom("Найдено", "ОК", widget.NewLabel("Проект найден: "+path), win)
				}
			} else {
				dialog.ShowCustom("Не найдено", "ОК", widget.NewLabel("Не удалось автоматически определить путь к проекту. Укажите вручную."), win)
			}
			btnDetect.Enable()
			btnDetect.SetText("🔍 Автоопределение")
			btnDetect.Refresh()
		}()
	}

	btnSave.OnTapped = func() {
		cfg, _ := wsl.LoadConfig()

		if entryPath.Text != "" {
			cfg.ProjectPath = entryPath.Text
		}
		if distro := entryDistro.Text; distro != "" {
			cfg.WslDistro = distro
		}
		if portStr := entryCdPort.Text; portStr != "" {
			if port, err := strconv.Atoi(portStr); err == nil {
				cfg.CdPort = port
			}
		}
		if namespace := strings.TrimSpace(entryCdNamespace.Text); namespace != "" {
			cfg.CdNamespace = namespace
		}
		if tailStr := entryLogTail.Text; tailStr != "" {
			if tail, err := strconv.Atoi(tailStr); err == nil {
				cfg.LogTail = tail
			}
		}
		if ttlStr := entryCacheTTL.Text; ttlStr != "" {
			if ttl, err := strconv.Atoi(ttlStr); err == nil {
				cfg.WslCacheTTL = ttl
			}
		}
		if maxSizeStr := entryMaxWSLCacheSize.Text; maxSizeStr != "" {
			if maxSize, err := strconv.ParseInt(maxSizeStr, 10, 64); err == nil && maxSize > 0 {
				cfg.MaxWSLCacheSize = maxSize
			}
		}
		if cleanupAtStr := entryWSLCacheCleanupAt.Text; cleanupAtStr != "" {
			if cleanupAt, err := strconv.Atoi(cleanupAtStr); err == nil && cleanupAt > 0 {
				cfg.WSLCacheCleanupAt = cleanupAt
			}
		}
		if refreshStr := entryRefreshInterval.Text; refreshStr != "" {
			if refresh, err := strconv.Atoi(refreshStr); err == nil {
				cfg.AutoRefreshInterval = refresh
			}
		}
		if idleStopStr := entryIdleStopMinutes.Text; idleStopStr != "" {
			if idleStop, err := strconv.Atoi(idleStopStr); err == nil && idleStop > 0 {
				cfg.IdleDaemonStopMinutes = idleStop
			}
		}
		cfg.EconomyMode = checkEconomyMode.Checked

		// Лимиты контейнеров
		cfg.DefaultCPU = entryCPU.Text
		cfg.DefaultMemory = entryMemory.Text

		if parallelStr := entryMaxParallel.Text; parallelStr != "" {
			if parallel, err := strconv.Atoi(parallelStr); err == nil && parallel >= 0 {
				cfg.MaxParallelism = parallel
			}
		}
		if concurrencyStr := entryContainerConcurrency.Text; concurrencyStr != "" {
			if concurrency, err := strconv.Atoi(concurrencyStr); err == nil && concurrency >= 1 {
				cfg.ContainerOperationConcurrency = concurrency
			}
		}

		if ttlStr := entryBuildkitTTL.Text; ttlStr != "" {
			if ttl, err := strconv.Atoi(ttlStr); err == nil && ttl >= 0 {
				cfg.BuildkitCacheTTL = ttl
			}
		}
		cfg.BuildkitMaxSize = entryBuildkitSize.Text

		cfg.SquashLayers = checkSquash.Checked
		cfg.Compression = compressionRadio.Selected
		cfg.CompressionLevel = int(compressionLevel.Value)
		cfg.DeploymentProxy = deploymentProxyConfigValue(proxyRadio.Selected)
		cfg.DeployEmail = strings.TrimSpace(entryDeployEmail.Text)
		cfg.DeployServiceBackend = strings.TrimSpace(entryBackendService.Text)
		cfg.DeployServiceFrontend = strings.TrimSpace(entryFrontendService.Text)
		if network := strings.TrimSpace(entryDeployNetwork.Text); network != "" {
			cfg.DeployNetwork = network
		}
		if port, err := strconv.Atoi(entryBackendPort.Text); err == nil && port > 0 {
			cfg.DeployServiceBackendPort = port
		}
		if port, err := strconv.Atoi(entryFrontendPort.Text); err == nil && port > 0 {
			cfg.DeployServiceFrontendPort = port
		}

		if err := wsl.SaveConfig(cfg); err != nil {
			dialog.ShowError(err, win)
			return
		}

		wsl.InitConfigCache(cfg)
		SetEconomyMode(cfg.EconomyMode)
		if cfg.IdleDaemonStopMinutes > 0 {
			wsl.SetIdleDaemonThresholdForRuntime(cfg.IdleDaemonStopMinutes)
		}
		dialog.ShowCustom("Сохранено", "ОК", widget.NewLabel("Конфигурация успешно сохранена в config.json"), win)
	}

	btnReset.OnTapped = func() {
		confirmDialog := dialog.NewCustomConfirm(
			"Сброс",
			"ОК",
			"Отмена",
			widget.NewLabel("Сбросить все настройки к значениям по умолчанию?"),
			func(confirmed bool) {
				if !confirmed {
					return
				}
				cfg := wsl.DefaultConfig()
				if err := wsl.SaveConfig(cfg); err != nil {
					dialog.ShowError(err, win)
					return
				}
				wsl.InitConfigCache(cfg)
				SetEconomyMode(cfg.EconomyMode)
				updateUI()
				dialog.ShowCustom("Сброшено", "ОК", widget.NewLabel("Настройки сброшены к значениям по умолчанию"), win)
			},
			win,
		)
		confirmDialog.Show()
	}

	// Информация
	infoText := "Здесь можно настроить все параметры приложения.\n\n" +
		"📁 Путь к проекту — откройте проводник, скопируйте путь и вставьте в поле\n" +
		"✅ Проверить путь — проверит существование папки и наличие docker-compose.yml\n" +
		"🔍 Автоопределение — автоматически найти docker-compose.yml\n" +
		"🐧 WSL-дистрибутив — имя дистрибутива WSL (по умолчанию: Ubuntu-24.04)\n" +
		"🔌 gRPC-порт — порт для подключения к containerd\n" +
		"📝 Лог-тайл — количество строк логов при отображении\n" +
		"⏱️ TTL кэша — время жизни кэша WSL-команд (сек)\n" +
		"🔄 Автообновление — интервал обновления списка контейнеров (сек)\n\n" +
		"⚙️ Сборка образов:\n" +
		"  📦 --squash — объединяет все слои в один (уменьшает размер)\n" +
		"  🗜️ --compression — алгоритм сжатия (gzip, zstd, none)\n" +
		"  🎚️ --compression-level — уровень сжатия 1-9 (9 = максимальное)\n\n" +
		"Файл конфигурации: config.json (в папке с приложением)"

	infoLabel := widget.NewLabel(infoText)
	infoLabel.Wrapping = fyne.TextTruncate

	// ----- Карточки -----
	// 1. Основные настройки
	basicCard := widget.NewCard("Основные настройки", "",
		container.NewVBox(
			widget.NewLabelWithStyle("Путь к проекту", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryPath),
			container.NewHBox(btnOpenExplorer, btnCheckPath, btnDetect),
			widget.NewSeparator(),

			widget.NewLabelWithStyle("WSL-дистрибутив", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryDistro),
			widget.NewSeparator(),

			widget.NewLabelWithStyle("gRPC-порт containerd", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryCdPort),
			widget.NewSeparator(),

			widget.NewLabelWithStyle("Namespace containerd", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryCdNamespace),
			widget.NewSeparator(),

			widget.NewLabelWithStyle("Количество строк логов", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryLogTail),
			widget.NewSeparator(),

			widget.NewLabelWithStyle("TTL кэша WSL (секунды)", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryCacheTTL),
			widget.NewSeparator(),

			widget.NewLabelWithStyle("Максимальный размер кэша WSL (байт)", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryMaxWSLCacheSize),
			widget.NewSeparator(),

			widget.NewLabelWithStyle("Порог очистки кэша WSL (записей)", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryWSLCacheCleanupAt),
			widget.NewSeparator(),

			widget.NewLabelWithStyle("Интервал автообновления (секунды)", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryRefreshInterval),
			widget.NewSeparator(),
			widget.NewLabelWithStyle("Автоостановка демона после простоя (минуты)", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryIdleStopMinutes),
		),
	)

	// 2. Настройки сборки образов
	buildCard := widget.NewCard("Настройки сборки образов", "",
		container.NewVBox(
			checkSquash,
			widget.NewSeparator(),
			widget.NewLabelWithStyle("Алгоритм сжатия", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			compressionRadio,
			container.NewHBox(compressionLevel, compressionLevelLabel),
			container.NewHBox(
				widget.NewLabel("💡 zstd — более эффективный, чем gzip"),
			),
		),
	)

	// 2b. Лимиты по умолчанию для контейнеров
	cpuHint := widget.NewLabel("💡 0.5 = 50% CPU, 2 = 2 ядра")
	cpuHint.TextStyle = fyne.TextStyle{Italic: true}
	memoryHint := widget.NewLabel("💡 512m = 512 МБ, 1g = 1 ГБ, оставьте пустым для без лимита")
	memoryHint.TextStyle = fyne.TextStyle{Italic: true}
	parallelHint := widget.NewLabel("💡 0 = без ограничений, 4 = до 4 параллельных сборок")
	parallelHint.TextStyle = fyne.TextStyle{Italic: true}
	buildkitTTLLimit := widget.NewLabel("💡 24 = очищать кэш старше 24 часов, 0 = отключить")
	buildkitTTLLimit.TextStyle = fyne.TextStyle{Italic: true}
	buildkitSizeLimit := widget.NewLabel("💡 5g = 5 ГБ, 10g = 10 ГБ, оставьте пустым для без лимита")
	buildkitSizeLimit.TextStyle = fyne.TextStyle{Italic: true}

	// 2c. Прокси для деплоя
	proxyCard := widget.NewCard("Прокси для деплоя", "",
		container.NewVBox(
			proxyRadio,
			proxyHint,
		),
	)

	// 2d. Имена сервисов для деплоя
	serviceCard := widget.NewCard("Имена сервисов", "Должны совпадать с именами в docker-compose.yml",
		container.NewVBox(
			widget.NewLabelWithStyle("Имя сервиса backend", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryBackendService),
			widget.NewSeparator(),
			widget.NewLabelWithStyle("Имя сервиса frontend", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryFrontendService),
			serviceHint,
		),
	)

	serviceCard.Content = container.NewVBox(
		widget.NewLabelWithStyle("Имя внешней сети", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		makeSettingRow(entryDeployNetwork),
		widget.NewLabel("Сеть должна быть объявлена в compose как external: true и подключена к сервисам."),
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Имя сервиса backend", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		makeSettingRow(entryBackendService),
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Имя сервиса frontend", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		makeSettingRow(entryFrontendService),
		serviceHint,
	)

	limitCard := widget.NewCard("Лимиты контейнеров", "Задайте лимиты, которые будут применяться при запуске новых контейнеров",
		container.NewVBox(
			widget.NewLabelWithStyle("Лимит CPU", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryCPU),
			cpuHint,
			widget.NewSeparator(),
			widget.NewLabelWithStyle("Лимит памяти", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryMemory),
			memoryHint,
			widget.NewSeparator(),
			widget.NewLabelWithStyle("Параллельная сборка", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryMaxParallel),
			parallelHint,
			widget.NewSeparator(),
			widget.NewLabelWithStyle("Параллельные операции контейнеров", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryContainerConcurrency),
			widget.NewLabel("💡 Максимум одновременно запускаемых или останавливаемых контейнеров"),
			widget.NewSeparator(),
			widget.NewLabelWithStyle("Очистка кэша BuildKit", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryBuildkitTTL),
			buildkitTTLLimit,
			widget.NewSeparator(),
			widget.NewLabelWithStyle("Максимальный размер кэша", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			makeSettingRow(entryBuildkitSize),
			buildkitSizeLimit,
		),
	)

	// 3. Управление и информация
	actionsCard := widget.NewCard("Управление", "",
		container.NewVBox(
			container.NewHBox(btnSave, btnReset),
			checkEconomyMode,
			widget.NewSeparator(),
			infoLabel,
		),
	)

	// Собираем всё с небольшими отступами
	content := container.NewVBox(
		container.NewPadded(basicCard),
		container.NewPadded(buildCard),
		container.NewPadded(proxyCard),
		container.NewPadded(serviceCard),
		container.NewPadded(limitCard),
		container.NewPadded(actionsCard),
	)

	return container.NewBorder(
		nil, nil, nil, nil,
		container.NewScroll(content),
	)
}
