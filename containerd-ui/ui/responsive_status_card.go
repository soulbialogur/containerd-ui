package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

const compactStatusCardWidth float32 = 180

type responsiveStatusCard struct {
	card        *widget.Card
	label       *widget.Label
	container   *fyne.Container
	fullText    string
	compactText string
	compact     bool
}

func newResponsiveStatusCard(title string) *responsiveStatusCard {
	label := widget.NewLabel("Загрузка...")
	card := widget.NewCard(title, "", label)
	result := &responsiveStatusCard{card: card, label: label}
	result.container = container.New(&responsiveStatusCardLayout{card: result}, card)
	return result
}

func (card *responsiveStatusCard) CanvasObject() fyne.CanvasObject {
	return card.container
}

func (card *responsiveStatusCard) SetStatus(fullText, compactText string) {
	card.fullText = fullText
	card.compactText = compactText
	card.updateText()
}

func (card *responsiveStatusCard) updateText() {
	text := card.fullText
	if card.compact {
		text = card.compactText
	}
	if text != "" {
		card.label.SetText(text)
		card.label.Refresh()
	}
}

type responsiveStatusCardLayout struct {
	card *responsiveStatusCard
}

func (l *responsiveStatusCardLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}

	compact := size.Width < compactStatusCardWidth
	if l.card.compact != compact {
		l.card.compact = compact
		l.card.updateText()
	}
	objects[0].Move(fyne.NewPos(0, 0))
	objects[0].Resize(size)
}

func (l *responsiveStatusCardLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) == 0 {
		return fyne.NewSize(0, 0)
	}

	minSize := objects[0].MinSize()
	return fyne.NewSize(0, minSize.Height)
}