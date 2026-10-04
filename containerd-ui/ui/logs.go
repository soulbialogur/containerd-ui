package ui

import (
	"containerd-ui/i18n"
	"containerd-ui/wsl"
	"context"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func BuildLogsTab(win fyne.Window) fyne.CanvasObject {
	var containers []wsl.Container
	selectedID := ""
	liveRequested := false
	lastLogTimestamp := ""
	var lastLogTime time.Time
	var liveCancel context.CancelFunc

	placeholder := i18n.T("logs.select_container")
	logText := widget.NewMultiLineEntry()
	logText.Wrapping = fyne.TextWrapWord
	logText.Disable()
	logText.SetPlaceHolder(i18n.T("logs.select_hint"))
	logText.Hide()

	emptyLogs := widget.NewLabel(i18n.T("logs.no_logs"))
	emptyLogs.Wrapping = fyne.TextWrapWord
	emptyLogs.Alignment = fyne.TextAlignCenter
	emptyLogs.Hide()

	showEmptyLogs := func() {
		logText.Hide()
		emptyLogs.Show()
	}
	showLogText := func(text string) {
		emptyLogs.Hide()
		logText.SetText(text)
		logText.Show()
		logText.Refresh()
	}
	appendLiveLog := func(line string) {
		if strings.TrimSpace(line) == "" {
			return
		}
		current := strings.TrimSpace(logText.Text)
		if current != "" {
			current += "\n"
		}
		lines := strings.Split(current+line, "\n")
		limit := wsl.GetLogTail()
		if limit > 0 && len(lines) > limit {
			lines = lines[len(lines)-limit:]
		}
		showLogText(strings.Join(lines, "\n"))
	}

	var stopLiveLogs func()
	var startLiveLogs func()

	loadLogs := func(id string) {
		if id == "" {
			if stopLiveLogs != nil {
				stopLiveLogs()
			}
			lastLogTimestamp = ""
			lastLogTime = time.Time{}
			safeUI(func() {
				showEmptyLogs()
				logText.SetText("")
				emptyLogs.SetText(i18n.T("logs.no_logs"))
				logText.Refresh()
			})
			return
		}
		if stopLiveLogs != nil {
			stopLiveLogs()
		}
		lastLogTimestamp = ""
		lastLogTime = time.Time{}
		go func() {
			select {
			case <-wsl.AppContext().Done():
				return
			default:
			}
			logs, timestamp, err := wsl.GetContainerLogsWithTimestamp(id, wsl.GetLogTail())
			safeUI(func() {
				if selectedID != id {
					return
				}
				if err == nil {
					lastLogTimestamp = timestamp
					lastLogTime, _ = time.Parse(time.RFC3339Nano, timestamp)
					if strings.TrimSpace(logs) == "" {
						emptyLogs.SetText(i18n.T("logs.no_logs"))
						showEmptyLogs()
					} else {
						showLogText(logs)
					}
					if liveRequested && startLiveLogs != nil {
						startLiveLogs()
					}
				} else {
					showLogText(i18n.T("op.error", err.Error()))
				}
			})
		}()
	}

	stopLiveLogs = func() {
		if liveCancel != nil {
			liveCancel()
			liveCancel = nil
		}
	}

	startLiveLogs = func() {
		stopLiveLogs()
		if selectedID == "" || economyMode.Load() {
			return
		}
		id := selectedID
		since := lastLogTimestamp
		if since == "" {
			since = time.Now().UTC().Format(time.RFC3339Nano)
		}
		ctx, cancel := context.WithCancel(wsl.AppContext())
		liveCancel = cancel
		go func() {
			err := wsl.StreamContainerLogs(ctx, id, since, func(timestamp, message string) {
				if ctx.Err() != nil {
					return
				}
				safeUI(func() {
					if ctx.Err() != nil || selectedID != id {
						return
					}
					if timestamp != "" {
						parsed, parseErr := time.Parse(time.RFC3339Nano, timestamp)
						if parseErr == nil && !lastLogTime.IsZero() && !parsed.After(lastLogTime) {
							return
						}
						if parseErr == nil {
							lastLogTime = parsed
							lastLogTimestamp = timestamp
						}
					}
					appendLiveLog(message)
				})
			})
			if err != nil && ctx.Err() == nil {
				safeUI(func() {
					if selectedID == id {
						appendLiveLog(i18n.T("op.error", err.Error()))
					}
				})
			}
		}()
	}

	refreshNewLogs := func(id string) {
		if id == "" {
			return
		}
		if lastLogTimestamp == "" {
			loadLogs(id)
			return
		}
		if liveRequested && liveCancel != nil {
			return
		}
	ctx, cancel := context.WithTimeout(wsl.AppContext(), wsl.TimeoutMedium)
		go func(since string) {
			defer cancel()
			entries, err := wsl.GetContainerLogEntriesSince(ctx, id, since)
			safeUI(func() {
				if selectedID != id {
					return
				}
				if err != nil {
					appendLiveLog(i18n.T("op.error", err.Error()))
					return
				}
				for _, entry := range entries {
					if entry.Timestamp != "" {
						parsed, parseErr := time.Parse(time.RFC3339Nano, entry.Timestamp)
						if parseErr != nil || (!lastLogTime.IsZero() && !parsed.After(lastLogTime)) {
							continue
						}
						lastLogTime = parsed
						lastLogTimestamp = entry.Timestamp
					}
					appendLiveLog(entry.Message)
				}
			})
		}(lastLogTimestamp)
	}

	selector := widget.NewSelect([]string{placeholder}, func(name string) {
		if name == placeholder || name == "" {
			selectedID = ""
			stopLiveLogs()
			lastLogTimestamp = ""
			lastLogTime = time.Time{}
			showEmptyLogs()
			logText.SetText("")
			return
		}
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
	selector.SetSelected(placeholder)

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
					showLogText(i18n.T("op.error", err.Error()))
					return
				}
				selectedName := ""
				previousSelectedID := selectedID
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
				if len(names) == 0 {
					stopLiveLogs()
					selector.Options = []string{placeholder}
					selector.SetSelected(placeholder)
					showEmptyLogs()
					return
				}
				selector.Options = names
				selector.Refresh()
				if selectedName != "" && newSelectedID != "" {
					selector.SetSelected(selectedName)
				} else {
					selector.SetSelected(placeholder)
				}
				if newSelectedID != "" {
					if newSelectedID == previousSelectedID {
						refreshNewLogs(newSelectedID)
					} else {
						loadLogs(newSelectedID)
					}
				} else {
					stopLiveLogs()
					lastLogTimestamp = ""
					lastLogTime = time.Time{}
					showEmptyLogs()
					emptyLogs.SetText(i18n.T("logs.no_logs"))
				}
			})
		}()
	}

	btnRefresh := widget.NewButton(i18n.T("logs.refresh"), func() {
		refresh()
	})

	liveCheck := widget.NewCheck(i18n.T("logs.live"), func(checked bool) {
		liveRequested = checked
		if checked {
			startLiveLogs()
		} else {
			stopLiveLogs()
		}
	})
	unsubscribeEconomyMode := RegisterEconomyModeListener(func(enabled bool) {
		safeUI(func() {
			if enabled {
				stopLiveLogs()
			} else if liveRequested {
				startLiveLogs()
			}
		})
	})
	go func() {
		<-wsl.AppContext().Done()
		unsubscribeEconomyMode()
	}()

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

	logPanel := container.NewStack(logText, emptyLogs)
	return withResponsiveScroll(container.NewBorder(topBar, nil, nil, nil, logPanel))
}
