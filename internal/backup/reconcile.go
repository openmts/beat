package backup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

// BackupTerminalRetention 是 failed/interrupted/missing 记录的固定保留窗口。
const BackupTerminalRetention = 90 * 24 * time.Hour

// ReconcileOutcome 汇总一次启动对账，供结构化日志与 Prometheus 脱敏输出。
type ReconcileOutcome struct {
	Recovered       int `json:"recovered"`
	Interrupted     int `json:"interrupted"`
	Missing         int `json:"missing"`
	RolledBack      int `json:"rolled_back"`
	Deleted         int `json:"deleted"`
	Expired         int `json:"expired"`
	Quarantined     int `json:"quarantined"`
	PartialRemoved  int `json:"partial_removed"`
	JournalsRemoved int `json:"journals_removed"`
	Deferred        int `json:"deferred"`
}

// Changed 判断本次对账是否改动了任何记录、归档或 journal。
func (outcome ReconcileOutcome) Changed() bool {
	return outcome.total() > 0
}

func (outcome ReconcileOutcome) total() int {
	return outcome.Recovered + outcome.Interrupted + outcome.Missing + outcome.RolledBack +
		outcome.Deleted + outcome.Expired + outcome.Quarantined + outcome.PartialRemoved +
		outcome.JournalsRemoved + outcome.Deferred
}

type reconciler struct {
	service     *Service
	ctx         context.Context
	outcome     ReconcileOutcome
	journal     model.RestoreJournal
	hasJournal  bool
	journalKept bool
}

// Reconcile 在 HTTP 之前对齐 SQLite 记录、私有归档与恢复 journal。
func (service *Service) Reconcile(ctx context.Context) (ReconcileOutcome, error) {
	reconciler := &reconciler{service: service, ctx: ctx}
	if err := reconciler.loadJournal(); err != nil {
		return reconciler.outcome, err
	}
	records, err := service.listAllRecords(ctx)
	if err != nil {
		return reconciler.outcome, err
	}
	for _, record := range records {
		if err := reconciler.reconcileRecord(record); err != nil {
			return reconciler.outcome, err
		}
	}
	if err := reconciler.resolveJournal(); err != nil {
		return reconciler.outcome, err
	}
	if err := reconciler.sweepFilesystem(records); err != nil {
		return reconciler.outcome, err
	}
	if err := reconciler.expireTerminalRecords(); err != nil {
		return reconciler.outcome, err
	}
	reconciler.log()
	return reconciler.outcome, nil
}

// listAllRecords 以固定页大小走完备份历史；游标基于不可变的 (created_at,id)。
func (service *Service) listAllRecords(ctx context.Context) ([]model.BackupRecord, error) {
	records := []model.BackupRecord{}
	cursor := ""
	for {
		page, err := service.records.ListBackupsPage(ctx, store.BackupQuery{Cursor: cursor,
			Limit: store.BackupMaximumPage})
		if err != nil {
			return nil, err
		}
		records = append(records, page.Items...)
		if page.NextCursor == "" {
			return records, nil
		}
		cursor = page.NextCursor
	}
}

func (reconciler *reconciler) reconcileRecord(record model.BackupRecord) error {
	switch record.State {
	case model.BackupStateRunning:
		return reconciler.reconcileRunning(record)
	case model.BackupStateDeleting:
		return reconciler.reconcileDeleting(record)
	case model.BackupStateStaging, model.BackupStateStaged:
		return reconciler.reconcileStaged(record)
	case model.BackupStateReady, model.BackupStateValidated:
		return reconciler.reconcileArchivePresence(record)
	default:
		return nil
	}
}

// reconcileRunning 让被重启打断的备份落到 interrupted，或在归档完整时直接接管为 ready。
func (reconciler *reconciler) reconcileRunning(record model.BackupRecord) error {
	next := record
	next.State = model.BackupStateInterrupted
	next.PreviousState = record.State
	next.UpdatedAt = reconciler.service.now()
	next.ErrorMessage = "backup interrupted by restart"
	action := "backup.reconcile.interrupted"
	if reconciler.archiveIsComplete(record.Filename) {
		completed := reconciler.service.now()
		next.State = model.BackupStateReady
		next.CompletedAt = &completed
		next.ErrorMessage = ""
		action = "backup.reconcile.recovered"
	}
	if err := reconciler.transition(next, []string{model.BackupStateRunning}, action); err != nil {
		return err
	}
	if next.State == model.BackupStateReady {
		reconciler.outcome.Recovered++
		return nil
	}
	reconciler.outcome.Interrupted++
	return nil
}

// reconcileDeleting 幂等完成删除：归档缺失视为成功，无法安全删除时保留可重试状态。
func (reconciler *reconciler) reconcileDeleting(record model.BackupRecord) error {
	if err := reconciler.service.removeArchive(record.Filename); err != nil {
		slog.WarnContext(reconciler.ctx, "backup deletion is deferred",
			"backup_id", record.ID, "error", err)
		reconciler.outcome.Deferred++
		return nil
	}
	if err := reconciler.service.records.Delete(reconciler.ctx, record.ID); err != nil {
		return err
	}
	reconciler.outcome.Deleted++
	return reconciler.service.audit(reconciler.ctx, reconciler.systemEvent(
		"backup.reconcile.deleted", record.ID, model.BackupStateDeleting, ""))
}

