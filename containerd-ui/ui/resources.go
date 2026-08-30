package ui

import (
	"containerd-ui/wsl"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func BuildResourcesTab() fyne.CanvasObject {
	var stats []wsl.ContainerStat
	var mu sync.Mutex
	var refreshLock sync.Mutex

	// Метки для отображения системных ресурсов
	lblRAM := widget.NewLabel("RAM: —")
	lblCPU := widget.NewLabel("CPU: —")
	lblDisk := widget.NewLabel("Диск: —")

	// Стиль для меток
	setLabelStyle := func(lbl *widget.Label) {
		lbl.TextStyle = fyne.TextStyle{Bold: true}
	}
	setLabelStyle(lblRAM)
	setLabelStyle(lblCPU)
	setLabelStyle(lblDisk)

	table := widget.NewTable(
		func() (int, int) {
			mu.Lock()
			defer mu.Unlock()
			return len(stats) + 1, 6
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("Wide Header Space Text Here")
		},
		func(i widget.TableCellID, o fyne.CanvasObject) {
			label := o.(*widget.Label)
			label.Wrapping = fyne.TextTruncate

			if i.Row == 0 {
				headers := []string{"ID", "Имя", "CPU %", "Память", "Сеть I/O", "Потоки"}
				label.SetText(headers[i.Col])
				label.TextStyle = fyne.TextStyle{Bold: true}
				return
			}

			mu.Lock()
			defer mu.Unlock()
			if i.Row-1 >= len(stats) {
				return
			}
			s := stats[i.Row-1]
			switch i.Col {
			case 0:
				label.SetText(s.ID)
			case 1:
				label.SetText(s.Name)
			case 2:
				label.SetText(s.CPU)
			case 3:
				label.SetText(s.Memory)
			case 4:
				label.SetText(s.NetIO)
			case 5:
				label.SetText(s.PIDs)
			}
		},
	)

	table.SetColumnWidth(0, 90)
	table.SetColumnWidth(1, 200)
	table.SetColumnWidth(2, 90)
	table.SetColumnWidth(3, 160)
	table.SetColumnWidth(4, 160)
	table.SetColumnWidth(5, 90)

	// Обновление — объединённый batch-вызов WSL (исправление #1)
	// Раньше: 3 отдельных вызова wsl.exe (GetHostResources + GetStats + GetSystemResources)
	// Теперь: GetSystemResources уже включает RAM/CPU, GetStats — отдельный вызов для stats
	refresh := func() {
		if !refreshLock.TryLock() {
			return
		}
		defer refreshLock.Unlock()

		go func() {
			var containerStats []wsl.ContainerStat
			var sysRes *wsl.SystemResources

			var wg sync.WaitGroup
			wg.Add(2)

			go func() {
				defer wg.Done()
				select {
				case <-wsl.AppContext().Done():
					return
				default:
				}

				if s, err := wsl.GetStats(); err == nil {
					containerStats = s
				}
			}()

			go func() {
				defer wg.Done()
				select {
				case <-wsl.AppContext().Done():
					return
				default:
				}

				// GetSystemResources уже кэширует результат на 5 секунд и объединяет
				// free + nproc + loadavg + df в ОДНОМ вызове wsl.exe
				if r, err := wsl.GetSystemResources(); err == nil {
					sysRes = r
				}
			}()

			wg.Wait()

			// Обновляем UI (через safeUI для потокобезопасности)
			if sysRes != nil {
				safeUI(func() {
					lblRAM.SetText("RAM: " + sysRes.RAMUsed + " / " + sysRes.RAMTotal + " (Свободно: " + sysRes.RAMFree + ")")
					lblCPU.SetText("CPU: " + sysRes.CPUCores + " ядер | Загрузка: " + sysRes.CPULoad)
					lblDisk.SetText("Диск: " + sysRes.DiskUsed + " / " + sysRes.DiskTotal + " (Свободно: " + sysRes.DiskFree + ")")
				})
			}

			mu.Lock()
			stats = containerStats
			mu.Unlock()
			safeUI(func() {
				table.Refresh()
				lblRAM.Refresh()
				lblCPU.Refresh()
				lblDisk.Refresh()
			})
		}()
	}

	// Создаём карточки для отображения системных ресурсов
	resourceCards := container.NewVBox(
		container.NewBorder(nil, nil, nil, widget.NewLabelWithStyle("Системные ресурсы", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			container.NewHBox(
				container.NewVBox(
					widget.NewLabelWithStyle("Оперативная память", fyne.TextAlignLeading, fyne.TextStyle{Bold: false}),
					lblRAM,
				),
				container.NewVBox(
					widget.NewLabelWithStyle("Процессор", fyne.TextAlignLeading, fyne.TextStyle{Bold: false}),
					lblCPU,
				),
				container.NewVBox(
					widget.NewLabelWithStyle("Диск (/)", fyne.TextAlignLeading, fyne.TextStyle{Bold: false}),
					lblDisk,
				),
			),
		),
		widget.NewSeparator(),
	)

	// Добавляем заголовок над таблицей
	topBar := container.NewBorder(
		resourceCards,
		nil, nil, nil,
		table,
	)

	// Управляем активностью вкладки: тикер останавливается при скрытии
	tab := newTabActive(true, TickerResources, refresh)

	// Регистрируем в глобальной карте по имени вкладки
	registerTabNamed("Ресурсы", tab)

	// Первый запуск при открытии вкладки
	refresh()

	return topBar
}
