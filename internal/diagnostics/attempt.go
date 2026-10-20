package diagnostics

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/netip"
	"time"

	"github.com/beat/backend/internal/model"
)

const (
	leaseDuration    = 30 * time.Second
	heartbeatGrace   = 30 * time.Second
	nonceBytes       = 32
	maximumFrameHops = model.MaximumDiagnosticHops
)

type LeaseRequest struct {
	Proof            NodeProof
	CapabilityDigest string
}

// AttemptRequest 是 Agent 对某次尝试的认证请求。
type AttemptRequest struct {
	Proof   NodeProof
	JobID   string
	Attempt int
	Nonce   string
}

// Lease 以 at-most-once 语义派发一条已审批作业；没有任务时返回 nil。
func (service *Service) Lease(ctx context.Context, request LeaseRequest) (*Lease, error) {
	var leased *Lease
	err := service.write(ctx, func(tx transaction) error {
		nodeID, err := tx.node(request.Proof)
		if err != nil {
			return err
		}
		document, digest, err := tx.capability(nodeID)
		if err != nil || digest != request.CapabilityDigest {
			return ErrCapability
		}
		job, target, err := tx.nextJob(nodeID, document)
		if err != nil || job.ID == "" {
			return err
		}
		nonce, err := newNonce()
		if err != nil {
			return ErrUnavailable
		}
		leased, err = tx.leaseJob(job, target, digest, nonce)
		return err
	})
	return leased, err
}

func (tx transaction) nextJob(nodeID string, document CapabilityDocument) (model.DiagnosticJob, model.DiagnosticTarget, error) {
	row := tx.tx.QueryRowContext(tx.ctx, `SELECT `+jobColumns+` FROM diagnostic_jobs
		WHERE source_node_id=? AND state='queued' AND cancel_requested=0 AND created_at>?
		ORDER BY created_at, id LIMIT 1`, nodeID, tx.now.Add(-QueueLifetime))
	job, err := scanJob(row)
	if errors.Is(err, errNoJob) {
		return model.DiagnosticJob{}, model.DiagnosticTarget{}, nil
	}
	if err != nil {
		return model.DiagnosticJob{}, model.DiagnosticTarget{}, err
	}
	target, err := tx.target(job.TargetID)
	if err != nil {
		return model.DiagnosticJob{}, model.DiagnosticTarget{}, ErrUnavailable
	}
	policy, err := tx.policy()
	if err != nil {
		return model.DiagnosticJob{}, model.DiagnosticTarget{}, err
	}
	if !allowsKindIn(document.Kinds, job.Kind) {
		return model.DiagnosticJob{}, model.DiagnosticTarget{}, ErrCapability
	}
	if err := approveTargetPolicy(policy, target); err != nil {
		return model.DiagnosticJob{}, model.DiagnosticTarget{}, err
	}
	return job, target, nil
}

func (tx transaction) leaseJob(job model.DiagnosticJob, target model.DiagnosticTarget, digest, nonce string) (*Lease, error) {
	destination, err := leaseDestination(target, job.IPFamily)
	if err != nil {
		return nil, err
	}
	expires := tx.now.Add(leaseDuration)
	attempt := job.Attempt + 1
	if err := tx.one(`UPDATE diagnostic_jobs SET state='leased', attempt=?, leased_at=?, updated_at=?
		WHERE id=? AND state='queued'`, attempt, tx.now, tx.now, job.ID); err != nil {
		return nil, err
	}
	if err := tx.one(`INSERT INTO diagnostic_attempts(job_id, attempt, state, lease_nonce, capability_digest, failure_code, leased_at)
		VALUES(?,?,?,?,?,'',?)`, job.ID, attempt, "leased", nonceHash(nonce), digest, tx.now); err != nil {
		return nil, err
	}
	if err := tx.audit(job.ActorUserID, "diagnostic.attempt.leased", job.ID); err != nil {
		return nil, err
	}
	return &Lease{JobID: job.ID, Attempt: attempt, Nonce: nonce, ExpiresAt: expires.UnixMilli(),
		Payload: JobPayload{Kind: job.Kind, Destination: destination, Protocol: job.Protocol,
			Direction: job.Direction, IPFamily: job.IPFamily, TargetName: job.TargetName, Port: target.Port,
			CapabilityDigest: digest, Probes: job.Probes, HopLimit: job.HopLimit, DurationSeconds: job.DurationSeconds,
			ParallelStreams: job.ParallelStreams, BitrateMbps: job.BitrateMbps, TargetRevision: job.TargetRevision,
			PolicyRevision: job.PolicyRevision}}, nil
}

