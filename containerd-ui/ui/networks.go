package ui

import (
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
	var details = widget.NewLabel("Выберите сеть для просмотра подключённых контейнеров")
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
			details.SetText("Ошибка: " + err.Error())
			return
		}
		if len(containers) == 0 {
			details.SetText("Подключённых контейнеров нет")
			return
		}
		details.SetText("Контейнеры:\n" + strings.Join(containers, "\n"))
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
				details.SetText("Ошибка загрузки сетей: " + err.Error())
				return
			}
			networks = data
			safeUI(func() { list.Refresh() })
		}()
	}

	btnCreate := widget.NewButton("Создать сеть", func() {
		nameEntry := widget.NewEntry()
		nameEntry.SetPlaceHolder("Имя сети")
		driver := widget.NewSelect([]string{"bridge", "host", "overlay"}, nil)
		driver.SetSelected("bridge")
		content := container.NewVBox(nameEntry, driver)
		dlg := dialog.NewCustomConfirm("Создание сети", "ОК", "Отмена", content, func(ok bool) {
			if !ok || strings.TrimSpace(nameEntry.Text) == "" {
				return
			}
			go func() {
				ctx, cancel := context.WithCancel(wsl.AppContext())
				defer cancel()
				if err := wsl.CreateNetwork(ctx, strings.TrimSpace(nameEntry.Text), driver.Selected); err != nil {
					details.SetText("Ошибка создания: " + err.Error())
					return
				}
				refresh()
			}()
		}, win)
		dlg.Show()
	})

	btnRemove := widget.NewButton("Удалить сеть", func() {
		if selectedName == "" || selectedName == "bridge" || selectedName == "host" || selectedName == "none" {
			dialog.ShowCustom("Удаление сети", "ОК", widget.NewLabel("Выберите пользовательскую сеть."), win)
			return
		}
		dialog.ShowCustomConfirm("Удаление сети", "ОК", "Отмена", widget.NewLabel("Удалить сеть "+selectedName+"?"), func(ok bool) {
			if !ok {
				return
			}
			go func() {
				ctx, cancel := context.WithCancel(wsl.AppContext())
				defer cancel()
				if err := wsl.RemoveNetwork(ctx, selectedName); err != nil {
					details.SetText("Ошибка удаления: " + err.Error())
					return
				}
				selectedName = ""
				refresh()
			}()
		}, win)
	})

	btnRefresh := widget.NewButton("Обновить", refresh)
	topBar := container.NewAdaptiveGrid(3, btnCreate, btnRemove, btnRefresh)
	refresh()

	return withResponsiveScroll(container.NewBorder(topBar, details, nil, nil, list))
}
