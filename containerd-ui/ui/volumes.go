package ui

import (
	"containerd-ui/i18n"
	"containerd-ui/wsl"
	"context"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

func BuildVolumesTab(win fyne.Window) fyne.CanvasObject {
	var volumes []wsl.Volume
	var volumeLoadGeneration uint64
	selectedName := ""

	newVolumeRow := func() fyne.CanvasObject {
		labels := make([]fyne.CanvasObject, 4)
		for i := range labels {
			label := widget.NewLabel("")
			label.Wrapping = fyne.TextTruncate
			labels[i] = label
		}
		return container.NewGridWithColumns(4, labels...)
	}

	volumeList := widget.NewList(
		func() int { return len(volumes) },
		newVolumeRow,
		func(id widget.ListItemID, object fyne.CanvasObject) {
			if id < 0 || id >= len(volumes) {
				return
			}
			v := volumes[id]
			labels := object.(*fyne.Container).Objects
			name := v.Name
			if len(name) > 35 {
				name = name[:32] + "..."
			}
			mount := v.Mountpoint
			if len(mount) > 45 {
				mount = "..." + mount[len(mount)-42:]
			}
			values := []string{name, v.Driver, mount, v.Size}
			for i, value := range values {
				labels[i].(*widget.Label).SetText(value)
			}
		},
	)

	header := container.NewGridWithColumns(4,
		widget.NewLabelWithStyle(i18n.T("volumes.header_name"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle(i18n.T("volumes.header_type"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle(i18n.T("volumes.header_mount"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle(i18n.T("volumes.header_size"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
	)
	loadVolumes := func(data []wsl.Volume) {
		volumeLoadGeneration++
		generation := volumeLoadGeneration
		volumes = append([]wsl.Volume(nil), data...)
		for index := range volumes {
			volumes[index].Size = "..."
		}
		volumeList.Refresh()
		snapshot := append([]wsl.Volume(nil), volumes...)
		go func() {
			ctx, cancel := context.WithCancel(wsl.AppContext())
			defer cancel()
			sizes := wsl.GetVolumeSizes(ctx, snapshot)
			if ctx.Err() != nil {
				return
			}
			safeUI(func() {
				if generation != volumeLoadGeneration {
					return
				}
				for index := range volumes {
					if size, ok := sizes[volumes[index].Name]; ok {
						volumes[index].Size = size
					} else {
						volumes[index].Size = "—"
					}
				}
				volumeList.Refresh()
			})
		}()
	}

	var btnRefresh *widget.Button
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
			safeUI(func() { setRefreshButtonLoading(btnRefresh, i18n.T("volumes.refresh"), true) })
			defer safeUI(func() { setRefreshButtonLoading(btnRefresh, i18n.T("volumes.refresh"), false) })
			select {
			case <-wsl.AppContext().Done():
				return
			default:
			}

			data, err := wsl.ListVolumes()
			if err == nil {
				safeUI(func() {
					loadVolumes(data)
				})
			}
		})
	}

	volumeList.OnSelected = func(id widget.ListItemID) {
		if id >= 0 && id < len(volumes) {
			selectedName = strings.TrimSpace(volumes[id].Name)
		}
	}

	btnRemove := widget.NewButton(i18n.T("volumes.remove"), func() {
		selectedName = strings.TrimSpace(selectedName)
		if selectedName == "" {
			return
		}
		volumeName := selectedName
		confirmDialog := dialog.NewCustomConfirm(
			i18n.T("volumes.remove_title"),
			i18n.T("dialogs.ok"),
			i18n.T("dialogs.cancel"),
			widget.NewLabel(i18n.T("volumes.confirm_remove", volumeName)),
			func(ok bool) {
				if ok {
					previousVolumes := append([]wsl.Volume(nil), volumes...)
					selectedName = ""
					for index, volume := range volumes {
						if volume.Name == volumeName {
							volumes = append(volumes[:index], volumes[index+1:]...)
							break
						}
					}
					volumeLoadGeneration++
					volumeList.Refresh()

					go func(name string) {
						select {
						case <-wsl.AppContext().Done():
							return
						default:
						}

						removeErr := wsl.RemoveVolume(name)
						data, listErr := wsl.ListVolumes()
						safeUI(func() {
							if listErr == nil {
								loadVolumes(data)
							} else if removeErr != nil {
								loadVolumes(previousVolumes)
							} else {
								volumeList.Refresh()
							}
							if removeErr != nil {
								showAppError(win, removeErr)
							} else if listErr != nil {
								showAppError(win, listErr)
							}
						})
					}(volumeName)
				}
			},
			win,
		)
		confirmDialog.Resize(fyne.NewSize(500, 180))
		confirmDialog.Show()
	})

	btnRefresh = widget.NewButton(i18n.T("volumes.refresh"), refresh)
	topBar := container.NewHBox(btnRemove, btnRefresh)
	refresh()

	return withResponsiveScroll(container.NewBorder(topBar, nil, nil, nil, container.NewBorder(header, nil, nil, nil, volumeList)))
}