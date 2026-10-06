package httpapi

import (
	"sync"
	"time"
)

// runEstimate is this process's moving estimate of how long one run takes
// (alarm analysis, health inspection), shown as remaining time while a run is
// in progress. Run state itself is kept in the repository.
type runEstimate struct {
	mu       sync.RWMutex
	ms       int64
	fallback time.Duration
}

func newRunEstimate(fallback time.Duration) *runEstimate {
	return &runEstimate{ms: fallback.Milliseconds(), fallback: fallback}
}

// get returns the current estimate in milliseconds.
func (e *runEstimate) get() int64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.ms <= 0 {
		return e.fallback.Milliseconds()
	}
	return e.ms
}

// observe folds one finished run's duration (milliseconds) into the estimate,
// weighting history three to one and keeping it between 5 s and 3 min.
func (e *runEstimate) observe(elapsed int64) {
	if elapsed <= 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	current := e.ms
	if current <= 0 {
		current = e.fallback.Milliseconds()
	}
	e.ms = min(max((current*3+elapsed)/4, 5000), 180000)
}
