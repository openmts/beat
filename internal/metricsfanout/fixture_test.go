package metricsfanout

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu         sync.Mutex
	wall, mono time.Time
}

func newClock() *fakeClock {
	return &fakeClock{wall: time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC), mono: time.Now()}
}

func (c *fakeClock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.wall }

func (c *fakeClock) monotonic() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.mono }

func (c *fakeClock) advance(delta time.Duration) {
	c.mu.Lock()
	c.wall = c.wall.Add(delta)
	c.mono = c.mono.Add(delta)
	c.mu.Unlock()
}

func (c *fakeClock) rollback(delta time.Duration) {
	c.mu.Lock()
	c.wall = c.wall.Add(-delta)
	c.mu.Unlock()
}

func fixture(t *testing.T, build func(context.Context) (Snapshot, error)) (*Service, *fakeClock) {
	t.Helper()
	clock := newClock()
	if build == nil {
		build = func(context.Context) (Snapshot, error) {
			return Snapshot{Value: map[string]any{"timestamp": clock.now().Format(time.RFC3339), "nodes": []any{}}, Timestamp: clock.now()}, nil
		}
	}
	s, err := New(Config{Build: build, ClientIP: func(r *http.Request) string { return r.RemoteAddr }, AllowOrigin: func(*http.Request) bool { return true }, Now: clock.now})
	if err != nil {
		t.Fatal(err)
	}
	s.clockNow = clock.monotonic
	s.global.at = clock.monotonic()
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s, clock
}

func await(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition did not become true")
		}
		time.Sleep(time.Millisecond)
	}
}

func publishFixture(t *testing.T, s *Service) {
	t.Helper()
	if _, err := s.build(t.Context()); err != nil {
		t.Fatal(err)
	}
}
