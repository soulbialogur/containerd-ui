// containers.go - оптимизированная версия BuildContainersTab
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

// safeUI выполняет функцию в главном потоке UI (потокобезопасное обновление)
func safeUI(f func()) {
	fyne.Do(f)
}

// tableData — оптимизированная структура для хранения данных таблицы
type tableData struct {
	mu   sync.RWMutex
	rows []wsl.Container
	idx  map[string]int // ID → индекс (O(1) поиск вместо линейного)
}

// runContainerOperations выполняет массовую операцию с ограничением числа запросов.
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

// newDataTable создаёт новую структуру данных таблицы
func newDataTable() *tableData {
	return &tableData{
		rows: make([]wsl.Container, 0, 32),
		idx:  make(map[string]int, 32),
	}
}

// getRows возвращает строки для отображения
func (td *tableData) getRows() []wsl.Container {
	td.mu.RLock()
	defer td.mu.RUnlock()
	result := make([]wsl.Container, len(td.rows))
	copy(result, td.rows)
	return result
}

// setRows обновляет строки и индексную мапу
func (td *tableData) setRows(rows []wsl.Container) {
	td.mu.Lock()
	td.rows = rows
	// Обновляем индексную мапу
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

// getIndex возвращает индекс контейнера по ID (O(1))
func (td *tableData) getIndex(id string) (int, bool) {
	td.mu.RLock()
	defer td.mu.RUnlock()
	idx, ok := td.idx[id]
	return idx, ok
}

// getRowCount возвращает количество строк
func (td *tableData) getRowCount() int {
	td.mu.RLock()
	defer td.mu.RUnlock()
	return len(td.rows)
}

// getRow возвращает строку по индексу
func (td *tableData) getRow(index int) (wsl.Container, bool) {
	td.mu.RLock()
	defer td.mu.RUnlock()
	if index < 0 || index >= len(td.rows) {
		return wsl.Container{}, false
	}
	return td.rows[index], true
}

// clear очищает данные
func (td *tableData) clear() {
	td.mu.Lock()
	td.rows = td.rows[:0] // Reset slice without freeing memory
	td.mu.Unlock()
}

func BuildContainersTab(win fyne.Window) fyne.CanvasObject {
	// Используем оптимизированную структуру данных
	data := newDataTable()
	var selectedID string

	// Прогресс-бар (объявляем раньше для использования в callbacks)
	progressBar := NewProgressBarComponent()

	// Менеджер операций
	opManager := NewOperationManager()

	// Подключаем событийное обновление UI
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

	// Обновление с debounce и более длинным интервалом для экономии CPU/IO.
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

	// Debounce для OnSelected — защита от повторных кликов
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

	// Асинхронные действия с прогресс-баром
	// Исправление #3: sync.WaitGroup для гарантированной остановки progress-горутин
	asyncAction := func(action func(progress *OperationManager, cancelCh chan struct{}) error, opType OperationType) {
		go func() {
			// Проверяем контекст приложения
			select {
			case <-wsl.AppContext().Done():
				return
			default:
			}

			cancelCh := make(chan struct{})
			var wg sync.WaitGroup // 👈 защита от утечек

			safeUI(func() {
				progressBar.SetCancelHandler(func() {
					select {
					case <-cancelCh:
						// Уже закрыт — ничего не делаем
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

			// Начинаем операцию
			opID := opManager.StartOperation(selectedID, opType)

			// Эмуляция прогресса (для операций с таймаутом)
			progressTicker := time.NewTicker(TickerProgress)
			defer progressTicker.Stop()

			// Goroutine прогресса — защищена WaitGroup для гарантированного выхода
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-progressTicker.C:
						// Обновляем прогресс
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
						// Приложение закрывается — выходим
						return
					}
				}
			}()

			// Выполняем действие
			err := action(opManager, cancelCh)

			// Завершаем operation
			if err != nil {
				opManager.FinishOperation(opID, false, err.Error())
			} else {
				// Убедимся, что прогресс 100%
				currentOp := opManager.GetOperation(opID)
				if currentOp != nil {
					currentOp.Progress = 1.0
					currentOp.Status = "Завершено успешно"
					opManager.SetOperation(opID, currentOp)
				}
				opManager.FinishOperation(opID, true, "")
			}

			// Останавливаем goroutine прогресса (безопасно — проверяем закрыт ли канал)
			select {
			case <-cancelCh:
				// Уже закрыт
			default:
				close(cancelCh)
			}
			wg.Wait() // 👈 ЖДЁМ завершения progress-горутины

			// Обновляем данные таблицы и список контейнеров
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
				// Если выбран контейнер — останавливаем его
				if selectedID != "" {
					asyncAction(func(progress *OperationManager, cancelCh chan struct{}) error {
						progress.UpdateOperation(selectedID, 0.1, "Остановка...")
						return wsl.StopContainer(selectedID)
					}, OpStop)
				} else {
					// Если ничего не выбрано — останавливаем ВСЕ контейнеры
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
				// Если выбран контейнер — удаляем его
				if selectedID != "" {
					dialog.ShowConfirm("Удаление", fmt.Sprintf("Удалить контейнер %s?", selectedID), func(ok bool) {
						if ok {
							asyncAction(func(progress *OperationManager, cancelCh chan struct{}) error {
								// Принудительно убиваем
								_, err := wsl.RunWSL("nerdctl kill " + selectedID + " 2>/dev/null; echo 'kill_done'")
								if err != nil {
									progress.UpdateOperation(selectedID, 0.2, "Внимание: ошибка kill — продолжаем...")
								}

								// Удаляем
								progress.UpdateOperation(selectedID, 0.6, "Удаление...")
								return wsl.RemoveContainer(selectedID)
							}, OpRemove)
						}
					}, win)
				} else {
					// Если ничего не выбрано — удаляем ВСЕ контейнеры
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

								// ОПТИМИЗАЦИЯ: Два вызова вместо N*2
								// Собираем ID всех контейнеров
								var ids []string
								for _, c := range containers {
									ids = append(ids, c.ID)
								}

								// ШАГ 1: Один kill для всех
								killCmd := "nerdctl kill " + strings.Join(ids, " ") + " 2>/dev/null"
								_, _ = wsl.RunWSL(killCmd)

								// ШАГ 2: Один rm -f для всех
								rmCmd := "nerdctl rm -f " + strings.Join(ids, " ") + " 2>/dev/null"
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

						// Запускаем длительную операцию в горутине
						go func() {
							// Проверяем контекст приложения
							select {
							case <-wsl.AppContext().Done():
								return
							default:
							}

							buildMode := (radio.Selected == "Собрать весь проект")

							// Сбрасываем кэш WSL перед сборкой
							wsl.InvalidateWSLCache()

							// Создаём cancellable context для отмены сборки
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

							// Выполняем сборку/запуск
							var err error
							var out string
							if buildMode {
								out, err = wsl.BuildAndRunProject(ctx)
							} else {
								out, err = wsl.RunProject(ctx)
							}

							// Парсим фазу сборки по выводу
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

							// Завершаем операцию
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

							// Обновляем таблицу контейнеров
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

				// Ищем контейнер в данных
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

				// Используем EntryDialog
				safeUI(func() {
					dialog.ShowEntryDialog("Введите новый образ", "myapp:latest", func(value string) {
						if value == "" {
							return
						}
					newImage := strings.TrimSpace(value)

					asyncAction(func(progress *OperationManager, cancelCh chan struct{}) error {
						progress.UpdateOperation(selectedID, 0.10, "Остановка контейнера...")
						_, stopErr := wsl.RunWSL(fmt.Sprintf("nerdctl stop %s", selectedID))
						if stopErr != nil {
							progress.UpdateOperation(selectedID, 0.15, "Внимание: не удалось остановить, продолжаем...")
						}

						progress.UpdateOperation(selectedID, 0.20, "Удаление старого контейнера...")
						_, err := wsl.RunWSL(fmt.Sprintf("nerdctl rm -f %s", selectedID))
						if err != nil {
							return fmt.Errorf("не удалось удалить контейнер: %w", err)
						}

						if currentImage != newImage {
							progress.UpdateOperation(selectedID, 0.30, "Удаление старого образа...")
							_, err := wsl.RunWSL(fmt.Sprintf("nerdctl rmi -f %s", currentImage))
							if err != nil {
								progress.UpdateOperation(selectedID, 0.35, "Внимание: старый образ не удалён")
							}
						}

						progress.UpdateOperation(selectedID, 0.40, "Загрузка нового образа...")
						pullOut, pullErr := wsl.RunWSL(fmt.Sprintf("nerdctl pull %s", newImage))
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

							runCmd := fmt.Sprintf("nerdctl run -d --name %s", currentImage)

							volumesOut, _ := wsl.RunWSL(fmt.Sprintf("nerdctl inspect --format '{{json .Mounts}}' %s 2>/dev/null", selectedID))
							if volumesOut != "" && volumesOut != "null" {
								runCmd += " --volumes-from " + selectedID
							}

							portsOut, _ := wsl.RunWSL(fmt.Sprintf("nerdctl inspect --format '{{json .HostConfig.PortBindings}}' %s 2>/dev/null", selectedID))
							if portsOut != "" && portsOut != "null" {
								runCmd += " --publish-all"
							}

							// Добавляем лимиты CPU и памяти из настроек
							if cpu := wsl.GetDefaultCPU(); cpu != "" {
								runCmd += fmt.Sprintf(" --cpus=%s", cpu)
							}
							if mem := wsl.GetDefaultMemory(); mem != "" {
								runCmd += fmt.Sprintf(" --memory=%s", mem)
							}

							runCmd += " " + newImage

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

// showErrorDialog показывает кастомный диалог с ошибкой, поддерживающий прокрутку, перенос и копирование текста
func showErrorDialog(win fyne.Window, errMsg string) {
	// Создаём Entry вместо Label — он поддерживает выделение текста для копирования
	entry := widget.NewMultiLineEntry()
	entry.SetText(errMsg)
	entry.Disable() // Делает Entry только для чтения (выглядит как Label)
	entry.Wrapping = fyne.TextWrapWord

	// Создаём скроллируемый контейнер
	scroll := container.NewScroll(entry)
	scroll.SetMinSize(fyne.NewSize(600, 400))

	// Создаём кастомный диалог
	dlg := dialog.NewCustomConfirm(
		"Ошибка сборки",
		"OK",
		"",
		scroll,
		func(closed bool) {
			// Ничего не делаем — просто закрываем
		},
		win,
	)
	dlg.Resize(fyne.NewSize(650, 450))
	dlg.Show()
}