package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const compactStatusCardWidth float32 = 180

func newResponsiveGrid(maxColumns int, objects ...fyne.CanvasObject) *fyne.Container {
	if maxColumns < 1 {
		maxColumns = 1
	}
	return container.New(&responsiveGridLayout{maxColumns: maxColumns}, objects...)
}

type responsiveGridLayout struct {
	maxColumns int
}

func (l *responsiveGridLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	columns := l.columnCount(objects, size.Width)
	if columns == 0 {
		return
	}

	padding := theme.Padding()
	count := visibleObjectCount(objects)
	rows := count / columns
	if count%columns != 0 {
		rows++
	}
	cellWidth := (size.Width - float32(columns-1)*padding) / float32(columns)
	cellHeight := (size.Height - float32(rows-1)*padding) / float32(rows)
	cellWidth = fyne.Max(cellWidth, 0)
	cellHeight = fyne.Max(cellHeight, 0)

	index := 0
	for _, object := range objects {
		if !object.Visible() {
			continue
		}
		row, column := index/columns, index%columns
		object.Move(fyne.NewPos(float32(column)*(cellWidth+padding), float32(row)*(cellHeight+padding)))
		object.Resize(fyne.NewSize(cellWidth, cellHeight))
		index++
	}
}

func (l *responsiveGridLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	minSize := fyne.NewSize(0, 0)
	for _, object := range objects {
		if object.Visible() {
			minSize = minSize.Max(object.MinSize())
		}
	}
	return minSize
}

func (l *responsiveGridLayout) columnCount(objects []fyne.CanvasObject, width float32) int {
	count := visibleObjectCount(objects)
	if count == 0 {
		return 0
	}

	maxColumns := l.maxColumns
	if maxColumns > count {
		maxColumns = count
	}
	minWidth := float32(0)
	for _, object := range objects {
		if object.Visible() && object.MinSize().Width > minWidth {
			minWidth = object.MinSize().Width
		}
	}
	if minWidth <= 0 {
		return maxColumns
	}

	columns := int((width + theme.Padding()) / (minWidth + theme.Padding()))
	if columns < 1 {
		return 1
	}
	if columns > maxColumns {
		return maxColumns
	}
	return columns
}

func visibleObjectCount(objects []fyne.CanvasObject) int {
	count := 0
	for _, object := range objects {
		if object.Visible() {
			count++
		}
	}
	return count
}

func newResponsiveFlow(objects ...fyne.CanvasObject) *fyne.Container {
	return container.New(responsiveFlowLayout{}, objects...)
}

type responsiveFlowLayout struct{}

func (responsiveFlowLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	padding := theme.Padding()
	x, y, rowHeight := float32(0), float32(0), float32(0)
	for _, object := range objects {
		if !object.Visible() {
			continue
		}
		objectSize := object.MinSize()
		if x > 0 && x+padding+objectSize.Width > size.Width {
			y += rowHeight + padding
			x, rowHeight = 0, 0
		}
		object.Move(fyne.NewPos(x, y))
		object.Resize(objectSize)
		x += objectSize.Width + padding
		if objectSize.Height > rowHeight {
			rowHeight = objectSize.Height
		}
	}
}

func (responsiveFlowLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	minSize := fyne.NewSize(0, 0)
	for _, object := range objects {
		if object.Visible() {
			minSize.Width = fyne.Max(minSize.Width, object.MinSize().Width)
			minSize.Height = fyne.Max(minSize.Height, object.MinSize().Height)
		}
	}
	return minSize
}

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

func (card *responsiveStatusCard) SetLoading(loading bool) {
	if loading {
		card.label.SetText("...")
		card.label.Refresh()
		return
	}
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
