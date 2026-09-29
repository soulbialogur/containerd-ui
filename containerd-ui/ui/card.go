package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

var (
	roundedCardBg     = color.NRGBA{0x16, 0x1b, 0x28, 0xff}
	roundedCardBorder = color.NRGBA{0x2a, 0x30, 0x40, 0xff}
)

func wrapRoundedCard(card *widget.Card) fyne.CanvasObject {
	bg := canvas.NewRectangle(roundedCardBg)
	bg.CornerRadius = 12
	bg.StrokeColor = roundedCardBorder
	bg.StrokeWidth = 1

	inner := container.New(layout.NewCustomPaddedLayout(12, 12, 14, 14), card)
	return container.NewStack(bg, inner)
}