// Start 记录 at-most-once 启动确认；同一 nonce 的重试不会创建第二次执行授权。
func (service *Service) Start(ctx context.Context, request AttemptRequest) error {
	return service.write(ctx, func(tx transaction) error {
		attempt, job, err := tx.authenticatedAttempt(request)
		if err != nil {
			return err
		}
		if attempt.State == "running" || attempt.State == "completed" {
			return nil
		}
		if attempt.State != "leased" {
			return ErrConflict
		}
		if !tx.now.Before(attempt.LeaseExpiresAt.Add(leaseDuration)) {
			return ErrConflict
		}
		if err := tx.one(`UPDATE diagnostic_attempts SET state='running', started_at=? WHERE job_id=? AND attempt=? AND state='leased'`,
			tx.now, request.JobID, request.Attempt); err != nil {
			return err
		}
		if err := tx.one(`UPDATE diagnostic_jobs SET state='running', started_at=?, updated_at=? WHERE id=?`, tx.now, tx.now, job.ID); err != nil {
			return err
		}
		return tx.audit(job.ActorUserID, "diagnostic.attempt.started", job.ID)
	})
}

// Heartbeat 返回取消信号，让 Agent 立即终止子进程树。
func (service *Service) Heartbeat(ctx context.Context, request AttemptRequest) (bool, error) {
	var cancel bool
	err := service.write(ctx, func(tx transaction) error {
		attempt, job, err := tx.authenticatedAttempt(request)
		if err != nil {
			return err
		}
		if attempt.State == "completed" {
			return ErrConflict
		}
		if attempt.State == "leased" && !tx.now.Before(attempt.LeaseExpiresAt.Add(leaseDuration)) {
			return ErrConflict
		}
		if err := tx.one(`UPDATE diagnostic_attempts SET heartbeat_at=? WHERE job_id=? AND attempt=?`, tx.now, job.ID, attempt.Attempt); err != nil {
			return err
		}
		cancel = job.CancelRequested
		return nil
	})
	return cancel, err
}

// authenticatedAttempt 校验节点凭据、nonce 与作业归属。
func (tx transaction) authenticatedAttempt(request AttemptRequest) (storedAttempt, model.DiagnosticJob, error) {
	nodeID, err := tx.node(request.Proof)
	if err != nil {
		return storedAttempt{}, model.DiagnosticJob{}, err
	}
	if request.JobID == "" || request.Attempt < 1 {
		return storedAttempt{}, model.DiagnosticJob{}, ErrInvalid
	}
	row := tx.tx.QueryRowContext(tx.ctx, `SELECT job_id, attempt, state, lease_nonce, leased_at, started_at, heartbeat_at
		FROM diagnostic_attempts WHERE job_id=? AND attempt=?`, request.JobID, request.Attempt)
	attempt, err := scanAttempt(row)
	if err != nil {
		return storedAttempt{}, model.DiagnosticJob{}, err
	}
	if subtle.ConstantTimeCompare([]byte(attempt.LeaseNonce), []byte(nonceHash(request.Nonce))) != 1 {
		return storedAttempt{}, model.DiagnosticJob{}, ErrForbidden
	}
	job, err := tx.job(request.JobID)
	if err != nil {
		return storedAttempt{}, model.DiagnosticJob{}, err
	}
	if job.SourceNodeID != nodeID {
		return storedAttempt{}, model.DiagnosticJob{}, ErrForbidden
	}
	return attempt, job, nil
}

// storedAttempt 的 LeaseExpiresAt 直接来自 leased_at；有效租约窗口由 leaseDuration 决定。
type storedAttempt struct {
	JobID          string
	State          string
	LeaseNonce     string
	LeaseExpiresAt time.Time
	HeartbeatAt    time.Time
	StartedAt      *time.Time
	Attempt        int
}

func newNonce() (string, error) {
	buffer := make([]byte, nonceBytes)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func nonceHash(nonce string) string {
	decoded, err := hex.DecodeString(nonce)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(decoded)
	return hex.EncodeToString(digest[:])
}

func leaseDestination(target model.DiagnosticTarget, family string) (string, error) {
	for _, candidate := range target.ResolvedAddresses {
		address, err := netip.ParseAddr(candidate)
		if err != nil {
			continue
		}
		if family == model.IPFamilyAuto || familyMatches(address, family) {
			return normalizeAddress(address).String(), nil
		}
	}
	return "", ErrCapability
}

var errNoJob = errors.New("no queued diagnostic job")
