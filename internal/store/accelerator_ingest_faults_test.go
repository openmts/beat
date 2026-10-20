package store

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/testutil"
)

func acceleratorIngestTx(t *testing.T, store *SQLiteStore) *sql.Tx {
	t.Helper()
	tx, err := store.DB.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatalf("begin accelerator ingest transaction: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	return tx
}

func acceleratorIngestContact(fixture *acceleratorFixture) AgentContact {
	report := testutil.AcceleratorReport(fixture.now.Add(-time.Second))
	return AgentContact{Identity: fixture.identity, ReportID: report.SampleID,
		ReportCapturedAt: report.CapturedAt, Accelerators: &report}
}

// TestAcceleratorContactStagingFailsClosed 覆盖暂存联系时的报告校验、策略读取与策略校验失败：
// 三类失败都必须把错误原样抛出，不能把不合规的清单写进待发布表。
func TestAcceleratorContactStagingFailsClosed(t *testing.T) {
	t.Run("invalid report", func(t *testing.T) {
		fixture := newAcceleratorFixture(t)
		contact := acceleratorIngestContact(fixture)
		contact.ReportCapturedAt = contact.Accelerators.CapturedAt.Add(-time.Minute)
		err := stageAcceleratorContact(t.Context(), acceleratorIngestTx(t, fixture.database), contact)
		if !errors.Is(err, model.ErrAgentReportInvalid) {
			t.Fatalf("stale report error = %v", err)
		}
	})
	t.Run("policy read blocked", func(t *testing.T) {
		fixture := newAcceleratorFixture(t)
		if _, err := fixture.database.DB.ExecContext(t.Context(),
			"ALTER TABLE accelerator_collection_policies RENAME TO cov_accelerator_policies_backup"); err != nil {
			t.Fatal(err)
		}
		err := stageAcceleratorContact(t.Context(), acceleratorIngestTx(t, fixture.database), acceleratorIngestContact(fixture))
		if err == nil {
			t.Fatal("unreadable accelerator policy table was accepted")
		}
	})
	t.Run("policy mismatch", func(t *testing.T) {
		fixture := newAcceleratorFixture(t)
		contact := acceleratorIngestContact(fixture)
		contact.Accelerators.PolicyRevision = 9
		err := stageAcceleratorContact(t.Context(), acceleratorIngestTx(t, fixture.database), contact)
		if !errors.Is(err, ErrAcceleratorPolicy) {
			t.Fatalf("policy revision mismatch error = %v", err)
		}
		var count int
		if err := fixture.database.DB.QueryRowContext(t.Context(),
			"SELECT COUNT(*) FROM accelerator_ingest_pending").Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("rejected contact staged %d pending rows", count)
		}
	})
}

// TestAcceleratorAcceptanceRejectsPolicyRevisionDrift 直接覆盖接受度校验的首条分支。
func TestAcceleratorAcceptanceRejectsPolicyRevisionDrift(t *testing.T) {
	report := testutil.AcceleratorReport(time.Now().UTC())
	policy := model.DefaultAcceleratorPolicy()
	policy.Revision = report.PolicyRevision + 3
	if err := validateAcceleratorAcceptance(report, policy); !errors.Is(err, ErrAcceleratorPolicy) {
		t.Fatalf("revision drift error = %v", err)
	}
}

// TestAcceleratorInventoryCapacityFaults 覆盖容量查询的三种失败：表不可读、行扫描失败与游标迭代失败。
func TestAcceleratorInventoryCapacityFaults(t *testing.T) {
	t.Run("table unreadable", func(t *testing.T) {
		fixture := newAcceleratorFixture(t)
		if _, err := fixture.database.DB.ExecContext(t.Context(),
			"ALTER TABLE node_accelerators RENAME TO cov_node_accelerators_backup"); err != nil {
			t.Fatal(err)
		}
		err := acceleratorInventoryCapacity(t.Context(), acceleratorIngestTx(t, fixture.database), acceleratorIngestContact(fixture))
		if err == nil {
			t.Fatal("unreadable inventory table was accepted")
		}
	})
	t.Run("row scan failure", func(t *testing.T) {
		fixture := newAcceleratorFixture(t)
		installInventoryView(t, fixture, `SELECT node_id, NULL AS fingerprint FROM cov_inventory_rows`)
		if _, err := fixture.database.DB.ExecContext(t.Context(),
			"INSERT INTO cov_inventory_rows(node_id, fingerprint) VALUES(?, NULL)", fixture.node.ID); err != nil {
			t.Fatal(err)
		}
		err := acceleratorInventoryCapacity(t.Context(), acceleratorIngestTx(t, fixture.database), acceleratorIngestContact(fixture))
		if err == nil {
			t.Fatal("NULL fingerprint row was accepted")
		}
	})
	t.Run("iteration failure", func(t *testing.T) {
		fixture := newAcceleratorFixture(t)
		installInventoryView(t, fixture, `SELECT node_id,
			CASE WHEN fingerprint = 'boom' THEN abs(-9223372036854775808) ELSE fingerprint END AS fingerprint
			FROM cov_inventory_rows`)
		for _, value := range []string{"ok", "boom"} {
			if _, err := fixture.database.DB.ExecContext(t.Context(),
				"INSERT INTO cov_inventory_rows(node_id, fingerprint) VALUES(?, ?)", fixture.node.ID, value); err != nil {
				t.Fatal(err)
			}
		}
		err := acceleratorInventoryCapacity(t.Context(), acceleratorIngestTx(t, fixture.database), acceleratorIngestContact(fixture))
		if err == nil {
			t.Fatal("expired cursor was accepted")
		}
	})
}

