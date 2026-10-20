package store

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

// TestMetricErasureCreateTargetResolutionFaults 覆盖目标解析的三条失败面：
// 实体不存在、确认短语与库内显示名不一致、目标表不可读。
func TestMetricErasureCreateTargetResolutionFaults(t *testing.T) {
	store := metricErasureFixture(t)
	ctx := t.Context()
	now := model.NowUTC()
	missingNode := metricErasureRequest(uuid.NewString(), model.MetricErasureDeleteNode,
		"00000000-0000-4000-8000-000000000000", "")
	if _, _, err := store.Create(ctx, MetricErasureCreate{Request: missingNode, Now: now}); !errors.Is(err, model.ErrMetricErasureMissing) {
		t.Fatalf("missing node = %v", err)
	}
	missingTask := metricErasureRequest(uuid.NewString(), model.MetricErasureDeleteNetworkTask,
		"00000000-0000-4000-8000-000000000001", "")
	if _, _, err := store.Create(ctx, MetricErasureCreate{Request: missingTask, Now: now}); !errors.Is(err, model.ErrMetricErasureMissing) {
		t.Fatalf("missing network task = %v", err)
	}
	mismatched := metricErasureRequest(uuid.NewString(), model.MetricErasureDeleteNode, erasureNodeID, "not-the-name")
	if _, _, err := store.Create(ctx, MetricErasureCreate{Request: mismatched, Now: now}); !errors.Is(err, model.ErrMetricErasureInvalid) {
		t.Fatalf("confirmation mismatch = %v", err)
	}
	if _, err := store.db.ExecContext(ctx, "ALTER TABLE nodes RENAME TO cov_nodes_backup"); err != nil {
		t.Fatal(err)
	}
	broken := metricErasureRequest(uuid.NewString(), model.MetricErasureDeleteNode, erasureNodeID, "server-one")
	if _, _, err := store.Create(ctx, MetricErasureCreate{Request: broken, Now: now}); err == nil ||
		errors.Is(err, model.ErrMetricErasureMissing) {
		t.Fatalf("unreadable node table = %v", err)
	}
}

// TestMetricErasureCreateRejectsUnauthorizedMetadataActor 确认元数据授权失败时
// 擦除任务在事务开启后立刻失败关闭，不落任何墓碑。
func TestMetricErasureCreateRejectsUnauthorizedMetadataActor(t *testing.T) {
	store := metricErasureFixture(t)
	ctx := WithMetadataActor(t.Context(), MetadataActor{DashboardActor: DashboardActor{
		UserID: "ghost", SessionID: "ghost-session", RequestID: "ghost-request"}})
	if _, _, err := store.Create(ctx, MetricErasureCreate{
		Request: erasureFleetRequest(uuid.NewString()), Now: model.NowUTC()}); err == nil {
		t.Fatal("unauthorized metadata actor created an erasure job")
	}
}

func blockMetricErasureJobInsert(t *testing.T, store *MetricErasureStore, statement string) {
	t.Helper()
	if _, err := store.db.ExecContext(t.Context(), statement); err != nil {
		t.Fatal(err)
	}
	create := MetricErasureCreate{Request: erasureFleetRequest(uuid.NewString()), Now: model.NowUTC()}
	if _, _, err := store.Create(t.Context(), create); err == nil {
		t.Fatal("blocked metric erasure job insert was accepted")
	}
}

