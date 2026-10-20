package store

import (
	"context"
	"errors"
	"testing"
)

func TestWaitForSQLiteRetryHonorsContextCancellation(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitForSQLiteRetry(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled retry wait = %v", err)
	}
	if err := waitForSQLiteRetry(context.Background()); err != nil {
		t.Fatalf("retry wait = %v", err)
	}
}