func installInventoryView(t *testing.T, fixture *acceleratorFixture, statement string) {
	t.Helper()
	if _, err := fixture.database.DB.ExecContext(t.Context(), `CREATE TABLE cov_inventory_rows(
		node_id TEXT, fingerprint TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.DB.ExecContext(t.Context(),
		"ALTER TABLE node_accelerators RENAME TO cov_node_accelerators_backup"); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.DB.ExecContext(t.Context(), "CREATE VIEW node_accelerators AS "+statement); err != nil {
		t.Fatal(err)
	}
}

// TestAcceleratorPendingReadFaults 覆盖待发布记录的清单解码失败与待发布列表的扫描失败。
func TestAcceleratorPendingReadFaults(t *testing.T) {
	t.Run("inventory decode failure", func(t *testing.T) {
		fixture := newAcceleratorFixture(t)
		if _, err := fixture.database.DB.ExecContext(t.Context(), `INSERT INTO accelerator_ingest_pending
			(node_id, report_id, sample_id, captured_at, payload_digest, inventory_json, credential_fingerprint, created_at)
			VALUES (?, ?, ?, ?, ?, '{"version":1}', ?, ?)`,
			fixture.node.ID, fixture.node.ID, fixture.node.ID, fixture.now,
			"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			fixture.identity.CredentialFingerprint, fixture.now); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.inventory.Pending(t.Context(), fixture.node.ID); err == nil {
			t.Fatal("corrupted pending inventory was decoded")
		}
	})
	t.Run("pending list succeeds", func(t *testing.T) {
		fixture := newAcceleratorFixture(t)
		fixture.prepare(t, fixture.report())
		ids, err := fixture.inventory.PendingNodes(t.Context())
		if err != nil || len(ids) != 1 || ids[0] != fixture.node.ID {
			t.Fatalf("pending nodes = %v, err = %v", ids, err)
		}
	})
	t.Run("pending node scan failure", func(t *testing.T) {
		fixture := newAcceleratorFixture(t)
		if _, err := fixture.database.DB.ExecContext(t.Context(), `INSERT INTO accelerator_ingest_pending
			(node_id, report_id, sample_id, captured_at, payload_digest, inventory_json, credential_fingerprint, created_at)
			VALUES (NULL, ?, ?, ?, ?, '{"version":1}', ?, ?)`,
			fixture.node.ID, fixture.node.ID, fixture.now,
			"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			fixture.identity.CredentialFingerprint, fixture.now); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.inventory.PendingNodes(t.Context()); err == nil {
			t.Fatal("NULL pending node was accepted")
		}
	})
}

// TestAcceleratorContactReportRejections 覆盖非活跃凭据的两条分支：静默停用与显式拒绝。
func TestAcceleratorContactReportRejections(t *testing.T) {
	contact := AgentContact{Identity: model.AgentIdentity{NodeID: "node", Mode: model.AgentCredentialLegacy}}
	contact.Accelerators = &model.AcceleratorReport{State: "disabled"}
	report, err := acceleratorContactReport(contact)
	if report != nil || err != nil {
		t.Fatalf("disabled collector = %+v, err = %v", report, err)
	}
	contact.Accelerators.InstallationEnabled = true
	if _, err = acceleratorContactReport(contact); !errors.Is(err, ErrAgentCredentialRejected) {
		t.Fatalf("enabled collector on inactive credential = %v", err)
	}
}
