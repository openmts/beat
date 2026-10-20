package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

func erasureOwnerContext(t *testing.T, database *sql.DB) context.Context {
	t.Helper()
	admins := NewAdminStore(database)
	issued := model.NowUTC()
	user := &model.AdminUser{ID: "erasure-owner", Username: "erasure-owner", DisplayName: "Erasure owner",
		Role: model.AdminRoleOwner, Enabled: true, PasswordHash: "fixture",
		PasswordChangedAt: issued, CreatedAt: issued, UpdatedAt: issued}
	if err := admins.CreateUser(t.Context(), user); err != nil {
		t.Fatal(err)
	}
	until := issued.Add(time.Hour)
	session := &model.AdminSession{ID: "erasure-owner-session", UserID: user.ID,
		TokenHash: bytes.Repeat([]byte{5}, 32), TokenPrefix: "erasure", CreatedAt: issued,
		LastActivityAt: issued, IdleExpiresAt: until, AbsoluteExpiresAt: until, ReauthenticatedUntil: &until}
	if err := admins.CreateSession(t.Context(), session); err != nil {
		t.Fatal(err)
	}
	return WithMetadataActor(t.Context(), MetadataActor{DashboardActor: DashboardActor{UserID: user.ID,
		SessionID: session.ID, RequestID: "erasure-fault", IPAddress: "192.0.2.12", UserAgent: "erasure fixture"}})
}

func queueEntityErasure(t *testing.T, store *MetricErasureStore, now time.Time) string {
	t.Helper()
	job := createMetricErasureJob(t, store, uuid.NewString(), model.MetricErasureDeleteNode,
		erasureNodeID, "server-one", now)
	return job.ID
}

func failEntityErasure(t *testing.T, store *MetricErasureStore, now time.Time) string {
	t.Helper()
	id := queueEntityErasure(t, store, now)
	if _, err := store.db.ExecContext(t.Context(),
		"UPDATE metric_erasure_jobs SET state = 'failed' WHERE id = ?", id); err != nil {
		t.Fatal(err)
	}
	return id
}

func installErasureTrigger(t *testing.T, store *MetricErasureStore, statement string) {
	t.Helper()
	if _, err := store.db.ExecContext(t.Context(), statement); err != nil {
		t.Fatal(err)
	}
}

// TestMetricErasureCancelFailsClosed 覆盖取消路径的授权、任务查询、状态更新、实体恢复与审计失败。
func TestMetricErasureCancelFailsClosed(t *testing.T) {
	now := time.Date(2026, time.October, 15, 10, 0, 0, 0, time.UTC)
	t.Run("unauthorized actor", func(t *testing.T) {
		store := metricErasureFixture(t)
		id := queueEntityErasure(t, store, now)
		ctx := WithMetadataActor(t.Context(), MetadataActor{DashboardActor: DashboardActor{
			UserID: "ghost", SessionID: "ghost-session", RequestID: "ghost-request"}})
		if err := store.Cancel(ctx, id, now); !errors.Is(err, ErrMetadataForbidden) {
			t.Fatalf("unauthorized cancel error = %v", err)
		}
	})
	t.Run("job lookup blocked", func(t *testing.T) {
		store := metricErasureFixture(t)
		installErasureTrigger(t, store,
			"ALTER TABLE metric_erasure_jobs RENAME TO cov_erasure_jobs_backup")
		if err := store.Cancel(t.Context(), uuid.NewString(), now); err == nil {
			t.Fatal("unreadable erasure ledger accepted a cancel")
		}
	})
	t.Run("state update blocked", func(t *testing.T) {
		store := metricErasureFixture(t)
		id := queueEntityErasure(t, store, now)
		installErasureTrigger(t, store, `CREATE TRIGGER cov_block_cancel BEFORE UPDATE ON metric_erasure_jobs
			BEGIN SELECT RAISE(ABORT,'cancel blocked'); END`)
		if err := store.Cancel(t.Context(), id, now); err == nil {
			t.Fatal("blocked cancel update was accepted")
		}
	})
	t.Run("entity restore blocked", func(t *testing.T) {
		store := metricErasureFixture(t)
		id := queueEntityErasure(t, store, now)
		installErasureTrigger(t, store, `CREATE TRIGGER cov_block_restore BEFORE UPDATE ON nodes
			BEGIN SELECT RAISE(ABORT,'restore blocked'); END`)
		if err := store.Cancel(t.Context(), id, now); err == nil {
			t.Fatal("blocked entity restore was accepted")
		}
	})
	t.Run("audit blocked", func(t *testing.T) {
		store := metricErasureFixture(t)
		id := queueEntityErasure(t, store, now)
		installErasureTrigger(t, store, `CREATE TRIGGER cov_block_cancel_audit BEFORE INSERT ON admin_audit_events
			WHEN NEW.action = 'metric_erasure.cancel' BEGIN SELECT RAISE(ABORT,'audit blocked'); END`)
		ctx := erasureOwnerContext(t, store.db)
		if err := store.Cancel(ctx, id, now); err == nil {
			t.Fatal("blocked cancel audit was accepted")
		}
	})
}

