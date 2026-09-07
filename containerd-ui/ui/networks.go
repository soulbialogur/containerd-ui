package ui

import (
	"containerd-ui/i18n"
	"containerd-ui/wsl"
	"context"
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

func BuildNetworksTab(win fyne.Window) fyne.CanvasObject {
	var networks []wsl.Network
	selectedName := ""
	var details = widget.NewLabel(i18n.T("networks.select_hint"))
	details.Wrapping = fyne.TextWrapWord

	list := widget.NewList(
		func() int { return len(networks) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, object fyne.CanvasObject) {
			network := networks[int(id)]
			object.(*widget.Label).SetText(fmt.Sprintf("%s  (%s)", network.Name, network.Driver))
		},
	)

	refreshDetails := func(name string) {
		ctx, cancel := context.WithCancel(wsl.AppContext())
		defer cancel()
		containers, err := wsl.GetNetworkContainers(ctx, name)
		if err != nil {
			details.SetText(i18n.T("common.error") + ": " + err.Error())
			return
		}
		if len(containers) == 0 {
			details.SetText(i18n.T("networks.no_containers"))
			return
		}
		details.SetText(i18n.T("networks.containers_list") + "\n" + strings.Join(containers, "\n"))
	}

	list.OnSelected = func(id widget.ListItemID) {
		if int(id) >= len(networks) {
			return
		}
		selectedName = networks[int(id)].Name
		go refreshDetails(selectedName)
	}

	refresh := func() {
		go func() {
			ctx, cancel := context.WithCancel(wsl.AppContext())
			defer cancel()
			data, err := wsl.ListNetworks(ctx)
			if err != nil {
				details.SetText(i18n.T("networks.load_error", err.Error()))
				return
			}
			networks = data
			safeUI(func() { list.Refresh() })
		}()
	}

	btnCreate := widget.NewButton(i18n.T("networks.create"), func() {
		nameEntry := widget.NewEntry()
		nameEntry.SetPlaceHolder(i18n.T("networks.name_placeholder"))
		driver := widget.NewSelect([]string{"bridge", "host", "overlay"}, nil)
		driver.SetSelected("bridge")
		content := container.NewVBox(nameEntry, driver)
		dlg := dialog.NewCustomConfirm(i18n.T("networks.create_title"), i18n.T("dialogs.ok"), i18n.T("dialogs.cancel"), content, func(ok bool) {
			if !ok || strings.TrimSpace(nameEntry.Text) == "" {
				return
			}
			go func() {
				ctx, cancel := context.WithCancel(wsl.AppContext())
				defer cancel()
				if err := wsl.CreateNetwork(ctx, strings.TrimSpace(nameEntry.Text), driver.Selected); err != nil {
					details.SetText(i18n.T("networks.create_error", err.Error()))
					return
				}
				refresh()
			}()
		}, win)
		dlg.Show()
	})

	btnRemove := widget.NewButton(i18n.T("networks.remove"), func() {
		if selectedName == "" || selectedName == "bridge" || selectedName == "host" || selectedName == "none" {
			dialog.ShowCustom(i18n.T("networks.remove_title"), i18n.T("dialogs.ok"), widget.NewLabel(i18n.T("networks.select_custom")), win)
			return
		}
		dialog.ShowCustomConfirm(i18n.T("networks.remove_title"), i18n.T("dialogs.ok"), i18n.T("dialogs.cancel"), widget.NewLabel(i18n.T("networks.confirm_remove", selectedName)), func(ok bool) {
			if !ok {
				return
			}
			go func() {
				ctx, cancel := context.WithCancel(wsl.AppContext())
				defer cancel()
				if err := wsl.RemoveNetwork(ctx, selectedName); err != nil {
					details.SetText(i18n.T("networks.remove_error", err.Error()))
					return
				}
				selectedName = ""
				refresh()
			}()
		}, win)
	})

	btnRefresh := widget.NewButton(i18n.T("networks.refresh"), refresh)
	topBar := container.NewAdaptiveGrid(3, btnCreate, btnRemove, btnRefresh)
	refresh()

	return withResponsiveScroll(container.NewBorder(topBar, details, nil, nil, list))
}