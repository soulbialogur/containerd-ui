package ui

import (
	"containerd-ui/i18n"
	"containerd-ui/wsl"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

func BuildImagesTab(win fyne.Window) fyne.CanvasObject {
	var images []wsl.Image
	selectedID := ""

	table := widget.NewTable(
		func() (int, int) { return len(images) + 1, 5 },
		func() fyne.CanvasObject {
			return widget.NewLabel("Wide Header Space Text Here")
		},
		func(i widget.TableCellID, o fyne.CanvasObject) {
			label := o.(*widget.Label)
			label.Wrapping = fyne.TextTruncate

			if i.Row == 0 {
				headers := []string{i18n.T("images.id"), i18n.T("images.repository"), i18n.T("images.tag"), i18n.T("images.size"), i18n.T("images.created")}
				label.SetText(headers[i.Col])
				label.TextStyle = fyne.TextStyle{Bold: true}
				return
			}

			img := images[i.Row-1]
			switch i.Col {
			case 0:
				label.SetText(img.ID)
			case 1:
				repo := img.Repository
				if len(repo) > 35 {
					repo = repo[:32] + "..."
				}
				label.SetText(repo)
			case 2:
				label.SetText(img.Tag)
			case 3:
				label.SetText(img.Size)
			case 4:
				label.SetText(wsl.FormatDateShort(img.CreatedAt))
			}
		},
	)

	responsiveTable := newResponsiveTable(table, []float32{55, 110, 55, 70, 85})

	refresh := func() {
		go func() {
			select {
			case <-wsl.AppContext().Done():
				return
			default:
			}

			data, err := wsl.ListImages()
			if err == nil {
				images = data
				safeUI(func() {
					table.Refresh()
				})
			}
		}()
	}

	table.OnSelected = func(id widget.TableCellID) {
		if id.Row > 0 && id.Row-1 < len(images) {
			selectedID = images[id.Row-1].ID
		}
	}

	btnRemove := widget.NewButton(i18n.T("images.remove"), func() {
		if selectedID != "" {
			dialog.ShowConfirm(i18n.T("images.remove"), i18n.T("images.confirm_remove", selectedID), func(ok bool) {
				if ok {
					go func() {
						select {
						case <-wsl.AppContext().Done():
							return
						default:
						}

						wsl.RemoveImage(selectedID)
						wsl.ClearImageSizeCache()
						data, err := wsl.ListImages()
						if err == nil {
							images = data
							safeUI(func() {
								table.Refresh()
							})
						}
					}()
				}
			}, win)
		}
	})
	btnRefresh := widget.NewButton(i18n.T("images.refresh"), refresh)

	topBar := container.NewHBox(btnRemove, btnRefresh)
	refresh()

	return withResponsiveScroll(container.NewBorder(topBar, nil, nil, nil, responsiveTable))
}