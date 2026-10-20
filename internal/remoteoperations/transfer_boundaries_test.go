package remoteoperations

import (
	"errors"
	"io"
	"testing"
	"time"
)

func discardDownload(_ DownloadInfo, reader io.Reader) error {
	_, err := io.Copy(io.Discard, reader)
	return err
}

func TestTransferDirectionsNeverAuthorizeOtherSurfaces(t *testing.T) {
	for _, direction := range []string{"deploy", "collect"} {
		t.Run(direction, func(t *testing.T) {
			f, _ := fileFixture(t)
			content := []byte("payload")
			transfer, err := f.service.CreateTransfer(t.Context(), f.actor, transferInput(f, direction, content))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.service.FinalizeTransfer(t.Context(), Actor{}, transfer.ID); err != ErrForbidden {
				t.Fatal(err)
			}
			if err := f.service.UploadChunk(t.Context(), Actor{}, ChunkInput{TransferID: transfer.ID}); err != ErrForbidden {
				t.Fatal(err)
			}
			if err := f.service.DownloadTransfer(t.Context(), f.actor, DownloadInput{ID: transfer.ID, Consume: discardDownload}); err != ErrForbidden {
				t.Fatal(err)
			}
			if direction == "deploy" {
				if _, err := f.service.FinalizeTransfer(t.Context(), f.actor, transfer.ID); err != ErrConflict {
					t.Fatal("incomplete upload finalized", err)
				}
				if err := f.service.UploadChunk(t.Context(), f.actor, ChunkInput{TransferID: transfer.ID, Content: content, Digest: fileDigest(content)}); err != nil {
					t.Fatal(err)
				}
				if _, err := f.service.FinalizeTransfer(t.Context(), f.actor, transfer.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := f.service.FinalizeTransfer(t.Context(), f.actor, transfer.ID); err != ErrForbidden {
					t.Fatal(err)
				}
				if err := f.service.UploadChunk(t.Context(), f.actor, ChunkInput{TransferID: transfer.ID}); err != ErrForbidden {
					t.Fatal(err)
				}
			}
			attempt := f.lease(t)
			if err := f.service.Start(t.Context(), attempt); err != nil {
				t.Fatal(err)
			}
			if direction == "deploy" {
				if err := f.service.CollectSource(t.Context(), attempt, SourceInfo{}); err != ErrForbidden {
					t.Fatal(err)
				}
				if err := f.service.UploadAgentChunk(t.Context(), AgentChunk{Attempt: attempt}); err != ErrForbidden {
					t.Fatal(err)
				}
				if err := f.service.FinalizeCollect(t.Context(), attempt); err != ErrForbidden {
					t.Fatal(err)
				}
			} else if _, err := f.service.DownloadAgentChunk(t.Context(), AgentChunk{Attempt: attempt}); err != ErrForbidden {
				t.Fatal(err)
			}
		})
	}
}

func TestTransferInputAndQuotaFailuresAreBounded(t *testing.T) {
	f, _ := fileFixture(t)
	for _, mutate := range []func(*TransferInput){func(i *TransferInput) { i.Direction = "invalid" }, func(i *TransferInput) { i.File.RelativePath = "../escape" }, func(i *TransferInput) { i.Filename = "bad\nname" }, func(i *TransferInput) { i.File.Mode = 0o777 }} {
		input := transferInput(f, "deploy", []byte("x"))
		mutate(&input)
		if _, err := f.service.CreateTransfer(t.Context(), f.actor, input); err != ErrInvalid {
			t.Fatal(err)
		}
	}
	input := transferInput(f, "deploy", []byte("x"))
	input.File.RootID = "not-allowed"
	if _, err := f.service.CreateTransfer(t.Context(), f.actor, input); err != ErrForbidden {
		t.Fatal(err)
	}
	input = transferInput(f, "deploy", []byte("x"))
	input.NodeIDs = nil
	if _, err := f.service.CreateTransfer(t.Context(), f.actor, input); err != ErrForbidden {
		t.Fatal(err)
	}
	if _, err := f.service.CreateTransfer(t.Context(), Actor{}, transferInput(f, "deploy", nil)); err != ErrForbidden {
		t.Fatal(err)
	}
	if _, err := f.service.FinalizeTransfer(t.Context(), f.actor, "missing"); err != ErrNotFound {
		t.Fatal(err)
	}
	if err := f.service.CancelTransfer(t.Context(), f.actor, "missing"); err != ErrNotFound {
		t.Fatal(err)
	}
	if err := f.service.CancelTransfer(t.Context(), Actor{}, "missing"); err != ErrForbidden {
		t.Fatal(err)
	}
}

func TestDisabledServiceStopsInflightWorkAndStillAcknowledgesResults(t *testing.T) {
	f := testFixture(t)
	f.task(t, "disable")
	attempt := f.lease(t)
	if err := f.service.Start(t.Context(), attempt); err != nil {
		t.Fatal(err)
	}
	f.service.enabled = false
	if cancel, err := f.service.Heartbeat(t.Context(), attempt); err != nil || !cancel {
		t.Fatal(cancel, err)
	}
	if err := f.service.Complete(t.Context(), Completion{Attempt: attempt, Result: Result{State: "canceled"}}); err != nil {
		t.Fatal(err)
	}
}

func TestCompletedResultConflictHasFailureAuditWithoutNewNotification(t *testing.T) {
	f := testFixture(t)
	f.task(t, "conflict")
	attempt := f.lease(t)
	if err := f.service.Start(t.Context(), attempt); err != nil {
		t.Fatal(err)
	}
	input := Completion{Attempt: attempt, Result: Result{State: "succeeded", Stdout: []byte("one")}}
	if err := f.service.Complete(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	input.Result.Stdout = []byte("two")
	if err := f.service.Complete(t.Context(), input); err != ErrConflict {
		t.Fatal(err)
	}
	var count int
	if err := f.database.DB.QueryRow("SELECT COUNT(*) FROM admin_audit_events WHERE action='operation.result.conflict' AND outcome='failure'").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if err := f.database.DB.QueryRow("SELECT COUNT(*) FROM notification_messages WHERE source_kind='operation'").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}

func TestLeaseCapacityAndStaleCapabilitiesDoNotExpandExecution(t *testing.T) {
	f := testFixture(t)
	f.task(t, "one")
	f.task(t, "two")
	first := f.lease(t)
	if next, err := f.service.Lease(t.Context(), LeaseRequest{Proof: NodeProof{Token: f.token}, CapabilityDigest: f.digest}); err != nil || next != nil {
		t.Fatal(next, err)
	}
	if _, err := f.service.Lease(t.Context(), LeaseRequest{Proof: NodeProof{Token: "invalid"}}); err != ErrForbidden {
		t.Fatal(err)
	}
	now := f.service.now().Add(CapabilityLifetime + time.Second)
	f.service.now = func() time.Time { return now }
	if _, err := f.service.Lease(t.Context(), LeaseRequest{Proof: NodeProof{Token: f.token}, CapabilityDigest: f.digest}); err != ErrForbidden {
		t.Fatal(err)
	}
	if err := f.service.Start(t.Context(), first); !errors.Is(err, ErrConflict) && !errors.Is(err, ErrForbidden) {
		t.Fatal(err)
	}
}
