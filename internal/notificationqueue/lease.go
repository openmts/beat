package notificationqueue

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"io"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/secretbox"
)

const deliveryLeaseLifetime = 90 * time.Second

type deliveryLease struct {
	ID, MessageID, ChannelID, ChannelType, EventKind string
	Revision                                         int64
	Attempt                                          int
	Hash                                             []byte
	ExpiresAt                                        int64
}

func (deliveryLease) String() string { return "notification delivery lease" }

func (service *Service) claim(ctx context.Context, workerID string) (*deliveryLease, error) {
	var lease *deliveryLease
	err := service.secrets.WithWrite(ctx, func(access secretbox.ResourceAccess) error {
		var err error
		lease, err = service.claimTx(ctx, claimOptions{workerID: workerID, access: access})
		return err
	})
	return lease, err
}

type claimOptions struct {
	workerID string
	access   secretbox.ResourceAccess
}

func (service *Service) claimTx(ctx context.Context, input claimOptions) (*deliveryLease, error) {
	tx, err := service.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, ErrQueueUnavailable
	}
	defer func() { _ = tx.Rollback() }()
	if err := service.recoverExpiredTx(ctx, tx, input.access); err != nil {
		return nil, err
	}
	capacity, err := claimCapacity(ctx, tx)
	if err != nil {
		return nil, err
	}
	if !capacity {
		return commitClaim(tx, nil)
	}
	lease, err := selectDueLease(ctx, tx, service.now().UnixMilli())
	if err != nil {
		return nil, err
	}
	if lease == nil {
		return commitClaim(tx, nil)
	}
	token := make([]byte, 32)
	if _, err := io.ReadFull(service.random, token); err != nil {
		return nil, ErrQueueUnavailable
	}
	hash := sha256.Sum256(token)
	clear(token)
	lease.Hash = hash[:]
	lease.ExpiresAt = service.now().Add(deliveryLeaseLifetime).UnixMilli()
	lease.Attempt++
	result, err := tx.ExecContext(ctx, `UPDATE notification_deliveries SET state='leased',attempt_count=?,lease_token_hash=?,lease_expires_at=?,worker_id=?,updated_at=?
		WHERE id=? AND state='pending' AND attempt_count=?`, lease.Attempt, lease.Hash, lease.ExpiresAt, input.workerID, service.now().UnixMilli(), lease.ID, lease.Attempt-1)
	if err := exactlyOne(result, err); err != nil {
		return nil, ErrQueueUnavailable
	}
	result, err = tx.ExecContext(ctx, "INSERT INTO notification_delivery_attempts(delivery_id,attempt_number,started_at,outcome) VALUES(?,?,?,'in_progress')", lease.ID, lease.Attempt, service.now().UnixMilli())
	if err := exactlyOne(result, err); err != nil {
		return nil, ErrQueueUnavailable
	}
	return commitClaim(tx, lease)
}

func commitClaim(tx *sql.Tx, lease *deliveryLease) (*deliveryLease, error) {
	if err := tx.Commit(); err != nil {
		return nil, ErrQueueUnavailable
	}
	return lease, nil
}

func claimCapacity(ctx context.Context, tx *sql.Tx) (bool, error) {
	settings, err := readSettings(ctx, tx)
	if err != nil {
		return false, err
	}
	var active int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM notification_deliveries WHERE state='leased'").Scan(&active); err != nil {
		return false, ErrQueueUnavailable
	}
	return active < settings.MaximumWorkers, nil
}

