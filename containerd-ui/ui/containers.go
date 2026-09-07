package ui

import (
	"containerd-ui/wsl"
	"context"
	"fmt"
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

type tableData struct {
	mu   sync.RWMutex
	rows []wsl.Container
	idx  map[string]int
}

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
	td.rows = rows
	if td.idx == nil {
		td.idx = make(map[string]int, len(rows))
	} else {
		for k := range td.idx {
			delete(td.idx, k)
		}
	}
	for i, c := range rows {
		td.idx[c.ID] = i
	}
	td.mu.Unlock()
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
		ops := opManager.GetActiveOperations()
		safeUI(func() {
			if len(ops) > 0 {
				progressBar.Update(ops[0])
			} else {
				progressBar.Hide()
			}
		})
	})

	newContainerRow := func() fyne.CanvasObject {
		labels := make([]fyne.CanvasObject, 5)
		for i := range labels {
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
			values := []string{row.ID, row.Name, row.Image, wsl.TranslateStatus(row.Status), row.Ports}
			for i, value := range values {
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

	header := container.NewGridWithColumns(5,
		widget.NewLabelWithStyle("ID", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle("Имя", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle("Образ", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle("Статус", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle("Порты", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
	)

	var refreshTimer *time.Timer
	var lastRefresh time.Time
	refresh := func() {
		if time.Since(lastRefresh) < 3*time.Second {
			return
		}
		lastRefresh = time.Now()
		if refreshTimer != nil {
			refreshTimer.Stop()
		}
		refreshTimer = time.AfterFunc(750*time.Millisecond, func() {
			go func() {
				select {
				case <-wsl.AppContext().Done():
					return
				default:
				}
				data.clear()
				containers, err := wsl.ListContainers(true)
				if err == nil {
					data.setRows(containers)
					safeUI(func() {
						containerList.Refresh()
					})
				}
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
							safeUI(func() {
								progressBar.Update(currentOp)
							})
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
					currentOp.Status = "Завершено успешно"
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
				data.setRows(containers)
				safeUI(func() {
					containerList.Refresh()
				})
			}
		}()
	}

	makeBtn := func(text string, tapped func()) fyne.CanvasObject {
		return widget.NewButton(text, tapped)
	}

	topBar := container.NewBorder(
		progressBar.Widget(),
		nil, nil, nil,
		container.NewAdaptiveGrid(4,
			makeBtn("Запустить", func() {
				if selectedID != "" {
					asyncAction(func(progress *OperationManager, cancelCh chan struct{}) error {
						progress.UpdateOperation(selectedID, 0.1, "Запуск...")
						return wsl.StartContainer(selectedID)
					}, OpStart)
				} else {
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
				}
			}),
			makeBtn("Остановить", func() {
				if selectedID != "" {
					asyncAction(func(progress *OperationManager, cancelCh chan struct{}) error {
						progress.UpdateOperation(selectedID, 0.1, "Остановка...")
						return wsl.StopContainer(selectedID)
					}, OpStop)
				} else {
					asyncAction(func(progress *OperationManager, cancelCh chan struct{}) error {
						containers, err := wsl.ListContainers(true)
						if err != nil {
							return err
						}
						var running []wsl.Container
						for _, c := range containers {
							if strings.Contains(strings.ToLower(c.Status), "running") || strings.Contains(strings.ToLower(c.Status), "up") {
								running = append(running, c)
							}
						}
						if len(running) == 0 {
							return nil
						}
						return runContainerOperations(running, cancelCh, wsl.StopContainer)
					}, OpStop)
				}
			}),
			makeBtn("Перезапустить", func() {
				if selectedID != "" {
					asyncAction(func(progress *OperationManager, cancelCh chan struct{}) error {
						progress.UpdateOperation(selectedID, 0.1, "Остановка...")
						time.Sleep(SleepOperation)
						err := wsl.StopContainer(selectedID)
						if err != nil {
							return err
						}
						progress.UpdateOperation(selectedID, 0.5, "Запуск...")
						time.Sleep(SleepOperation)
						return wsl.StartContainer(selectedID)
					}, OpRestart)
				}
			}),
			makeBtn("Удалить", func() {
				if selectedID != "" {
					dialog.ShowConfirm("Удаление", fmt.Sprintf("Удалить контейнер %s?", selectedID), func(ok bool) {
						if ok {
							asyncAction(func(progress *OperationManager, cancelCh chan struct{}) error {
								_, err := wsl.RunWSL("nerdctl kill " + wsl.ShellQuote(selectedID) + " 2>/dev/null; echo 'kill_done'")
								if err != nil {
									progress.UpdateOperation(selectedID, 0.2, "Внимание: ошибка kill — продолжаем...")
								}
								progress.UpdateOperation(selectedID, 0.6, "Удаление...")
								return wsl.RemoveContainer(selectedID)
							}, OpRemove)
						}
					}, win)
				} else {
					dialog.ShowConfirm("Удаление всех", "Удалить ВСЕ контейнеры?", func(ok bool) {
						if ok {
							asyncAction(func(progress *OperationManager, cancelCh chan struct{}) error {
								containers, err := wsl.ListContainers(true)
								if err != nil {
									return err
								}
								if len(containers) == 0 {
									return nil
								}
								var ids []string
								for _, c := range containers {
									ids = append(ids, c.ID)
								}
								var quotedIDs []string
								for _, id := range ids {
									quotedIDs = append(quotedIDs, wsl.ShellQuote(id))
								}
								killCmd := "nerdctl kill " + strings.Join(quotedIDs, " ") + " 2>/dev/null"
								_, _ = wsl.RunWSL(killCmd)
								rmCmd := "nerdctl rm -f " + strings.Join(quotedIDs, " ") + " 2>/dev/null"
								_, _ = wsl.RunWSL(rmCmd)
								return nil
							}, OpRemove)
						}
					}, win)
				}
			}),
			makeBtn("Собрать", func() {
				radio := widget.NewRadioGroup([]string{"Собрать весь проект", "Только контейнеры (без сборки)"}, nil)
				radio.Horizontal = true
				radio.SetSelected("Собрать весь проект")

				content := fyne.NewContainerWithLayout(
					layout.NewVBoxLayout(),
					widget.NewLabel("Режим сборки:"),
					radio,
				)

				dlg := dialog.NewCustomConfirm(
					"Сборка и запуск",
					"Собрать",
					"Отмена",
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
							buildMode := (radio.Selected == "Собрать весь проект")

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

							var err error
							var out string
							if buildMode {
								out, err = wsl.BuildAndRunProject(ctx)
							} else {
								out, err = wsl.RunProject(ctx)
							}

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
								opManager.FinishOperation(opID, false, err.Error())
								safeUI(func() {
									showErrorDialog(win, err.Error())
								})
							} else {
								currentOp := opManager.GetOperation(opID)
								if currentOp != nil {
									currentOp.Progress = 1.0
									currentOp.Status = "✅ Сборка завершена успешно"
									opManager.SetOperation(opID, currentOp)
								}
								opManager.FinishOperation(opID, true, "")
							}

							containers, _ := wsl.ListContainers(true)
							data.setRows(containers)
							safeUI(func() {
								containerList.Refresh()
							})
						}()
					},
					win,
				)
				dlg.Show()
			}),
			makeBtn("Обновить образ", func() {
				if selectedID == "" {
					safeUI(func() {
						dialog.ShowCustom("Выберите контейнер", "ОК", widget.NewLabel("Сначала выберите контейнер для обновления"), win)
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
						dialog.ShowError(fmt.Errorf("не удалось определить образ контейнера"), win)
					})
					return
				}

				safeUI(func() {
					dialog.ShowEntryDialog("Введите новый образ", "myapp:latest", func(value string) {
						if value == "" {
							return
						}
						newImage := strings.TrimSpace(value)

						asyncAction(func(progress *OperationManager, cancelCh chan struct{}) error {
							progress.UpdateOperation(selectedID, 0.10, "Остановка контейнера...")
							_, stopErr := wsl.RunWSL(fmt.Sprintf("nerdctl stop %s", wsl.ShellQuote(selectedID)))
							if stopErr != nil {
								progress.UpdateOperation(selectedID, 0.15, "Внимание: не удалось остановить, продолжаем...")
							}

							progress.UpdateOperation(selectedID, 0.20, "Удаление старого контейнера...")
							_, err := wsl.RunWSL(fmt.Sprintf("nerdctl rm -f %s", wsl.ShellQuote(selectedID)))
							if err != nil {
								return fmt.Errorf("не удалось удалить контейнер: %w", err)
							}

							if currentImage != newImage {
								progress.UpdateOperation(selectedID, 0.30, "Удаление старого образа...")
								_, err := wsl.RunWSL(fmt.Sprintf("nerdctl rmi -f %s", wsl.ShellQuote(currentImage)))
								if err != nil {
									progress.UpdateOperation(selectedID, 0.35, "Внимание: старый образ не удалён")
								}
							}

							progress.UpdateOperation(selectedID, 0.40, "Загрузка нового образа...")
							pullOut, pullErr := wsl.RunWSL(fmt.Sprintf("nerdctl pull %s", wsl.ShellQuote(newImage)))
							if pullErr != nil {
								return fmt.Errorf("не удалось загрузить образ: %w", pullErr)
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

							progress.UpdateOperation(selectedID, 0.85, "Пересоздание контейнера...")

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
								return fmt.Errorf("не удалось пересоздать контейнер: %w", runErr)
							}

							progress.UpdateOperation(selectedID, 0.95, "Обновление завершено!")
							return nil
						}, OpStart)
					}, win)
				})
			}),
			makeBtn("Обновить", refresh),
		),
	)

	refresh()

	return withResponsiveScroll(container.NewBorder(topBar, nil, nil, nil, container.NewBorder(header, nil, nil, nil, containerList)))
}

func showErrorDialog(win fyne.Window, errMsg string) {
	entry := widget.NewMultiLineEntry()
	entry.SetText(errMsg)
	entry.Disable()
	entry.Wrapping = fyne.TextWrapWord

	scroll := container.NewScroll(entry)
	scroll.SetMinSize(fyne.NewSize(600, 400))

	dlg := dialog.NewCustomConfirm(
		"Ошибка сборки",
		"OK",
		"",
		scroll,
		func(closed bool) {},
		win,
	)
	dlg.Resize(fyne.NewSize(650, 450))
	dlg.Show()
}