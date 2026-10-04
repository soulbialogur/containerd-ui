package ui

import (
	"containerd-ui/i18n"
	"containerd-ui/wsl"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2/widget"
)

func setRefreshButtonLoading(button *widget.Button, label string, loading bool) {
	if button == nil {
		return
	}
	if loading {
		button.SetText(i18n.T("common.loading"))
		button.Disable()
		return
	}
	button.SetText(label)
	button.Enable()
}

var economyMode atomic.Bool
var economyModeListenersMu sync.Mutex
var economyModeListeners []economyModeListener

type economyModeListener struct {
	id uint64
	fn func(bool)
}

var nextEconomyModeListenerID uint64

type tabActive struct {
	mu                 sync.Mutex
	active             bool
	autoRefreshEnabled bool
	period             time.Duration
	ticker             *time.Ticker
	done               chan struct{}
	onTick             func()
}

func newTabActive(initialActive bool, period time.Duration, onTick func()) *tabActive {
	t := &tabActive{
		active:             initialActive,
		autoRefreshEnabled: true,
		period:             period,
		onTick:             onTick,
		done:               make(chan struct{}),
	}
	if initialActive && !economyMode.Load() {
		t.startTicker(period)
	}
	return t
}

func (ta *tabActive) startTicker(period time.Duration) {
	if ta.ticker != nil || !ta.active || !ta.autoRefreshEnabled || economyMode.Load() {
		return
	}
	if ta.done == nil {
		ta.done = make(chan struct{})
	}
	ticker := time.NewTicker(period)
	ta.ticker = ticker
	done := ta.done
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				ta.mu.Lock()
				shouldTick := ta.active
				ta.mu.Unlock()
				if shouldTick && !economyMode.Load() && ta.onTick != nil {
					ta.onTick()
				}
			case <-done:
				return
			case <-wsl.AppContext().Done():
				return
			}
		}
	}()
}

func SetEconomyMode(enabled bool) {
	if economyMode.Swap(enabled) == enabled {
		return
	}

	allTabsMu.Lock()
	for _, tab := range allTabs {
		tab.mu.Lock()
		if enabled {
			tab.stopTicker()
		} else {
			tab.startTicker(tab.period)
		}
		tab.mu.Unlock()
	}
	allTabsMu.Unlock()

	economyModeListenersMu.Lock()
	listeners := append([]economyModeListener(nil), economyModeListeners...)
	economyModeListenersMu.Unlock()
	for _, listener := range listeners {
		listener.fn(enabled)
	}
}

func RegisterEconomyModeListener(listener func(bool)) func() {
	if listener == nil {
		return func() {}
	}
	economyModeListenersMu.Lock()
	nextEconomyModeListenerID++
	id := nextEconomyModeListenerID
	economyModeListeners = append(economyModeListeners, economyModeListener{id: id, fn: listener})
	enabled := economyMode.Load()
	economyModeListenersMu.Unlock()
	listener(enabled)

	var once sync.Once
	return func() {
		once.Do(func() {
			economyModeListenersMu.Lock()
			for index, registered := range economyModeListeners {
				if registered.id != id {
					continue
				}
				copy(economyModeListeners[index:], economyModeListeners[index+1:])
				economyModeListeners[len(economyModeListeners)-1] = economyModeListener{}
				economyModeListeners = economyModeListeners[:len(economyModeListeners)-1]
				break
			}
			economyModeListenersMu.Unlock()
		})
	}
}

func (ta *tabActive) stopTicker() {
	if ta.ticker != nil {
		ta.ticker.Stop()
		ta.ticker = nil
	}
	if ta.done != nil {
		close(ta.done)
		ta.done = nil
	}
}

func (ta *tabActive) SetActive(active bool) {
	ta.mu.Lock()
	defer ta.mu.Unlock()
	if active == ta.active {
		return
	}
	if active {
		ta.active = true
		ta.startTicker(ta.period)
	} else {
		ta.active = false
		ta.stopTicker()
	}
}

func (ta *tabActive) SetAutoRefreshEnabled(enabled bool) {
	ta.mu.Lock()
	defer ta.mu.Unlock()
	if enabled == ta.autoRefreshEnabled {
		return
	}
	ta.autoRefreshEnabled = enabled
	if enabled {
		ta.startTicker(ta.period)
	} else {
		ta.stopTicker()
	}
}

func (ta *tabActive) IsActive() bool {
	ta.mu.Lock()
	defer ta.mu.Unlock()
	return ta.active
}

func (ta *tabActive) Stop() {
	ta.mu.Lock()
	defer ta.mu.Unlock()
	ta.active = false
	ta.stopTicker()
}

var (
	allTabsMu  sync.Mutex
	allTabs    []*tabActive
	tabsByName map[string]*tabActive
)

func init() {
	tabsByName = make(map[string]*tabActive)
}

func registerTab(ta *tabActive) {
	allTabsMu.Lock()
	defer allTabsMu.Unlock()
	allTabs = append(allTabs, ta)
}

func registerTabNamed(name string, ta *tabActive) {
	allTabsMu.Lock()
	defer allTabsMu.Unlock()
	allTabs = append(allTabs, ta)
	tabsByName[name] = ta
}

func getTabByName(name string) *tabActive {
	allTabsMu.Lock()
	defer allTabsMu.Unlock()
	return tabsByName[name]
}

func DeactivateAllTabs() {
	allTabsMu.Lock()
	defer allTabsMu.Unlock()
	for _, ta := range allTabs {
		ta.SetActive(false)
	}
}

func ActivateTabByIndex(idx int) {
	allTabsMu.Lock()
	defer allTabsMu.Unlock()
	if idx >= 0 && idx < len(allTabs) {
		allTabs[idx].SetActive(true)
	}
}

func ActivateTabByName(name string) {
	allTabsMu.Lock()
	defer allTabsMu.Unlock()
	if ta, ok := tabsByName[name]; ok {
		ta.SetActive(true)
	}
}

func StopAllTabs() {
	allTabsMu.Lock()
	defer allTabsMu.Unlock()
	for _, ta := range allTabs {
		ta.Stop()
	}
	allTabs = nil
	tabsByName = make(map[string]*tabActive)
}

func Shutdown() {
	StopAllTabs()
	wsl.Shutdown()
}
