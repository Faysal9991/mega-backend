package postgres

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Pinger is implemented by the existing runtime pgxpool.Pool.
type Pinger interface{ Ping(context.Context) error }

const maxAvailabilityWaiters = 32

type availabilityFlight struct {
	done    chan struct{}
	healthy bool
}

// Availability performs on-demand, cached/coalesced checks; it owns no pool,
// background goroutine or unbounded queue. Saturated waiters fail closed.
type Availability struct {
	pinger       Pinger
	timeout, ttl time.Duration
	now          func() time.Time
	mu           sync.Mutex
	healthy      bool
	expires      time.Time
	flight       *availabilityFlight
	waiters      int
}

func NewAvailability(pinger Pinger, timeout, ttl time.Duration) (*Availability, error) {
	if pinger == nil || timeout < time.Millisecond || timeout > 5*time.Second || ttl < time.Millisecond || ttl > 5*time.Second {
		return nil, errors.New("postgres: availability requires a pinger and bounded timeout/cache TTL")
	}
	return &Availability{pinger: pinger, timeout: timeout, ttl: ttl, now: time.Now}, nil
}

func (a *Availability) Available(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	a.mu.Lock()
	if a.now().Before(a.expires) {
		healthy := a.healthy
		a.mu.Unlock()
		return healthy
	}
	if flight := a.flight; flight != nil {
		if a.waiters >= maxAvailabilityWaiters {
			a.mu.Unlock()
			return false
		}
		a.waiters++
		a.mu.Unlock()
		defer func() { a.mu.Lock(); a.waiters--; a.mu.Unlock() }()
		waiting, cancel := context.WithTimeout(ctx, a.timeout)
		defer cancel()
		select {
		case <-flight.done:
			return flight.healthy && ctx.Err() == nil && waiting.Err() == nil
		case <-waiting.Done():
			return false
		}
	}
	flight := &availabilityFlight{done: make(chan struct{})}
	a.flight = flight
	a.mu.Unlock()
	healthy := false
	// Publish a failed result and release waiters even if an injected pinger
	// panics; the original panic still propagates to existing HTTP recovery.
	defer func() {
		a.mu.Lock()
		a.healthy = healthy
		a.expires = a.now().Add(a.ttl)
		flight.healthy = healthy
		a.flight = nil
		close(flight.done)
		a.mu.Unlock()
	}()
	// A disconnected first probe cannot cancel the shared result for its peers.
	// This synchronous check still has a hard independent driver context deadline.
	checking, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.timeout)
	defer cancel()
	healthy = a.pinger.Ping(checking) == nil && checking.Err() == nil
	return healthy && ctx.Err() == nil
}
