package ui

import (
	"containerd-ui/i18n"
	"containerd-ui/wsl"

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
	logText.SetPlaceHolder(i18n.T("logs.select_hint"))

	loadLogs := func(id string) {
		if id == "" {
			safeUI(func() {
				logText.SetText(i18n.T("logs.select_hint"))
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
					logText.SetText(i18n.T("op.error", err.Error()))
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
					logText.SetText(i18n.T("op.error", err.Error()))
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

	btnRefresh := widget.NewButton(i18n.T("logs.refresh"), func() {
		if selectedID != "" {
			loadLogs(selectedID)
		}
	})

	btnRefreshList := widget.NewButton(i18n.T("logs.list"), refresh)

	btnClearLogs := widget.NewButton(i18n.T("logs.clear"), func() {
		if selectedID == "" {
			safeUI(func() {
				logText.SetText(i18n.T("logs.select_clear_hint"))
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
					logText.SetText(i18n.T("logs.clear_error", err.Error()))
				} else {
					loadLogs(selectedID)
				}
				logText.Refresh()
			})
		}()
	})

	topBar := container.NewHBox(
		widget.NewLabel(i18n.T("logs.container")),
		selector,
		btnRefreshList,
		btnRefresh,
		btnClearLogs,
	)

	refresh()

	return withResponsiveScroll(container.NewBorder(topBar, nil, nil, nil, logText))
}