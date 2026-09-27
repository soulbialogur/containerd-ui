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

	// Тяжёлые вкладки создаются после появления окна. Это не блокирует холодный
	// старт ожиданием WSL и позволяет показывать интерфейс сразу.
	newPlaceholder := func() fyne.CanvasObject {
		return container.NewCenter(widget.NewLabel("Загрузка..."))
	}
	statusTabItem := container.NewTabItem(i18n.T("tabs.status"), newPlaceholder())
	containersTabItem := container.NewTabItem(i18n.T("tabs.containers"), newPlaceholder())
	imagesTabItem := container.NewTabItem(i18n.T("tabs.images"), newPlaceholder())
	volumesTabItem := container.NewTabItem(i18n.T("tabs.volumes"), newPlaceholder())
	networksTabItem := container.NewTabItem(i18n.T("tabs.networks"), newPlaceholder())
	resourcesTabItem := container.NewTabItem(i18n.T("tabs.resources"), newPlaceholder())
	logsTabItem := container.NewTabItem(i18n.T("tabs.logs"), newPlaceholder())
	databaseTabItem := container.NewTabItem(i18n.T("tabs.database"), newPlaceholder())
	cleanTabItem := container.NewTabItem(i18n.T("tabs.clean"), newPlaceholder())
	deployTabItem := container.NewTabItem(i18n.T("tabs.deploy"), newPlaceholder())
	settingsTabItem := container.NewTabItem(i18n.T("tabs.settings"), newPlaceholder())

	tabs := container.NewAppTabs(
		statusTabItem,
		containersTabItem,
		imagesTabItem,
		volumesTabItem,
		networksTabItem,
		resourcesTabItem,
		logsTabItem,
		databaseTabItem,
		cleanTabItem,
		deployTabItem,
		settingsTabItem,
	)

	tabs.SetTabLocation(container.TabLocationTop)
	ui.SetStatusMetricNavigation(func(tabName string) {
		for _, item := range tabs.Items {
			if item.Text == tabName {
				tabs.Select(item)
				return
			}
		}
	})

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

	// Вкладки создаются по одной после появления окна. Последовательность
	// снижает конкуренцию за WSL при холодном запуске.
	go func() {
		loadTab := func(item *container.TabItem, build func() fyne.CanvasObject) {
			content := build()
			fyne.Do(func() {
				item.Content = content
				tabs.Refresh()
			})
		}
		loadTab(statusTabItem, func() fyne.CanvasObject { return ui.BuildStatusTab(win) })
		loadTab(containersTabItem, func() fyne.CanvasObject { return ui.BuildContainersTab(win) })
		loadTab(imagesTabItem, func() fyne.CanvasObject { return ui.BuildImagesTab(win) })
		loadTab(volumesTabItem, func() fyne.CanvasObject { return ui.BuildVolumesTab(win) })
		loadTab(networksTabItem, func() fyne.CanvasObject { return ui.BuildNetworksTab(win) })
		loadTab(resourcesTabItem, func() fyne.CanvasObject { return ui.BuildResourcesTab() })
		loadTab(logsTabItem, func() fyne.CanvasObject { return ui.BuildLogsTab(win) })
		loadTab(databaseTabItem, func() fyne.CanvasObject { return ui.BuildDatabaseTab() })
		loadTab(cleanTabItem, func() fyne.CanvasObject { return ui.BuildCleanTab() })
		loadTab(deployTabItem, func() fyne.CanvasObject { return ui.BuildDeployTab(win) })
		loadTab(settingsTabItem, func() fyne.CanvasObject { return ui.BuildSettingsTab(win) })
	}()

	win.ShowAndRun()
}
