package ui

import (
	"containerd-ui/i18n"
	"containerd-ui/wsl"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func isMissingDBVolumeError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "том не найден") ||
		strings.Contains(msg, "volume not found") ||
		(strings.Contains(msg, "not found") && strings.Contains(msg, "db"))
}

func BuildDatabaseTab() fyne.CanvasObject {
	volName := wsl.GetDBVolumeName()

	lblSize := widget.NewLabel(i18n.T("database.size_unknown"))
	emptyState := widget.NewLabel(i18n.T("database.volume_missing"))
	emptyState.Wrapping = fyne.TextWrapWord
	emptyState.Hidden = true

	var files []string
	filesList := widget.NewList(
		func() int { return len(files) },
		func() fyne.CanvasObject { return widget.NewLabel("...") },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			o.(*widget.Label).SetText(files[i])
		},
	)

	var btnCheck *widget.Button
	btnCheck = widget.NewButton(i18n.T("database.check"), func() {
		go func() {
			safeUI(func() { setRefreshButtonLoading(btnCheck, i18n.T("database.check"), true) })
			defer safeUI(func() { setRefreshButtonLoading(btnCheck, i18n.T("database.check"), false) })
			select {
			case <-wsl.AppContext().Done():
				return
			default:
			}

			size, dbFiles, err := wsl.GetDBInfo(volName)
			safeUI(func() {
				if err == nil {
					lblSize.SetText(i18n.T("database.size", size))
					files = dbFiles
					emptyState.Hidden = true
					filesList.Refresh()
					return
				}
				if isMissingDBVolumeError(err) {
					lblSize.SetText(i18n.T("database.size_unknown"))
					files = nil
					emptyState.SetText(i18n.T("database.volume_missing"))
					emptyState.Hidden = false
					filesList.Refresh()
					return
				}
				lblSize.SetText(i18n.T("database.read_error", volName, localizedWslError(err)))
				emptyState.Hidden = true
				files = nil
				filesList.Refresh()
			})
		}()
	})

	topBar := container.NewHBox(
		widget.NewLabel(i18n.T("database.volume")),
		widget.NewLabel(volName),
		btnCheck,
		lblSize,
	)

	btnCheck.OnTapped()

	content := container.NewBorder(emptyState, nil, nil, nil, filesList)
	return withResponsiveScroll(container.NewBorder(topBar, nil, nil, nil, content))
}
