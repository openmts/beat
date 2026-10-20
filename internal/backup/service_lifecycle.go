package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

// Actor 标识一次备份状态变更的来源，用于审计归因；对账使用固定系统主体。
type Actor struct {
	ID        string
	Username  string
	Type      string
	RequestID string
}

// SystemActor 是启动对账与离线运维使用的固定主体，绝不冒充浏览器管理员。
func SystemActor() Actor {
	return Actor{Username: "system", Type: model.AuditActorService}
}

// transitionRequest 汇总一次带审计的状态推进，避免超长参数列表。
type transitionRequest struct {
	record   model.BackupRecord
	expected []string
	actor    Actor
	action   string
}

// Delete 先提交 deleting，再删除归档，最后删除记录；任何一步失败都留下可重试状态。
func (service *Service) Delete(ctx context.Context, id string, actor Actor) error {
	record, err := service.record(ctx, id)
	if err != nil {
		return err
	}
	switch record.State {
	case model.BackupStateRunning, model.BackupStateStaging, model.BackupStateStaged:
		return errors.New("backup is not deletable in its current state")
	}
	if record.State != model.BackupStateDeleting {
		deleting := *record
		deleting.PreviousState = record.State
		deleting.State = model.BackupStateDeleting
		deleting.UpdatedAt = service.now()
		if err := service.transition(ctx, transitionRequest{record: deleting,
			expected: []string{record.State}, actor: actor, action: "backup.delete.started"}); err != nil {
			return err
		}
	}
	if err := service.removeArchive(record.Filename); err != nil {
		return err
	}
	if err := service.records.Delete(ctx, id); err != nil {
		return err
	}
	return service.audit(ctx, &model.AdminAuditEvent{
		ID: uuid.NewString(), RequestID: actor.RequestID, ActorID: actor.ID,
		ActorUsername: actor.Username, ActorType: actor.Type, Action: "backup.delete.completed",
		ResourceType: "backup", ResourceID: id, Outcome: model.AuditOutcomeSuccess,
		DetailJSON: backupTransitionDetail(record.State, model.BackupStateDeleting, ""),
		CreatedAt:  service.now()})
}

// transition 在一个事务内以 CAS 推进记录并写入审计；并发改写时返回状态冲突。
func (service *Service) transition(ctx context.Context, request transitionRequest) error {
	return service.records.Transition(ctx, store.BackupTransition{
		Record: request.record, Expected: request.expected,
		Audit: model.AdminAuditEvent{
			ID: uuid.NewString(), RequestID: request.actor.RequestID, ActorID: request.actor.ID,
			ActorUsername: request.actor.Username, ActorType: request.actor.Type, Action: request.action,
			ResourceType: "backup", ResourceID: request.record.ID, Outcome: model.AuditOutcomeSuccess,
			DetailJSON: backupTransitionDetail(firstExpectedState(request.expected),
				request.record.State, request.record.RestoreGeneration),
			CreatedAt: service.now()}})
}

func (service *Service) audit(ctx context.Context, event *model.AdminAuditEvent) error {
	return service.records.Audit(ctx, event)
}

// firstExpectedState 让审计明细在调用方漏填期望状态时仍然安全。
func firstExpectedState(expected []string) string {
	if len(expected) == 0 {
		return ""
	}
	return expected[0]
}

// rejectConflictingJournal 拒绝在同一数据目录上暂存第二个恢复，
// 避免覆盖另一个待应用的私有 journal。
func (service *Service) rejectConflictingJournal(id string) error {
	journal, err := readJournal(service.journalPath())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read pending restore before staging: %w", err)
	}
	if journal.State == "pending" && journal.BackupID != id {
		return ErrRestoreStaged
	}
	return nil
}

// abortStaging 回滚未完成的暂存：清掉本记录的 journal 并把行退回暂存前状态。
func (service *Service) abortStaging(ctx context.Context, id string, actor Actor) error {
	record, err := service.records.Get(ctx, id)
	if err != nil || record == nil {
		return err
	}
	if err := service.clearJournal(id); err != nil {
		return err
	}
	if record.State != model.BackupStateStaging && record.State != model.BackupStateStaged {
		return nil
	}
	rollback := *record
	rollback.State = previousRestoreState(record.PreviousState)
	rollback.PreviousState = ""
	rollback.RestoreGeneration = ""
	rollback.UpdatedAt = service.now()
	return service.transition(ctx, transitionRequest{record: rollback,
		expected: []string{record.State}, actor: actor, action: "backup.restore.rollback"})
}

// previousRestoreState 只接受 ready/validated 作为回滚目标，其余一律退回 validated。
func previousRestoreState(state string) string {
	if state == model.BackupStateReady || state == model.BackupStateValidated {
		return state
	}
	return model.BackupStateValidated
}

// journalPath 定位与备份目录同级的私有恢复 journal。
func (service *Service) journalPath() string {
	return filepath.Join(filepath.Dir(service.root), "restore.pending.json")
}

// clearJournal 只清理属于该备份的 journal，其他待处理恢复保持原样。
func (service *Service) clearJournal(id string) error {
	path := service.journalPath()
	journal, err := readJournal(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read pending restore before rollback: %w", err)
	}
	if journal.BackupID != id {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove pending restore: %w", err)
	}
	return nil
}

// regularArchivePresent 只在路径存在且确实是普通文件时返回 true；符号链接与目录不算归档。
func regularArchivePresent(path string) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect backup archive: %w", err)
	}
	return info.Mode().IsRegular(), nil
}

// removeArchive 只删除备份目录下的普通归档文件；缺失按幂等成功处理。
func (service *Service) removeArchive(filename string) error {
	path, err := service.archivePath(filename)
	if err != nil {
		return err
	}
	return removeArchiveIfPresent(path)
}

// removeArchiveIfPresent 删除普通文件；符号链接与目录一律拒绝，绝不跟随。
func removeArchiveIfPresent(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect backup archive: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("backup archive is not a regular file")
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove backup archive: %w", err)
	}
	return nil
}

func backupTransitionDetail(from, to, generation string) string {
	detail := struct {
		From       string `json:"from"`
		To         string `json:"to"`
		Generation string `json:"generation,omitempty"`
	}{From: from, To: to, Generation: generation}
	encoded, err := json.Marshal(detail)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}
