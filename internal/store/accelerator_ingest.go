package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/beat/backend/internal/model"
)

type AcceleratorPending struct {
	NodeID, ReportID, PayloadDigest, CredentialFingerprint string
	Snapshot                                               model.AcceleratorInventorySnapshot
	CreatedAt                                              time.Time
}

func stageAcceleratorContact(ctx context.Context, tx *sql.Tx, contact AgentContact) error {
	report, err := acceleratorContactReport(contact)
	if err != nil || report == nil {
		return err
	}
	if err := report.Validate(contact.ReportCapturedAt); err != nil {
		return err
	}
	policy, err := effectiveAcceleratorPolicy(ctx, tx, acceleratorPolicySelection{nodeID: contact.Identity.NodeID, installation: report.InstallationEnabled})
	if err != nil {
		return err
	}
	if err = validateAcceleratorAcceptance(*report, policy); err != nil {
		return err
	}
	if err = acceleratorInventoryCapacity(ctx, tx, contact); err != nil {
		return err
	}
	encoded, err := json.Marshal(report.Inventory())
	if err != nil {
		return err
	}
	digest, err := report.Digest()
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO accelerator_ingest_pending(node_id,report_id,sample_id,captured_at,payload_digest,inventory_json,credential_fingerprint,created_at)
 VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(node_id) DO UPDATE SET report_id=excluded.report_id,sample_id=excluded.sample_id,captured_at=excluded.captured_at,
 payload_digest=excluded.payload_digest,inventory_json=excluded.inventory_json,credential_fingerprint=excluded.credential_fingerprint,created_at=excluded.created_at`,
		contact.Identity.NodeID, contact.ReportID, report.SampleID, report.CapturedAt, digest, string(encoded), contact.Identity.CredentialFingerprint, time.Now().UTC())
	return acceleratorExactlyOne(result, err)
}

func validateAcceleratorAcceptance(report model.AcceleratorReport, policy model.AcceleratorPolicy) error {
	if report.PolicyRevision != policy.Revision {
		return ErrAcceleratorPolicy
	}
	if !policy.Enabled && acceleratorReportCollecting(report) {
		return ErrAcceleratorPolicy
	}
	if len(report.Devices) > policy.MaximumDevices {
		return ErrAcceleratorCapacity
	}
	if policy.Configured && report.CadenceMS != int64(policy.SampleIntervalSeconds)*1000 {
		return ErrAcceleratorPolicy
	}
	for _, device := range report.Devices {
		for key := range device.Metrics {
			if !slices.Contains(policy.Metrics, key) {
				return ErrAcceleratorPolicy
			}
		}
	}
	return nil
}

func acceleratorInventoryCapacity(ctx context.Context, tx *sql.Tx, contact AgentContact) error {
	rows, err := tx.QueryContext(ctx, `SELECT fingerprint FROM node_accelerators WHERE node_id=? LIMIT ?`, contact.Identity.NodeID, MaximumAcceleratorInventory+1)
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		known[id] = true
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	for _, device := range contact.Accelerators.Devices {
		known[device.Fingerprint] = true
	}
	if len(known) > MaximumAcceleratorInventory {
		return ErrAcceleratorCapacity
	}
	return nil
}

func (s *AcceleratorStore) Pending(ctx context.Context, nodeID string) (*AcceleratorPending, error) {
	var item AcceleratorPending
	var encoded string
	err := s.db.QueryRowContext(ctx, `SELECT node_id,report_id,payload_digest,credential_fingerprint,inventory_json,created_at FROM accelerator_ingest_pending WHERE node_id=?`, nodeID).Scan(&item.NodeID, &item.ReportID, &item.PayloadDigest, &item.CredentialFingerprint, &encoded, &item.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	item.Snapshot, err = model.DecodeAcceleratorInventory(encoded)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *AcceleratorStore) PendingNodes(ctx context.Context) (ids []string, resultErr error) {
	rows, err := s.db.QueryContext(ctx, `SELECT node_id FROM accelerator_ingest_pending ORDER BY created_at,node_id LIMIT 128`)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func acceleratorContactReport(contact AgentContact) (*model.AcceleratorReport, error) {
	report := contact.Accelerators
	if report == nil || contact.Identity.Mode == model.AgentCredentialActive {
		return report, nil
	}
	if report.State == "disabled" && !report.InstallationEnabled {
		return nil, nil
	}
	return nil, ErrAgentCredentialRejected
}

func acceleratorReportCollecting(report model.AcceleratorReport) bool {
	return report.State == "ok" || report.State == "partial" || len(report.Devices) > 0
}
