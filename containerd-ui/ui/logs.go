package ui

import (
	"containerd-ui/wsl"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func BuildLogsTab(win fyne.Window) fyne.CanvasObject {
	var containers []wsl.Container
	selectedID := ""

	logText := widget.NewMultiLineEntry()
	logText.Wrapping = fyne.TextWrapWord
	logText.Disable()
	logText.SetPlaceHolder("Выберите контейнер для просмотра логов")

	// Единая функция загрузки логов
	loadLogs := func(id string) {
		if id == "" {
			safeUI(func() {
				logText.SetText("Выберите контейнер для просмотра логов")
				logText.Refresh()
			})
			return
		}
		go func() {
			select {
			case <-wsl.AppContext().Done():
				return
			default:
			}
			logs, err := wsl.GetContainerLogs(id, 200)
			safeUI(func() {
				if err == nil {
					logText.SetText(logs)
				} else {
					logText.SetText("Ошибка: " + err.Error())
				}
				logText.Refresh()
			})
		}()
	}

	selector := widget.NewSelect([]string{}, func(name string) {
		for _, c := range containers {
			displayName := c.Name
			if displayName == "" {
				displayName = c.ID
			}
			if displayName == name {
				selectedID = c.ID
				loadLogs(selectedID)
				return
			}
		}
	})

	refresh := func() {
		go func() {
			select {
			case <-wsl.AppContext().Done():
				return
			default:
			}
			data, err := wsl.ListContainers(true)
			safeUI(func() {
				if err != nil {
					logText.SetText("Ошибка: " + err.Error())
					return
				}
				containers = data
				names := make([]string, 0, len(containers))
				for _, c := range containers {
					displayName := c.Name
					if displayName == "" {
						displayName = c.ID
					}
					names = append(names, displayName)
				}
				selector.Options = names
				selector.Refresh()
			})
		}()
	}

	btnRefresh := widget.NewButton("Обновить логи", func() {
		if selectedID != "" {
			loadLogs(selectedID)
		}
	})

	btnRefreshList := widget.NewButton("Список", refresh)

	// НОВАЯ КНОПКА: очистка логов выбранного контейнера
	btnClearLogs := widget.NewButton("Очистить логи", func() {
		if selectedID == "" {
			safeUI(func() {
				logText.SetText("Выберите контейнер для очистки логов")
				logText.Refresh()
			})
			return
		}
		go func() {
			select {
			case <-wsl.AppContext().Done():
				return
			default:
			}
			err := wsl.ClearContainerLogs(selectedID)
			safeUI(func() {
				if err != nil {
					logText.SetText("Ошибка очистки логов: " + err.Error())
				} else {
					// После очистки обновляем отображение (лог станет пустым)
					loadLogs(selectedID)
				}
				logText.Refresh()
			})
		}()
	})

	topBar := container.NewHBox(
		widget.NewLabel("Контейнер:"),
		selector,
		btnRefreshList,
		btnRefresh,
		btnClearLogs, // добавлена в панель
	)

	refresh()

	_ = strings.TrimSpace
	_ = win

	return withResponsiveScroll(container.NewBorder(topBar, nil, nil, nil, logText))
}