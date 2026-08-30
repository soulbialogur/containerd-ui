package ui

import (
	"containerd-ui/wsl"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func BuildDatabaseTab() fyne.CanvasObject {
	volName := wsl.GetDBVolumeName()

	lblSize := widget.NewLabel("Размер: —")

	var files []string
	filesList := widget.NewList(
		func() int { return len(files) },
		func() fyne.CanvasObject { return widget.NewLabel("...") },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			o.(*widget.Label).SetText(files[i])
		},
	)

	btnCheck := widget.NewButton("Проверить", func() {
		go func() {
			// Проверяем контекст приложения
			select {
			case <-wsl.AppContext().Done():
				return
			default:
			}
			
			size, dbFiles, err := wsl.GetDBInfo(volName)
			if err == nil {
				lblSize.SetText("Размер: " + size)
				files = dbFiles
				filesList.Refresh()
			} else {
				lblSize.SetText("Ошибка: " + err.Error())
			}
		}()
	})

	topBar := container.NewHBox(
		widget.NewLabel("Том:"),
		widget.NewLabel(volName),
		btnCheck,
		lblSize,
	)

	btnCheck.OnTapped()

	return container.NewBorder(topBar, nil, nil, nil, filesList)
}