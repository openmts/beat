package agentrollout

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"time"
)

// Recover 仅停止不再安全的新分配；不会自动扩大wave或重发未知更新。
func (s *Service) Recover(ctx context.Context) error {
	if !s.enabled {
		return nil
	}
	db, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ErrUnavailable
	}
	defer func() { _ = db.Rollback() }()
	tx := &transaction{ctx: ctx, db: db, at: s.now(), username: "system"}
	if err := tx.expireAssignments(); err != nil {
		return err
	}
	if err := tx.expireReleases(); err != nil {
		return err
	}
	if err := db.Commit(); err != nil {
		return ErrUnavailable
	}
	return nil
}

// ReconcileRestore 仅修改尚未激活的私有恢复数据库；现有服务器不会被访问。
func ReconcileRestore(ctx context.Context, path string, now time.Time) (resultErr error) {
	uri := (&url.URL{Scheme: "file", Path: path, RawQuery: "mode=rw&_txlock=immediate&_pragma=foreign_keys(1)"}).String()
	database, err := sql.Open("sqlite", uri)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, database.Close()) }()
	present, err := restoredSchemaPresent(ctx, database)
	if err != nil {
		return err
	}
	if !present {
		return nil
	}
	db, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = db.Rollback() }()
	tx := &transaction{ctx: ctx, db: db, at: now, username: "system"}
	if err := tx.restorePause(); err != nil {
		return err
	}
	return db.Commit()
}

func (tx *transaction) restorePause() error {
	if err := tx.exec("UPDATE agent_rollouts SET state='paused',reason='restored',updated_at=? WHERE state NOT IN('completed','aborted')", tx.at); err != nil {
		return err
	}
	if err := tx.exec(`UPDATE agent_rollout_attempts SET state='unknown',ended_at=? WHERE state IN('offered','observing')`, tx.at); err != nil {
		return err
	}
	if err := tx.exec(`UPDATE agent_rollout_targets SET state='held',reason='restored',healthy_since=NULL,updated_at=? WHERE rollout_id IN(SELECT id FROM agent_rollouts WHERE state='paused' AND reason='restored') AND wave>=(SELECT current_wave FROM agent_rollouts WHERE id=rollout_id)`, tx.at); err != nil {
		return err
	}
	if err := tx.exec("UPDATE agent_rollout_targets SET nonce_hash=NULL,lease_expires_at=NULL"); err != nil {
		return err
	}
	if err := tx.exec("UPDATE agent_rollout_attempts SET nonce_hash=X''"); err != nil {
		return err
	}
	if err := tx.exec("UPDATE agent_releases SET verified=0"); err != nil {
		return err
	}
	if err := tx.exec("UPDATE agent_release_artifacts SET state='missing',updated_at=?", tx.at); err != nil {
		return err
	}
	if err := tx.audit(audit{Action: "rollout.restore.paused", Kind: "rollout", ID: "restore", Reason: "verified_reimport_required"}); err != nil {
		return err
	}
	return tx.verifyRestoredState()
}
