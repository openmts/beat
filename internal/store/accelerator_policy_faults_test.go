package store

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/testutil"
)

func covAcceleratorScope(fixture *acceleratorFixture) AcceleratorPolicyScope {
	return AcceleratorPolicyScope{Kind: "node", ID: fixture.node.ID}
}

func covAcceleratorWrite() model.AcceleratorPolicyWrite {
	return model.AcceleratorPolicyWrite{SampleIntervalSeconds: 10, MaximumDevices: 32,
		Metrics: model.AcceleratorMetricKeys}
}

// TestAcceleratorPolicyReadFaults 覆盖策略读取、JSON 解码与安装快照回退的失败面。
func TestAcceleratorPolicyReadFaults(t *testing.T) {
	t.Run("policy ledger unreadable", func(t *testing.T) {
		fixture := newAcceleratorFixture(t)
		if _, err := fixture.database.DB.Exec(
			"ALTER TABLE accelerator_collection_policies RENAME TO cov_accel_policies_backup"); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.inventory.AgentPolicy(t.Context(), fixture.node.ID, false); err == nil {
			t.Fatal("unreadable policy ledger was accepted")
		}
		if _, err := fixture.inventory.Policy(t.Context(), fixture.actor, covAcceleratorScope(fixture)); err == nil {
			t.Fatal("unreadable policy ledger was accepted by Policy")
		}
	})
	t.Run("policy json invalid", func(t *testing.T) {
		fixture := newAcceleratorFixture(t)
		if err := installCovAcceleratorPolicyLedger(fixture.database); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.database.DB.Exec(`INSERT INTO accelerator_collection_policies
			(scope_kind,scope_id,enabled,sample_interval_seconds,maximum_devices,metrics_json,revision,created_by,updated_by,updated_at)
			VALUES('node', ?, 1, 10, 32, 'not-json', 1, 'owner', 'owner', ?)`,
			fixture.node.ID, fixture.now); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.inventory.AgentPolicy(t.Context(), fixture.node.ID, false); err == nil {
			t.Fatal("undecodable policy metrics were accepted")
		}
	})
	t.Run("installation snapshot applied", func(t *testing.T) {
		fixture := newAcceleratorFixture(t)
		if err := seedCovAcceleratorSnapshot(t, fixture); err != nil {
			t.Fatal(err)
		}
		policy, err := fixture.inventory.Policy(t.Context(), fixture.actor, covAcceleratorScope(fixture))
		if err != nil || !policy.Enabled || policy.EffectiveSource != "installation" ||
			policy.SampleIntervalSeconds != int(covAcceleratorCadenceMS/1000) {
			t.Fatalf("installation policy = %+v, %v", policy, err)
		}
	})
	t.Run("installation snapshot unreadable", func(t *testing.T) {
		fixture := newAcceleratorFixture(t)
		if _, err := fixture.database.DB.Exec(
			"ALTER TABLE node_accelerator_state RENAME TO cov_accel_state_backup"); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.inventory.Policy(t.Context(), fixture.actor, covAcceleratorScope(fixture)); err == nil {
			t.Fatal("unreadable installation snapshot was accepted")
		}
	})
}

const covAcceleratorCadenceMS = 15000

func installCovAcceleratorPolicyLedger(database *SQLiteStore) error {
	_, err := database.DB.Exec(`ALTER TABLE accelerator_collection_policies RENAME TO cov_accel_policies_backup;
		CREATE TABLE accelerator_collection_policies (
		scope_kind TEXT NOT NULL, scope_id TEXT NOT NULL, enabled INTEGER NOT NULL,
		sample_interval_seconds INTEGER NOT NULL, maximum_devices INTEGER NOT NULL, metrics_json TEXT NOT NULL,
		revision INTEGER NOT NULL, created_by TEXT NOT NULL, updated_by TEXT NOT NULL, updated_at DATETIME NOT NULL,
		PRIMARY KEY(scope_kind,scope_id))`)
	return err
}

func seedCovAcceleratorSnapshot(t *testing.T, fixture *acceleratorFixture) error {
	t.Helper()
	snapshot := testutil.AcceleratorReport(fixture.now).Inventory()
	snapshot.InstallationEnabled = true
	snapshot.CadenceMS = covAcceleratorCadenceMS
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	_, err = fixture.database.DB.ExecContext(t.Context(), `INSERT INTO node_accelerator_state
		(node_id,sample_id,captured_at,payload_digest,inventory_json,accepted_at) VALUES(?,?,?,?,?,?)
		ON CONFLICT(node_id) DO UPDATE SET inventory_json=excluded.inventory_json`,
		fixture.node.ID, snapshot.SampleID, snapshot.CapturedAt, strings.Repeat("b", 64), string(encoded), fixture.now)
	return err
}

