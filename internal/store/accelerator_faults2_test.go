package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/testutil"
)

func TestAcceleratorPendingMatchesRejectsMismatchedOrStaleCandidates(t *testing.T) {
	f := newAcceleratorFixture(t)
	report := f.report()
	attempt := f.prepare(t, report)
	if _, err := attempt.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	attempt.Close()
	pending, err := f.inventory.Pending(t.Context(), f.node.ID)
	if err != nil || pending == nil {
		t.Fatalf("待发布快照 = %+v/%v", pending, err)
	}
	transaction := alertTestTx(t, f.database.DB)
	if matches, err := f.inventory.pendingMatches(t.Context(), transaction,
		AcceleratorPending{NodeID: f.node.ID, ReportID: "any"}); err != nil || matches {
		t.Fatalf("报告号不匹配时 = %v/%v", matches, err)
	}
	stale := *pending
	stale.ReportID = "other-report"
	if matches, err := f.inventory.pendingMatches(t.Context(), transaction, stale); err != nil || matches {
		t.Fatalf("报告号不匹配时 = %v/%v", matches, err)
	}
	rejected := *pending
	rejected.CredentialFingerprint = "0000000000000000000000000000000000000000000000000000000000000000"
	if _, err := f.inventory.pendingMatches(t.Context(), transaction, rejected); !errors.Is(err, ErrAgentCredentialRejected) {
		t.Fatalf("凭据指纹不匹配 = %v", err)
	}
	if _, err := transaction.ExecContext(t.Context(), `INSERT INTO node_accelerator_state
		(node_id,sample_id,captured_at,payload_digest,inventory_json,accepted_at) VALUES(?,?,?,?,?,?)
		ON CONFLICT(node_id) DO UPDATE SET captured_at=excluded.captured_at`,
		f.node.ID, "future-sample", f.now.Add(time.Hour), strings.Repeat("b", 64), "{}", f.now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.inventory.pendingMatches(t.Context(), transaction, *pending); !errors.Is(err, ErrAgentSampleOrder) {
		t.Fatalf("更旧的快照 = %v", err)
	}
	if _, err := transaction.ExecContext(t.Context(), "UPDATE nodes SET credential_state='pending_claim' WHERE id=?", f.node.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.inventory.pendingMatches(t.Context(), transaction, *pending); err == nil {
		t.Fatal("隐藏节点仍匹配待发布快照")
	}
}

func TestAcceleratorPublishFailsClosedOnPolicyAndStorageFaults(t *testing.T) {
	f := newAcceleratorFixture(t)
	snapshot := testutil.AcceleratorReport(f.now).Inventory()
	policy, err := f.inventory.AgentPolicy(t.Context(), f.node.ID, snapshot.InstallationEnabled)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.PolicyRevision = policy.Revision
	closed := NewAcceleratorStore(closedSQLiteStore(t).DB)
	if err := closed.Publish(t.Context(), acceleratorPublicationFor(f, snapshot)); err == nil {
		t.Fatal("关闭的数据库接受了发布")
	}
	transaction := alertTestTx(t, f.database.DB)
	stale := snapshot
	stale.PolicyRevision = policy.Revision + 1
	if err := publishAcceleratorSnapshot(t.Context(), transaction, AcceleratorPending{NodeID: f.node.ID, PayloadDigest: digest64(), Snapshot: stale}); !errors.Is(err, ErrAcceleratorPolicy) {
		t.Fatalf("过期策略修订 = %v", err)
	}
	disabled := snapshot
	disabled.InstallationEnabled = false
	if err := publishAcceleratorSnapshot(t.Context(), transaction, AcceleratorPending{NodeID: f.node.ID, PayloadDigest: digest64(), Snapshot: disabled}); err != nil {
		t.Fatalf("未启用采集的发布 = %v", err)
	}
	incomplete := snapshot
	incomplete.State = "collecting"
	if err := publishAcceleratorSnapshot(t.Context(), transaction, AcceleratorPending{NodeID: f.node.ID, PayloadDigest: digest64(), Snapshot: incomplete}); err != nil {
		t.Fatalf("未完成的发布 = %v", err)
	}
	foreign := snapshot
	foreign.Capabilities = append(append([]model.AcceleratorCapability{}, snapshot.Capabilities...),
		model.AcceleratorCapability{Vendor: "amd", State: "ok"})
	if err := publishAcceleratorSnapshot(t.Context(), transaction, AcceleratorPending{NodeID: f.node.ID, PayloadDigest: digest64(), Snapshot: foreign}); err != nil {
		t.Fatalf("含其它厂商能力的发布 = %v", err)
	}
	if err := transaction.Rollback(); err != nil {
		t.Fatal(err)
	}
}

func acceleratorPublicationFor(f *acceleratorFixture, snapshot model.AcceleratorInventorySnapshot) AcceleratorPublication {
	digest := digest64()
	return AcceleratorPublication{
		Pending: AcceleratorPending{NodeID: f.node.ID, ReportID: "report-publish", PayloadDigest: digest,
			Snapshot: snapshot, CredentialFingerprint: f.identity.CredentialFingerprint},
		Receipt: model.AcceleratorSampleReceipt{NodeID: f.node.ID, PayloadDigest: digest,
			Snapshot: snapshot},
	}
}

// digest64 返回满足 payload_digest 长度约束的占位摘要。
func digest64() string { return strings.Repeat("a", 64) }
