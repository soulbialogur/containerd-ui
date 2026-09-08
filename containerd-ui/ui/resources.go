package ui

import (
	"containerd-ui/i18n"
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

	lblRAM := widget.NewLabel(i18n.T("resources.ram", "—", "—", "—"))
	lblCPU := widget.NewLabel(i18n.T("resources.cpu_info", "—", "—"))
	lblDisk := widget.NewLabel(i18n.T("resources.disk_info", "—", "—", "—"))

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
				headers := []string{
					i18n.T("containers.id"),
					i18n.T("containers.name"),
					i18n.T("resources.cpu_percent"),
					i18n.T("resources.memory"),
					i18n.T("resources.net_io"),
					i18n.T("resources.pids"),
				}
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

	table.SetColumnWidth(0, 55)
	table.SetColumnWidth(1, 100)
	table.SetColumnWidth(2, 55)
	table.SetColumnWidth(3, 90)
	table.SetColumnWidth(4, 90)
	table.SetColumnWidth(5, 55)

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

				if r, err := wsl.GetSystemResources(); err == nil {
					sysRes = r
				}
			}()

			wg.Wait()

			if sysRes != nil {
				safeUI(func() {
					lblRAM.SetText(i18n.T("resources.ram", sysRes.RAMUsed, sysRes.RAMTotal, sysRes.RAMFree))
					lblCPU.SetText(i18n.T("resources.cpu_info", sysRes.CPUCores, sysRes.CPULoad))
					lblDisk.SetText(i18n.T("resources.disk_info", sysRes.DiskUsed, sysRes.DiskTotal, sysRes.DiskFree))
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

	resourceCards := container.NewVBox(
		container.NewBorder(nil, nil, nil, widget.NewLabelWithStyle(i18n.T("resources.title"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			container.NewHBox(
				container.NewVBox(
					widget.NewLabelWithStyle(i18n.T("resources.ram_label"), fyne.TextAlignLeading, fyne.TextStyle{Bold: false}),
					lblRAM,
				),
				container.NewVBox(
					widget.NewLabelWithStyle(i18n.T("resources.cpu_label"), fyne.TextAlignLeading, fyne.TextStyle{Bold: false}),
					lblCPU,
				),
				container.NewVBox(
					widget.NewLabelWithStyle(i18n.T("resources.disk_label"), fyne.TextAlignLeading, fyne.TextStyle{Bold: false}),
					lblDisk,
				),
			),
		),
		widget.NewSeparator(),
	)

	topBar := container.NewBorder(
		resourceCards,
		nil, nil, nil,
		table,
	)

	tab := newTabActive(true, TickerResources, refresh)
	registerTabNamed(i18n.T("tabs.resources"), tab)

	refresh()

	return withResponsiveScroll(topBar)
}