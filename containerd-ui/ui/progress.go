package ui

import (
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

// OperationType тип операции
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

// OperationProgress состояние операции
type OperationProgress struct {
	Type       OperationType
	Status     string
	Progress   float32 // 0.0 - 1.0
	Error      error
	Finished   bool
	FinishedAt time.Time // время завершения для автоматической очистки
}

// OperationManager менеджер операций с прогрессом
type OperationManager struct {
	mu         sync.RWMutex
	operations map[string]*OperationProgress
	onUpdate   func() // callback для обновления UI
}

// NewOperationManager создаёт новый менеджер операций
func NewOperationManager() *OperationManager {
	return &OperationManager{
		operations: make(map[string]*OperationProgress),
	}
}

// SetOnUpdate устанавливает callback для обновления UI
func (om *OperationManager) SetOnUpdate(fn func()) {
	om.onUpdate = fn
}

// GetOperation получает статус операции
func (om *OperationManager) GetOperation(id string) *OperationProgress {
	om.mu.RLock()
	defer om.mu.RUnlock()
	if op, ok := om.operations[id]; ok {
		cp := *op
		return &cp
	}
	return nil
}

// SetOperation устанавливает статус операции
func (om *OperationManager) SetOperation(id string, op *OperationProgress) {
	om.mu.Lock()
	defer om.mu.Unlock()
	om.operations[id] = op
}

// RemoveOperation удаляет операцию из списка
func (om *OperationManager) RemoveOperation(id string) {
	om.mu.Lock()
	defer om.mu.Unlock()
	delete(om.operations, id)
}

// StartOperation начинает новую операцию
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

// UpdateOperation обновляет прогресс операции
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
	
	// Вызываем callback для обновления UI (событийный подход)
	if om.onUpdate != nil {
		om.onUpdate()
	}
}

// FinishOperation завершает операцию
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
	
	// Вызываем callback для обновления UI
	if om.onUpdate != nil {
		om.onUpdate()
	}
	
	// Автоматическая очистка завершённой операции через 30 секунд
	go func() {
		time.Sleep(30 * time.Second)
		om.RemoveOperation(id)
	}()
}

// GetActiveOperations получает список активных операций
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

// CleanupFinished удаляет все завершённые операции, завершённые раньше maxAge
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

// CleanupAllFinished удаляет все завершённые операции без учёта времени
func (om *OperationManager) CleanupAllFinished() {
	om.mu.Lock()
	defer om.mu.Unlock()
	
	for id, op := range om.operations {
		if op.Finished {
			delete(om.operations, id)
		}
	}
}

// ProgressBarComponent компонент прогресс-бара
type ProgressBarComponent struct {
	bar      *widget.ProgressBar
	label    *widget.Label
	cancel   *widget.Button
	closeBtn *widget.Button
	onCancel func()
	onClose  func()
	onUpdate func() // событийный callback для обновления UI
}

// NewProgressBarComponent создаёт новый компонент прогресс-бара
func NewProgressBarComponent() *ProgressBarComponent {
	bar := widget.NewProgressBar()
	bar.TextFormatter = func() string {
		return ""
	}
	
	label := widget.NewLabel("")
	label.TextStyle = fyne.TextStyle{Bold: true}
	
	cancel := widget.NewButton("Отмена", func() {
		// Будет установлен пользователем
	})
	cancel.Hide()
	
	closeBtn := widget.NewButton("✕", func() {
		// Будет установлен пользователем
	})
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

// SetCancelHandler устанавливает обработчик отмены
func (pbc *ProgressBarComponent) SetCancelHandler(handler func()) {
	pbc.onCancel = handler
	pbc.cancel.OnTapped = handler
}

// SetCloseHandler устанавливает обработчик закрытия
func (pbc *ProgressBarComponent) SetCloseHandler(handler func()) {
	pbc.onClose = handler
	pbc.closeBtn.OnTapped = handler
}

// Show показывает прогресс-бар
func (pbc *ProgressBarComponent) Show(operationID string, opType OperationType) {
	pbc.bar.Show()
	pbc.bar.SetValue(0)
	pbc.bar.Hide() // Скрываем до начала прогресса
	pbc.label.Show()
	
	if opType == OpBuild {
		pbc.cancel.Show()
	}
}

// Hide скрывает прогресс-бар
func (pbc *ProgressBarComponent) Hide() {
	pbc.bar.Hide()
	pbc.label.Hide()
	pbc.cancel.Hide()
	pbc.closeBtn.Hide()
}

// Update обновляет прогресс-бар
func (pbc *ProgressBarComponent) Update(progress *OperationProgress) {
	if progress == nil {
		pbc.Hide()
		if pbc.onUpdate != nil {
			pbc.onUpdate()
		}
		return
	}
	
	// Если операция завершена — показываем результат
	if progress.Finished {
		pbc.bar.SetValue(float64(progress.Progress))
		pbc.bar.Hide() // Скрываем прогресс-бар
		pbc.label.Show()
		pbc.label.SetText(progress.Status)
		
		// Показываем кнопку закрытия
		pbc.closeBtn.Show()
		
		// Убираем кнопку отмены если была
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
	
	// Вызываем событийный callback для обновления UI
	if pbc.onUpdate != nil {
		pbc.onUpdate()
	}
}

// Widget возвращает виджет для отображения
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

// ActiveOperationsComponent компонент для отображения активных операций
type ActiveOperationsComponent struct {
	list     *widget.List
	manager  *OperationManager
	refresh  func()
}

// NewActiveOperationsComponent создаёт новый компонент активных операций
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

// Widget возвращает виджет для отображения
func (aoc *ActiveOperationsComponent) Widget() fyne.CanvasObject {
	return aoc.list
}
