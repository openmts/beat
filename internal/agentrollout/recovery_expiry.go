package agentrollout

import "database/sql"

func (tx *transaction) expireAssignments() error {
	rows, err := tx.db.QueryContext(tx.ctx, "SELECT id,rollout_id FROM agent_rollout_targets WHERE state IN('offered','observing') AND lease_expires_at<=? LIMIT ?", tx.at, recoveryBatchSize)
	if err != nil {
		return ErrUnavailable
	}
	targets, err := readExpiredTargets(rows)
	if err != nil {
		return err
	}
	for _, target := range targets {
		if err := tx.exec("UPDATE agent_rollout_attempts SET state='unknown',ended_at=? WHERE id=(SELECT current_attempt_id FROM agent_rollout_targets WHERE id=?)", tx.at, target.id); err != nil {
			return err
		}
		if err := tx.exec("UPDATE agent_rollout_targets SET state='held',reason='assignment_unknown',nonce_hash=NULL,lease_expires_at=NULL,healthy_since=NULL,updated_at=? WHERE id=?", tx.at, target.id); err != nil {
			return err
		}
		if err := tx.targetEvent(target.id, "assignment.unknown", "lease_expired"); err != nil {
			return err
		}
		if err := tx.pauseAutomatically(target.rollout, "assignment_unknown"); err != nil {
			return err
		}
	}
	return nil
}

func (tx *transaction) expireReleases() error {
	rows, err := tx.db.QueryContext(tx.ctx, `SELECT o.id,CASE WHEN r.revoked_at IS NOT NULL THEN 'release_revoked' ELSE 'release_expired' END FROM agent_rollouts o JOIN agent_releases r ON r.id=o.release_id WHERE o.state IN('active','paused') AND (r.expires_at<=? OR r.revoked_at IS NOT NULL) AND o.reason NOT IN('release_revoked','release_expired','signing_key_revoked') LIMIT ?`, tx.at, recoveryBatchSize)
	if err != nil {
		return ErrUnavailable
	}
	type item struct{ id, reason string }
	items := []item{}
	for rows.Next() {
		var value item
		if err := rows.Scan(&value.id, &value.reason); err != nil {
			_ = rows.Close()
			return ErrUnavailable
		}
		items = append(items, value)
	}
	scanErr, closeErr := rows.Err(), rows.Close()
	if scanErr != nil || closeErr != nil {
		return ErrUnavailable
	}
	for _, value := range items {
		if err := tx.pauseAutomatically(value.id, value.reason); err != nil {
			return err
		}
	}
	return nil
}

func (tx *transaction) pauseAutomatically(id, reason string) error {
	result, err := tx.db.ExecContext(tx.ctx, "UPDATE agent_rollouts SET state='paused',reason=?,updated_at=? WHERE id=? AND state NOT IN('completed','aborted') AND reason<>?", reason, tx.at, id, reason)
	if err != nil {
		return ErrUnavailable
	}
	count, err := result.RowsAffected()
	if err != nil {
		return ErrUnavailable
	}
	if count == 0 {
		return nil
	}
	return tx.event(audit{Action: "rollout.paused.automatic", Kind: "rollout", ID: id, Reason: reason})
}

type expiredTarget struct{ id, rollout string }

func readExpiredTargets(rows *sql.Rows) ([]expiredTarget, error) {

	targets := []expiredTarget{}
	for rows.Next() {
		var target expiredTarget
		if err := rows.Scan(&target.id, &target.rollout); err != nil {
			_ = rows.Close()
			return nil, ErrUnavailable
		}
		targets = append(targets, target)
	}
	scanErr, closeErr := rows.Err(), rows.Close()
	if scanErr != nil || closeErr != nil {
		return nil, ErrUnavailable
	}

	return targets, nil
}