func selectDueLease(ctx context.Context, tx *sql.Tx, now int64) (*deliveryLease, error) {
	lease := new(deliveryLease)
	err := tx.QueryRowContext(ctx, `SELECT d.id,d.message_id,d.channel_id,d.channel_revision,d.attempt_count,r.channel_type,m.event_kind
		FROM notification_deliveries d JOIN notification_messages m ON m.id=d.message_id
		JOIN alert_channels c ON c.id=d.channel_id JOIN alert_channel_revisions r ON r.channel_id=d.channel_id AND r.revision=d.channel_revision
		LEFT JOIN notification_channel_health h ON h.channel_id=c.id
		WHERE d.state='pending' AND d.attempt_count<6 AND d.next_attempt_at<=? AND c.deleted_at IS NULL
		AND (c.enabled=1 OR m.event_kind='notification.test')
		AND NOT EXISTS(SELECT 1 FROM notification_deliveries active WHERE active.channel_id=d.channel_id AND active.state='leased')
		AND (m.event_kind='notification.test' OR ((h.current_revision<>d.channel_revision OR COALESCE(h.open_until,0)<=?)
		 AND NOT EXISTS(SELECT 1 FROM notification_deliveries previous WHERE previous.channel_id=d.channel_id AND previous.state IN ('pending','leased')
		 AND (previous.created_at<d.created_at OR previous.created_at=d.created_at AND previous.id<d.id))))
		ORDER BY CASE WHEN m.event_kind='notification.test' THEN 0 ELSE 1 END,d.created_at,d.id LIMIT 1`, now, now).Scan(
		&lease.ID, &lease.MessageID, &lease.ChannelID, &lease.Revision, &lease.Attempt, &lease.ChannelType, &lease.EventKind)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, ErrQueueUnavailable
	}
	return lease, nil
}

func (service *Service) recoverExpiredTx(ctx context.Context, tx *sql.Tx, access secretbox.ResourceAccess) error {
	now := service.now().UnixMilli()
	rows, err := tx.QueryContext(ctx, `SELECT d.id,d.attempt_count,d.channel_id,d.channel_revision,m.event_kind FROM notification_deliveries d
		JOIN notification_messages m ON m.id=d.message_id WHERE d.state='leased' AND d.lease_expires_at<=? ORDER BY d.lease_expires_at LIMIT 64`, now)
	if err != nil {
		return ErrQueueUnavailable
	}
	leases := []deliveryLease{}
	for rows.Next() {
		var lease deliveryLease
		if err := rows.Scan(&lease.ID, &lease.Attempt, &lease.ChannelID, &lease.Revision, &lease.EventKind); err != nil {
			_ = rows.Close()
			return ErrQueueUnavailable
		}
		leases = append(leases, lease)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return ErrQueueUnavailable
	}
	for _, lease := range leases {
		if err := expireOneLease(ctx, tx, expiredLease{lease: lease, now: now}); err != nil {
			return err
		}
		if lease.Attempt >= 6 && lease.EventKind != "notification.delivery_failed" {
			input := failureInput{access: access, completion: completion{lease: lease, result: model.NotificationSendResult{ErrorCode: "lease_expired", Stage: "recovery"}}}
			if err := service.deferCapacityFailure(ctx, tx, input); err != nil {
				return err
			}
		}
	}
	return nil
}

type expiredLease struct {
	lease deliveryLease
	now   int64
}

func expireOneLease(ctx context.Context, tx *sql.Tx, input expiredLease) error {
	lease := input.lease
	state := "pending"
	var completed any
	if lease.Attempt >= 6 {
		state = "dead"
		completed = input.now
	}
	if _, err := tx.ExecContext(ctx, `UPDATE notification_delivery_attempts SET completed_at=?,duration_ms=MAX(0,?-started_at),outcome='lease_expired',error_code='lease_expired',error_stage='recovery'
		WHERE delivery_id=? AND attempt_number=? AND outcome='in_progress'`, input.now, input.now, lease.ID, lease.Attempt); err != nil {
		return ErrQueueUnavailable
	}
	result, err := tx.ExecContext(ctx, `UPDATE notification_deliveries SET state=?,lease_token_hash=NULL,lease_expires_at=NULL,worker_id='',next_attempt_at=?,completed_at=?,updated_at=?,error_code='lease_expired',error_stage='recovery'
		WHERE id=? AND state='leased' AND lease_expires_at<=?`, state, input.now, completed, input.now, lease.ID, input.now)
	if err := exactlyOne(result, err); err != nil {
		return ErrQueueUnavailable
	}
	return nil
}
