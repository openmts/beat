package remoteoperations

import (
	"context"
	"database/sql"
	"errors"
)

// DiscardTransferDraft 删除一个还没被任何 Agent 任务接手的上传草稿。
// 草稿（只上传了分块、从未 finalize）在建传输时就把目标行写成 queued，于是会一直钉住
// `DELETE /nodes/{id}`（409 node has active remote operations），直到 6 小时的产物过期清扫。
// 这里让运维按需清掉草稿：先校验引用（在途任务或正在扫描的产物）并按冲突拒绝，再删除
// 目标行、产物行与私有的分块文件，最后写一条 `operation.transfer.discarded` 审计。
func (service *Service) DiscardTransferDraft(ctx context.Context, actor Actor, id string) error {
	if service.spool == nil {
		return ErrUnavailable
	}
	spool := service.spool
	return service.write(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, true, true); err != nil {
			return err
		}
		return tx.discardTransferDraft(actor, id, spool)
	})
}

func (tx transaction) discardTransferDraft(actor Actor, id string, spool *Spool) error {
	var taskID, artifactID sql.NullString
	var state string
	err := tx.tx.QueryRowContext(tx.ctx, "SELECT task_id,artifact_id,state FROM operation_transfers WHERE id=?", id).
		Scan(&taskID, &artifactID, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return ErrUnavailable
	}
	if taskID.Valid && taskID.String != "" {
		// 已经有 Agent 任务接手：删掉它等于丢掉在途工作的记账，按冲突拒绝。
		return ErrConflict
	}
	if state != "pending" && state != "uploading" {
		// 终态记录（corrupt/failed/canceled/expired）是审计与排障证据，只能显式删除，
		// 不能借草稿通道消失；它们也不再钉住节点。
		return ErrConflict
	}
	if artifactID.Valid && artifactID.String != "" {
		if err := tx.discardDraftArtifact(artifactID.String, id, spool); err != nil {
			return err
		}
	}
	if err := tx.exec("DELETE FROM operation_transfer_targets WHERE transfer_id=?", id); err != nil {
		return err
	}
	if err := tx.one("DELETE FROM operation_transfers WHERE id=?", id); err != nil {
		return err
	}
	return tx.audit(actor.UserID, "operation.transfer.discarded", id)
}

// discardDraftArtifact 清掉草稿私有的分块文件与产物行，只在没有别人引用它时执行。
func (tx transaction) discardDraftArtifact(artifactID, transferID string, spool *Spool) error {
	var scanID string
	var referenced int
	err := tx.tx.QueryRowContext(tx.ctx, `SELECT COALESCE(a.scan_id,''),
 (SELECT COUNT(*) FROM operation_transfers WHERE artifact_id=? AND id<>?) FROM operation_artifacts a WHERE a.id=?`,
		artifactID, transferID, artifactID).Scan(&scanID, &referenced)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return ErrUnavailable
	}
	if scanID != "" || referenced > 0 {
		// 扫描器正在读写，或另一个传输还引用同一份产物：不能在这里删除。
		return ErrConflict
	}
	// 先摘掉产物引用再删产物行：外键要求删除时没有别的行指向它（与 expireArtifact 同一顺序）。
	if err := tx.exec("UPDATE operation_transfers SET artifact_id=NULL,updated_at=? WHERE id=? AND artifact_id=?",
		tx.now.UnixMilli(), transferID, artifactID); err != nil {
		return err
	}
	names, err := tx.draftChunkNames(artifactID)
	if err != nil {
		return err
	}
	for _, name := range names {
		if err := spool.remove(name); err != nil {
			return ErrUnavailable
		}
	}
	if err := tx.exec("DELETE FROM operation_artifact_chunks WHERE artifact_id=?", artifactID); err != nil {
		return err
	}
	return tx.one("DELETE FROM operation_artifacts WHERE id=?", artifactID)
}

func (tx transaction) draftChunkNames(artifactID string) ([]string, error) {
	rows, err := tx.tx.QueryContext(tx.ctx, "SELECT storage_name FROM operation_artifact_chunks WHERE artifact_id=?", artifactID)
	if err != nil {
		return nil, ErrUnavailable
	}
	names := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			return nil, ErrUnavailable
		}
		names = append(names, name)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, ErrUnavailable
	}
	return names, nil
}
