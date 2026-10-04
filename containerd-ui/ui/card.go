package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func wrapRoundedCard(card *widget.Card) fyne.CanvasObject {
	settings := fyne.CurrentApp().Settings()
	currentTheme := settings.Theme()
	variant := settings.ThemeVariant()

	bg := canvas.NewRectangle(currentTheme.Color(theme.ColorNameHeaderBackground, variant))
	bg.CornerRadius = 12
	bg.StrokeColor = currentTheme.Color(theme.ColorNameSeparator, variant)
	bg.StrokeWidth = 1

	inner := container.New(layout.NewCustomPaddedLayout(12, 12, 14, 14), card)
	return container.NewStack(bg, inner)
}
