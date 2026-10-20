package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

var ErrAcceleratorPolicy = errors.New("accelerator policy changed or disabled")

var ErrAcceleratorUnavailable = errors.New("accelerator persistence unavailable")

var ErrAcceleratorCapacity = errors.New("accelerator inventory capacity exceeded")

const MaximumAcceleratorInventory = 256

type AcceleratorStore struct{ db *sql.DB }

func NewAcceleratorStore(db *sql.DB) *AcceleratorStore { return &AcceleratorStore{db: db} }

type AcceleratorActor struct{ UserID, SessionID, RequestID, IPAddress, UserAgent, Username, SessionPrefix string }

type AcceleratorPolicyScope struct{ Kind, ID string }

type AcceleratorPolicyChange struct {
	Scope AcceleratorPolicyScope
	Write model.AcceleratorPolicyWrite
}

func readAcceleratorPolicy(ctx context.Context, db schemaExecutor, scope AcceleratorPolicyScope) (model.AcceleratorPolicy, error) {
	policy := model.DefaultAcceleratorPolicy()
	policy.ScopeKind, policy.ScopeID = scope.Kind, scope.ID
	var metrics string
	err := db.QueryRowContext(ctx, `SELECT enabled,sample_interval_seconds,maximum_devices,metrics_json,revision FROM accelerator_collection_policies WHERE scope_kind=? AND scope_id=?`, scope.Kind, scope.ID).Scan(&policy.Enabled, &policy.SampleIntervalSeconds, &policy.MaximumDevices, &metrics, &policy.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return policy, nil
	}
	if err != nil {
		return policy, err
	}
	if err = json.Unmarshal([]byte(metrics), &policy.Metrics); err != nil {
		return policy, err
	}
	policy.Configured, policy.EffectiveSource = true, scope.Kind
	return policy, model.ValidateAcceleratorPolicy(model.AcceleratorPolicyWrite{Enabled: policy.Enabled, SampleIntervalSeconds: policy.SampleIntervalSeconds, MaximumDevices: policy.MaximumDevices, Metrics: policy.Metrics, ExpectedRevision: policy.Revision})
}

type acceleratorPolicySelection struct {
	nodeID       string
	installation bool
}

func effectiveAcceleratorPolicy(ctx context.Context, db schemaExecutor, selection acceleratorPolicySelection) (model.AcceleratorPolicy, error) {
	policy, err := readAcceleratorPolicy(ctx, db, AcceleratorPolicyScope{Kind: "node", ID: selection.nodeID})
	if err == nil && !policy.Configured && selection.installation {
		policy.Enabled = true
		policy.EffectiveSource = "installation"
	}
	return policy, err
}

func (s *AcceleratorStore) AgentPolicy(ctx context.Context, nodeID string, installation bool) (model.AcceleratorPolicy, error) {
	return effectiveAcceleratorPolicy(ctx, s.db, acceleratorPolicySelection{nodeID: nodeID, installation: installation})
}

