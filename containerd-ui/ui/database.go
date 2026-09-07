package ui

import (
	"containerd-ui/i18n"
	"containerd-ui/wsl"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func BuildDatabaseTab() fyne.CanvasObject {
	volName := wsl.GetDBVolumeName()

	lblSize := widget.NewLabel(i18n.T("database.size_unknown"))

	var files []string
	filesList := widget.NewList(
		func() int { return len(files) },
		func() fyne.CanvasObject { return widget.NewLabel("...") },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			o.(*widget.Label).SetText(files[i])
		},
	)

	btnCheck := widget.NewButton(i18n.T("database.check"), func() {
		go func() {
			select {
			case <-wsl.AppContext().Done():
				return
			default:
			}

			size, dbFiles, err := wsl.GetDBInfo(volName)
			if err == nil {
				lblSize.SetText(i18n.T("database.size", size))
				files = dbFiles
				filesList.Refresh()
			} else {
				lblSize.SetText(i18n.T("common.error") + ": " + err.Error())
			}
		}()
	})

	topBar := container.NewHBox(
		widget.NewLabel(i18n.T("database.volume")),
		widget.NewLabel(volName),
		btnCheck,
		lblSize,
	)

	btnCheck.OnTapped()

	return withResponsiveScroll(container.NewBorder(topBar, nil, nil, nil, filesList))
}