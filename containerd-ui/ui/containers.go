package ui

import (
	"containerd-ui/i18n"
	"containerd-ui/wsl"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

func safeUI(f func()) {
	fyne.Do(f)
}

func withSelectionOrAll(selectedID string, onSelected, onAll func()) {
	if selectedID != "" {
		onSelected()
		return
	}
	onAll()
}

type tableData struct {
	mu            sync.RWMutex
	rows          []wsl.Container
	idx           map[string]int
	sortField     containerSortField
	sortAscending bool
	sortActive    bool
}

type containerSortField uint8

const (
	sortByID containerSortField = iota
	sortByName
	sortByStatus
)

func runContainerOperations(containers []wsl.Container, cancelCh <-chan struct{}, operation func(string) error) error {
	if len(containers) == 0 {
		return nil
	}
	workerCount := wsl.GetContainerOperationConcurrency()
	if workerCount > len(containers) {
		workerCount = len(containers)
	}
	jobs := make(chan wsl.Container)
	var workers sync.WaitGroup
	var errorMu sync.Mutex
	var firstErr error

	worker := func() {
		defer workers.Done()
		for {
			select {
			case <-cancelCh:
				return
			case <-wsl.AppContext().Done():
				return
			case container, ok := <-jobs:
				if !ok {
					return
				}
				if err := operation(container.ID); err != nil {
					errorMu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					errorMu.Unlock()
				}
			}
		}
	}

	workers.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go worker()
	}

	for _, container := range containers {
		select {
		case <-cancelCh:
			close(jobs)
			workers.Wait()
			return context.Canceled
		case <-wsl.AppContext().Done():
			close(jobs)
			workers.Wait()
			return context.Canceled
		case jobs <- container:
		}
	}
	close(jobs)
	workers.Wait()
	return firstErr
}

func runningContainers(containers []wsl.Container) []wsl.Container {
	var running []wsl.Container
	for _, container := range containers {
		status := strings.ToLower(container.Status)
		if strings.Contains(status, "running") || strings.Contains(status, "up") {
			running = append(running, container)
		}
	}
	return running
}

func newDataTable() *tableData {
	return &tableData{
		rows: make([]wsl.Container, 0, 32),
		idx:  make(map[string]int, 32),
	}
}

func (td *tableData) getRows() []wsl.Container {
	td.mu.RLock()
	defer td.mu.RUnlock()
	result := make([]wsl.Container, len(td.rows))
	copy(result, td.rows)
	return result
}

func (td *tableData) setRows(rows []wsl.Container) {
	td.mu.Lock()
	td.rows = append([]wsl.Container(nil), rows...)
	if td.sortActive {
		td.sortRowsLocked()
	}
	td.rebuildIndexLocked()
	td.mu.Unlock()
}

func (td *tableData) rebuildIndexLocked() {
	if td.idx == nil {
		td.idx = make(map[string]int, len(td.rows))
	} else {
		for k := range td.idx {
			delete(td.idx, k)
		}
	}
	for i, c := range td.rows {
		td.idx[c.ID] = i
	}
}

func (td *tableData) toggleSort(field containerSortField) bool {
	td.mu.Lock()
	if td.sortActive && td.sortField == field {
		td.sortAscending = !td.sortAscending
	} else {
		td.sortField = field
		td.sortAscending = true
		td.sortActive = true
	}
	td.sortRowsLocked()
	td.rebuildIndexLocked()
	ascending := td.sortAscending
	td.mu.Unlock()
	return ascending
}

func (td *tableData) sortRowsLocked() {
	valueFor := func(row wsl.Container) string {
		switch td.sortField {
		case sortByID:
			return row.ID
		case sortByName:
			if row.Name == "" {
				return "—"
			}
			return row.Name
		case sortByStatus:
			return row.Status
		default:
			return ""
		}
	}
	sort.SliceStable(td.rows, func(left, right int) bool {
		comparison := strings.Compare(
			strings.ToLower(valueFor(td.rows[left])),
			strings.ToLower(valueFor(td.rows[right])),
		)
		if td.sortAscending {
			return comparison < 0
		}
		return comparison > 0
	})
}

