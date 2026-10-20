package agentrollout

import (
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
)

// 演练与失败发布会长期占据列表，运维此前只能 revoke。删除路径的两条硬规则：
// 发布必须先 revoke 且不再被任何 rollout 引用；rollout 必须已经进入终态，且没有在途分配
// （`offered`/`observing` 的租约仍在 Agent 手上）。产物按 SHA-256 去重共享，只在没有任何
// 发布再引用该摘要时才删除文件。显式删除与保留期自动清理共用同一组事务内助手。

// DeleteRollout 删除终态 rollout 及其目标、尝试与事件记录；在途分配存在时拒绝。
func (s *Service) DeleteRollout(ctx context.Context, actor Actor, id string) error {
	if !deletableIdentifier(id) {
		return ErrInvalid
	}
	return s.mutate(ctx, actor, func(tx *transaction) error {
		targets, err := tx.deleteRollout(id)
		if err != nil {
			return err
		}
		return tx.audit(audit{Action: "rollout.delete", Kind: "rollout", ID: id, Count: targets})
	})
}

// deleteRollout 在事务内删除终态 rollout 及其从属记录，返回被删除的目标行数。
func (tx *transaction) deleteRollout(id string) (int, error) {
	rollout, err := tx.rollout(id)
	if err != nil {
		return 0, err
	}
	if !terminalRolloutState(rollout.State) {
		return 0, ErrConflict
	}
	inflight, err := tx.countRolloutTargets(id, "state IN('offered','observing')")
	if err != nil {
		return 0, err
	}
	if inflight > 0 {
		return 0, ErrConflict
	}
	targets, err := tx.countRolloutTargets(id, "1=1")
	if err != nil {
		return 0, err
	}
	for _, statement := range []string{
		"DELETE FROM agent_rollout_events WHERE rollout_id=?",
		"DELETE FROM agent_rollout_attempts WHERE target_id IN (SELECT id FROM agent_rollout_targets WHERE rollout_id=?)",
		"DELETE FROM agent_rollout_targets WHERE rollout_id=?",
	} {
		if err := tx.exec(statement, id); err != nil {
			return 0, err
		}
	}
	result, err := tx.db.ExecContext(tx.ctx, "DELETE FROM agent_rollouts WHERE id=?", id)
	if err := requireOne(result, err); err != nil {
		return 0, err
	}
	return targets, nil
}

// DeleteRelease 删除已 revoke 且没有任何 rollout 引用的发布；产物文件只在摘要不再被
// 其它发布引用时从磁盘删除。
func (s *Service) DeleteRelease(ctx context.Context, actor Actor, id string) error {
	if !deletableIdentifier(id) {
		return ErrInvalid
	}
	deletion := releaseDeletion{}
	err := s.mutate(ctx, actor, func(tx *transaction) error {
		var err error
		if deletion, err = tx.deleteRelease(id); err != nil {
			return err
		}
		return tx.audit(audit{Action: "release.delete", Kind: "release", ID: id, Count: deletion.artifacts})
	})
	if err != nil {
		return err
	}
	s.removeArtifacts(deletion.orphans)
	return nil
}

// releaseDeletion 记录一次发布删除的结果：产物行数与失去全部引用的产物摘要。
type releaseDeletion struct {
	artifacts int
	orphans   []string
}

