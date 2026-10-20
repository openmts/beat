package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/beat/backend/internal/model"
)

type AcceleratorPublication struct {
	Pending AcceleratorPending
	Receipt model.AcceleratorSampleReceipt
}

func (s *AcceleratorStore) Publish(ctx context.Context, input AcceleratorPublication) error {
	pending, receipt := input.Pending, input.Receipt
	if pending.NodeID != receipt.NodeID || pending.PayloadDigest != receipt.PayloadDigest || pending.Snapshot.SampleID != receipt.Snapshot.SampleID || !pending.Snapshot.CapturedAt.Equal(receipt.Snapshot.CapturedAt) {
		return ErrAgentSampleConflict
	}
	pending.Snapshot = receipt.Snapshot
	tx, err := beginWriteTx(ctx, s.db)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	current, err := s.pendingMatches(ctx, tx, pending)
	if err != nil || !current {
		return err
	}
	if err = publishAcceleratorSnapshot(ctx, tx, pending); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM accelerator_ingest_pending WHERE node_id=? AND report_id=?`, pending.NodeID, pending.ReportID)
	if err = acceleratorExactlyOne(result, err); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *AcceleratorStore) pendingMatches(ctx context.Context, tx *sql.Tx, pending AcceleratorPending) (bool, error) {
	var reportID string
	err := tx.QueryRowContext(ctx, `SELECT report_id FROM accelerator_ingest_pending WHERE node_id=?`, pending.NodeID).Scan(&reportID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if reportID != pending.ReportID {
		return false, nil
	}
	node, err := scanNode(tx.QueryRowContext(ctx, "SELECT "+nodeSelectColumns+
		" FROM nodes WHERE "+hiddenNodeFilter+" AND id=?", pending.NodeID))
	if err != nil {
		return false, err
	}
	if !matchesAgentCredential(node, model.AgentIdentity{NodeID: pending.NodeID, Mode: model.AgentCredentialActive, CredentialFingerprint: pending.CredentialFingerprint}) {
		return false, ErrAgentCredentialRejected
	}
	var captured time.Time
	err = tx.QueryRowContext(ctx, `SELECT captured_at FROM node_accelerator_state WHERE node_id=?`, pending.NodeID).Scan(&captured)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if err == nil && pending.Snapshot.CapturedAt.Before(captured) {
		return false, ErrAgentSampleOrder
	}
	return true, nil
}

func publishAcceleratorSnapshot(ctx context.Context, tx *sql.Tx, pending AcceleratorPending) error {
	snapshot := pending.Snapshot
	policy, err := effectiveAcceleratorPolicy(ctx, tx, acceleratorPolicySelection{nodeID: pending.NodeID, installation: snapshot.InstallationEnabled})
	if err != nil {
		return err
	}
	if snapshot.PolicyRevision != policy.Revision {
		return ErrAcceleratorPolicy
	}
	if err := writeAcceleratorState(ctx, tx, pending); err != nil {
		return err
	}
	if !policy.Enabled {
		_, err = tx.ExecContext(ctx, `UPDATE node_accelerators SET inactive_at=? WHERE node_id=? AND inactive_at IS NULL`, snapshot.CapturedAt, pending.NodeID)
		return err
	}
	if !acceleratorSnapshotComplete(snapshot.State) {
		return nil
	}
	for _, capability := range snapshot.Capabilities {
		if !acceleratorSnapshotComplete(capability.State) {
			continue
		}
		if err = publishAcceleratorVendor(ctx, tx, acceleratorVendorPublication{nodeID: pending.NodeID, vendor: capability.Vendor, snapshot: snapshot}); err != nil {
			return err
		}
	}
	return nil
}

type acceleratorVendorPublication struct {
	nodeID, vendor string
	snapshot       model.AcceleratorInventorySnapshot
}

func publishAcceleratorVendor(ctx context.Context, tx *sql.Tx, input acceleratorVendorPublication) error {
	if _, err := tx.ExecContext(ctx, `UPDATE node_accelerators SET inactive_at=? WHERE node_id=? AND vendor=? AND inactive_at IS NULL`, input.snapshot.CapturedAt, input.nodeID, input.vendor); err != nil {
		return err
	}
	for _, device := range input.snapshot.Devices {
		if device.Vendor != input.vendor {
			continue
		}
		if err := upsertAcceleratorDevice(ctx, tx, acceleratorDeviceWrite{nodeID: input.nodeID, at: input.snapshot.CapturedAt, device: device}); err != nil {
			return err
		}
	}
	return nil
}

type acceleratorDeviceWrite struct {
	nodeID string
	at     time.Time
	device model.AcceleratorInventoryDevice
}

func upsertAcceleratorDevice(ctx context.Context, tx *sql.Tx, input acceleratorDeviceWrite) error {
	device := input.device
	capabilities, err := json.Marshal(device.Capabilities)
	if err != nil {
		return err
	}
	codes := []string{}
	for _, issue := range device.Errors {
		codes = append(codes, issue.Metric+":"+issue.Code)
	}
	encodedErrors, err := json.Marshal(codes)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO node_accelerators(node_id,fingerprint,device_id,ordinal,vendor,model,driver_version,capabilities_json,error_codes_json,first_seen_at,last_seen_at)
 VALUES(?,?,?,(SELECT COALESCE(MAX(ordinal),0)+1 FROM node_accelerators WHERE node_id=?),?,?,?,?,?,?,?)
 ON CONFLICT(node_id,fingerprint) DO UPDATE SET model=excluded.model,driver_version=excluded.driver_version,capabilities_json=excluded.capabilities_json,error_codes_json=excluded.error_codes_json,last_seen_at=excluded.last_seen_at,inactive_at=NULL`,
		input.nodeID, device.Fingerprint, model.AcceleratorDeviceID(input.nodeID, device.Fingerprint), input.nodeID, device.Vendor, device.Model, device.DriverVersion, string(capabilities), string(encodedErrors), input.at, input.at)
	return acceleratorExactlyOne(result, err)
}

func writeAcceleratorState(ctx context.Context, tx *sql.Tx, pending AcceleratorPending) error {
	snapshot := pending.Snapshot
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO node_accelerator_state(node_id,sample_id,captured_at,payload_digest,inventory_json,accepted_at) VALUES(?,?,?,?,?,?)
 ON CONFLICT(node_id) DO UPDATE SET sample_id=excluded.sample_id,captured_at=excluded.captured_at,payload_digest=excluded.payload_digest,inventory_json=excluded.inventory_json,accepted_at=excluded.accepted_at`, pending.NodeID, snapshot.SampleID, snapshot.CapturedAt, pending.PayloadDigest, string(encoded), time.Now().UTC())
	if err = acceleratorExactlyOne(result, err); err != nil {
		return err
	}

	return nil
}

func acceleratorSnapshotComplete(state string) bool { return state == "ok" || state == "partial" }