func (s *AcceleratorStore) Policy(ctx context.Context, actor AcceleratorActor, scope AcceleratorPolicyScope) (model.AcceleratorPolicy, error) {
	tx, err := beginWriteTx(ctx, s.db)
	if err != nil {
		return model.AcceleratorPolicy{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = acceleratorAuthorize(ctx, tx, acceleratorAuthorization{actor: &actor, scope: scope}); err != nil {
		return model.AcceleratorPolicy{}, err
	}
	policy, err := readAcceleratorPolicy(ctx, tx, scope)
	if err != nil {
		return policy, err
	}
	if !policy.Configured && scope.Kind == "node" {
		policy, err = acceleratorInstallationPolicy(ctx, tx, policy)
	}
	return policy, err
}

func acceleratorInstallationPolicy(ctx context.Context, db schemaExecutor, policy model.AcceleratorPolicy) (model.AcceleratorPolicy, error) {
	var encoded string
	err := db.QueryRowContext(ctx, `SELECT inventory_json FROM node_accelerator_state WHERE node_id=?`, policy.ScopeID).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return policy, nil
	}
	if err != nil {
		return policy, err
	}
	snapshot, err := model.DecodeAcceleratorInventory(encoded)
	if err == nil && snapshot.InstallationEnabled {
		policy.Enabled = true
		policy.EffectiveSource = "installation"
		policy.SampleIntervalSeconds = int(snapshot.CadenceMS / 1000)
	}
	return policy, err
}

func (s *AcceleratorStore) SavePolicy(ctx context.Context, actor AcceleratorActor, change AcceleratorPolicyChange) (model.AcceleratorPolicy, error) {
	if err := model.ValidateAcceleratorPolicy(change.Write); err != nil {
		return model.AcceleratorPolicy{}, err
	}
	tx, err := beginWriteTx(ctx, s.db)
	if err != nil {
		return model.AcceleratorPolicy{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = acceleratorAuthorize(ctx, tx, acceleratorAuthorization{actor: &actor, scope: change.Scope, write: true}); err != nil {
		return model.AcceleratorPolicy{}, err
	}
	previous, err := readAcceleratorPolicy(ctx, tx, change.Scope)
	if err != nil {
		return previous, err
	}
	if previous.Revision != change.Write.ExpectedRevision {
		return previous, ErrAcceleratorPolicy
	}
	if err = writeAcceleratorPolicy(ctx, tx, acceleratorPolicyMutation{actor: actor, change: change}); err != nil {
		return previous, err
	}
	if err = tx.Commit(); err != nil {
		return previous, err
	}
	return s.Policy(ctx, actor, change.Scope)
}

type acceleratorPolicyMutation struct {
	actor  AcceleratorActor
	change AcceleratorPolicyChange
}

func writeAcceleratorPolicy(ctx context.Context, tx *sql.Tx, input acceleratorPolicyMutation) error {
	change, actor := input.change, input.actor
	keys := slices.Clone(change.Write.Metrics)
	slices.Sort(keys)
	encoded, err := json.Marshal(keys)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO accelerator_collection_policies(scope_kind,scope_id,enabled,sample_interval_seconds,maximum_devices,metrics_json,revision,created_by,updated_by,updated_at)
 VALUES(?,?,?,?,?,?,1,?,?,?) ON CONFLICT(scope_kind,scope_id) DO UPDATE SET enabled=excluded.enabled,sample_interval_seconds=excluded.sample_interval_seconds,
 maximum_devices=excluded.maximum_devices,metrics_json=excluded.metrics_json,revision=revision+1,updated_by=excluded.updated_by,updated_at=excluded.updated_at`, change.Scope.Kind, change.Scope.ID, change.Write.Enabled, change.Write.SampleIntervalSeconds, change.Write.MaximumDevices, string(encoded), actor.UserID, actor.UserID, time.Now().UTC())
	if err = acceleratorExactlyOne(result, err); err != nil {
		return err
	}
	if change.Scope.Kind == "node" && !change.Write.Enabled {
		if _, err = tx.ExecContext(ctx, `UPDATE node_accelerators SET inactive_at=? WHERE node_id=? AND inactive_at IS NULL`, time.Now().UTC(), change.Scope.ID); err != nil {
			return err
		}
	}
	return acceleratorAudit(ctx, tx, input)
}

func acceleratorAudit(ctx context.Context, tx *sql.Tx, input acceleratorPolicyMutation) error {
	actor := input.actor
	detail, err := json.Marshal(map[string]string{"scope_kind": input.change.Scope.Kind})
	if err != nil {
		return err
	}
	event := model.AdminAuditEvent{ID: uuid.NewString(), RequestID: actor.RequestID, ActorID: actor.UserID, ActorUsername: actor.Username, Action: "accelerator.policy.update", ResourceType: "accelerator_policy", ResourceID: input.change.Scope.ID, Outcome: model.AuditOutcomeSuccess, DetailJSON: string(detail), IPAddress: actor.IPAddress, UserAgent: actor.UserAgent, SessionPrefix: actor.SessionPrefix, CreatedAt: time.Now().UTC()}
	return insertAuditEvent(ctx, tx, &event)
}
