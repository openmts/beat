package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func attemptFixture() (Attempt, Proof) {
	nonce := make([]byte, 32)
	hash := sha256.Sum256(nonce)
	now := time.Unix(100, 0)
	return Attempt{ID: "attempt", NodeID: "node", State: "leased", NonceHash: hash[:], LeaseExpiresAt: now.Add(time.Minute)}, Proof{NodeID: "node", Nonce: hex.EncodeToString(nonce), Now: now}
}

func TestAttemptProofAndAtMostOnceStateTransitions(t *testing.T) {
	attempt, proof := attemptFixture()
	for _, bad := range []Proof{{}, {NodeID: "other", Nonce: proof.Nonce}, {NodeID: "node", Nonce: strings.Repeat("x", 64)}, {NodeID: "node", Nonce: strings.Repeat("1", 64)}} {
		if attempt.Start(bad) != ErrForbidden {
			t.Fatal("bad start proof")
		}
		if _, err := attempt.Heartbeat(bad); err != ErrForbidden {
			t.Fatal(err)
		}
		if attempt.Complete(bad, Result{State: "succeeded"}) != ErrForbidden {
			t.Fatal("bad result proof")
		}
	}
	if _, err := attempt.Heartbeat(proof); err != ErrConflict {
		t.Fatal(err)
	}
	if err := attempt.Start(proof); err != nil {
		t.Fatal(err)
	}
	started := *attempt.StartedAt
	proof.Now = proof.Now.Add(time.Hour)
	if err := attempt.Start(proof); err != nil || !attempt.StartedAt.Equal(started) {
		t.Fatal("duplicate start changed authorization")
	}
	attempt.Reconcile(proof.Now)
	if attempt.State != "unknown" {
		t.Fatal(attempt.State)
	}
	if _, err := attempt.Heartbeat(proof); err != nil || attempt.State != "running" {
		t.Fatal(err)
	}
	attempt.Cancel()
	if cancel, err := attempt.Heartbeat(proof); err != nil || !cancel {
		t.Fatal(cancel, err)
	}
	if err := attempt.Complete(proof, Result{State: "canceled"}); err != nil {
		t.Fatal(err)
	}
	if err := attempt.Complete(proof, Result{State: "canceled"}); err != nil {
		t.Fatal(err)
	}
	if err := attempt.Complete(proof, Result{State: "failed"}); err != ErrConflict {
		t.Fatal(err)
	}
	attempt.Cancel()
	attempt.Reconcile(proof.Now)
	if attempt.State != "canceled" {
		t.Fatal(attempt.State)
	}
	if _, err := attempt.Heartbeat(proof); err != ErrConflict {
		t.Fatal(err)
	}
}

func TestAttemptUnstartedAndInvalidResults(t *testing.T) {
	attempt, proof := attemptFixture()
	if attempt.Complete(proof, Result{State: "failed"}) != ErrConflict {
		t.Fatal("unstarted result")
	}
	if attempt.Complete(proof, Result{State: "invalid"}) != ErrInvalid {
		t.Fatal("invalid result")
	}
	attempt.Cancel()
	if attempt.State != "canceled" || attempt.Start(proof) != ErrConflict {
		t.Fatal("canceled start")
	}
	attempt, proof = attemptFixture()
	proof.Now = attempt.LeaseExpiresAt
	attempt.Reconcile(proof.Now)
	if attempt.State != "expired" || attempt.Start(proof) != ErrConflict {
		t.Fatal("expired lease")
	}
	for _, result := range []Result{{State: "invalid"}, {State: "succeeded", ExitCode: 1}, {State: "succeeded", Truncated: true}, {State: "failed", Stdout: make([]byte, 10)}, {State: "failed", Stdout: make([]byte, 1), Stderr: make([]byte, 2)}} {
		if result.Validate(2) != ErrInvalid {
			t.Fatal("invalid result accepted")
		}
	}
	if (Result{State: "failed"}).Validate(0) != ErrInvalid {
		t.Fatal("invalid limit")
	}
}

func TestResultMetadataValidation(t *testing.T) {
	content := []byte("output")
	digest := sha256.Sum256(content)
	result := Result{State: "succeeded", Reason: "completed", StartedAt: 1, FinishedAt: 2, Stdout: content, StdoutBytes: len(content), StdoutSHA256: hex.EncodeToString(digest[:])}
	if err := result.Validate(100); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Result){func(r *Result) { r.Signal = 129 }, func(r *Result) { r.FinishedAt = 0 }, func(r *Result) { r.StdoutBytes = 1 }, func(r *Result) { r.Reason = "private path" }, func(r *Result) { r.StdoutSHA256 = "invalid" }} {
		invalid := result
		mutate(&invalid)
		if invalid.Validate(100) != ErrInvalid {
			t.Fatal("invalid metadata accepted")
		}
	}
}
