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

func networkDriverOptions() []string {
	return []string{"bridge", "host", "macvlan", "ipvlan"}
}

func BuildNetworksTab(win fyne.Window) fyne.CanvasObject {
	var networks []wsl.Network
	selectedName := ""
	var details = widget.NewLabel(i18n.T("networks.select_hint"))
	details.Wrapping = fyne.TextWrapWord

	list := widget.NewList(
		func() int { return len(networks) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, object fyne.CanvasObject) {
			if id < 0 || id >= len(networks) {
				return
			}
			network := networks[int(id)]
			object.(*widget.Label).SetText(fmt.Sprintf("%s  (%s)", network.Name, network.Driver))
		},
	)

	refreshDetails := func(name string) {
		ctx, cancel := context.WithCancel(wsl.AppContext())
		defer cancel()
		containers, err := wsl.GetNetworkContainers(ctx, name)
		if err != nil {
			safeUI(func() {
				if selectedName == name {
					details.SetText(i18n.T("common.error") + ": " + err.Error())
				}
			})
			return
		}
		safeUI(func() {
			if selectedName != name {
				return
			}
			if len(containers) == 0 {
				details.SetText(i18n.T("networks.no_containers"))
				return
			}
			details.SetText(i18n.T("networks.containers_list") + "\n" + strings.Join(containers, "\n"))
		})
	}

	list.OnSelected = func(id widget.ListItemID) {
		if id < 0 || int(id) >= len(networks) {
			return
		}
		selectedName = networks[int(id)].Name
		go refreshDetails(selectedName)
	}

	var btnRefresh *widget.Button
	refresh := func() {
		go func() {
			safeUI(func() { setRefreshButtonLoading(btnRefresh, i18n.T("networks.refresh"), true) })
			defer safeUI(func() { setRefreshButtonLoading(btnRefresh, i18n.T("networks.refresh"), false) })
			ctx, cancel := context.WithCancel(wsl.AppContext())
			defer cancel()
			data, err := wsl.ListNetworks(ctx)
			if err != nil {
				safeUI(func() {
					details.SetText(i18n.T("networks.load_error", err.Error()))
				})
				return
			}
			safeUI(func() {
				networks = data
				list.Refresh()
			})
		}()
	}

	btnCreate := widget.NewButton(i18n.T("networks.create"), func() {
		nameEntry := widget.NewEntry()
		nameEntry.SetPlaceHolder(i18n.T("networks.name_placeholder"))
		driver := widget.NewSelect(networkDriverOptions(), nil)
		driver.SetSelected("bridge")
		content := container.NewVBox(nameEntry, driver)
		dlg := dialog.NewCustomConfirm(i18n.T("networks.create_title"), i18n.T("dialogs.ok"), i18n.T("dialogs.cancel"), content, func(ok bool) {
			name := strings.TrimSpace(nameEntry.Text)
			selectedDriver := driver.Selected
			if !ok || name == "" {
				return
			}
			go func() {
				ctx, cancel := context.WithCancel(wsl.AppContext())
				defer cancel()
				if err := wsl.CreateNetwork(ctx, name, selectedDriver); err != nil {
					safeUI(func() {
						details.SetText(i18n.T("networks.create_error", err.Error()))
					})
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
		networkName := selectedName
		dialog.ShowCustomConfirm(i18n.T("networks.remove_title"), i18n.T("dialogs.ok"), i18n.T("dialogs.cancel"), widget.NewLabel(i18n.T("networks.confirm_remove", networkName)), func(ok bool) {
			if !ok {
				return
			}
			go func() {
				ctx, cancel := context.WithCancel(wsl.AppContext())
				defer cancel()
				if err := wsl.RemoveNetwork(ctx, networkName); err != nil {
					safeUI(func() {
						details.SetText(i18n.T("networks.remove_error", err.Error()))
					})
					return
				}
				safeUI(func() {
					if selectedName == networkName {
						selectedName = ""
					}
				})
				refresh()
			}()
		}, win)
	})

	btnRefresh = widget.NewButton(i18n.T("networks.refresh"), refresh)
	topBar := container.NewAdaptiveGrid(3, btnCreate, btnRemove, btnRefresh)
	refresh()

	return withResponsiveScroll(container.NewBorder(topBar, details, nil, nil, list))
}
