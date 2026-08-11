package model

import (
	"testing"
	"time"
)

func TestNowUTC(t *testing.T) {
	now := NowUTC()
	if now.Location() != time.UTC {
		t.Fatalf("location = %v, want UTC", now.Location())
	}
	if time.Since(now) > time.Second {
		t.Fatalf("NowUTC returned stale time: %v", now)
	}
}
