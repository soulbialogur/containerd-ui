package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

const sidebarWidth float32 = 264

func panelWrap(obj fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(bgSecondary)
	bg.CornerRadius = 10
	bg.StrokeColor = borderColor
	bg.StrokeWidth = 1

	inner := container.New(layout.NewCustomPaddedLayout(14, 14, 16, 16), obj)
	panel := container.NewStack(bg, inner)

	return container.New(layout.NewCustomPaddedLayout(8, 8, 8, 8), panel)
}

func newRightSidebar(title string, content fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(bgSecondary)
	bg.CornerRadius = 10
	bg.StrokeColor = borderColor
	bg.StrokeWidth = 1

	head := container.New(layout.NewCustomPaddedLayout(10, 8, 12, 12),
		widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
	)

	body := container.NewBorder(
		container.NewVBox(head, hLine()),
		nil, nil, nil,
		container.NewVScroll(
			container.New(layout.NewCustomPaddedLayout(10, 10, 12, 12), content),
		),
	)

	spacer := canvas.NewRectangle(color.Transparent)
	spacer.SetMinSize(fyne.NewSize(sidebarWidth, 0))

	panel := container.NewStack(bg, body, spacer)

	return container.New(layout.NewCustomPaddedLayout(8, 8, 8, 8), panel)
}

func hLine() fyne.CanvasObject {
	line := canvas.NewRectangle(borderColor)
	line.SetMinSize(fyne.NewSize(1, 1))
	return line
}

func sidebarSection(text string) fyne.CanvasObject {
	t := canvas.NewText(text, textSecondary)
	t.TextSize = 11
	t.TextStyle = fyne.TextStyle{Bold: true}
	return container.New(layout.NewCustomPaddedLayout(10, 2, 2, 2), t)
}

func sidebarTile(icon fyne.Resource, text string, tapped func()) fyne.CanvasObject {
	btn := widget.NewButtonWithIcon(text, icon, tapped)
	btn.Alignment = widget.ButtonAlignLeading
	return btn
}