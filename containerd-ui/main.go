package main

import (
	"containerd-ui/i18n"
	"containerd-ui/ui"
	"containerd-ui/wsl"
	"os"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget" // <-- добавлено
)

var cachedIcon []byte

func loadIcon(path string) fyne.Resource {
	if cachedIcon != nil {
		return fyne.NewStaticResource(filepath.Base(path), cachedIcon)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	cachedIcon = data
	return fyne.NewStaticResource(filepath.Base(path), data)
}

func main() {
	config, err := wsl.LoadConfig()
	if err != nil {
		config = wsl.DefaultConfig()
	}

	// Язык интерфейса хранится в config.json (поле language)
	locale := i18n.LocaleRU
	if config.Language == "en" {
		locale = i18n.LocaleEN
	}
	i18n.SetLocale(locale)

	wsl.InitConfigCache(config)
	ui.SetEconomyMode(config.EconomyMode)

	myApp := app.New()
	myApp.Settings().SetTheme(&darkTheme{})

	var icon fyne.Resource
	if exePath, err := os.Executable(); err == nil {
		iconPath := filepath.Join(filepath.Dir(exePath), "app.ico")
		if res := loadIcon(iconPath); res != nil {
			icon = res
		}
	}

	win := myApp.NewWindow(i18n.T("app.title"))
	win.Resize(fyne.NewSize(1100, 700))

	if icon != nil {
		myApp.SetIcon(icon)
		win.SetIcon(icon)
	}

	// StatusTab выполняет WSL-запросы при построении. Не создаём его до
	// запуска event loop, иначе зависший WSL может задержать появление окна.
	statusPlaceholder := container.NewCenter(widget.NewLabel("Загрузка статуса WSL..."))
	statusTabItem := container.NewTabItem(i18n.T("tabs.status"), statusPlaceholder)

	containersTab := ui.BuildContainersTab(win)
	imagesTab := ui.BuildImagesTab(win)
	volumesTab := ui.BuildVolumesTab(win)
	networksTab := ui.BuildNetworksTab(win)
	resourcesTab := ui.BuildResourcesTab()
	logsTab := ui.BuildLogsTab(win)
	databaseTab := ui.BuildDatabaseTab()
	cleanTab := ui.BuildCleanTab()
	deployTab := ui.BuildDeployTab(win)
	settingsTab := ui.BuildSettingsTab(win)

	tabs := container.NewAppTabs(
		statusTabItem,
		container.NewTabItem(i18n.T("tabs.containers"), containersTab),
		container.NewTabItem(i18n.T("tabs.images"), imagesTab),
		container.NewTabItem(i18n.T("tabs.volumes"), volumesTab),
		container.NewTabItem(i18n.T("tabs.networks"), networksTab),
		container.NewTabItem(i18n.T("tabs.resources"), resourcesTab),
		container.NewTabItem(i18n.T("tabs.logs"), logsTab),
		container.NewTabItem(i18n.T("tabs.database"), databaseTab),
		container.NewTabItem(i18n.T("tabs.clean"), cleanTab),
		container.NewTabItem(i18n.T("tabs.deploy"), deployTab),
		container.NewTabItem(i18n.T("tabs.settings"), settingsTab),
	)

	tabs.SetTabLocation(container.TabLocationTop)

	tabs.OnSelected = func(item *container.TabItem) {
		ui.DeactivateAllTabs()
		if item != nil {
			ui.ActivateTabByName(item.Text)
		}
	}

	win.SetOnClosed(func() {
		ui.StopAllTabs()
		wsl.Shutdown()
	})

	win.SetContent(tabs)

	// Все потенциально блокирующие WSL-проверки выполняются после того, как
	// окно уже может быть показано пользователю.
	go func() {
		// BuildStatusTab больше не выполняет WSL-запрос синхронно.
		statusTab := ui.BuildStatusTab(win)
		fyne.Do(func() {
			statusTabItem.Content = statusTab
			tabs.Refresh()
		})
	}()

	win.ShowAndRun()
}