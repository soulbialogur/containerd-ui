package ui

import (
	"containerd-ui/wsl"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

func BuildVolumesTab(win fyne.Window) fyne.CanvasObject {
	var volumes []wsl.Volume
	selectedName := ""

	table := widget.NewTable(
		func() (int, int) { return len(volumes) + 1, 3 },
		func() fyne.CanvasObject {
			return widget.NewLabel("Wide Header Space Text Here")
		},
		func(i widget.TableCellID, o fyne.CanvasObject) {
			label := o.(*widget.Label)
			label.Wrapping = fyne.TextTruncate

			if i.Row == 0 {
				headers := []string{"Имя тома", "Тип", "Точка монтирования"}
				label.SetText(headers[i.Col])
				label.TextStyle = fyne.TextStyle{Bold: true}
				return
			}

			v := volumes[i.Row-1]
			switch i.Col {
			case 0:
				name := v.Name
				if len(name) > 35 {
					name = name[:32] + "..."
				}
				label.SetText(name)
			case 1:
				label.SetText(v.Driver)
			case 2:
				mp := v.Mountpoint
				if len(mp) > 45 {
					mp = "..." + mp[len(mp)-42:]
				}
				label.SetText(mp)
			}
		},
	)

	table.SetColumnWidth(0, 100)
	table.SetColumnWidth(1, 60)
	table.SetColumnWidth(2, 140)

	// Debounce для refresh — защита от частых вызовов
	var refreshTimer *time.Timer
	var lastRefresh time.Time

	refresh := func() {
		if time.Since(lastRefresh) < 2*time.Second {
			return
		}
		lastRefresh = time.Now()

		if refreshTimer != nil {
			refreshTimer.Stop()
		}

		refreshTimer = time.AfterFunc(DebounceVolumeRefresh, func() {
			select {
			case <-wsl.AppContext().Done():
				return
			default:
			}

			data, err := wsl.ListVolumes()
			if err == nil {
				volumes = data
				safeUI(func() {
					table.Refresh()
				})
			}
		})
	}

	table.OnSelected = func(id widget.TableCellID) {
		if id.Row > 0 && id.Row-1 < len(volumes) {
			selectedName = volumes[id.Row-1].Name
		}
	}

	btnRemove := widget.NewButton("Удалить том", func() {
		if selectedName != "" {
			if strings.HasPrefix(selectedName, "soul-dialogue-") {
				dialog.ShowCustom("Защита", "ОК", widget.NewLabel("Нельзя удалить системный том!"), win)
				return
			}
			dialog.ShowConfirm("Удаление", "Удалить том "+selectedName+"?", func(ok bool) {
				if ok {
					go func() {
						select {
						case <-wsl.AppContext().Done():
							return
						default:
						}

						wsl.RemoveVolume(selectedName)
						data, err := wsl.ListVolumes()
						if err == nil {
							volumes = data
							safeUI(func() {
								table.Refresh()
							})
						}
					}()
					selectedName = ""
				}
			}, win)
		}
	})
	btnRefresh := widget.NewButton("Обновить", refresh)

	topBar := container.NewHBox(btnRemove, btnRefresh)
	refresh()

	return withResponsiveScroll(container.NewBorder(topBar, nil, nil, nil, table))
}
