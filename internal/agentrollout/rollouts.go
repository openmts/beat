package agentrollout

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/agentrollout/protocol"
)

const maximumTargets = 1000

type Policy struct {
	CanaryCount            int       `json:"canary_count"`
	WaveSize               int       `json:"wave_size"`
	MaxConcurrent          int       `json:"max_concurrent"`
	ObservationSeconds     int       `json:"observation_seconds"`
	ReportFreshnessSeconds int       `json:"report_freshness_seconds"`
	MaxRestarts            int       `json:"max_restarts"`
	MaxProtocolErrors      int       `json:"max_protocol_errors"`
	MaxMetricErrors        int       `json:"max_metric_errors"`
	MaxTaskFailures        int       `json:"max_task_failures"`
	AllowPreview           bool      `json:"allow_preview"`
	WindowStart            time.Time `json:"window_start"`
	WindowEnd              time.Time `json:"window_end"`
}

type CreateInput struct {
	Name                string   `json:"name"`
	ReleaseID           string   `json:"release_id"`
	NodeIDs             []string `json:"node_ids"`
	GroupIDs            []string `json:"group_ids"`
	Tags                []string `json:"tags"`
	Policy              Policy   `json:"policy"`
	Action              string   `json:"action"`
	AcknowledgeRollback bool     `json:"acknowledge_rollback"`
}

type Rollout struct {
	ServerTime  *time.Time `json:"server_time,omitempty"`
	Action      string     `json:"action"`
	ID          string     `json:"id"`
	ReleaseID   string     `json:"release_id"`
	Name        string     `json:"name"`
	State       string     `json:"state"`
	Reason      string     `json:"reason"`
	CurrentWave int        `json:"current_wave"`
	TotalWaves  int        `json:"total_waves"`
	Policy      Policy     `json:"policy"`
}

type Target struct {
	HealthySince   *time.Time `json:"healthy_since"`
	LastReportAt   *time.Time `json:"last_report_at"`
	LeaseExpiresAt *time.Time `json:"lease_expires_at"`
	ID             string     `json:"id"`
	NodeID         string     `json:"node_id"`
	Name           string     `json:"name"`
	Version        string     `json:"source_version"`
	OS             string     `json:"os"`
	Arch           string     `json:"arch"`
	State          string     `json:"state"`
	Reason         string     `json:"reason"`
	Wave           int        `json:"wave"`
}

func normalizePolicy(policy Policy, now time.Time) (Policy, error) {
	policy = defaultPolicy(policy, now)
	if !validPolicyConcurrency(policy) || !validPolicyHealth(policy) || !validPolicyWindow(policy, now) {
		return policy, ErrInvalid
	}
	return policy, nil
}

func defaultPolicy(policy Policy, now time.Time) Policy {
	if policy.CanaryCount == 0 {
		policy.CanaryCount = defaultCanaryCount
	}
	if policy.WaveSize == 0 {
		policy.WaveSize = defaultWaveSize
	}
	if policy.MaxConcurrent == 0 {
		policy.MaxConcurrent = defaultConcurrentAssignments
	}
	if policy.ObservationSeconds == 0 {
		policy.ObservationSeconds = defaultObservationSeconds
	}
	if policy.ReportFreshnessSeconds == 0 {
		policy.ReportFreshnessSeconds = defaultReportFreshnessSeconds
	}
	if policy.WindowStart.IsZero() {
		policy.WindowStart = now
	}
	if policy.WindowEnd.IsZero() {
		policy.WindowEnd = now.Add(defaultRolloutWindow)
	}
	return policy
}

func validPolicyConcurrency(p Policy) bool {
	return p.CanaryCount >= 1 && p.CanaryCount <= maximumTargets && p.WaveSize >= 1 && p.WaveSize <= maximumTargets && p.MaxConcurrent >= 1 && p.MaxConcurrent <= maximumConcurrentAssignments
}

func validPolicyHealth(p Policy) bool {
	return p.ObservationSeconds >= minimumObservationSeconds && p.ObservationSeconds <= maximumObservationSeconds && p.ReportFreshnessSeconds >= minimumReportFreshnessSeconds && p.ReportFreshnessSeconds <= maximumReportFreshnessSeconds && p.MaxRestarts >= 0 && p.MaxProtocolErrors >= 0 && p.MaxMetricErrors >= 0 && p.MaxTaskFailures >= 0
}

