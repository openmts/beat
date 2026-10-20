package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/beat/backend/internal/model"
)

func (s *AcceleratorStore) State(ctx context.Context, nodeID string) (*model.AcceleratorInventorySnapshot, error) {
	var encoded string
	err := s.db.QueryRowContext(ctx, `SELECT inventory_json FROM node_accelerator_state WHERE node_id=?`, nodeID).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	snapshot, err := model.DecodeAcceleratorInventory(encoded)
	if err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func (s *AcceleratorStore) Devices(ctx context.Context, nodeID string) (values []model.AcceleratorDeviceAdmin, resultErr error) {
	rows, err := s.db.QueryContext(ctx, `SELECT device_id,ordinal,vendor,model,fingerprint,driver_version,capabilities_json,error_codes_json,first_seen_at,last_seen_at,inactive_at
 FROM node_accelerators WHERE node_id=? ORDER BY ordinal LIMIT ?`, nodeID, MaximumAcceleratorInventory+1)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	values = []model.AcceleratorDeviceAdmin{}
	for rows.Next() {
		var value model.AcceleratorDeviceAdmin
		var caps, codes string
		var inactive sql.NullTime
		if err = rows.Scan(&value.DeviceID, &value.Ordinal, &value.Vendor, &value.Model, &value.Fingerprint, &value.DriverVersion, &caps, &codes, &value.FirstSeenAt, &value.LastSeenAt, &inactive); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(caps), &value.Capabilities); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(codes), &value.ErrorCodes); err != nil {
			return nil, err
		}
		value.InactiveAt = nullableTime(inactive)
		value.Metrics = map[string]float64{}
		value.Stale = true
		values = append(values, value)
	}
	if len(values) > MaximumAcceleratorInventory {
		return nil, ErrAcceleratorCapacity
	}
	return values, rows.Err()
}

func (s *AcceleratorStore) DiscardPending(ctx context.Context, pending AcceleratorPending) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM accelerator_ingest_pending WHERE node_id=? AND report_id=?`, pending.NodeID, pending.ReportID)
	return err
}

func (s *AcceleratorStore) AuthorizeRead(ctx context.Context, actor AcceleratorActor, nodeID string) error {
	return acceleratorAuthorize(ctx, s.db, acceleratorAuthorization{actor: &actor, scope: AcceleratorPolicyScope{Kind: "node", ID: nodeID}})
}

func (s *AcceleratorStore) PublicNode(ctx context.Context, nodeID string) (bool, error) {
	var public bool
	err := s.db.QueryRowContext(ctx, `SELECT is_public FROM nodes WHERE id=?`, nodeID).Scan(&public)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return public, err
}

func (s *AcceleratorStore) Ready(ctx context.Context) error {
	var count int
	return s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM accelerator_ingest_pending`).Scan(&count)
}

func (s *AcceleratorStore) Enabled(ctx context.Context) (bool, error) {
	var enabled bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM accelerator_collection_policies WHERE enabled=1)
 OR EXISTS(SELECT 1 FROM node_accelerator_state s WHERE json_extract(s.inventory_json,'$.installation_enabled')=1
 AND NOT EXISTS(SELECT 1 FROM accelerator_collection_policies p WHERE p.scope_kind='node' AND p.scope_id=s.node_id AND p.enabled=0))
 OR EXISTS(SELECT 1 FROM accelerator_ingest_pending s WHERE json_extract(s.inventory_json,'$.installation_enabled')=1
 AND NOT EXISTS(SELECT 1 FROM accelerator_collection_policies p WHERE p.scope_kind='node' AND p.scope_id=s.node_id AND p.enabled=0))`).Scan(&enabled)
	return enabled, err
}

type AcceleratorStats struct {
	ActiveDevices, InactiveDevices, Pending int64
	OldestPendingAt                         *time.Time
}

func (s *AcceleratorStore) Stats(ctx context.Context) (AcceleratorStats, error) {
	var value AcceleratorStats
	var oldest sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(inactive_at IS NULL),0),COALESCE(SUM(inactive_at IS NOT NULL),0) FROM node_accelerators`).Scan(&value.ActiveDevices, &value.InactiveDevices)
	if err != nil {
		return value, err
	}
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM accelerator_ingest_pending`).Scan(&value.Pending); err != nil {
		return value, err
	}
	err = s.db.QueryRowContext(ctx, `SELECT created_at FROM accelerator_ingest_pending ORDER BY created_at LIMIT 1`).Scan(&oldest)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return value, err
	}
	value.OldestPendingAt = nullableTime(oldest)
	return value, nil
}
