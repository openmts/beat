package geoip

import (
	"context"
	"testing"
	"time"
)

func TestMaintenanceObserverAndWakePaths(t *testing.T) {
	service := publicationFixture(t)
	service.now = func() time.Time { return time.Now().UTC() }
	observed := []error{}
	service.SetObserver(func(err error) { observed = append(observed, err) })
	empty := make(chan time.Time)
	service.maintainDue(t.Context(), empty)
	due := make(chan time.Time, 1)
	due <- time.Now()
	service.maintainDue(t.Context(), due)
	if len(observed) != 1 || observed[0] != ErrUnavailable {
		t.Fatal(observed)
	}
	due <- time.Now()
	if err := service.waitWork(t.Context(), empty, due); err != ErrUnavailable {
		t.Fatal(err)
	}
	tick := make(chan time.Time, 1)
	tick <- time.Now()
	if err := service.waitWork(t.Context(), tick, empty); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := service.waitWork(ctx, empty, empty); err != context.Canceled {
		t.Fatal(err)
	}
}
