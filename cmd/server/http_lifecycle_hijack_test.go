package main

import (
	"context"
	"net"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTrackedResponseWriterRejectsNonHijacker(t *testing.T) {
	recorder := httptest.NewRecorder()
	writer := &trackedResponseWriter{ResponseWriter: recorder, tracker: newHijackedTracker()}
	conn, buffer, err := writer.Hijack()
	if err == nil || conn != nil || buffer != nil {
		t.Fatalf("hijack = %v, %v, %v", conn, buffer, err)
	}
	if writer.Unwrap() != recorder {
		t.Fatal("unwrap returned a different response writer")
	}
	writer.finish()
}

func TestHijackedTrackerBoundsShutdownWait(t *testing.T) {
	tracker := newHijackedTracker()
	server, client := net.Pipe()
	defer func() { _ = client.Close() }()
	tracker.begin(server)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := tracker.closeAndWait(ctx); err == nil {
		t.Fatal("timed-out shutdown wait succeeded")
	}
	if time.Since(start) < 15*time.Millisecond {
		t.Fatal("shutdown did not wait for the hijacked handler")
	}
	tracker.end(server)
}

func TestHijackedTrackerSignalsIdleAgainAfterReuse(t *testing.T) {
	tracker := newHijackedTracker()
	firstServer, firstClient := net.Pipe()
	tracker.begin(firstServer)
	tracker.end(firstServer)
	_ = firstClient.Close()

	secondServer, secondClient := net.Pipe()
	tracker.begin(secondServer)
	tracker.end(secondServer)
	_ = secondClient.Close()

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := tracker.closeAndWait(ctx); err != nil {
		t.Fatal(err)
	}
}