// deleteRelease 在事务内删除发布与产物行，并算出不再被任何发布引用的摘要。
func (tx *transaction) deleteRelease(id string) (releaseDeletion, error) {
	stored, err := tx.loadRelease(id)
	if err != nil {
		return releaseDeletion{}, err
	}
	if !stored.revoked.Valid {
		return releaseDeletion{}, ErrConflict
	}
	rollouts, err := tx.countReleaseRollouts(id)
	if err != nil {
		return releaseDeletion{}, err
	}
	if rollouts > 0 {
		return releaseDeletion{}, ErrConflict
	}
	artifacts, err := tx.countReleaseArtifacts(id)
	if err != nil {
		return releaseDeletion{}, err
	}
	digests, err := tx.releaseArtifactDigests(id)
	if err != nil {
		return releaseDeletion{}, err
	}
	if err := tx.exec("DELETE FROM agent_release_artifacts WHERE release_id=?", id); err != nil {
		return releaseDeletion{}, err
	}
	result, err := tx.db.ExecContext(tx.ctx, "DELETE FROM agent_releases WHERE id=?", id)
	if err := requireOne(result, err); err != nil {
		return releaseDeletion{}, err
	}
	orphans, err := tx.unreferencedArtifactDigests(digests)
	if err != nil {
		return releaseDeletion{}, err
	}
	return releaseDeletion{artifacts: artifacts, orphans: orphans}, nil
}

// removeArtifacts 删除失去全部引用的产物文件；删除失败只留下惰性文件，不影响已提交的事务。
func (s *Service) removeArtifacts(digests []string) {
	for _, digest := range digests {
		_ = os.Remove(filepath.Join(s.directory, digest+artifactFileSuffix))
	}
}

func (tx *transaction) countRolloutTargets(rolloutID, condition string) (int, error) {
	var count int
	err := tx.db.QueryRowContext(tx.ctx, "SELECT COUNT(*) FROM agent_rollout_targets WHERE rollout_id=? AND "+condition, rolloutID).Scan(&count)
	if err != nil {
		return 0, ErrUnavailable
	}
	return count, nil
}

func (tx *transaction) countReleaseRollouts(releaseID string) (int, error) {
	var count int
	err := tx.db.QueryRowContext(tx.ctx, "SELECT COUNT(*) FROM agent_rollouts WHERE release_id=?", releaseID).Scan(&count)
	if err != nil {
		return 0, ErrUnavailable
	}
	return count, nil
}

func (tx *transaction) countReleaseArtifacts(releaseID string) (int, error) {
	var count int
	err := tx.db.QueryRowContext(tx.ctx, "SELECT COUNT(*) FROM agent_release_artifacts WHERE release_id=?", releaseID).Scan(&count)
	if err != nil {
		return 0, ErrUnavailable
	}
	return count, nil
}

// releaseArtifactDigests 读取待删除发布的产物摘要；必须在删除产物行之前调用。
func (tx *transaction) releaseArtifactDigests(releaseID string) ([]string, error) {
	rows, err := tx.db.QueryContext(tx.ctx, "SELECT DISTINCT digest FROM agent_release_artifacts WHERE release_id=?", releaseID)
	if err != nil {
		return nil, ErrUnavailable
	}
	return scanIdentifierColumn(rows)
}

// unreferencedArtifactDigests 返回不再被任何发布引用的摘要；调用时该发布的产物行必须
// 已经删除，避免把自己算成引用者。
func (tx *transaction) unreferencedArtifactDigests(digests []string) ([]string, error) {
	orphans := []string{}
	for _, digest := range digests {
		if !validArtifactDigest(digest) {
			continue
		}
		var references int
		if err := tx.db.QueryRowContext(tx.ctx, "SELECT COUNT(*) FROM agent_release_artifacts WHERE digest=?", digest).Scan(&references); err != nil {
			return nil, ErrUnavailable
		}
		if references == 0 {
			orphans = append(orphans, digest)
		}
	}
	return orphans, nil
}

type textRows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close() error
}

// scanIdentifierColumn 读取单列文本结果集，返回所有行。
func scanIdentifierColumn(rows textRows) ([]string, error) {
	defer func() { _ = rows.Close() }()
	values := []string{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, ErrUnavailable
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, ErrUnavailable
	}
	return values, nil
}

func terminalRolloutState(state string) bool {
	return state == "completed" || state == "aborted"
}

func deletableIdentifier(value string) bool {
	return value != "" && len(value) <= maximumIdentifierBytes
}

func validArtifactDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
