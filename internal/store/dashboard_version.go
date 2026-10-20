package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/beat/backend/internal/model"
)

// DashboardVersionRestore 描述一次「恢复到指定历史版本」的请求。
// Revision 是目标历史版本号，ExpectedRevision 是客户端看到的当前版本号（乐观并发）。
type DashboardVersionRestore struct {
	ID               string
	Revision         int
	ExpectedRevision int
	Summary          string
	Actor            DashboardActor
}

// dashboardVersionColumns 把创建者解析成用户名：版本列表是给人看的，不暴露内部用户 UUID；
// 历史回填或系统动作拿不到管理员时退回原始标识（例如 'system'）。
const dashboardVersionColumns = `v.dashboard_id, v.revision, v.title, v.summary, v.created_at,
	COALESCE(u.username, v.created_by)`

// ListVersions 返回看板的历史版本元数据，按版本号从新到旧；定义体不在这里返回。
func (store *DashboardStore) ListVersions(ctx context.Context, id string) ([]model.MetricDashboardVersion, error) {
	rows, err := store.db.QueryContext(ctx, "SELECT "+dashboardVersionColumns+` FROM dashboard_versions v
		LEFT JOIN admin_users u ON u.id = v.created_by
		WHERE v.dashboard_id=? ORDER BY v.revision DESC LIMIT ?`, id, model.MaxDashboardVersions)
	if err != nil {
		return nil, fmt.Errorf("list dashboard versions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	values := []model.MetricDashboardVersion{}
	for rows.Next() {
		var value model.MetricDashboardVersion
		if err := rows.Scan(&value.DashboardID, &value.Revision, &value.Title, &value.Summary,
			&value.CreatedAt, &value.CreatedBy); err != nil {
			return nil, fmt.Errorf("scan dashboard version: %w", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

// VersionDefinition 读取某个历史版本的完整定义；版本号不存在时返回 ErrDashboardNotFound。
func (store *DashboardStore) VersionDefinition(ctx context.Context, id string, revision int) (*model.DashboardDefinition, error) {
	return readDashboardVersionDefinition(ctx, store.db, id, revision)
}

// RestoreVersion 把看板换回指定历史版本。恢复动作本身也写入一个新版本，历史不会被覆盖，
// 因此恢复后仍可再退回「恢复之前」的版本。
func (store *DashboardStore) RestoreVersion(ctx context.Context, request DashboardVersionRestore) (*model.MetricDashboard, error) {
	// 与 Save 一致：存储层自己校验备注，调用方漏校验时也不会把非法备注写进历史。
	if err := model.ValidateDashboardVersionSummary(request.Summary); err != nil {
		return nil, err
	}
	transaction, err := beginWriteTx(ctx, store.db)
	if err != nil {
		return nil, err
	}
	defer func() { _ = transaction.Rollback() }()
	current, err := readDashboard(ctx, transaction, request.ID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, ErrDashboardNotFound
	}
	definition, err := readDashboardVersionDefinition(ctx, transaction, request.ID, request.Revision)
	if err != nil {
		return nil, err
	}
	encoded, err := encodeDashboard(*definition)
	if err != nil {
		return nil, err
	}
	mutation := dashboardMutation{ctx: ctx, transaction: transaction, summary: request.Summary, restore: true,
		write: DashboardWrite{Value: current.value, ExpectedRevision: request.ExpectedRevision, Actor: request.Actor}}
	if err := mutation.prepare(); err != nil {
		return nil, err
	}
	mutation.write.Value.Definition, mutation.encoded = *definition, encoded
	if err := mutation.persist(); err != nil {
		return nil, err
	}
	if err := mutation.audit("metrics.dashboard.version_restore"); err != nil {
		return nil, err
	}
	if err := transaction.Commit(); err != nil {
		return nil, err
	}
	return &mutation.write.Value, nil
}

func readDashboardVersionDefinition(ctx context.Context, database schemaExecutor, id string, revision int) (*model.DashboardDefinition, error) {
	var encoded string
	err := database.QueryRowContext(ctx,
		"SELECT definition_json FROM dashboard_versions WHERE dashboard_id=? AND revision=?", id, revision).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDashboardNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read dashboard version: %w", err)
	}
	var definition model.DashboardDefinition
	if err := json.Unmarshal([]byte(encoded), &definition); err != nil {
		return nil, fmt.Errorf("decode dashboard version: %w", err)
	}
	if err := model.ValidateDashboard(definition); err != nil {
		return nil, err
	}
	return &definition, nil
}

// recordVersion 在同一个写事务里记录本次修订，并把超出上限的旧版本裁掉。
func (mutation *dashboardMutation) recordVersion() error {
	value := mutation.write.Value
	if _, err := mutation.transaction.ExecContext(mutation.ctx, `INSERT INTO dashboard_versions
		(dashboard_id, revision, title, summary, definition_json, created_at, created_by)
		VALUES(?,?,?,?,?,?,?) ON CONFLICT(dashboard_id, revision) DO UPDATE SET
		title=excluded.title, summary=excluded.summary, definition_json=excluded.definition_json,
		created_at=excluded.created_at, created_by=excluded.created_by`,
		value.ID, value.Revision, value.Definition.Title, mutation.summary, mutation.encoded,
		value.UpdatedAt, mutation.write.Actor.UserID); err != nil {
		return fmt.Errorf("record dashboard version: %w", err)
	}
	if _, err := mutation.transaction.ExecContext(mutation.ctx, `DELETE FROM dashboard_versions
		WHERE dashboard_id=? AND revision NOT IN (SELECT revision FROM dashboard_versions
		WHERE dashboard_id=? ORDER BY revision DESC LIMIT ?)`,
		value.ID, value.ID, model.MaxDashboardVersions); err != nil {
		return fmt.Errorf("prune dashboard versions: %w", err)
	}
	return nil
}
