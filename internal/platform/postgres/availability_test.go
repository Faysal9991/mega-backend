package postgres

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type pingFunc func(context.Context) error

func (f pingFunc) Ping(ctx context.Context) error { return f(ctx) }
func newAvailabilityTest(t *testing.T, ping Pinger) (*Availability, *atomic.Int64) {
	t.Helper()
	a, err := NewAvailability(ping, time.Second, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	clock := &atomic.Int64{}
	clock.Store(time.Now().UnixNano())
	a.now = func() time.Time { return time.Unix(0, clock.Load()) }
	return a, clock
}
func TestAvailabilityCacheLossAndRecovery(t *testing.T) {
	var calls atomic.Int64
	var down atomic.Bool
	a, clock := newAvailabilityTest(t, pingFunc(func(ctx context.Context) error {
		calls.Add(1)
		if _, ok := ctx.Deadline(); !ok {
			t.Error("unbounded ping")
		}
		if down.Load() {
			return errors.New("PRIVATE_SENTINEL")
		}
		return nil
	}))
	ctx := context.Background()
	if !a.Available(ctx) || !a.Available(ctx) || calls.Load() != 1 {
		t.Fatal("healthy cache")
	}
	down.Store(true)
	clock.Add(int64(time.Second))
	if a.Available(ctx) || a.Available(ctx) || calls.Load() != 2 {
		t.Fatal("outage cache")
	}
	down.Store(false)
	clock.Add(int64(time.Second))
	if !a.Available(ctx) || calls.Load() != 3 {
		t.Fatal("availability did not recover")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if a.Available(canceled) || calls.Load() != 3 {
		t.Fatal("canceled caller checked database")
	}
}

// Observes context propagation only after a waiter has joined the flight;
// channel barriers avoid sleeps or manually inventing active waiter counts.
type observedWaitContext struct {
	context.Context
	joined chan<- struct{}
	once   sync.Once
}

func (c *observedWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { c.joined <- struct{}{} })
	return c.Context.Done()
}
func TestAvailabilityConcurrentChecksAndBoundedWaiters(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int64
	a, _ := newAvailabilityTest(t, pingFunc(func(ctx context.Context) error {
		calls.Add(1)
		close(entered)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}))
	owner := make(chan bool, 1)
	go func() { owner <- a.Available(context.Background()) }()
	<-entered
	joined := make(chan struct{}, maxAvailabilityWaiters)
	parent, cancel := context.WithCancel(context.Background())
	canceled := make(chan bool, 1)
	go func() { canceled <- a.Available(&observedWaitContext{Context: parent, joined: joined}) }()
	<-joined
	cancel()
	if <-canceled {
		t.Fatal("canceled waiter became ready")
	}
	results := make(chan bool, maxAvailabilityWaiters)
	for range maxAvailabilityWaiters {
		go func() { results <- a.Available(&observedWaitContext{Context: context.Background(), joined: joined}) }()
	}
	for range maxAvailabilityWaiters {
		select {
		case <-joined:
		case <-time.After(time.Second):
			t.Fatal("waiters did not join")
		}
	}
	var wg sync.WaitGroup
	for range 64 {
		wg.Go(func() {
			if a.Available(context.Background()) {
				t.Error("overflow did not fail closed")
			}
		})
	}
	wg.Wait()
	close(release)
	if !<-owner {
		t.Fatal("owner did not recover")
	}
	for range maxAvailabilityWaiters {
		if !<-results {
			t.Fatal("coalesced result unhealthy")
		}
	}
	for range 64 {
		wg.Go(func() {
			if !a.Available(context.Background()) {
				t.Error("cached burst unhealthy")
			}
		})
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("probe storm reached pool")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.waiters != 0 {
		t.Fatal("waiter slots leaked")
	}
}

func TestAvailabilityOwnerCancellationAndPanic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	a, _ := newAvailabilityTest(t, pingFunc(func(check context.Context) error {
		cancel()
		if check.Err() != nil {
			t.Error("caller canceled shared check")
		}
		return nil
	}))
	if a.Available(ctx) || !a.Available(context.Background()) {
		t.Fatal("canceled owner poisoned shared cache")
	}
	var calls int
	a, clock := newAvailabilityTest(t, pingFunc(func(context.Context) error {
		calls++
		if calls == 1 {
			panic("PRIVATE_SENTINEL")
		}
		return nil
	}))
	func() {
		defer func() {
			if recover() != "PRIVATE_SENTINEL" {
				t.Error("panic swallowed")
			}
		}()
		a.Available(context.Background())
	}()
	if a.Available(context.Background()) {
		t.Fatal("panic cached as healthy")
	}
	clock.Add(int64(time.Second))
	if !a.Available(context.Background()) {
		t.Fatal("panic leaked in-flight check")
	}
}
func TestAvailabilityTimeout(t *testing.T) {
	a, err := NewAvailability(pingFunc(func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }), time.Millisecond, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if a.Available(context.Background()) {
		t.Fatal("timeout became ready")
	}
	for _, duration := range []time.Duration{0, 6 * time.Second} {
		if _, err := NewAvailability(pingFunc(func(context.Context) error { return nil }), duration, time.Second); err == nil {
			t.Fatal("invalid timeout")
		}
		if _, err := NewAvailability(pingFunc(func(context.Context) error { return nil }), time.Second, duration); err == nil {
			t.Fatal("invalid ttl")
		}
	}
	if _, err := NewAvailability(nil, time.Second, time.Second); err == nil {
		t.Fatal("nil pinger")
	}
}