// TestMetricErasureRetryFailsClosed 覆盖重试路径的授权、任务查询、状态更新与审计失败。
func TestMetricErasureRetryFailsClosed(t *testing.T) {
	now := time.Date(2026, time.October, 15, 11, 0, 0, 0, time.UTC)
	t.Run("unauthorized actor", func(t *testing.T) {
		store := metricErasureFixture(t)
		id := failEntityErasure(t, store, now)
		ctx := WithMetadataActor(t.Context(), MetadataActor{DashboardActor: DashboardActor{
			UserID: "ghost", SessionID: "ghost-session", RequestID: "ghost-request"}})
		if err := store.Retry(ctx, id, now); !errors.Is(err, ErrMetadataForbidden) {
			t.Fatalf("unauthorized retry error = %v", err)
		}
	})
	t.Run("queued job rejected", func(t *testing.T) {
		store := metricErasureFixture(t)
		id := queueEntityErasure(t, store, now)
		if err := store.Retry(t.Context(), id, now); !errors.Is(err, model.ErrMetricErasureConflict) {
			t.Fatalf("queued retry error = %v", err)
		}
	})
	t.Run("job lookup blocked", func(t *testing.T) {
		store := metricErasureFixture(t)
		installErasureTrigger(t, store,
			"ALTER TABLE metric_erasure_jobs RENAME TO cov_erasure_jobs_backup")
		if err := store.Retry(t.Context(), uuid.NewString(), now); err == nil {
			t.Fatal("unreadable erasure ledger accepted a retry")
		}
	})
	t.Run("state update blocked", func(t *testing.T) {
		store := metricErasureFixture(t)
		id := failEntityErasure(t, store, now)
		installErasureTrigger(t, store, `CREATE TRIGGER cov_block_retry BEFORE UPDATE ON metric_erasure_jobs
			BEGIN SELECT RAISE(ABORT,'retry blocked'); END`)
		if err := store.Retry(t.Context(), id, now); err == nil {
			t.Fatal("blocked retry update was accepted")
		}
	})
	t.Run("audit blocked", func(t *testing.T) {
		store := metricErasureFixture(t)
		id := failEntityErasure(t, store, now)
		installErasureTrigger(t, store, `CREATE TRIGGER cov_block_retry_audit BEFORE INSERT ON admin_audit_events
			WHEN NEW.action = 'metric_erasure.retry' BEGIN SELECT RAISE(ABORT,'audit blocked'); END`)
		ctx := erasureOwnerContext(t, store.db)
		if err := store.Retry(ctx, id, now); err == nil {
			t.Fatal("blocked retry audit was accepted")
		}
	})
}

// TestMetricErasureFinalizeEntityFaults 覆盖实体收尾的存储不可用、运维守卫、节点与网络任务删除失败。
func TestMetricErasureFinalizeEntityFaults(t *testing.T) {
	now := time.Date(2026, time.October, 15, 12, 0, 0, 0, time.UTC)
	nodeJob := model.MetricErasureJob{Scope: model.MetricErasureDeleteNode, TargetID: erasureNodeID}
	taskJob := model.MetricErasureJob{Scope: model.MetricErasureDeleteNetworkTask, TargetID: erasureTaskID}
	t.Run("closed database", func(t *testing.T) {
		store := NewMetricErasureStore(closedSQLiteStore(t).DB)
		if err := store.FinalizeEntity(t.Context(), nodeJob, now); err == nil {
			t.Fatal("closed database accepted an entity finalize")
		}
	})
	t.Run("operation guard blocked", func(t *testing.T) {
		store := metricErasureFixture(t)
		installErasureTrigger(t, store,
			"ALTER TABLE operation_transfer_targets RENAME TO cov_transfer_targets_backup")
		if err := store.FinalizeEntity(t.Context(), nodeJob, now); err == nil {
			t.Fatal("unreadable operation guard accepted an entity finalize")
		}
	})
	t.Run("node delete blocked", func(t *testing.T) {
		store := metricErasureFixture(t)
		installErasureTrigger(t, store, `CREATE TRIGGER cov_block_node_delete BEFORE DELETE ON nodes
			BEGIN SELECT RAISE(ABORT,'node delete blocked'); END`)
		if err := store.FinalizeEntity(t.Context(), nodeJob, now); err == nil {
			t.Fatal("blocked node delete was accepted")
		}
	})
	t.Run("network task delete blocked", func(t *testing.T) {
		store := metricErasureFixture(t)
		installErasureTrigger(t, store, `CREATE TRIGGER cov_block_task_delete BEFORE DELETE ON network_tasks
			BEGIN SELECT RAISE(ABORT,'task delete blocked'); END`)
		if err := store.FinalizeEntity(t.Context(), taskJob, now); err == nil {
			t.Fatal("blocked network task delete was accepted")
		}
	})
}

// TestMetricErasureRestoreEntityScopes 覆盖墓碑恢复的实体范围分支与写入失败。
func TestMetricErasureRestoreEntityScopes(t *testing.T) {
	now := time.Date(2026, time.October, 15, 13, 0, 0, 0, time.UTC)
	store := metricErasureFixture(t)
	taskJob := model.MetricErasureJob{Scope: model.MetricErasureDeleteNetworkTask, TargetID: erasureTaskID}
	if err := restoreMetricErasureEntity(t.Context(), store.db, taskJob, now); err != nil {
		t.Fatalf("network task restore error = %v", err)
	}
	historyJob := model.MetricErasureJob{Scope: model.MetricErasureNodeHistory, TargetID: erasureNodeID}
	if err := restoreMetricErasureEntity(t.Context(), store.db, historyJob, now); err != nil {
		t.Fatalf("history scope restore error = %v", err)
	}
	closed := NewMetricErasureStore(closedSQLiteStore(t).DB)
	if err := restoreMetricErasureEntity(t.Context(), closed.db, taskJob, now); err == nil {
		t.Fatal("closed database restored a tombstone")
	}
}
