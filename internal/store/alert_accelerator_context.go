package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
)

type AlertAcceleratorContext struct {
	Found                bool
	Inactive             bool
	Name, Vendor, Digest string
	Snapshot             *model.AcceleratorInventorySnapshot
	Policy               model.AcceleratorPolicy
}

// AcceleratorContext 只读库存与已发布的原生receipt元数据，不在SQLite读取GPU数值。
func (s *AlertEvaluationStore) AcceleratorContext(ctx context.Context, key AlertEvaluationKey) (AlertAcceleratorContext, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AlertAcceleratorContext{}, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := readAlertAcceleratorContext(ctx, tx, key)
	if err != nil {
		return result, err
	}
	return result, tx.Commit()
}

func readAlertAcceleratorContext(ctx context.Context, db schemaExecutor, key AlertEvaluationKey) (AlertAcceleratorContext, error) {
	var result AlertAcceleratorContext
	var ordinal int
	var name string
	var inactive sql.NullTime
	err := db.QueryRowContext(ctx, "SELECT ordinal,model,vendor,inactive_at FROM node_accelerators WHERE node_id=? AND device_id=?", key.NodeID, key.DeviceID).Scan(&ordinal, &name, &result.Vendor, &inactive)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return result, err
	}
	result.Found, result.Inactive = err == nil, inactive.Valid
	if result.Found {
		result.Name = fmt.Sprintf("GPU %d · %s", ordinal, name)
	}
	var raw string
	err = db.QueryRowContext(ctx, "SELECT inventory_json,payload_digest FROM node_accelerator_state WHERE node_id=?", key.NodeID).Scan(&raw, &result.Digest)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return result, err
	}
	installation := false
	if err == nil {
		snapshot, err := model.DecodeAcceleratorInventory(raw)
		if err != nil {
			return result, err
		}
		result.Snapshot, installation = &snapshot, snapshot.InstallationEnabled
	}
	result.Policy, err = effectiveAcceleratorPolicy(ctx, db, acceleratorPolicySelection{nodeID: key.NodeID, installation: installation})
	return result, err
}

func recheckAcceleratorEvaluation(ctx context.Context, db schemaExecutor, change AlertEvaluationCommit) error {
	state := change.Decision.State
	current, err := readAlertAcceleratorContext(ctx, db, AlertEvaluationKey{RuleID: state.RuleID, NodeID: state.NodeID, DeviceID: state.DeviceID})
	if err != nil {
		return err
	}
	sampleID := ""
	if current.Snapshot != nil {
		sampleID = current.Snapshot.SampleID
	}
	for _, reference := range change.Policies {
		if reference.AcceleratorDeviceID != state.DeviceID || reference.AcceleratorDigest != current.Digest || reference.AcceleratorPolicyRevision != current.Policy.Revision || reference.AcceleratorSampleID != sampleID {
			return alerteval.ErrConflict
		}
	}
	if state.Reason == model.AcceleratorDeviceUnavailable && !current.DeviceUnavailable(state.NodeID, state.DeviceID) {
		return alerteval.ErrConflict
	}
	return nil
}

func (s *AlertEvaluationStore) AcceleratorTargets(ctx context.Context, nodeID, selector string) ([]string, error) {
	statement := "SELECT device_id FROM node_accelerators WHERE node_id=?"
	args := []any{nodeID}
	if selector == model.AcceleratorAnyDevice {
		statement += " AND inactive_at IS NULL"
	} else {
		statement += " AND device_id=?"
		args = append(args, selector)
	}
	statement += " ORDER BY device_id LIMIT ?"
	args = append(args, model.MaximumAccelerators+1)
	rows, err := s.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if len(ids) > model.MaximumAccelerators {
		return nil, alerteval.ErrCapacity
	}
	return ids, rows.Err()
}

func (source AlertAcceleratorContext) DeviceUnavailable(nodeID, deviceID string) bool {
	if !confirmedAcceleratorCollection(source) || !reportedAcceleratorVendor(source.Snapshot.Capabilities, source.Vendor) {
		return false
	}
	for _, device := range source.Snapshot.Devices {
		if model.AcceleratorDeviceID(nodeID, device.Fingerprint) == deviceID {
			return false
		}
	}
	return true
}

func confirmedAcceleratorCollection(source AlertAcceleratorContext) bool {
	if !source.Found || !source.Inactive || !source.Policy.Enabled || source.Snapshot == nil || source.Snapshot.PolicyRevision != source.Policy.Revision {
		return false
	}
	return source.Snapshot.State == "ok" || source.Snapshot.State == "partial"
}

func reportedAcceleratorVendor(capabilities []model.AcceleratorCapability, vendor string) bool {
	for _, capability := range capabilities {
		if capability.Vendor == vendor && (capability.State == "ok" || capability.State == "partial") {
			return true
		}
	}
	return false
}