// TestMetricErasureCreateFailsClosedOnLedgerFaults 覆盖请求号查询、唯一约束、
// 静默忽略插入与提交期外键失败四种账本故障。
func TestMetricErasureCreateFailsClosedOnLedgerFaults(t *testing.T) {
	t.Run("request lookup", func(t *testing.T) {
		store := metricErasureFixture(t)
		if _, err := store.db.ExecContext(t.Context(),
			"ALTER TABLE metric_erasure_jobs RENAME TO cov_erasure_backup"); err != nil {
			t.Fatal(err)
		}
		create := MetricErasureCreate{Request: erasureFleetRequest(uuid.NewString()), Now: model.NowUTC()}
		if _, _, err := store.Create(t.Context(), create); err == nil {
			t.Fatal("unreadable request ledger was accepted")
		}
	})
	t.Run("unique violation", func(t *testing.T) {
		store := metricErasureFixture(t)
		if _, err := store.db.ExecContext(t.Context(), `CREATE TRIGGER cov_erasure_unique
			BEFORE INSERT ON metric_erasure_jobs
			BEGIN SELECT RAISE(ABORT,'UNIQUE constraint failed: metric_erasure_jobs.id'); END`); err != nil {
			t.Fatal(err)
		}
		create := MetricErasureCreate{Request: erasureFleetRequest(uuid.NewString()), Now: model.NowUTC()}
		if _, _, err := store.Create(t.Context(), create); !errors.Is(err, model.ErrMetricErasureConflict) {
			t.Fatalf("unique violation = %v", err)
		}
	})
	t.Run("ignored insert", func(t *testing.T) {
		store := metricErasureFixture(t)
		blockMetricErasureJobInsert(t, store, `CREATE TRIGGER cov_erasure_ignore
			BEFORE INSERT ON metric_erasure_jobs BEGIN SELECT RAISE(IGNORE); END`)
	})
	t.Run("deferred commit", func(t *testing.T) {
		store := metricErasureFixture(t)
		installCustomMetricCommitFault(t, store.db, "INSERT ON metric_erasure_jobs")
		create := MetricErasureCreate{Request: erasureFleetRequest(uuid.NewString()), Now: model.NowUTC()}
		if _, _, err := store.Create(t.Context(), create); err == nil {
			t.Fatal("deferred commit failure was accepted")
		}
	})
}

// TestMetricErasureCreateFailsClosedOnTombstoneFaults 覆盖删除前守卫不可读、
// 墓碑更新被阻断与墓碑未命中三种情况。
func TestMetricErasureCreateFailsClosedOnTombstoneFaults(t *testing.T) {
	deleteNode := func(requestID string) MetricErasureCreate {
		return MetricErasureCreate{Request: metricErasureRequest(requestID, model.MetricErasureDeleteNode,
			erasureNodeID, "server-one"), Now: model.NowUTC()}
	}
	t.Run("delete guard unreadable", func(t *testing.T) {
		store := metricErasureFixture(t)
		if _, err := store.db.ExecContext(t.Context(), "DROP TABLE operation_transfer_targets"); err != nil {
			t.Fatal(err)
		}
		if _, _, err := store.Create(t.Context(), deleteNode(uuid.NewString())); err == nil {
			t.Fatal("unreadable delete guard was accepted")
		}
	})
	t.Run("tombstone blocked", func(t *testing.T) {
		store := metricErasureFixture(t)
		if _, err := store.db.ExecContext(t.Context(), `CREATE TRIGGER cov_tombstone_abort
			BEFORE UPDATE ON nodes BEGIN SELECT RAISE(ABORT,'tombstone blocked'); END`); err != nil {
			t.Fatal(err)
		}
		if _, _, err := store.Create(t.Context(), deleteNode(uuid.NewString())); err == nil {
			t.Fatal("blocked tombstone was accepted")
		}
	})
	t.Run("tombstone missed", func(t *testing.T) {
		store := metricErasureFixture(t)
		if _, err := store.db.ExecContext(t.Context(), `CREATE TRIGGER cov_tombstone_ignore
			BEFORE UPDATE ON nodes BEGIN SELECT RAISE(IGNORE); END`); err != nil {
			t.Fatal(err)
		}
		if _, _, err := store.Create(t.Context(), deleteNode(uuid.NewString())); !errors.Is(err, model.ErrMetricErasureConflict) {
			t.Fatalf("missed tombstone = %v", err)
		}
	})
}

// TestMetricErasureCreateFailsClosedWhenAuditWriteIsBlocked 确认已授权的元数据主体
// 在审计写入失败时整个擦除事务回滚。
func TestMetricErasureCreateFailsClosedWhenAuditWriteIsBlocked(t *testing.T) {
	fixture := newMetadataFixture(t)
	store := NewMetricErasureStore(fixture.db)
	installCustomMetricFault(t, fixture.db, `CREATE TRIGGER cov_erasure_audit
		BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(ABORT,'audit blocked'); END`)
	create := MetricErasureCreate{Now: model.NowUTC(), Request: model.MetricErasureCreateRequest{
		Scope: model.MetricErasureNodeHistory, TargetID: fixture.node.ID,
		RequestID: uuid.NewString(), Confirmation: fixture.node.Name}}
	if _, _, err := store.Create(fixture.ctx, create); err == nil {
		t.Fatal("blocked audit was accepted")
	}
	var count int
	if err := fixture.db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM metric_erasure_jobs").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("failed erasure left %d queued jobs", count)
	}
}
