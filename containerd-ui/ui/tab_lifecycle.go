package ui

import (
	"sync"
	"sync/atomic"
	"time"
)

var economyMode atomic.Bool

// tabActive управляет жизненным циклом фоновой активности вкладки.
// Когда вкладка скрыта (SetActive(false)), тикер останавливается,
// а горутина выходит — предотвращая бесполезное потребление CPU.
type tabActive struct {
	mu      sync.Mutex
	active  bool
	period  time.Duration
	ticker  *time.Ticker
	done    chan struct{}
	onTick  func() // вызывается при тике, только если active == true
}

// newTabActive создаёт новый менеджер активности вкладки.
// initialActive — начальное состояние (обычно true для первой отрисовки).
func newTabActive(initialActive bool, period time.Duration, onTick func()) *tabActive {
	t := &tabActive{
		active: initialActive,
		period: period,
		onTick: onTick,
		done:   make(chan struct{}),
	}

	if initialActive {
		t.startTicker(period)
	}

	return t
}

// startTicker запускает тикер в отдельной горутине.
func (ta *tabActive) startTicker(period time.Duration) {
	ta.ticker = time.NewTicker(period)

	go func() {
		for {
			select {
			case <-ta.ticker.C:
				ta.mu.Lock()
				shouldTick := ta.active
				ta.mu.Unlock()

				if shouldTick && !economyMode.Load() && ta.onTick != nil {
					ta.onTick()
				}
			case <-ta.done:
				return
			}
		}
	}()
}

// SetEconomyMode приостанавливает фоновые обновления вкладок.
func SetEconomyMode(enabled bool) {
	economyMode.Store(enabled)
}

// SetActive включает или отключает активность вкладки.
// При отключении тикер полностью останавливается и горутина выходит.
func (ta *tabActive) SetActive(active bool) {
	ta.mu.Lock()
	defer ta.mu.Unlock()

	if active == ta.active {
		return
	}

	if active {
		// Активируем — запускаем новый тикер
		ta.active = true
		ta.startTicker(ta.period)
	} else {
		// Деактивируем — останавливаем тикер
		ta.active = false
		if ta.ticker != nil {
			ta.ticker.Stop()
			ta.ticker = nil
		}
		close(ta.done)
		ta.done = make(chan struct{})
	}
}

// IsActive возвращает текущее состояние активности.
func (ta *tabActive) IsActive() bool {
	ta.mu.Lock()
	defer ta.mu.Unlock()
	return ta.active
}

// Stop полностью останавливает менеджер (вызывается при уничтожении вкладки).
func (ta *tabActive) Stop() {
	ta.mu.Lock()
	defer ta.mu.Unlock()

	ta.active = false
	if ta.ticker != nil {
		ta.ticker.Stop()
		ta.ticker = nil
	}
	if ta.done != nil {
		close(ta.done)
		ta.done = nil
	}
}

// ============================================================================
// Глобальное управление вкладками
// ============================================================================

var (
	allTabsMu sync.Mutex
	allTabs   []*tabActive
	tabsByName map[string]*tabActive
)

func init() {
	tabsByName = make(map[string]*tabActive)
}

// registerTab регистрирует менеджер вкладки для глобального управления.
func registerTab(ta *tabActive) {
	allTabsMu.Lock()
	defer allTabsMu.Unlock()
	allTabs = append(allTabs, ta)
}

// registerTabNamed регистрирует менеджер вкладки по имени.
func registerTabNamed(name string, ta *tabActive) {
	allTabsMu.Lock()
	defer allTabsMu.Unlock()
	allTabs = append(allTabs, ta)
	tabsByName[name] = ta
}

// getTabByName находит tabActive по имени вкладки.
func getTabByName(name string) *tabActive {
	allTabsMu.Lock()
	defer allTabsMu.Unlock()
	return tabsByName[name]
}

// DeactivateAllTabs останавливает все вкладки.
func DeactivateAllTabs() {
	allTabsMu.Lock()
	defer allTabsMu.Unlock()
	for _, ta := range allTabs {
		ta.SetActive(false)
	}
}

// ActivateTabByIndex активирует вкладку по индексу (считая только те, что в массиве).
func ActivateTabByIndex(idx int) {
	allTabsMu.Lock()
	defer allTabsMu.Unlock()
	if idx >= 0 && idx < len(allTabs) {
		allTabs[idx].SetActive(true)
	}
}

// ActivateTabByName активирует вкладку по имени.
func ActivateTabByName(name string) {
	allTabsMu.Lock()
	defer allTabsMu.Unlock()
	if ta, ok := tabsByName[name]; ok {
		ta.SetActive(true)
	}
}

// StopAllTabs полностью останавливает все вкладки (вызывается при закрытии приложения).
func StopAllTabs() {
	allTabsMu.Lock()
	defer allTabsMu.Unlock()
	for _, ta := range allTabs {
		ta.Stop()
	}
	allTabs = nil
}
