package remoteoperations

import (
	"io"
	"strings"
	"testing"
)

type transferFaultFixture struct {
	fixture
	transfer TransferSummary
	attempt  AttemptRequest
	content  []byte
}

func transferFault(t *testing.T, action string) transferFaultFixture {
	t.Helper()
	f, _ := fileFixture(t)
	test := transferFaultFixture{fixture: f, content: []byte("payload")}
	if action == "create" {
		return test
	}
	direction := "deploy"
	if strings.HasPrefix(action, "collect") || action == "download" {
		direction = "collect"
	}
	var err error
	test.transfer, err = f.service.CreateTransfer(t.Context(), f.actor, transferInput(f, direction, test.content))
	if err != nil {
		t.Fatal(err)
	}
	if direction == "collect" {
		return prepareCollectFault(t, test, action)
	}
	if action == "upload" {
		return test
	}
	if err := f.service.UploadChunk(t.Context(), f.actor, ChunkInput{TransferID: test.transfer.ID, Index: 0, Digest: fileDigest(test.content), Content: test.content}); err != nil {
		t.Fatal(err)
	}
	if action == "finalize" || action == "cancel" || action == "recover" {
		return test
	}
	if _, err := f.service.FinalizeTransfer(t.Context(), f.actor, test.transfer.ID); err != nil {
		t.Fatal(err)
	}
	if action == "lease" {
		return test
	}
	test.attempt = f.lease(t)
	if action == "start" {
		return test
	}
	if err := f.service.Start(t.Context(), test.attempt); err != nil {
		t.Fatal(err)
	}
	return test
}

func prepareCollectFault(t *testing.T, test transferFaultFixture, action string) transferFaultFixture {
	t.Helper()
	test.attempt = test.lease(t)
	if err := test.service.Start(t.Context(), test.attempt); err != nil {
		t.Fatal(err)
	}
	if action == "collect_source" {
		return test
	}
	if err := test.service.CollectSource(t.Context(), test.attempt, SourceInfo{Size: int64(len(test.content)), Digest: fileDigest(test.content)}); err != nil {
		t.Fatal(err)
	}
	if action == "collect_upload" {
		return test
	}
	if err := test.service.UploadAgentChunk(t.Context(), AgentChunk{Attempt: test.attempt, Index: 0, Digest: fileDigest(test.content), Content: test.content}); err != nil {
		t.Fatal(err)
	}
	if action == "collect_finalize" {
		return test
	}
	if err := test.service.FinalizeCollect(t.Context(), test.attempt); err != nil {
		t.Fatal(err)
	}
	return test
}

func (test transferFaultFixture) act(t *testing.T, action string) error {
	t.Helper()
	switch action {
	case "create":
		_, err := test.service.CreateTransfer(t.Context(), test.actor, transferInput(test.fixture, "deploy", test.content))
		return err
	case "upload":
		return test.service.UploadChunk(t.Context(), test.actor, ChunkInput{TransferID: test.transfer.ID, Index: 0, Digest: fileDigest(test.content), Content: test.content})
	case "finalize":
		_, err := test.service.FinalizeTransfer(t.Context(), test.actor, test.transfer.ID)
		return err
	case "lease":
		_, err := test.service.Lease(t.Context(), LeaseRequest{Proof: NodeProof{Token: test.token}, CapabilityDigest: test.digest})
		return err
	case "start":
		return test.service.Start(t.Context(), test.attempt)
	case "heartbeat":
		_, err := test.service.Heartbeat(t.Context(), test.attempt)
		return err
	case "cancel":
		return test.service.CancelTransfer(t.Context(), test.actor, test.transfer.ID)
	case "collect_source":
		return test.service.CollectSource(t.Context(), test.attempt, SourceInfo{Size: int64(len(test.content)), Digest: fileDigest(test.content)})
	case "collect_upload":
		return test.service.UploadAgentChunk(t.Context(), AgentChunk{Attempt: test.attempt, Index: 0, Digest: fileDigest(test.content), Content: test.content})
	case "collect_finalize":
		return test.service.FinalizeCollect(t.Context(), test.attempt)
	case "download":
		return test.service.DownloadTransfer(t.Context(), test.actor, DownloadInput{ID: test.transfer.ID, Consume: func(_ DownloadInfo, reader io.Reader) error { _, err := io.Copy(io.Discard, reader); return err }})
	case "recover":
		return test.service.Recover(t.Context())
	}
	t.Fatalf("unknown fixture action %s", action)
	return nil
}

