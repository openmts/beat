package remoteoperations

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

const maximumScanDuration = 2 * time.Minute

// errArtifactCorrupt 表示上传的字节与声明的摘要不符。它按语义属于客户端内容错误（4xx），
// 由 ErrCorrupt 暴露给 HTTP 层；这里保留未导出名以维持既有的 `err == errArtifactCorrupt` 比较。
var errArtifactCorrupt = errors.New("artifact integrity failure")

var errScanRejected = errors.New("artifact scanner rejected the content")

type scanAdmission struct {
	collectionRescan bool
	actor            Actor
	agent            *AttemptRequest
	transferID       string
}

type artifactScan struct {
	previousState string
	admission     scanAdmission
	item          artifact
	id            string
	existingTask  string
}

func (service *Service) finalizeTransfer(ctx context.Context, input scanAdmission) (string, error) {
	if service.spool == nil {
		return "", ErrUnavailable
	}
	scan := artifactScan{admission: input, id: uuid.NewString()}
	ctx, cancel := context.WithTimeout(ctx, maximumScanDuration)
	release, err := service.trackScan(scan.id, cancel)
	if err != nil {
		cancel()
		return "", err
	}
	defer release()
	if err := service.write(ctx, func(tx transaction) error { return tx.claimScan(&scan, service.spool) }); err != nil {
		return "", err
	}
	if scan.item.state == "ready" {
		return scan.existingTask, nil
	}
	if err := service.inspectArtifact(ctx, scan); err != nil {
		return "", errors.Join(err, service.abandonScan(context.WithoutCancel(ctx), scan, err))
	}
	var taskID string
	err = service.write(ctx, func(tx transaction) error {
		if err := tx.checkScan(scan); err != nil {
			return err
		}
		current, err := tx.artifact(scan.admission.transferID)
		if err != nil {
			return err
		}
		if current.requireScan && service.spool.scanner == nil {
			return ErrForbidden
		}
		lifetime, err := tx.artifactRetention(scan.admission.transferID)
		if err != nil {
			return err
		}
		if err := tx.one("UPDATE operation_artifacts SET state='ready',scan_status=?,scan_id='',scan_deadline=0,expires_at=?,updated_at=? WHERE id=? AND scan_id=?", service.scanStatus(), tx.now.Add(lifetime).UnixMilli(), tx.now.UnixMilli(), scan.item.id, scan.id); err != nil {
			return err
		}
		taskID = scan.existingTask
		if scan.item.direction == "deploy" && taskID == "" {
			var err error
			taskID, err = tx.createTransferTask(scan.admission.actor, scan.admission.transferID)
			if err != nil {
				return err
			}
		}
		return tx.audit(scan.admission.actor.UserID, "operation.transfer.finalized", scan.admission.transferID)
	})
	if err != nil {
		return "", errors.Join(err, service.abandonScan(context.WithoutCancel(ctx), scan, err))
	}
	return taskID, nil
}

func (tx transaction) authorizeScan(input *scanAdmission) (artifact, error) {
	if input.agent != nil {
		item, id, err := tx.attemptArtifact(*input.agent)
		if err != nil {
			return item, err
		}
		if item.direction != "collect" {
			return item, ErrForbidden
		}
		input.transferID = id
		return item, nil
	}
	if err := tx.authorize(input.actor, false, true); err != nil {
		return artifact{}, err
	}
	item, err := tx.artifact(input.transferID)
	if err != nil {
		return item, err
	}
	if item.direction == "collect" {
		if item.state == "ready" {
			input.collectionRescan = true
		} else if !input.collectionRescan || item.state != "scanning" {
			return item, ErrForbidden
		}
	} else if item.direction != "deploy" {
		return item, ErrForbidden
	}
	return item, nil
}

func (tx transaction) claimScan(scan *artifactScan, spool *Spool) error {
	item, err := tx.authorizeScan(&scan.admission)
	if err != nil {
		return err
	}
	scan.item = item
	if err := tx.tx.QueryRowContext(tx.ctx, "SELECT COALESCE(task_id,'') FROM operation_transfers WHERE id=?", scan.admission.transferID).Scan(&scan.existingTask); err != nil {
		return ErrUnavailable
	}
	if item.state == "ready" && (!item.requireScan || item.scanStatus == "clean") {
		return nil
	}
	previous := item.state
	scan.previousState = previous
	if item.state == "ready" {
		item.state = "uploading"
		scan.item.state = "scanning"
	}
	if item.state == "scanning" {
		return ErrBusy
	}
	if item.state != "uploading" {
		return ErrConflict
	}
	if item.requireScan && spool.scanner == nil {
		return ErrForbidden
	}
	var count int
	if err := tx.tx.QueryRowContext(tx.ctx, "SELECT COUNT(*) FROM operation_artifact_chunks WHERE artifact_id=?", item.id).Scan(&count); err != nil {
		return ErrUnavailable
	}
	if int64(count) != (item.size+SpoolChunkBytes-1)/SpoolChunkBytes {
		return ErrConflict
	}
	return tx.one("UPDATE operation_artifacts SET state='scanning',scan_id=?,scan_deadline=?,updated_at=? WHERE id=? AND state=?", scan.id, tx.now.Add(maximumScanDuration).UnixMilli(), tx.now.UnixMilli(), item.id, previous)
}

func (tx transaction) checkScan(scan artifactScan) error {
	item, err := tx.authorizeScan(&scan.admission)
	if err != nil {
		return err
	}
	var scanID string
	if err := tx.tx.QueryRowContext(tx.ctx, "SELECT scan_id FROM operation_artifacts WHERE id=?", item.id).Scan(&scanID); err != nil {
		return ErrUnavailable
	}
	if item.id != scan.item.id || item.state != "scanning" || scanID != scan.id {
		return ErrConflict
	}
	return nil
}

func (tx transaction) artifactRetention(id string) (time.Duration, error) {
	var policyID string
	if err := tx.tx.QueryRowContext(tx.ctx, "SELECT policy_id FROM operation_transfers WHERE id=?", id).Scan(&policyID); err != nil {
		return 0, ErrUnavailable
	}
	policy, err := tx.policy(policyID)
	if err != nil {
		return 0, err
	}
	return time.Duration(policy.RetentionLimits().ArtifactHours) * time.Hour, nil
}