// TestAcceleratorPolicyWriteFaults 覆盖策略保存的校验、事务、乐观锁、提交与停用联动失败。
func TestAcceleratorPolicyWriteFaults(t *testing.T) {
	t.Run("validation", func(t *testing.T) {
		fixture := newAcceleratorFixture(t)
		invalid := covAcceleratorWrite()
		invalid.SampleIntervalSeconds = 1
		change := AcceleratorPolicyChange{Scope: covAcceleratorScope(fixture), Write: invalid}
		if _, err := fixture.inventory.SavePolicy(t.Context(), fixture.actor, change); err == nil {
			t.Fatal("invalid policy write was accepted")
		}
	})
	t.Run("closed database", func(t *testing.T) {
		fixture := newAcceleratorFixture(t)
		closed := NewAcceleratorStore(closedSQLiteStore(t).DB)
		change := AcceleratorPolicyChange{Scope: covAcceleratorScope(fixture), Write: covAcceleratorWrite()}
		if _, err := closed.SavePolicy(t.Context(), fixture.actor, change); err == nil {
			t.Fatal("closed database saved a policy")
		}
	})
	t.Run("ledger unreadable", func(t *testing.T) {
		fixture := newAcceleratorFixture(t)
		if _, err := fixture.database.DB.Exec(
			"ALTER TABLE accelerator_collection_policies RENAME TO cov_accel_policies_backup"); err != nil {
			t.Fatal(err)
		}
		change := AcceleratorPolicyChange{Scope: covAcceleratorScope(fixture), Write: covAcceleratorWrite()}
		if _, err := fixture.inventory.SavePolicy(t.Context(), fixture.actor, change); err == nil {
			t.Fatal("unreadable policy ledger accepted a write")
		}
	})
	t.Run("revision mismatch", func(t *testing.T) {
		fixture := newAcceleratorFixture(t)
		change := AcceleratorPolicyChange{Scope: covAcceleratorScope(fixture), Write: covAcceleratorWrite()}
		if _, err := fixture.inventory.SavePolicy(t.Context(), fixture.actor, change); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.inventory.SavePolicy(t.Context(), fixture.actor, change); !errors.Is(err, ErrAcceleratorPolicy) {
			t.Fatalf("stale revision = %v", err)
		}
	})
	t.Run("disable update blocked", func(t *testing.T) {
		fixture := newAcceleratorFixture(t)
		if _, err := fixture.database.DB.ExecContext(t.Context(), `INSERT INTO node_accelerators
			(node_id,fingerprint,device_id,ordinal,vendor,model,capabilities_json,first_seen_at,last_seen_at,inactive_at)
			VALUES(?,?,?,1,'nvidia','cov-model','[]',?,?,NULL)`, fixture.node.ID,
			strings.Repeat("a", 64), covAcceleratorDevice, fixture.now, fixture.now); err != nil {
			t.Fatal(err)
		}
		installCustomMetricFault(t, fixture.database.DB, `CREATE TRIGGER cov_accel_disable
			BEFORE UPDATE ON node_accelerators BEGIN SELECT RAISE(ABORT,'disable blocked'); END`)
		write := covAcceleratorWrite()
		write.Enabled = false
		change := AcceleratorPolicyChange{Scope: covAcceleratorScope(fixture), Write: write}
		if _, err := fixture.inventory.SavePolicy(t.Context(), fixture.actor, change); err == nil {
			t.Fatal("blocked accelerator disable was accepted")
		}
	})
	t.Run("deferred commit", func(t *testing.T) {
		fixture := newAcceleratorFixture(t)
		installCustomMetricCommitFault(t, fixture.database.DB, "INSERT ON accelerator_collection_policies")
		change := AcceleratorPolicyChange{Scope: covAcceleratorScope(fixture), Write: covAcceleratorWrite()}
		if _, err := fixture.inventory.SavePolicy(t.Context(), fixture.actor, change); err == nil {
			t.Fatal("deferred policy commit failure was accepted")
		}
	})
}