func TestTransferWriteFailuresNeverAcknowledgePartialState(t *testing.T) {
	cases := []struct{ action, table, verb, condition string }{
		{"create", "operation_artifacts", "INSERT", ""}, {"create", "operation_transfers", "INSERT", ""}, {"create", "operation_transfer_targets", "INSERT", ""},
		{"create", "admin_audit_events", "INSERT", ""}, {"upload", "operation_artifact_chunks", "INSERT", ""}, {"upload", "admin_audit_events", "INSERT", ""},
		{"finalize", "operation_artifacts", "UPDATE", ""}, {"finalize", "operation_artifacts", "UPDATE", "WHEN NEW.state='ready'"},
		{"finalize", "operation_tasks", "INSERT", ""}, {"finalize", "operation_transfers", "UPDATE", ""},
		{"lease", "operation_attempts", "INSERT", ""}, {"lease", "operation_task_targets", "UPDATE", ""},
		{"start", "operation_attempts", "UPDATE", ""}, {"start", "operation_transfer_targets", "UPDATE", ""}, {"start", "operation_transfers", "UPDATE", ""},
		{"heartbeat", "operation_transfer_targets", "UPDATE", ""}, {"cancel", "operation_artifacts", "UPDATE", ""}, {"cancel", "operation_transfers", "UPDATE", ""},
		{"cancel", "operation_transfer_targets", "UPDATE", ""}, {"cancel", "admin_audit_events", "INSERT", ""}, {"cancel", "notification_messages", "INSERT", ""},
		{"collect_source", "operation_artifacts", "UPDATE", ""}, {"collect_source", "operation_transfers", "UPDATE", ""},
		{"collect_upload", "operation_artifact_chunks", "INSERT", ""}, {"collect_upload", "admin_audit_events", "INSERT", ""},
		{"collect_finalize", "operation_artifacts", "UPDATE", "WHEN NEW.state='ready'"}, {"download", "admin_audit_events", "INSERT", ""},
		{"recover", "operation_artifact_chunks", "DELETE", ""}, {"recover", "operation_artifacts", "DELETE", ""}, {"recover", "notification_messages", "INSERT", ""},
	}
	for _, item := range cases {
		t.Run(item.action+item.table+item.condition, func(t *testing.T) {
			f := transferFault(t, item.action)
			if item.action == "recover" {
				f.sql(t, "UPDATE operation_artifacts SET expires_at=0")
			}
			f.sql(t, "CREATE TRIGGER reject_transfer BEFORE "+item.verb+" ON "+item.table+" "+item.condition+" BEGIN SELECT RAISE(ABORT,'fixture'); END")
			if err := f.act(t, item.action); err == nil {
				t.Fatal("partial mutation acknowledged despite injected persistence failure")
			}
		})
	}
}

func TestTransferReadFailuresDoNotExposeData(t *testing.T) {
	for _, item := range []struct{ action, table string }{{"upload", "operation_artifact_chunks"}, {"finalize", "operation_policies"}, {"collect_source", "operation_capabilities"}, {"download", "operation_artifacts"}, {"heartbeat", "operation_policies"}} {
		t.Run(item.action+item.table, func(t *testing.T) {
			f := transferFault(t, item.action)
			f.sql(t, "ALTER TABLE "+item.table+" RENAME TO fixture_missing_table")
			if err := f.act(t, item.action); err == nil {
				t.Fatal("missing dependency acknowledged")
			}
		})
	}
}
