package wsl

import (
	"sync"
	"sync/atomic"
	"time"
)

type CacheEventType int

const (
	CacheEventContainers CacheEventType = iota
	CacheEventImages
	CacheEventVolumes
	CacheEventStats
	CacheEventAll
)

type CacheEvent struct {
	Type      CacheEventType
	Timestamp time.Time
	Reason    string
}

type CacheMetrics struct {
	Hits   atomic.Int64
	Misses atomic.Int64
	Errors atomic.Int64
}

type CacheManager struct {
	mu         sync.RWMutex
	events     []CacheEvent
	maxEvents  int
	metrics    map[string]*CacheMetrics
	subscribers []func(CacheEvent)
}

var GlobalCacheManager = &CacheManager{
	maxEvents:   100,
	metrics:     make(map[string]*CacheMetrics),
	subscribers: make([]func(CacheEvent), 0),
}

func (cm *CacheManager) GetMetrics(cacheName string) *CacheMetrics {
	cm.mu.RLock()
	metrics, ok := cm.metrics[cacheName]
	cm.mu.RUnlock()

	if !ok {
		metrics = &CacheMetrics{}
		cm.mu.Lock()
		cm.metrics[cacheName] = metrics
		cm.mu.Unlock()
	}

	return metrics
}

func (cm *CacheManager) RecordHit(cacheName string) {
	cm.GetMetrics(cacheName).Hits.Add(1)
}

func (cm *CacheManager) RecordMiss(cacheName string) {
	cm.GetMetrics(cacheName).Misses.Add(1)
}

func (cm *CacheManager) RecordError(cacheName string) {
	cm.GetMetrics(cacheName).Errors.Add(1)
}

func (cm *CacheManager) Subscribe(fn func(CacheEvent)) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.subscribers = append(cm.subscribers, fn)
}

func (cm *CacheManager) Publish(event CacheEvent) {
	event.Timestamp = time.Now()

	cm.mu.Lock()
	cm.events = append(cm.events, event)
	if len(cm.events) > cm.maxEvents {
		cm.events = cm.events[len(cm.events)-cm.maxEvents:]
	}
	cm.mu.Unlock()

	for _, fn := range cm.subscribers {
		fn(event)
	}
}

func (cm *CacheManager) Invalidate(eventType CacheEventType, reason string) {
	event := CacheEvent{
		Type:   eventType,
		Reason: reason,
	}

	cm.Publish(event)
}

func (cm *CacheManager) GetRecentEvents(n int) []CacheEvent {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if n > len(cm.events) {
		n = len(cm.events)
	}

	result := make([]CacheEvent, n)
	copy(result, cm.events[len(cm.events)-n:])
	return result
}

func (cm *CacheManager) GetSummary() map[string]interface{} {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	summary := make(map[string]interface{})
	for name, metrics := range cm.metrics {
		hits := metrics.Hits.Load()
		misses := metrics.Misses.Load()
		errors := metrics.Errors.Load()
		total := hits + misses
		hitRate := float32(0)
		if total > 0 {
			hitRate = float32(hits) / float32(total) * 100
		}

		summary[name] = map[string]interface{}{
			"hits":    hits,
			"misses":  misses,
			"errors":  errors,
			"total":   total,
			"hitRate": hitRate,
		}
	}

	summary["recentEvents"] = cm.GetRecentEvents(10)
	return summary
}
