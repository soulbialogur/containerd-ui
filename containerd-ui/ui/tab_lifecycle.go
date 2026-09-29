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
		button.SetText(i18n.T("dialogs.loading"))
		button.Disable()
		return
	}
	button.SetText(label)
	button.Enable()
}

var economyMode atomic.Bool
var economyModeListenersMu sync.Mutex
var economyModeListeners []func(bool)

type tabActive struct {
	mu     sync.Mutex
	active bool
	period time.Duration
	ticker *time.Ticker
	done   chan struct{}
	onTick func()
}

func newTabActive(initialActive bool, period time.Duration, onTick func()) *tabActive {
	t := &tabActive{
		active: initialActive,
		period: period,
		onTick: onTick,
		done:   make(chan struct{}),
	}
	if initialActive && !economyMode.Load() {
		t.startTicker(period)
	}
	return t
}

func (ta *tabActive) startTicker(period time.Duration) {
	if ta.ticker != nil || !ta.active || economyMode.Load() {
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
	listeners := append([]func(bool){}, economyModeListeners...)
	economyModeListenersMu.Unlock()
	for _, listener := range listeners {
		listener(enabled)
	}
}

func RegisterEconomyModeListener(listener func(bool)) {
	if listener == nil {
		return
	}
	economyModeListenersMu.Lock()
	economyModeListeners = append(economyModeListeners, listener)
	enabled := economyMode.Load()
	economyModeListenersMu.Unlock()
	listener(enabled)
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