// reconcileStaged 只保留“staged 行 + 同代次 journal + 归档仍在”的待重启恢复。
func (reconciler *reconciler) reconcileStaged(record model.BackupRecord) error {
	if reconciler.stagedRestoreConfirmed(record) {
		reconciler.journalKept = true
		return nil
	}
	next := record
	next.State = previousRestoreState(record.PreviousState)
	next.PreviousState = ""
	next.RestoreGeneration = ""
	next.UpdatedAt = reconciler.service.now()
	if err := reconciler.transition(next, []string{record.State}, "backup.restore.rollback"); err != nil {
		return err
	}
	reconciler.outcome.RolledBack++
	return nil
}

// reconcileArchivePresence 把 ready/validated 但没有普通归档的记录标为 missing。
func (reconciler *reconciler) reconcileArchivePresence(record model.BackupRecord) error {
	present, err := reconciler.archiveIsRegular(record.Filename)
	if err != nil || present {
		return err
	}
	next := record
	next.State = model.BackupStateMissing
	next.PreviousState = record.State
	next.UpdatedAt = reconciler.service.now()
	if err := reconciler.transition(next, []string{record.State}, "backup.reconcile.missing"); err != nil {
		return err
	}
	reconciler.outcome.Missing++
	return nil
}

func (reconciler *reconciler) stagedRestoreConfirmed(record model.BackupRecord) bool {
	if record.State != model.BackupStateStaged || !reconciler.hasJournal {
		return false
	}
	journal := reconciler.journal
	if journal.State != "pending" || journal.Origin == model.RestoreOriginOffline {
		return false
	}
	if journal.BackupID != record.ID || journal.Generation == "" ||
		journal.Generation != record.RestoreGeneration {
		return false
	}
	present, err := reconciler.archiveIsRegular(record.Filename)
	return err == nil && present
}

func (reconciler *reconciler) loadJournal() error {
	journal, err := readJournal(reconciler.service.journalPath())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read startup restore journal: %w", err)
	}
	reconciler.journal = journal
	reconciler.hasJournal = true
	return nil
}

// resolveJournal 丢弃未被 staged 记录确认的在线 pending journal，绝不静默应用。
// offline journal 由本机运维命令拥有，保持原样等待下次启动应用。
func (reconciler *reconciler) resolveJournal() error {
	if !reconciler.hasJournal || reconciler.journalKept || reconciler.journal.State != "pending" ||
		reconciler.journal.Origin == model.RestoreOriginOffline {
		return nil
	}
	if err := os.Remove(reconciler.service.journalPath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove unconfirmed restore journal: %w", err)
	}
	reconciler.outcome.JournalsRemoved++
	return reconciler.service.audit(reconciler.ctx, reconciler.systemEvent(
		"backup.reconcile.journal_discarded", reconciler.journal.BackupID,
		model.BackupStateStaging, reconciler.journal.Generation))
}

func (reconciler *reconciler) expireTerminalRecords() error {
	cutoff := reconciler.service.now().Add(-BackupTerminalRetention)
	expired, err := reconciler.service.records.ExpireTerminal(reconciler.ctx,
		model.BackupTerminalStates, cutoff)
	if err != nil {
		return err
	}
	if expired == 0 {
		return nil
	}
	reconciler.outcome.Expired = int(expired)
	return reconciler.service.audit(reconciler.ctx, reconciler.systemEvent(
		"backup.reconcile.expired", "", model.BackupStateFailed, ""))
}

func (reconciler *reconciler) transition(
	next model.BackupRecord,
	expected []string,
	action string,
) error {
	reconciledAt := reconciler.service.now()
	next.ReconciledAt = &reconciledAt
	err := reconciler.service.transition(reconciler.ctx, transitionRequest{record: next,
		expected: expected, actor: SystemActor(), action: action})
	if errors.Is(err, store.ErrBackupStateConflict) {
		// 并发变更已经推进了状态，对账记录为延后而不是失败。
		reconciler.outcome.Deferred++
		return nil
	}
	return err
}

func (reconciler *reconciler) archiveIsRegular(filename string) (bool, error) {
	path, err := reconciler.service.archivePath(filename)
	if err != nil {
		return false, err
	}
	return regularArchivePresent(path)
}

func (reconciler *reconciler) archiveIsComplete(filename string) bool {
	path, err := reconciler.service.archivePath(filename)
	if err != nil {
		return false
	}
	validated, err := validateArchive(reconciler.ctx, path, reconciler.service.root)
	if err != nil {
		return false
	}
	if err := os.RemoveAll(validated.root); err != nil {
		return false
	}
	return true
}

func (reconciler *reconciler) systemEvent(action, backupID, from, generation string) *model.AdminAuditEvent {
	actor := SystemActor()
	return &model.AdminAuditEvent{
		ID: uuid.NewString(), ActorUsername: actor.Username, ActorType: actor.Type,
		Action: action, ResourceType: "backup", ResourceID: backupID,
		Outcome:    model.AuditOutcomeSuccess,
		DetailJSON: backupTransitionDetail(from, "", generation),
		CreatedAt:  reconciler.service.now()}
}

func (reconciler *reconciler) log() {
	if !reconciler.outcome.Changed() {
		return
	}
	slog.InfoContext(reconciler.ctx, "backup reconciliation completed",
		"recovered", reconciler.outcome.Recovered,
		"interrupted", reconciler.outcome.Interrupted,
		"missing", reconciler.outcome.Missing,
		"rolled_back", reconciler.outcome.RolledBack,
		"deleted", reconciler.outcome.Deleted,
		"expired", reconciler.outcome.Expired,
		"quarantined", reconciler.outcome.Quarantined,
		"partial_removed", reconciler.outcome.PartialRemoved,
		"journals_removed", reconciler.outcome.JournalsRemoved,
		"deferred", reconciler.outcome.Deferred)
}
