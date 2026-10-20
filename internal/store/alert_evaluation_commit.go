package store

import (
	"context"
	"database/sql"
	"strings"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
)

type AlertMetricPolicyReference struct {
	Key                                    string
	Revision                               int64
	DefinitionVersion                      string
	CadenceMS                              int64
	AcceleratorPolicyRevision              int64
	AcceleratorSampleID                    string
	AcceleratorDeviceID, AcceleratorDigest string
}

type AlertEvaluationCommit struct {
	Decision             alerteval.StepResult
	ExpectedStateVersion int64
	RuleRevision         int64
	Policies             []AlertMetricPolicyReference
	LeaseTokenHash       []byte
}

func (s *AlertEvaluationStore) Commit(ctx context.Context, change AlertEvaluationCommit) error {
	ctx, cancel := context.WithTimeout(ctx, alerteval.CommitTimeout)
	defer cancel()
	tx, err := beginWriteTx(ctx, s.db)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.CommitTx(ctx, tx, change); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *AlertEvaluationStore) CommitTx(ctx context.Context, tx *sql.Tx, change AlertEvaluationCommit) error {
	if tx == nil || change.Decision.State.Version != change.ExpectedStateVersion+1 || len(change.Decision.Actions) > 2 {
		return alerteval.ErrConflict
	}
	if err := authorizeEvaluationCommit(ctx, tx, change); err != nil {
		return err
	}
	if err := recheckEvaluationIncident(ctx, tx, change); err != nil {
		return err
	}
	if err := writeEvaluationState(ctx, tx, change); err != nil {
		return err
	}
	for _, action := range change.Decision.Actions {
		if err := applyEvaluationAction(ctx, tx, evaluationAction{action: action, state: change.Decision.State}); err != nil {
			return err
		}
	}
	return nil
}

func writeEvaluationState(ctx context.Context, tx *sql.Tx, change AlertEvaluationCommit) error {
	state := change.Decision.State
	values := evaluationStateValues(state)
	if change.ExpectedStateVersion == 0 {
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM alert_evaluation_states").Scan(&count); err != nil {
			return err
		}
		if count >= alerteval.MaximumStateRows {
			return alerteval.ErrCapacity
		}
		result, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO alert_evaluation_states("+evaluationStateColumns+") VALUES("+sqlPlaceholders(len(values))+")", values...)
		return requireEvaluationMutation(result, err)
	}
	columns := strings.Split(strings.ReplaceAll(evaluationStateColumns, "\n\t", ""), ",")[3:]
	for index := range columns {
		columns[index] = strings.TrimSpace(columns[index]) + "=?"
	}
	query := "UPDATE alert_evaluation_states SET " + strings.Join(columns, ",") + ",lease_token_hash=NULL,lease_expires_at=NULL,worker_id='' WHERE rule_id=? AND node_id=? AND device_id=? AND state_version=?"
	arguments := append(values[3:], state.RuleID, state.NodeID, state.DeviceID, change.ExpectedStateVersion)
	if len(change.LeaseTokenHash) > 0 {
		query += " AND lease_token_hash=? AND lease_expires_at>?"
		arguments = append(arguments, change.LeaseTokenHash, model.NowUTC().UnixMilli())
	} else {
		query += " AND lease_token_hash IS NULL"
	}
	result, err := tx.ExecContext(ctx, query, arguments...)
	return requireEvaluationMutation(result, err)
}

func requireEvaluationMutation(result sql.Result, err error) error {
	changed, err := eventMutationResult(result, err)
	if err != nil {
		return err
	}
	if !changed {
		return alerteval.ErrConflict
	}
	return nil
}