func validPolicyWindow(p Policy, now time.Time) bool {
	return p.WindowEnd.After(p.WindowStart) && p.WindowEnd.After(now) && p.WindowEnd.Sub(p.WindowStart) <= maximumRolloutWindow
}

func (s *Service) Create(ctx context.Context, actor Actor, input CreateInput) (Rollout, error) {
	if input.Action == "" {
		input.Action = protocol.AssignmentUpgrade
	}
	if input.Action != protocol.AssignmentUpgrade && input.Action != protocol.AssignmentRollback || input.Action == protocol.AssignmentRollback && !input.AcknowledgeRollback {
		return Rollout{}, ErrInvalid
	}
	if input.Name == "" || len(input.Name) > maximumIdentifierBytes {
		return Rollout{}, ErrInvalid
	}
	policy, err := normalizePolicy(input.Policy, s.now())
	if err != nil {
		return Rollout{}, err
	}
	rollout := Rollout{Action: input.Action, ID: uuid.NewString(), ReleaseID: input.ReleaseID, Name: input.Name, State: "paused", Reason: "awaiting_promotion", CurrentWave: -1, Policy: policy}
	err = s.mutate(ctx, actor, func(tx *transaction) error { return s.createRollout(tx, &rollout, input) })
	if err != nil {
		return Rollout{}, err
	}
	return rollout, nil
}

func (tx *transaction) insertTargets(rollout Rollout, targets []Target) error {
	for index, target := range targets {
		wave := 0
		if index >= rollout.Policy.CanaryCount {
			wave = 1 + (index-rollout.Policy.CanaryCount)/rollout.Policy.WaveSize
		}
		if err := tx.exec(`INSERT INTO agent_rollout_targets(id,rollout_id,node_id,node_snapshot_id,node_name,source_version,os,arch,wave,state,reason,updated_at) VALUES(?,?,?,?,?,?,?,?,?,'pending','',?)`, uuid.NewString(), rollout.ID, target.NodeID, target.NodeID, target.Name, target.Version, target.OS, target.Arch, wave, tx.at); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) createRollout(tx *transaction, rollout *Rollout, input CreateInput) error {
	policy := rollout.Policy
	release, _, err := s.releaseInTransaction(tx, input.ReleaseID)
	if err != nil {
		return err
	}
	if release.Manifest.MinimumServerSchema > s.schema {
		return ErrManifest
	}
	targets, err := tx.snapshotTargets(input)
	if err != nil {
		return err
	}
	rollout.TotalWaves = 1
	if len(targets) > policy.CanaryCount {
		rollout.TotalWaves += (len(targets) - policy.CanaryCount + policy.WaveSize - 1) / policy.WaveSize
	}
	encoded, err := json.Marshal(policy)
	if err != nil {
		return ErrInvalid
	}
	if err := tx.exec(`INSERT INTO agent_rollouts(action,id,release_id,name,state,reason,policy_json,current_wave,total_waves,created_by,created_at,updated_at) VALUES(?,?,?,?,'paused','awaiting_promotion',?,-1,?,?,?,?)`, rollout.Action, rollout.ID, rollout.ReleaseID, rollout.Name, string(encoded), rollout.TotalWaves, tx.actor.UserID, tx.at, tx.at); err != nil {
		return err
	}
	if err := tx.insertTargets(*rollout, targets); err != nil {
		return err
	}
	var count int
	if err := tx.db.QueryRowContext(tx.ctx, "SELECT COUNT(*) FROM agent_rollout_targets WHERE rollout_id=?", rollout.ID).Scan(&count); err != nil || count != len(targets) {
		return ErrUnavailable
	}
	return tx.audit(audit{Action: rolloutCreateEvent(rollout.Action), Kind: "rollout", ID: rollout.ID, Count: len(targets)})
}

func rolloutCreateEvent(action string) string {
	if action == protocol.AssignmentRollback {
		return "rollout.rollback.create"
	}
	return "rollout.create"
}
