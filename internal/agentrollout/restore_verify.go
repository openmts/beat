package agentrollout

// verifyRestoredState 不把 SQL 未报错视为校正完成，拒绝触发器忽略或回写旧凭据。
func (tx *transaction) verifyRestoredState() error {
	queries := []string{
		"SELECT COUNT(*) FROM agent_releases WHERE verified<>0",
		"SELECT COUNT(*) FROM agent_release_artifacts WHERE state<>'missing'",
		"SELECT COUNT(*) FROM agent_rollout_targets WHERE nonce_hash IS NOT NULL OR lease_expires_at IS NOT NULL",
		"SELECT COUNT(*) FROM agent_rollout_attempts WHERE length(nonce_hash)<>0 OR state IN('offered','observing')",
		"SELECT COUNT(*) FROM agent_rollouts WHERE state NOT IN('completed','aborted') AND (state<>'paused' OR reason<>'restored')",
		`SELECT COUNT(*) FROM agent_rollout_targets WHERE rollout_id IN(SELECT id FROM agent_rollouts WHERE state='paused' AND reason='restored')
 AND wave>=(SELECT current_wave FROM agent_rollouts WHERE id=rollout_id) AND (state<>'held' OR reason<>'restored' OR healthy_since IS NOT NULL)`,
	}
	for _, query := range queries {
		var invalid int
		if err := tx.db.QueryRowContext(tx.ctx, query).Scan(&invalid); err != nil || invalid != 0 {
			return ErrUnavailable
		}
	}
	return nil
}
