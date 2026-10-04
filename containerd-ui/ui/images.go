package ui

import (
	"containerd-ui/i18n"
	"containerd-ui/wsl"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

func BuildImagesTab(win fyne.Window) fyne.CanvasObject {
	var images []wsl.Image
	selectedID := ""

	newImageRow := func() fyne.CanvasObject {
		labels := make([]fyne.CanvasObject, 5)
		for i := range labels {
			label := widget.NewLabel("")
			label.Wrapping = fyne.TextTruncate
			labels[i] = label
		}
		return container.NewGridWithColumns(5, labels...)
	}

	imageList := widget.NewList(
		func() int { return len(images) },
		newImageRow,
		func(id widget.ListItemID, o fyne.CanvasObject) {
			if id < 0 || id >= len(images) {
				return
			}
			labels := o.(*fyne.Container).Objects
			img := images[id]
			repo := img.Repository
			if slash := strings.LastIndex(repo, "/"); slash >= 0 && slash+1 < len(repo) {
				repo = repo[slash+1:]
			}
			if len(repo) > 35 {
				repo = repo[:32] + "..."
			}
			values := []string{img.ID, repo, img.Tag, img.Size, wsl.FormatDateShort(img.CreatedAt)}
			for i, value := range values {
				labels[i].(*widget.Label).SetText(value)
			}
		},
	)

	header := container.NewGridWithColumns(5,
		widget.NewLabelWithStyle(i18n.T("images.id"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle(i18n.T("images.repository"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle(i18n.T("images.tag"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle(i18n.T("images.size"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle(i18n.T("images.created"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
	)
	var btnRefresh *widget.Button

	refresh := func() {
		go func() {
			safeUI(func() { setRefreshButtonLoading(btnRefresh, i18n.T("images.refresh"), true) })
			defer safeUI(func() { setRefreshButtonLoading(btnRefresh, i18n.T("images.refresh"), false) })
			select {
			case <-wsl.AppContext().Done():
				return
			default:
			}

			data, err := wsl.ListImages()
			if err == nil {
				safeUI(func() {
					rendered := data
					images = rendered
					imageList.Refresh()
				})
			}
		}()
	}

	imageList.OnSelected = func(id widget.ListItemID) {
		if id >= 0 && id < len(images) {
			selectedID = images[id].ID
		}
	}

	btnRemove := widget.NewButton(i18n.T("images.remove"), func() {
		if selectedID != "" {
			confirmDialog := dialog.NewCustomConfirm(
				i18n.T("images.remove"),
				i18n.T("dialogs.ok"),
				i18n.T("dialogs.cancel"),
				widget.NewLabel(i18n.T("images.confirm_remove", selectedID)),
				func(ok bool) {
					if ok {
						imageID := selectedID
						previousImages := append([]wsl.Image(nil), images...)
						selectedID = ""
						for index, image := range images {
							if image.ID == imageID {
								images = append(images[:index], images[index+1:]...)
								break
							}
						}
						imageList.Refresh()

						go func() {
							select {
							case <-wsl.AppContext().Done():
								return
							default:
							}

							removeErr := wsl.RemoveImage(imageID)
							wsl.ClearImageSizeCache()
							data, listErr := wsl.ListImages()
							safeUI(func() {
								if listErr == nil {
									images = data
								} else if removeErr != nil {
									images = previousImages
								}
								imageList.Refresh()
								if removeErr != nil {
									showAppError(win, removeErr)
								} else if listErr != nil {
									showAppError(win, listErr)
								}
							})
						}()
					}
				},
				win,
			)
			confirmDialog.Resize(fyne.NewSize(500, 180))
			confirmDialog.Show()
		}
	})
	btnRefresh = widget.NewButton(i18n.T("images.refresh"), refresh)

	topBar := container.NewHBox(btnRemove, btnRefresh)
	refresh()

	return withResponsiveScroll(container.NewBorder(topBar, nil, nil, nil, container.NewBorder(header, nil, nil, nil, imageList)))
}
