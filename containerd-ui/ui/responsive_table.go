package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// newResponsiveTable keeps the preferred table width available for horizontal scrolling.
func newResponsiveTable(table *widget.Table, preferredWidths []float32) *fyne.Container {
	return container.New(&responsiveTableLayout{
		table:           table,
		preferredWidths: preferredWidths,
	}, table)
}

type responsiveTableLayout struct {
	table           *widget.Table
	preferredWidths []float32
	lastWidths      []float32
}

func (l *responsiveTableLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}

	widths := l.columnWidths(size.Width)
	if len(widths) != len(l.lastWidths) {
		l.lastWidths = make([]float32, len(widths))
	}
	for column, width := range widths {
		if l.lastWidths[column] != width {
			l.table.SetColumnWidth(column, width)
			l.lastWidths[column] = width
		}
	}

	objects[0].Move(fyne.NewPos(0, 0))
	objects[0].Resize(size)
}

func (l *responsiveTableLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) == 0 {
		return fyne.NewSize(0, 0)
	}

	width := float32(0)
	for _, preferredWidth := range l.preferredWidths {
		width += preferredWidth
	}
	return fyne.NewSize(width, objects[0].MinSize().Height)
}

func (l *responsiveTableLayout) columnWidths(_ float32) []float32 {
	widths := make([]float32, len(l.preferredWidths))
	copy(widths, l.preferredWidths)
	return widths
}

func withHorizontalScroll(content fyne.CanvasObject) fyne.CanvasObject {
	return container.NewHScroll(content)
}

func withVerticalScroll(content fyne.CanvasObject) fyne.CanvasObject {
	return container.NewVScroll(content)
}

func withResponsiveScroll(content fyne.CanvasObject) fyne.CanvasObject {
	return withVerticalScroll(withHorizontalScroll(content))
}