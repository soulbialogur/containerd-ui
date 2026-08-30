package ui

import "time"

const (
	CacheStatus           = 5 * time.Second
	TickerAutoRefresh     = 30 * time.Second
	DebounceSelected      = 300 * time.Millisecond
	SleepOperation        = 200 * time.Millisecond
	SleepVolumeOperation  = 500 * time.Millisecond
	TickerProgress        = 100 * time.Millisecond
	SleepContainerList    = 300 * time.Millisecond
	DebounceVolumeRefresh = 300 * time.Millisecond
	TickerResources       = 5 * time.Second
)