func (td *tableData) getIndex(id string) (int, bool) {
	td.mu.RLock()
	defer td.mu.RUnlock()
	idx, ok := td.idx[id]
	return idx, ok
}

func (td *tableData) getRowCount() int {
	td.mu.RLock()
	defer td.mu.RUnlock()
	return len(td.rows)
}

func (td *tableData) getRow(index int) (wsl.Container, bool) {
	td.mu.RLock()
	defer td.mu.RUnlock()
	if index < 0 || index >= len(td.rows) {
		return wsl.Container{}, false
	}
	return td.rows[index], true
}

func (td *tableData) clear() {
	td.mu.Lock()
	td.rows = td.rows[:0]
	td.mu.Unlock()
}

func BuildContainersTab(win fyne.Window) fyne.CanvasObject {
	data := newDataTable()
	var selectedID string

	progressBar := NewProgressBarComponent()
	opManager := NewOperationManager()

	opManager.SetOnUpdate(func() {
		safeUI(func() {
			ops := opManager.GetActiveOperations()
			if len(ops) == 0 {
				if latest := opManager.GetLatestFinished(); latest != nil && latest.Error != nil {
					ops = []*OperationProgress{latest}
				}
			}
			if len(ops) > 0 {
				progressBar.Update(ops[0])
			} else {
				progressBar.Hide()
			}
		})
	})

	newContainerRow := func() fyne.CanvasObject {
		labels := make([]fyne.CanvasObject, 5)
		idButton := widget.NewButton("", nil)
		idButton.Alignment = widget.ButtonAlignLeading
		idButton.Importance = widget.LowImportance
		labels[0] = idButton
		for i := 1; i < len(labels); i++ {
			label := widget.NewLabel("")
			label.Wrapping = fyne.TextTruncate
			labels[i] = label
		}
		return container.NewGridWithColumns(5, labels...)
	}

	containerList := widget.NewList(
		func() int { return data.getRowCount() },
		newContainerRow,
		func(id widget.ListItemID, object fyne.CanvasObject) {
			row, ok := data.getRow(int(id))
			if !ok {
				return
			}
			grid := object.(*fyne.Container)
			labels := grid.Objects
			imageName := row.Image
			if slash := strings.LastIndex(imageName, "/"); slash >= 0 && slash+1 < len(imageName) {
				imageName = imageName[slash+1:]
			}
			values := []string{row.ID, row.Name, imageName, wsl.TranslateStatus(row.Status), row.Ports}
			for i, value := range values {
				if i == 0 {
					id := row.ID
					button := labels[i].(*widget.Button)
					button.SetText(value)
					button.OnTapped = func() {
						fyne.CurrentApp().Clipboard().SetContent(id)
					}
					continue
				}
				label := labels[i].(*widget.Label)
				if i == 1 && value == "" {
					value = "—"
				}
				if i == 2 && len(value) > 30 {
					value = value[:27] + "..."
				}
				label.SetText(value)
				label.TextStyle = fyne.TextStyle{Bold: i == 3}
			}
		},
	)
	updateRows := func(rows []wsl.Container) {
		selectedContainerID := selectedID
		data.setRows(rows)
		containerList.Refresh()
		containerList.UnselectAll()
		if index, ok := data.getIndex(selectedContainerID); ok {
			containerList.Select(widget.ListItemID(index))
		}
	}

	var sortHeaders []struct {
		button *widget.Button
		title  string
	}
	makeSortHeader := func(title string, field containerSortField) fyne.CanvasObject {
		button := widget.NewButton(title, nil)
		button.Alignment = widget.ButtonAlignLeading
		button.Importance = widget.LowImportance
		sortHeaders = append(sortHeaders, struct {
			button *widget.Button
			title  string
		}{button: button, title: title})
		button.OnTapped = func() {
			ascending := data.toggleSort(field)
			for _, header := range sortHeaders {
				header.button.SetText(header.title)
			}
			direction := " ↓"
			if ascending {
				direction = " ↑"
			}
			button.SetText(title + direction)
			containerList.Refresh()
			containerList.UnselectAll()
			if index, ok := data.getIndex(selectedID); ok {
				containerList.Select(widget.ListItemID(index))
			}
		}
		return button
	}

	header := container.NewGridWithColumns(5,
		makeSortHeader(i18n.T("containers.id"), sortByID),
		makeSortHeader(i18n.T("containers.name"), sortByName),
		widget.NewLabelWithStyle(i18n.T("containers.image"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		makeSortHeader(i18n.T("containers.status"), sortByStatus),
		widget.NewLabelWithStyle(i18n.T("containers.ports"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
	)

	var refreshTimer *time.Timer
	var btnRefresh *widget.Button
	refresh := func(reportErrors bool) {
		if refreshTimer != nil {
			refreshTimer.Stop()
		}
		refreshTimer = time.AfterFunc(100*time.Millisecond, func() {
			go func() {
				safeUI(func() { setRefreshButtonLoading(btnRefresh, i18n.T("containers.refresh"), true) })
				defer safeUI(func() { setRefreshButtonLoading(btnRefresh, i18n.T("containers.refresh"), false) })
				select {
				case <-wsl.AppContext().Done():
					return
				default:
				}
				wsl.CDInvalidateContainersCache()
				containers, err := wsl.ListContainers(true)
				if err != nil {
					if reportErrors {
						safeUI(func() {
							showErrorDialog(win, i18n.T("containers.refresh_error", localizedWslError(err)))
						})
					}
					return
				}
				safeUI(func() {
					updateRows(containers)
				})
			}()
		})
	}

	var selectedTimer *time.Timer
	containerList.OnSelected = func(id widget.ListItemID) {
		if selectedTimer != nil {
			selectedTimer.Stop()
		}
		selectedTimer = time.AfterFunc(DebounceSelected, func() {
			if int(id) < data.getRowCount() {
				container, ok := data.getRow(int(id))
				if ok {
					selectedID = container.ID
				}
			}
		})
	}

	asyncAction := func(action func(progress *OperationManager, cancelCh chan struct{}) error, opType OperationType) {
		go func() {
			select {
			case <-wsl.AppContext().Done():
				return
			default:
			}
			cancelCh := make(chan struct{})
			var wg sync.WaitGroup

			safeUI(func() {
				progressBar.SetCancelHandler(func() {
					select {
					case <-cancelCh:
					default:
						close(cancelCh)
					}
				})
				progressBar.SetCloseHandler(func() {
					safeUI(func() {
						progressBar.Hide()
						containerList.Refresh()
					})
				})
			})

			opID := opManager.StartOperation(selectedID, opType)

			progressTicker := time.NewTicker(TickerProgress)
			defer progressTicker.Stop()

			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-progressTicker.C:
						currentOp := opManager.GetOperation(opID)
						if currentOp != nil {
							currentOp.Progress += 0.05
							if currentOp.Progress > 0.95 {
								currentOp.Progress = 0.95
							}
							opManager.SetOperation(opID, currentOp)
						}
					case <-cancelCh:
						return
					case <-wsl.AppContext().Done():
						return
					}
				}
			}()

			err := action(opManager, cancelCh)

			if err != nil {
				opManager.FinishOperation(opID, false, err.Error())
			} else {
				currentOp := opManager.GetOperation(opID)
				if currentOp != nil {
					currentOp.Progress = 1.0
					currentOp.Status = i18n.T("op.done")
					opManager.SetOperation(opID, currentOp)
				}
				opManager.FinishOperation(opID, true, "")
			}

			select {
			case <-cancelCh:
			default:
				close(cancelCh)
			}
			wg.Wait()

			containers, listErr := wsl.ListContainers(true)
			if listErr == nil {
				safeUI(func() {
					updateRows(containers)
				})
			}
		}()
	}

	makeBtn := func(text string, tapped func()) fyne.CanvasObject {
		return widget.NewButton(text, tapped)
	}
	showConfirm := func(title, message string, onConfirm func()) {
		confirmDialog := dialog.NewCustomConfirm(
			title,
			i18n.T("dialogs.ok"),
			i18n.T("dialogs.cancel"),
			widget.NewLabel(message),
			func(ok bool) {
				if ok {
					onConfirm()
				}
			},
			win,
		)
		confirmDialog.Resize(fyne.NewSize(420, 180))
		confirmDialog.Show()
	}

	btnRefresh = widget.NewButton(i18n.T("containers.refresh"), func() { refresh(true) })

	topBar := container.NewBorder(
		progressBar.Widget(),
		nil, nil, nil,
		container.NewAdaptiveGrid(4,
			makeBtn(i18n.T("containers.start"), func() {
				withSelectionOrAll(selectedID, func() {
					asyncAction(func(progress *OperationManager, cancelCh chan struct{}) error {
						progress.UpdateOperation(selectedID, 0.1, i18n.T("containers.progress_start"))
						return wsl.StartContainer(selectedID)
					}, OpStart)
				}, func() {
					asyncAction(func(progress *OperationManager, cancelCh chan struct{}) error {
						containers, err := wsl.ListContainers(true)
						if err != nil {
							return err
						}
						var stopped []wsl.Container
						for _, container := range containers {
							status := strings.ToLower(container.Status)
							if !strings.Contains(status, "running") && !strings.Contains(status, "up") {
								stopped = append(stopped, container)
							}
						}
						return runContainerOperations(stopped, cancelCh, wsl.StartContainer)
					}, OpStart)
				})
			}),
			makeBtn(i18n.T("containers.stop"), func() {
				withSelectionOrAll(selectedID, func() {
					asyncAction(func(progress *OperationManager, cancelCh chan struct{}) error {
						progress.UpdateOperation(selectedID, 0.1, i18n.T("containers.progress_stop"))
						return wsl.StopContainer(selectedID)
					}, OpStop)
				}, func() {
					asyncAction(func(progress *OperationManager, cancelCh chan struct{}) error {
						containers, err := wsl.ListContainers(true)
						if err != nil {
							return err
						}
						running := runningContainers(containers)
						if len(running) == 0 {
							return nil
						}
						return runContainerOperations(running, cancelCh, wsl.StopContainer)
					}, OpStop)
				})
			}),
			makeBtn(i18n.T("containers.restart"), func() {
				restartContainer := func(id string, progress *OperationManager) error {
					if progress != nil {
						progress.UpdateOperation(id, 0.1, i18n.T("containers.progress_stop"))
					}
					time.Sleep(SleepOperation)
					if err := wsl.StopContainer(id); err != nil {
						return fmt.Errorf("%s: %w", i18n.T("containers.err_stop_container", id), err)
					}
					if progress != nil {
						progress.UpdateOperation(id, 0.5, i18n.T("containers.progress_start"))
					}
					time.Sleep(SleepOperation)
					if err := wsl.StartContainer(id); err != nil {
						return fmt.Errorf("%s: %w", i18n.T("containers.err_start_container", id), err)
					}
					return nil
				}
				withSelectionOrAll(selectedID, func() {
					asyncAction(func(progress *OperationManager, cancelCh chan struct{}) error {
						return restartContainer(selectedID, progress)
					}, OpRestart)
				}, func() {
					showConfirm(i18n.T("containers.restart"), i18n.T("containers.confirm_restart_all"), func() {
						asyncAction(func(_ *OperationManager, cancelCh chan struct{}) error {
							containers, err := wsl.ListContainers(true)
							if err != nil {
									return err
								}
								return runContainerOperations(runningContainers(containers), cancelCh, func(id string) error {
									return restartContainer(id, nil)
								})
							}, OpRestart)
						})
					})
			}),
			makeBtn(i18n.T("containers.remove"), func() {
				withSelectionOrAll(selectedID, func() {
					showConfirm(i18n.T("containers.remove_title"), i18n.T("containers.confirm_remove", selectedID), func() {
						asyncAction(func(progress *OperationManager, cancelCh chan struct{}) error {
							progress.UpdateOperation(selectedID, 0.4, i18n.T("containers.progress_remove"))
							return wsl.RemoveContainer(selectedID)
						}, OpRemove)
					})
				}, func() {
					showConfirm(i18n.T("containers.remove_all_title"), i18n.T("containers.confirm_remove_all"), func() {
						asyncAction(func(progress *OperationManager, cancelCh chan struct{}) error {
							containers, err := wsl.ListContainers(true)
							if err != nil {
								return err
							}
							if len(containers) == 0 {
								return nil
							}
							return runContainerOperations(containers, cancelCh, wsl.RemoveContainer)
						}, OpRemove)
					})
				})
			}),
			makeBtn(i18n.T("containers.build"), func() {
				radio := widget.NewRadioGroup([]string{i18n.T("containers.build_full"), i18n.T("containers.build_only_run")}, nil)
				radio.Horizontal = true
				radio.SetSelected(i18n.T("containers.build_full"))

				content := fyne.NewContainerWithLayout(
					layout.NewVBoxLayout(),
					widget.NewLabel(i18n.T("containers.build_mode")),
					radio,
				)

				dlg := dialog.NewCustomConfirm(
					i18n.T("containers.build_title"),
					i18n.T("containers.build"),
					i18n.T("dialogs.cancel"),
					content,
					func(ok bool) {
						if !ok {
							return
						}
						go func() {
							select {
							case <-wsl.AppContext().Done():
								return
							default:
							}
							buildMode := (radio.Selected == i18n.T("containers.build_full"))

							wsl.InvalidateWSLCache()

							ctx, cancel := context.WithCancel(context.Background())
							defer cancel()

							cancelCh := make(chan struct{})
							safeUI(func() {
								progressBar.SetCancelHandler(func() {
									close(cancelCh)
									cancel()
								})
								progressBar.SetCloseHandler(func() {
									safeUI(func() {
										progressBar.Hide()
										containerList.Refresh()
									})
								})
								progressBar.Show("build", OpBuild)
							})

							opID := opManager.StartOperation("build", OpBuild)
							// Сбрасываем трекер фаз ровно один раз — в начале
							// сборки. Внутри DetectBuildPhase сброса больше нет,
							// иначе phaseStartTime прыгает и прогресс откатывается.
							wsl.ResetBuildProgress()

							// Реальный прогресс: строки вывода сборки анализируются
							// по мере их появления (wsl стримит их в onLine), фазы
							// и проценты определяются по ним, а не по таймеру.
							var outMu sync.Mutex
							var outTail []string
							lastPhaseUpdate := time.Now()

							onLine := func(line string) {
								outMu.Lock()
								outTail = append(outTail, line)
								if len(outTail) > 200 {
									outTail = outTail[len(outTail)-200:]
								}
								recent := strings.Join(outTail, "\n")
								outMu.Unlock()

								if time.Since(lastPhaseUpdate) < 500*time.Millisecond {
									return
								}
								lastPhaseUpdate = time.Now()

								phase := wsl.DetectBuildPhase(recent)
								currentOp := opManager.GetOperation(opID)
								if currentOp == nil || currentOp.Finished {
									return
								}
								if phase.Progress > currentOp.Progress {
									currentOp.Progress = phase.Progress
								}
								if currentOp.Progress > 0.95 {
									currentOp.Progress = 0.95
								}
								currentOp.Status = wsl.FormatBuildStatus(phase)
								opManager.SetOperation(opID, currentOp)
							}

							var err error
							var out string
							out, err = runBuildWithPasswordRetry(win, ctx, onLine, buildMode, func(text string) {
								onLine("#1 [internal] " + text)
							})

							if out != "" {
								lines := strings.Split(out, "\n")
								var lastLines []string
								start := 0
								if len(lines) > 20 {
									start = len(lines) - 20
								}
								lastLines = lines[start:]
								recentOutput := strings.Join(lastLines, "\n")
								phase := wsl.DetectBuildPhase(recentOutput)
								opManager.UpdateOperation("build", phase.Progress, wsl.FormatBuildStatus(phase))
							}

							if err != nil {
								errorText := err.Error()
								var buildErr *wsl.BuildError
								if !errors.As(err, &buildErr) {
									if cleanedOutput := strings.TrimSpace(wsl.CleanWSLUserOutput(out)); cleanedOutput != "" &&
									!strings.Contains(errorText, cleanedOutput) {
										outputLines := strings.Split(cleanedOutput, "\n")
										if len(outputLines) > 30 {
											outputLines = outputLines[len(outputLines)-30:]
										}
										errorText += "\n\n" + i18n.T("containers.build_output_tail") + "\n" + strings.Join(outputLines, "\n")
									}
								}
								opManager.FinishOperation(opID, false, errorText)
								safeUI(func() {
									showBuildErrorDialog(win, errorText)
								})
							} else {
								currentOp := opManager.GetOperation(opID)
								if currentOp != nil {
									currentOp.Progress = 1.0
									currentOp.Status = i18n.T("containers.build_done")
									opManager.SetOperation(opID, currentOp)
								}
								opManager.FinishOperation(opID, true, "")
							}

							containers, _ := wsl.ListContainers(true)
							safeUI(func() {
								updateRows(containers)
							})
						}()
					},
					win,
				)
				dlg.Show()
			}),
			makeBtn(i18n.T("containers.update_image"), func() {
				if selectedID == "" {
					safeUI(func() {
						dialog.ShowCustom(i18n.T("containers.select_first"), i18n.T("dialogs.ok"), widget.NewLabel(i18n.T("containers.select_first_msg")), win)
					})
					return
				}

				var currentImage string
				data.mu.RLock()
				for _, c := range data.rows {
					if c.ID == selectedID {
						currentImage = c.Image
						break
					}
				}
				data.mu.RUnlock()

				if currentImage == "" {
					safeUI(func() {
						showErrorDialog(win, i18n.T("containers.image_detect_error"))
					})
					return
				}

				safeUI(func() {
					imageEntry := widget.NewEntry()
					imageEntry.SetPlaceHolder(i18n.T("containers.image_placeholder"))
					dialog.NewCustomConfirm(i18n.T("containers.enter_image"), i18n.T("dialogs.ok"), i18n.T("dialogs.cancel"), imageEntry, func(confirmed bool) {
						if !confirmed || strings.TrimSpace(imageEntry.Text) == "" {
							return
						}
						newImage := strings.TrimSpace(imageEntry.Text)

						asyncAction(func(progress *OperationManager, cancelCh chan struct{}) error {
							progress.UpdateOperation(selectedID, 0.10, i18n.T("containers.progress_stop"))
							_, stopErr := wsl.RunWSL(fmt.Sprintf("nerdctl stop %s", wsl.ShellQuote(selectedID)))
							if stopErr != nil {
								progress.UpdateOperation(selectedID, 0.15, i18n.T("containers.progress_stop_warn"))
							}

							progress.UpdateOperation(selectedID, 0.20, i18n.T("containers.progress_remove_old_container"))
							_, err := wsl.RunWSL(fmt.Sprintf("nerdctl rm -f %s", wsl.ShellQuote(selectedID)))
							if err != nil {
								return fmt.Errorf("%s: %v", i18n.T("containers.err_remove_container"), err)
							}

							if currentImage != newImage {
								progress.UpdateOperation(selectedID, 0.30, i18n.T("containers.progress_remove_old_image"))
								_, err := wsl.RunWSL(fmt.Sprintf("nerdctl rmi -f %s", wsl.ShellQuote(currentImage)))
								if err != nil {
									progress.UpdateOperation(selectedID, 0.35, i18n.T("containers.progress_remove_old_image"))
								}
							}

							progress.UpdateOperation(selectedID, 0.40, i18n.T("containers.progress_pull"))
							pullOut, pullErr := wsl.RunWSL(fmt.Sprintf("nerdctl pull %s", wsl.ShellQuote(newImage)))
							if pullErr != nil {
								return fmt.Errorf("%s: %v", i18n.T("containers.err_pull_image"), pullErr)
							}

							if pullOut != "" {
								lines := strings.Split(pullOut, "\n")
								for i, line := range lines {
									select {
									case <-cancelCh:
										return nil
									default:
									}
									if strings.Contains(line, "Pulling") || strings.Contains(line, "Downloading") || strings.Contains(line, "Verifying") {
										progress.UpdateOperation(selectedID, 0.40+float32(i)/float32(len(lines))*0.40, line)
									}
								}
							}

							progress.UpdateOperation(selectedID, 0.85, i18n.T("containers.progress_recreate"))

							runCmd := fmt.Sprintf("nerdctl run -d --name %s", wsl.ShellQuote(currentImage))

							volumesOut, _ := wsl.RunWSL(fmt.Sprintf("nerdctl inspect --format '{{json .Mounts}}' %s 2>/dev/null", wsl.ShellQuote(selectedID)))
							if volumesOut != "" && volumesOut != "null" {
								runCmd += " --volumes-from " + wsl.ShellQuote(selectedID)
							}

							portsOut, _ := wsl.RunWSL(fmt.Sprintf("nerdctl inspect --format '{{json .HostConfig.PortBindings}}' %s 2>/dev/null", wsl.ShellQuote(selectedID)))
							if portsOut != "" && portsOut != "null" {
								runCmd += " --publish-all"
							}

							if cpu := wsl.GetDefaultCPU(); cpu != "" {
								runCmd += fmt.Sprintf(" --cpus=%s", cpu)
							}
							if mem := wsl.GetDefaultMemory(); mem != "" {
								runCmd += fmt.Sprintf(" --memory=%s", mem)
							}

							runCmd += " " + wsl.ShellQuote(newImage)

							_, runErr := wsl.RunWSL(runCmd)
							if runErr != nil {
								return fmt.Errorf("%s: %v", i18n.T("containers.err_recreate_container"), runErr)
							}

							progress.UpdateOperation(selectedID, 0.95, i18n.T("containers.progress_update_done"))
							return nil
						}, OpStart)
					}, win).Show()
				})
			}),
			btnRefresh,
		),
	)

	refresh(false)

	return withResponsiveScroll(container.NewBorder(topBar, nil, nil, nil, container.NewBorder(header, nil, nil, nil, containerList)))
}

func showErrorDialog(win fyne.Window, errMsg string) {
	showErrorDialogWithTitle(win, i18n.T("dialogs.error"), errMsg)
}

func showBuildErrorDialog(win fyne.Window, errMsg string) {
	showErrorDialogWithTitle(win, i18n.T("containers.build_error_title"), errMsg)
}

func showErrorDialogWithTitle(win fyne.Window, title, errMsg string) {
	entry := widget.NewMultiLineEntry()
	entry.SetText(errMsg)
	entry.Disable()
	entry.Wrapping = fyne.TextWrapWord

	scroll := container.NewScroll(entry)
	scroll.SetMinSize(fyne.NewSize(600, 400))

	dlg := dialog.NewCustom(title, i18n.T("dialogs.ok"), scroll, win)
	dlg.Resize(fyne.NewSize(650, 450))
	dlg.Show()
}

// promptSudoPassword показывает модальный диалог ввода пароля sudo и
// блокирующе ожидает результат (вызывается из фоновой goroutine сборки).
// Возвращает ok=false, если пользователь отменил.
func promptSudoPassword(win fyne.Window, title, detail string) (string, bool) {
	type result struct {
		password string
		ok       bool
	}
	ch := make(chan result, 1)

	safeUI(func() {
		entry := widget.NewEntry()
		entry.Password = true
		entry.SetPlaceHolder("Пароль sudo для " + wsl.GetWslDistro())

		content := container.NewVBox(
			widget.NewLabel(detail),
			entry,
		)

		dialog.NewCustomConfirm(
			title,
			i18n.T("dialogs.ok"),
			i18n.T("dialogs.cancel"),
			content,
			func(confirmed bool) {
				if confirmed && entry.Text != "" {
					ch <- result{password: entry.Text, ok: true}
				} else {
					ch <- result{ok: false}
				}
			},
			win,
		).Show()
	})

	select {
	case r := <-ch:
		return r.password, r.ok
	case <-time.After(5 * time.Minute):
		// Диалог «проглочен» — считаем отменой, чтобы goroutine сборки
		// не зависла навсегда.
		return "", false
	}
}

func runBuildWithPasswordRetry(
	win fyne.Window,
	ctx context.Context,
	onLine func(string),
	buildMode bool,
	notify func(string),
) (string, error) {
	_ = win
	_ = notify
	wsl.ResetBuildProgress()
	if buildMode {
		return wsl.BuildAndRunProject(ctx, onLine)
	}
	return wsl.RunProject(ctx, onLine)
}
