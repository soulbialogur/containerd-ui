package ui

import (
	"containerd-ui/i18n"
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

	newVolumeRow := func() fyne.CanvasObject {
		labels := make([]fyne.CanvasObject, 3)
		for i := range labels {
			label := widget.NewLabel("")
			label.Wrapping = fyne.TextTruncate
			labels[i] = label
		}
		return container.NewGridWithColumns(3, labels...)
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
			values := []string{name, v.Driver, mount}
			for i, value := range values {
				labels[i].(*widget.Label).SetText(value)
			}
		},
	)

	header := container.NewGridWithColumns(3,
		widget.NewLabelWithStyle(i18n.T("volumes.header_name"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle(i18n.T("volumes.header_type"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle(i18n.T("volumes.header_mount"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
	)

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
				volumes = data
				safeUI(func() {
					volumeList.Refresh()
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
		dialog.ShowConfirm(
			i18n.T("volumes.remove_title"),
			i18n.T("volumes.confirm_remove", volumeName),
			func(ok bool) {
				if ok {
					go func(name string) {
						select {
						case <-wsl.AppContext().Done():
							return
						default:
						}

						removeErr := wsl.RemoveVolume(name)
						data, err := wsl.ListVolumes()
						if err == nil {
							volumes = data
							safeUI(func() {
								volumeList.Refresh()
							})
						}
						if removeErr != nil {
							safeUI(func() {
								dialog.ShowError(removeErr, win)
							})
							return
						}
						selectedName = ""
					}(volumeName)
				}
			},
			win,
		)
	})

	btnRefresh = widget.NewButton(i18n.T("volumes.refresh"), refresh)
	topBar := container.NewHBox(btnRemove, btnRefresh)
	refresh()

	return withResponsiveScroll(container.NewBorder(topBar, nil, nil, nil, container.NewBorder(header, nil, nil, nil, volumeList)))
}