package store

import (
	"errors"
	"strings"
	"testing"
)

func TestOperationGroupConflictExposesPoliciesAndCause(t *testing.T) {
	conflict := OperationGroupConflict{Policies: []string{"policy-a", "policy-b"}}
	if !errors.Is(conflict, ErrOperationGroupBusy) {
		t.Fatal("conflict does not unwrap to the group-busy cause")
	}
	if message := conflict.Error(); !strings.Contains(message, "policy-a") || !strings.Contains(message, "policy-b") {
		t.Fatalf("conflict message = %q", message)
	}
}
