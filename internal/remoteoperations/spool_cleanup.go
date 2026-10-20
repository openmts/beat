package remoteoperations

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/google/uuid"
)

// ReconcileSpool 只能在拥有 Server 进程租约时调用；仅处理私有目录中的 UUID 文件。
func (service *Service) ReconcileSpool(ctx context.Context) error { return service.Recover(ctx) }

func (tx transaction) expireArtifact(spool *Spool, id string) error {
	rows, err := tx.tx.QueryContext(tx.ctx, "SELECT storage_name FROM operation_artifact_chunks WHERE artifact_id=?", id)
	if err != nil {
		return ErrUnavailable
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			return ErrUnavailable
		}
		names = append(names, name)
	}
	err = errors.Join(rows.Err(), rows.Close())
	if err != nil {
		return ErrUnavailable
	}
	for _, name := range names {
		if err := spool.remove(name); err != nil {
			return ErrUnavailable
		}
	}
	if err := tx.exec("DELETE FROM operation_artifact_chunks WHERE artifact_id=?", id); err != nil {
		return err
	}
	if err := tx.exec("UPDATE operation_transfers SET artifact_id=NULL,state=CASE WHEN state IN ('corrupt','canceled') THEN state ELSE 'expired' END,updated_at=? WHERE artifact_id=?", tx.now.UnixMilli(), id); err != nil {
		return err
	}
	return tx.one("DELETE FROM operation_artifacts WHERE id=?", id)
}

func (tx transaction) reconcileSpoolFiles(spool *Spool) (resultErr error) {
	directory, err := spool.root.Open(".")
	if err != nil {
		return ErrUnavailable
	}
	defer func() { resultErr = errors.Join(resultErr, directory.Close()) }()
	for {
		entries, err := directory.ReadDir(64)
		if err != nil && !errors.Is(err, io.EOF) {
			return ErrUnavailable
		}
		for _, entry := range entries {
			if err := tx.reconcileSpoolEntry(spool, entry); err != nil {
				return err
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
	}
}

func (tx transaction) reconcileSpoolEntry(spool *Spool, entry os.DirEntry) error {
	id, err := uuid.Parse(entry.Name())
	if err != nil || id.String() != entry.Name() || entry.IsDir() {
		return ErrUnavailable
	}
	var count int
	if err := tx.tx.QueryRowContext(tx.ctx, "SELECT COUNT(*) FROM operation_artifact_chunks WHERE storage_name=?", entry.Name()).Scan(&count); err != nil {
		return ErrUnavailable
	}
	if count == 0 {
		if err := spool.remove(entry.Name()); err != nil {
			return ErrUnavailable
		}
	}
	return nil
}
