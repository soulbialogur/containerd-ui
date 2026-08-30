package main

import (
	"containerd-ui/ui"
	"containerd-ui/wsl"
	"errors"
	"os"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
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
	os.Setenv("FYNE_LOCALE", "ru_RU")
	config, err := wsl.LoadConfig()
	if err != nil {
		config = wsl.DefaultConfig()
	}
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

	win := myApp.NewWindow("Containerd UI")
	win.Resize(fyne.NewSize(1100, 700))

	if icon != nil {
		myApp.SetIcon(icon)
		win.SetIcon(icon)
	}

	status := wsl.CheckService()
	if !status["wsl"].(bool) {
		dialog.ShowError(
			errors.New("WSL "+wsl.GetWslDistro()+" не найден. Установите: wsl --install "+wsl.GetWslDistro()),
			win,
		)
	}

	statusTab := ui.BuildStatusTab()
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
		container.NewTabItem("Статус", statusTab),
		container.NewTabItem("Контейнеры", containersTab),
		container.NewTabItem("Образы", imagesTab),
		container.NewTabItem("Тома", volumesTab),
		container.NewTabItem("Сети", networksTab),
		container.NewTabItem("Ресурсы", resourcesTab),
		container.NewTabItem("Логи", logsTab),
		container.NewTabItem("База данных", databaseTab),
		container.NewTabItem("Очистка", cleanTab),
		container.NewTabItem("Деплой", deployTab),
		container.NewTabItem("Настройки", settingsTab),
	)

	tabs.SetTabLocation(container.TabLocationTop)

	// Обработчик переключения вкладок – останавливаем фоновые тикеры всех вкладок,
	// затем активируем только выбранную.
	tabs.OnSelected = func(item *container.TabItem) {
		ui.DeactivateAllTabs()
		if item != nil {
			ui.ActivateTabByName(item.Text)
		}
	}

	win.SetOnClosed(func() {
		ui.StopAllTabs() // останавливаем все тикеры
		wsl.Shutdown()
	})

	win.SetContent(tabs)
	win.ShowAndRun()
}
