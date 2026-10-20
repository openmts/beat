package runtimediag

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

func TestProfileCaptureWritesInstantaneousProfiles(t *testing.T) {
	for _, kind := range []string{KindGoroutine, KindHeap, KindAllocs, KindThreadCreate, KindMutex, KindBlock} {
		t.Run(kind, func(t *testing.T) {
			var buffer bytes.Buffer
			if err := ProfileCapture(context.Background(), kind, 0, &buffer); err != nil {
				t.Fatal(err)
			}
			if buffer.Len() == 0 {
				t.Fatal("empty profile")
			}
		})
	}
	if err := ProfileCapture(context.Background(), "not-a-profile", 0, io.Discard); err == nil {
		t.Fatal("unknown profile accepted")
	}
}

func TestProfileCaptureStopsTimedKindsOnCancel(t *testing.T) {
	for _, kind := range []string{KindCPU, KindTrace} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			started := time.Now()
			err := ProfileCapture(ctx, kind, 5*time.Second, io.Discard)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancelled capture returned %v", err)
			}
			if elapsed := time.Since(started); elapsed > 2*time.Second {
				t.Fatalf("cancelled capture blocked for %s", elapsed)
			}
		})
	}
}
