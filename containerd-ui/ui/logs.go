package ui

import (
	"containerd-ui/i18n"
	"containerd-ui/wsl"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func BuildLogsTab(win fyne.Window) fyne.CanvasObject {
	var containers []wsl.Container
	selectedID := ""
	var liveTicker *time.Ticker
	var liveStop chan struct{}

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

	stopLiveLogs := func() {
		if liveTicker != nil {
			liveTicker.Stop()
			liveTicker = nil
		}
		if liveStop != nil {
			close(liveStop)
			liveStop = nil
		}
	}

	startLiveLogs := func() {
		stopLiveLogs()
		if selectedID == "" {
			return
		}
		liveTicker = time.NewTicker(time.Second)
		liveStop = make(chan struct{})
		go func(id string, ticker *time.Ticker, stop <-chan struct{}) {
			for {
				select {
				case <-ticker.C:
					loadLogs(id)
				case <-stop:
					return
				case <-wsl.AppContext().Done():
					return
				}
			}
		}(selectedID, liveTicker, liveStop)
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
				if liveTicker != nil {
					startLiveLogs()
				}
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
				selectedName := ""
				for _, c := range containers {
					if c.ID == selectedID {
						selectedName = c.Name
						break
					}
				}
				containers = data
				names := make([]string, 0, len(containers))
				newSelectedID := ""
				for _, c := range containers {
					displayName := c.Name
					if displayName == "" {
						displayName = c.ID
					}
					names = append(names, displayName)
					if selectedName != "" && displayName == selectedName {
						newSelectedID = c.ID
					}
				}
				selectedID = newSelectedID
				selector.Options = names
				selector.Refresh()
				if selectedName != "" && newSelectedID != "" {
					selector.SetSelected(selectedName)
				}
				if newSelectedID != "" {
					loadLogs(newSelectedID)
				} else {
					logText.SetText(i18n.T("logs.select_hint"))
					logText.Refresh()
				}
			})
		}()
	}

	btnRefresh := widget.NewButton(i18n.T("logs.refresh"), func() {
		refresh()
	})

	liveCheck := widget.NewCheck(i18n.T("logs.live"), func(checked bool) {
		if checked {
			startLiveLogs()
		} else {
			stopLiveLogs()
		}
	})

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
		btnRefresh,
		liveCheck,
		btnClearLogs,
	)

	refresh()

	return withResponsiveScroll(container.NewBorder(topBar, nil, nil, nil, logText))
}
