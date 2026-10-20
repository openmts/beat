package migrationstatus

import "github.com/beat/backend/internal/store"

// Observer 把启动期迁移阶段转发给标记写入器。
// 标记发布失败不能中断迁移，因此这里显式忽略写入错误。
type Observer struct{ writer *Writer }

// NewObserver 为已有写入器创建 store.MigrationObserver 适配器。
func NewObserver(writer *Writer) *Observer { return &Observer{writer: writer} }

// MigrationStarted 记录来源 schema 与二进制的目标 schema。
func (observer *Observer) MigrationStarted(source int) {
	_ = observer.writer.Started(source, store.MaximumSupportedSchemaVersion)
}

// MigrationApplied 记录已推进到的迁移号与名称。
func (observer *Observer) MigrationApplied(migration store.Migration, current int) {
	_ = observer.writer.Applied(current, migration.Name)
}

// MigrationFailed 记录固定失败代码与是否需要外部回滚。
func (observer *Observer) MigrationFailed(code string, rollbackRequired bool) {
	_ = observer.writer.Failed(code, rollbackRequired)
}

// MigrationCompleted 记录成功完成的标记。
func (observer *Observer) MigrationCompleted(int) { _ = observer.writer.Completed() }
