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

type cacheSubscriber struct {
	id uint64
	fn func(CacheEvent)
}

type CacheMetrics struct {
	Hits   atomic.Int64
	Misses atomic.Int64
	Errors atomic.Int64
}

type CacheManager struct {
	mu                  sync.RWMutex
	events              []CacheEvent
	maxEvents           int
	metrics             map[string]*CacheMetrics
	subscribers         []cacheSubscriber
	nextSubscriberID    uint64
}

var GlobalCacheManager = &CacheManager{
	maxEvents:   100,
	metrics:     make(map[string]*CacheMetrics),
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

func (cm *CacheManager) Subscribe(fn func(CacheEvent)) func() {
	if fn == nil {
		return func() {}
	}
	cm.mu.Lock()
	cm.nextSubscriberID++
	id := cm.nextSubscriberID
	cm.subscribers = append(cm.subscribers, cacheSubscriber{id: id, fn: fn})
	cm.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			cm.mu.Lock()
			for index, subscriber := range cm.subscribers {
				if subscriber.id != id {
					continue
				}
				copy(cm.subscribers[index:], cm.subscribers[index+1:])
				cm.subscribers[len(cm.subscribers)-1] = cacheSubscriber{}
				cm.subscribers = cm.subscribers[:len(cm.subscribers)-1]
				break
			}
			cm.mu.Unlock()
		})
	}
}

func (cm *CacheManager) Publish(event CacheEvent) {
	event.Timestamp = time.Now()

	cm.mu.Lock()
	cm.events = append(cm.events, event)
	if len(cm.events) > cm.maxEvents {
		cm.events = cm.events[len(cm.events)-cm.maxEvents:]
	}
	subscribers := append([]cacheSubscriber(nil), cm.subscribers...)
	cm.mu.Unlock()

	for _, subscriber := range subscribers {
		subscriber.fn(event)
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
	return cm.getRecentEventsLocked(n)
}

func (cm *CacheManager) getRecentEventsLocked(n int) []CacheEvent {
	if n < 0 {
		n = 0
	}

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

	summary["recentEvents"] = cm.getRecentEventsLocked(10)
	return summary
}
