package ui

import (
	"containerd-ui/wsl"
	"sync"
	"sync/atomic"
	"time"
)

var economyMode atomic.Bool

type tabActive struct {
	mu      sync.Mutex
	active  bool
	period  time.Duration
	ticker  *time.Ticker
	done    chan struct{}
	onTick  func()
}

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

func (ta *tabActive) startTicker(period time.Duration) {
	ta.ticker = time.NewTicker(period)
	go func() {
		defer ta.ticker.Stop()
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
			case <-wsl.AppContext().Done():
				return
			}
		}
	}()
}

func SetEconomyMode(enabled bool) {
	economyMode.Store(enabled)
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
		if ta.ticker != nil {
			ta.ticker.Stop()
			ta.ticker = nil
		}
		close(ta.done)
		ta.done = make(chan struct{})
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
	if ta.ticker != nil {
		ta.ticker.Stop()
		ta.ticker = nil
	}
	if ta.done != nil {
		close(ta.done)
		ta.done = nil
	}
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
