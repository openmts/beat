package protocol

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"slices"
	"time"
)

const LeaseDuration = 30 * time.Second

const HeartbeatGrace = 30 * time.Second

type Attempt struct {
	ID             string     `json:"id"`
	NodeID         string     `json:"node_id"`
	State          string     `json:"state"`
	NonceHash      []byte     `json:"-"`
	LeaseExpiresAt time.Time  `json:"lease_expires_at"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	HeartbeatAt    time.Time  `json:"heartbeat_at"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	ResultDigest   string     `json:"-"`
}

type Proof struct {
	NodeID string    `json:"-"`
	Nonce  string    `json:"nonce"`
	Now    time.Time `json:"-"`
}

func (attempt *Attempt) Authenticate(proof Proof) error {
	if len(proof.Nonce) != 64 || proof.NodeID == "" || attempt.NodeID != proof.NodeID {
		return ErrForbidden
	}
	nonce, err := hex.DecodeString(proof.Nonce)
	if err != nil {
		return ErrForbidden
	}
	digest := sha256.Sum256(nonce)
	if subtle.ConstantTimeCompare(digest[:], attempt.NonceHash) != 1 {
		return ErrForbidden
	}
	return nil
}

// Start 只在持久化事务内调用；已授权的同 nonce 重试不创建第二次执行授权。
func (attempt *Attempt) Start(proof Proof) error {
	if err := attempt.Authenticate(proof); err != nil {
		return err
	}
	if attempt.StartedAt != nil {
		return nil
	}
	if attempt.State != "leased" || !proof.Now.Before(attempt.LeaseExpiresAt) {
		return ErrConflict
	}
	attempt.State = "running"
	attempt.StartedAt = &proof.Now
	attempt.HeartbeatAt = proof.Now
	return nil
}

func (attempt *Attempt) Heartbeat(proof Proof) (bool, error) {
	if err := attempt.Authenticate(proof); err != nil {
		return false, err
	}
	if attempt.StartedAt == nil || attempt.CompletedAt != nil {
		return false, ErrConflict
	}
	attempt.HeartbeatAt = proof.Now
	if attempt.State == "unknown" {
		attempt.State = "running"
	}
	return attempt.State == "cancel_requested", nil
}

func (attempt *Attempt) Cancel() {
	if attempt.CompletedAt != nil {
		return
	}
	if attempt.StartedAt == nil {
		attempt.State = "canceled"
		return
	}
	attempt.State = "cancel_requested"
}

func (attempt *Attempt) Reconcile(now time.Time) {
	if attempt.CompletedAt != nil {
		return
	}
	if attempt.State == "leased" && !now.Before(attempt.LeaseExpiresAt) {
		attempt.State = "expired"
		return
	}
	if attempt.State == "running" && now.Sub(attempt.HeartbeatAt) > HeartbeatGrace {
		attempt.State = "unknown"
	}
}

type Result struct {
	Reason       string `json:"reason,omitempty"`
	Signal       int    `json:"signal,omitempty"`
	StartedAt    int64  `json:"started_at,omitempty"`
	FinishedAt   int64  `json:"finished_at,omitempty"`
	StdoutBytes  int    `json:"stdout_bytes,omitempty"`
	StderrBytes  int    `json:"stderr_bytes,omitempty"`
	StdoutSHA256 string `json:"stdout_sha256,omitempty"`
	StderrSHA256 string `json:"stderr_sha256,omitempty"`
	State        string `json:"state"`
	ExitCode     int    `json:"exit_code"`
	Stdout       []byte `json:"stdout"`
	Stderr       []byte `json:"stderr"`
	Truncated    bool   `json:"truncated"`
}

func (result Result) Validate(limit int) error {
	if err := result.validateMetadata(); err != nil {
		return err
	}
	if limit < 1 || limit > MaximumOutputBytes || len(result.Stdout) > limit || len(result.Stderr) > limit-len(result.Stdout) {
		return ErrInvalid
	}
	if !slices.Contains([]string{"succeeded", "failed", "timed_out", "canceled", "output_limit", "interrupted"}, result.State) {
		return ErrInvalid
	}
	if result.State == "succeeded" && (result.ExitCode != 0 || result.Truncated) {
		return ErrInvalid
	}
	return nil
}

func (attempt *Attempt) Complete(proof Proof, result Result) error {
	if err := attempt.Authenticate(proof); err != nil {
		return err
	}
	if err := result.Validate(MaximumOutputBytes); err != nil {
		return err
	}
	if attempt.StartedAt == nil {
		return ErrConflict
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return ErrInvalid
	}
	defer clear(encoded)
	digest := sha256.Sum256(encoded)
	fingerprint := hex.EncodeToString(digest[:])
	if attempt.CompletedAt != nil {
		if attempt.ResultDigest == fingerprint {
			return nil
		}
		return ErrConflict
	}
	attempt.State = result.State
	attempt.ResultDigest = fingerprint
	attempt.CompletedAt = &proof.Now
	return nil
}

func (result Result) validateMetadata() error {
	if result.Signal < 0 || result.Signal > 128 || result.StartedAt < 0 || result.FinishedAt < result.StartedAt {
		return ErrInvalid
	}
	if result.StdoutBytes != 0 && result.StdoutBytes != len(result.Stdout) || result.StderrBytes != 0 && result.StderrBytes != len(result.Stderr) {
		return ErrInvalid
	}
	if result.Reason != "" && !slices.Contains([]string{"completed", "exit_nonzero", "timeout", "canceled", "output_limit", "interrupted", "local_policy", "sandbox_unavailable", "storage_failure", "start_failed", "file_integrity", "source_changed", "transfer_conflict"}, result.Reason) {
		return ErrInvalid
	}
	if !validOutputDigest(result.Stdout, result.StdoutSHA256) || !validOutputDigest(result.Stderr, result.StderrSHA256) {
		return ErrInvalid
	}
	return nil
}

func validOutputDigest(content []byte, digest string) bool {
	if digest == "" {
		return true
	}
	actual := sha256.Sum256(content)
	return digest == hex.EncodeToString(actual[:])
}
