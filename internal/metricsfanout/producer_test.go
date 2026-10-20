package metricsfanout

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type countedJSON struct{ calls *atomic.Int64 }

func (value countedJSON) MarshalJSON() ([]byte, error) {
	value.calls.Add(1)
	return []byte(`{"nodes":[],"timestamp":"original"}`), nil
}

func TestSingleProducerEncodesOnceAndSharesImmutableGeneration(t *testing.T) {
	var builds, encodes atomic.Int64
	clock := newClock()
	s, err := New(Config{Build: func(context.Context) (Snapshot, error) {
		builds.Add(1)
		return Snapshot{Value: countedJSON{&encodes}, Timestamp: clock.now()}, nil
	}, ClientIP: func(*http.Request) string { return "127.0.0.1" }, AllowOrigin: func(*http.Request) bool { return true }, Now: clock.now})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	s.clockNow = clock.monotonic
	ticks := make(chan time.Time)
	s.ticks = ticks
	result := make(chan error, 1)
	go func() { result <- s.Run(t.Context()) }()
	await(t, func() bool { return s.Stats().Generation.Generation == 1 })
	clients := make([]*client, 100)
	for index := range clients {
		clients[index] = &client{pending: make(chan *generation, 1)}
		if !s.attach(clients[index]) {
			t.Fatal("attach")
		}
	}
	if builds.Load() != 1 || encodes.Load() != 1 {
		t.Fatal("client-proportional work", builds.Load(), encodes.Load())
	}
	shared := <-clients[0].pending
	for _, c := range clients[1:] {
		if value := <-c.pending; value != shared {
			t.Fatal("generation copied per client")
		}
	}
	info, payload := s.Latest()
	payload[0] = 'x'
	_, again := s.Latest()
	if !json.Valid(again) || info.Generation != 1 {
		t.Fatal("caller mutated shared payload")
	}
	for _, c := range clients {
		s.detach(c)
	}
	clock.advance(Cadence)
	ticks <- clock.monotonic()
	await(t, func() bool { return builds.Load() == 2 })
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestFailuresPreserveOriginalTimestampAndClockRollbackCannotRefresh(t *testing.T) {
	s, clock := fixture(t, nil)
	publishFixture(t, s)
	original, payload := s.Latest()
	s.config.Build = func(context.Context) (Snapshot, error) { return Snapshot{}, errors.New("private SQL /path token") }
	clock.advance(10 * time.Second)
	publishFixture(t, s)
	info, current := s.Latest()
	if info.Timestamp != original.Timestamp || !info.Stale || string(current) != string(payload) {
		t.Fatal("last-good snapshot changed")
	}
	clock.advance(StaleWindow)
	info, current = s.Latest()
	if info.Available || current != nil {
		t.Fatal("expired snapshot returned")
	}
	clock.rollback(time.Hour)
	info, current = s.Latest()
	if info.Available || current != nil {
		t.Fatal("clock rollback revived last-good")
	}
}

func TestBuildFailuresAndPanicClasses(t *testing.T) {
	cases := []struct {
		value  any
		reason Reason
	}{{math.NaN(), ReasonEncoding}, {strings.Repeat("x", MaximumSnapshotBytes), ReasonOversize}, {nil, ReasonSnapshot}, {(*int)(nil), ReasonSnapshot}}
	for _, test := range cases {
		s, clock := fixture(t, nil)
		s.config.Build = func(context.Context) (Snapshot, error) {
			return Snapshot{Value: test.value, Timestamp: clock.now()}, nil
		}
		publishFixture(t, s)
		if s.Stats().Failure != test.reason {
			t.Fatal(s.Stats())
		}
	}
	s, _ := fixture(t, func(context.Context) (Snapshot, error) { panic("secret panic") })
	if err := s.Run(t.Context()); err != ErrBuilderPanic {
		t.Fatal(err)
	}
	if s.Stats().Running || s.Stats().Failure != ReasonPanic {
		t.Fatal(s.Stats())
	}
}

func TestNoOverlapSkipsQueuedTicksAndCloseWaitsForBuilder(t *testing.T) {
	var calls atomic.Int64
	entered, release := make(chan struct{}), make(chan struct{})
	s, clock := fixture(t, nil)
	s.config.Build = func(ctx context.Context) (Snapshot, error) {
		calls.Add(1)
		close(entered)
		select {
		case <-release:
			return Snapshot{Value: map[string]bool{"ok": true}, Timestamp: clock.now()}, nil
		case <-ctx.Done():
			return Snapshot{}, ctx.Err()
		}
	}
	ticks := make(chan time.Time, 1)
	s.ticks = ticks
	done := make(chan error, 1)
	go func() { done <- s.Run(t.Context()) }()
	<-entered
	if err := s.Run(t.Context()); err != ErrRunning {
		t.Fatal(err)
	}
	clock.advance(20 * time.Second)
	ticks <- clock.monotonic().Add(-Cadence)
	close(release)
	await(t, func() bool { return s.Stats().Builds == 1 })
	time.Sleep(10 * time.Millisecond)
	if calls.Load() != 1 || s.Stats().SkippedCadences != 4 {
		t.Fatal("overlap/catchup build", calls.Load(), s.Stats())
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := s.Run(t.Context()); err != ErrClosed {
		t.Fatal(err)
	}
}

func TestCloseCancelsAdmittedBuildAndObserverFailureIsTerminal(t *testing.T) {
	entered := make(chan struct{})
	s, _ := fixture(t, func(ctx context.Context) (Snapshot, error) {
		close(entered)
		<-ctx.Done()
		return Snapshot{}, ctx.Err()
	})
	done := make(chan error, 1)
	go func() { done <- s.Run(t.Context()) }()
	<-entered
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if s.Stats().Builds != 0 {
		t.Fatal("cancellation counted as failure")
	}
	other, _ := fixture(t, nil)
	other.config.Observer = func(Event) { panic("private observer") }
	if err := other.Run(t.Context()); err != ErrObserverPanic {
		t.Fatal(err)
	}
	if !other.Stats().ObserverFailed {
		t.Fatal("observer panic hidden")
	}
}
