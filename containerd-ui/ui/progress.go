package ui

import (
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

type OperationType int

const (
	OpStart OperationType = iota
	OpStop
	OpRestart
	OpRemove
	OpBuild
)

func (ot OperationType) String() string {
	switch ot {
	case OpStart:
		return "Запуск контейнера"
	case OpStop:
		return "Остановка контейнера"
	case OpRestart:
		return "Перезапуск контейнера"
	case OpRemove:
		return "Удаление контейнера"
	case OpBuild:
		return "Сборка проекта"
	default:
		return "Неизвестная операция"
	}
}

type OperationProgress struct {
	Type       OperationType
	Status     string
	Progress   float32
	Error      error
	Finished   bool
	FinishedAt time.Time
}

type OperationManager struct {
	mu         sync.RWMutex
	operations map[string]*OperationProgress
	onUpdate   func()
}

func NewOperationManager() *OperationManager {
	return &OperationManager{
		operations: make(map[string]*OperationProgress),
	}
}

func (om *OperationManager) SetOnUpdate(fn func()) {
	om.onUpdate = fn
}

func (om *OperationManager) GetOperation(id string) *OperationProgress {
	om.mu.RLock()
	defer om.mu.RUnlock()
	if op, ok := om.operations[id]; ok {
		cp := *op
		return &cp
	}
	return nil
}

func (om *OperationManager) SetOperation(id string, op *OperationProgress) {
	om.mu.Lock()
	defer om.mu.Unlock()
	om.operations[id] = op
}

func (om *OperationManager) RemoveOperation(id string) {
	om.mu.Lock()
	defer om.mu.Unlock()
	delete(om.operations, id)
}

func (om *OperationManager) StartOperation(id string, opType OperationType) string {
	op := &OperationProgress{
		Type:     opType,
		Status:   "Выполняется...",
		Progress: 0.0,
		Finished: false,
	}
	om.SetOperation(id, op)
	return id
}

func (om *OperationManager) UpdateOperation(id string, progress float32, status string) {
	om.mu.RLock()
	op, ok := om.operations[id]
	om.mu.RUnlock()
	if !ok {
		return
	}
	op.Progress = progress
	op.Status = status
	om.SetOperation(id, op)
	if om.onUpdate != nil {
		om.onUpdate()
	}
}

func (om *OperationManager) FinishOperation(id string, success bool, errMsg string) {
	om.mu.RLock()
	op, ok := om.operations[id]
	om.mu.RUnlock()
	if !ok {
		return
	}
	op.Progress = 1.0
	op.Finished = true
	op.FinishedAt = time.Now()
	if success {
		op.Status = "Завершено успешно"
	} else {
		op.Status = "Ошибка: " + errMsg
		op.Error = nil
	}
	om.SetOperation(id, op)
	if om.onUpdate != nil {
		om.onUpdate()
	}
	go func() {
		time.Sleep(30 * time.Second)
		om.RemoveOperation(id)
	}()
}

func (om *OperationManager) GetActiveOperations() []*OperationProgress {
	om.mu.RLock()
	defer om.mu.RUnlock()
	var active []*OperationProgress
	for _, op := range om.operations {
		if !op.Finished {
			cp := *op
			active = append(active, &cp)
		}
	}
	return active
}

func (om *OperationManager) CleanupFinished(maxAge time.Duration) {
	om.mu.Lock()
	defer om.mu.Unlock()
	now := time.Now()
	for id, op := range om.operations {
		if op.Finished && now.Sub(op.FinishedAt) > maxAge {
			delete(om.operations, id)
		}
	}
}

func (om *OperationManager) CleanupAllFinished() {
	om.mu.Lock()
	defer om.mu.Unlock()
	for id, op := range om.operations {
		if op.Finished {
			delete(om.operations, id)
		}
	}
}

type ProgressBarComponent struct {
	bar      *widget.ProgressBar
	label    *widget.Label
	cancel   *widget.Button
	closeBtn *widget.Button
	onCancel func()
	onClose  func()
	onUpdate func()
}

func NewProgressBarComponent() *ProgressBarComponent {
	bar := widget.NewProgressBar()
	bar.TextFormatter = func() string {
		return ""
	}
	label := widget.NewLabel("")
	label.TextStyle = fyne.TextStyle{Bold: true}
	cancel := widget.NewButton("Отмена", func() {})
	cancel.Hide()
	closeBtn := widget.NewButton("✕", func() {})
	closeBtn.Hide()
	return &ProgressBarComponent{
		bar:      bar,
		label:    label,
		cancel:   cancel,
		closeBtn: closeBtn,
		onCancel: func() {},
		onClose:  func() {},
	}
}

func (pbc *ProgressBarComponent) SetCancelHandler(handler func()) {
	pbc.onCancel = handler
	pbc.cancel.OnTapped = handler
}

func (pbc *ProgressBarComponent) SetCloseHandler(handler func()) {
	pbc.onClose = handler
	pbc.closeBtn.OnTapped = handler
}

func (pbc *ProgressBarComponent) Show(operationID string, opType OperationType) {
	pbc.bar.Show()
	pbc.bar.SetValue(0)
	pbc.bar.Hide()
	pbc.label.Show()
	if opType == OpBuild {
		pbc.cancel.Show()
	}
}

func (pbc *ProgressBarComponent) Hide() {
	pbc.bar.Hide()
	pbc.label.Hide()
	pbc.cancel.Hide()
	pbc.closeBtn.Hide()
}

func (pbc *ProgressBarComponent) Update(progress *OperationProgress) {
	if progress == nil {
		pbc.Hide()
		if pbc.onUpdate != nil {
			pbc.onUpdate()
		}
		return
	}
	if progress.Finished {
		pbc.bar.SetValue(float64(progress.Progress))
		pbc.bar.Hide()
		pbc.label.Show()
		pbc.label.SetText(progress.Status)
		pbc.closeBtn.Show()
		pbc.cancel.Hide()
		return
	}
	pbc.bar.Show()
	pbc.bar.SetValue(float64(progress.Progress))
	pbc.label.Show()
	pbc.label.SetText(progress.Status)
	if progress.Type == OpBuild {
		pbc.cancel.Show()
	}
	if pbc.onUpdate != nil {
		pbc.onUpdate()
	}
}

func (pbc *ProgressBarComponent) Widget() fyne.CanvasObject {
	return container.NewVBox(
		pbc.label,
		pbc.bar,
		container.NewHBox(
			pbc.cancel,
			layout.NewSpacer(),
			pbc.closeBtn,
		),
	)
}

type ActiveOperationsComponent struct {
	list    *widget.List
	manager *OperationManager
	refresh func()
}

func NewActiveOperationsComponent(manager *OperationManager, refreshFunc func()) *ActiveOperationsComponent {
	list := widget.NewList(
		func() int {
			ops := manager.GetActiveOperations()
			return len(ops)
		},
		func() fyne.CanvasObject {
			return container.NewHBox(
				widget.NewLabel(""),
				widget.NewProgressBar(),
			)
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			ops := manager.GetActiveOperations()
			if int(id) < len(ops) {
				op := ops[id]
				items := o.(*fyne.Container).Objects
				items[0].(*widget.Label).SetText(op.Status)
				items[1].(*widget.ProgressBar).SetValue(float64(op.Progress))
			}
		},
	)
	return &ActiveOperationsComponent{
		list:    list,
		manager: manager,
		refresh: refreshFunc,
	}
}

func (aoc *ActiveOperationsComponent) Widget() fyne.CanvasObject {
	return aoc.list
}