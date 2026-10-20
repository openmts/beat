package remoteoperations

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/beat/backend/internal/secretbox"
)

type unreadScanner struct{}

func (unreadScanner) Scan(context.Context, io.Reader) error { return nil }

type callbackScanner struct{ after func() }

func (scanner callbackScanner) Scan(_ context.Context, reader io.Reader) error {
	if _, err := io.Copy(io.Discard, reader); err != nil {
		return err
	}
	scanner.after()
	return nil
}

func TestFinalDigestAndRequiredScannerFailuresNeverDispatch(t *testing.T) {
	for _, kind := range []string{"digest", "unread", "frame"} {
		t.Run(kind, func(t *testing.T) {
			f, _ := fileFixture(t)
			content := []byte("bytes")
			input := transferInput(f, "deploy", content)
			if kind == "digest" {
				input.File.Digest = fileDigest([]byte("other"))
			}
			transfer, err := f.service.CreateTransfer(t.Context(), f.actor, input)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.service.UploadChunk(t.Context(), f.actor, ChunkInput{TransferID: transfer.ID, Content: content, Digest: fileDigest(content)}); err != nil {
				t.Fatal(err)
			}
			if kind == "unread" {
				f.service.spool.scanner = unreadScanner{}
			}
			if kind == "frame" {
				installCryptoFault(t, f, cryptoFaultSecrets{frameOpen: true})
			}
			if _, err := f.service.FinalizeTransfer(t.Context(), f.actor, transfer.ID); err == nil {
				t.Fatal("invalid artifact finalized")
			}
			var count int
			if err := f.database.DB.QueryRow("SELECT COUNT(*) FROM operation_tasks").Scan(&count); err != nil || count != 0 {
				t.Fatal("artifact failure created work", count, err)
			}
		})
	}
}

func TestScannerRechecksAuthorityBeforeFinalization(t *testing.T) {
	f, _ := fileFixture(t)
	transfer := createUploaded(t, f, []byte("bytes"))
	f.service.spool.scanner = callbackScanner{after: func() { f.sql(t, "UPDATE admin_sessions SET revoked_at=?", f.service.now()) }}
	if _, err := f.service.FinalizeTransfer(t.Context(), f.actor, transfer.ID); !errors.Is(err, ErrForbidden) {
		t.Fatal(err)
	}
	var state string
	if err := f.database.DB.QueryRow("SELECT state FROM operation_artifacts").Scan(&state); err != nil || state != "uploading" {
		t.Fatal("revoked session finalized content", state, err)
	}
}

func TestScannerRechecksCurrentOwnerPolicyBeforeDispatch(t *testing.T) {
	f, _ := fileFixture(t)
	transfer := createUploaded(t, f, []byte("bytes"))
	f.service.spool.scanner = callbackScanner{after: func() { f.sql(t, "UPDATE operation_policies SET enabled=0") }}
	if _, err := f.service.FinalizeTransfer(t.Context(), f.actor, transfer.ID); !errors.Is(err, ErrForbidden) {
		t.Fatal(err)
	}
	var count int
	if err := f.database.DB.QueryRow("SELECT COUNT(*) FROM operation_tasks").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}

func TestCorruptPayloadsCannotBeLeasedOrViewed(t *testing.T) {
	f := testFixture(t)
	id := f.task(t, "corrupt")
	err := f.service.secrets.WithWrite(t.Context(), func(access secretbox.ResourceAccess) error {
		sealed, err := access.Seal([]byte("not json"), secretbox.OperationTaskAAD(id))
		if err != nil {
			return err
		}
		_, err = f.database.DB.Exec("UPDATE operation_tasks SET payload_encrypted=? WHERE id=?", sealed, id)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Lease(t.Context(), LeaseRequest{Proof: NodeProof{Token: f.token}, CapabilityDigest: f.digest}); err != ErrUnavailable {
		t.Fatal(err)
	}
	if _, err := f.service.TaskContent(t.Context(), f.actor, id); err != ErrUnavailable {
		t.Fatal(err)
	}
}

func TestClosedServiceWillNotStartAnotherScanner(t *testing.T) {
	f, _ := fileFixture(t)
	transfer := createUploaded(t, f, []byte("bytes"))
	f.service.Close()
	if _, err := f.service.FinalizeTransfer(t.Context(), f.actor, transfer.ID); err != ErrUnavailable {
		t.Fatal(err)
	}
}

func TestTightenedScanPolicyBlocksDispatchUntilRescanWithoutNewTask(t *testing.T) {
	f, _ := fileFixture(t)
	transfer := createUploaded(t, f, []byte("bytes"))
	first, err := f.service.FinalizeTransfer(t.Context(), f.actor, transfer.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.policy.RequireScan = true
	if _, err := f.service.SavePolicy(t.Context(), f.actor, PolicyInput{ID: f.policyID, Version: 2, Name: "Require scan", Policy: f.policy, NodeIDs: []string{f.nodeID}}); err != nil {
		t.Fatal(err)
	}
	if lease, err := f.service.Lease(t.Context(), LeaseRequest{Proof: NodeProof{Token: f.token}, CapabilityDigest: f.digest}); err != nil || lease != nil {
		t.Fatal("unscanned artifact dispatched", err)
	}
	if _, err := f.service.FinalizeTransfer(t.Context(), f.actor, transfer.ID); !errors.Is(err, ErrForbidden) {
		t.Fatal(err)
	}
	f.service.spool.scanner = scanAll{}
	second, err := f.service.FinalizeTransfer(t.Context(), f.actor, transfer.ID)
	if err != nil || first != second {
		t.Fatal("rescan created a different task", first, second, err)
	}
	var count int
	if err := f.database.DB.QueryRow("SELECT COUNT(*) FROM operation_tasks").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}

func TestCollectedDownloadAvailabilityTracksScanAndExpiry(t *testing.T) {
	f := transferFault(t, "download")
	item, err := f.service.GetTransfer(t.Context(), f.actor, f.transfer.ID)
	if err != nil || !item.DownloadAvailable {
		t.Fatal(item, err)
	}
	f.policy.RequireScan = true
	if _, err := f.service.SavePolicy(t.Context(), f.actor, PolicyInput{ID: f.policyID, Version: 2, Name: "Require scan", Policy: f.policy, NodeIDs: []string{f.nodeID}}); err != nil {
		t.Fatal(err)
	}
	item, err = f.service.GetTransfer(t.Context(), f.actor, f.transfer.ID)
	if err != nil || item.DownloadAvailable {
		t.Fatal("unscanned download exposed", item, err)
	}
	f.service.spool.scanner = scanAll{}
	if _, err := f.service.FinalizeTransfer(t.Context(), f.actor, f.transfer.ID); err != nil {
		t.Fatal(err)
	}
	items, err := f.service.ListTransfers(t.Context(), f.actor)
	if err != nil || len(items) != 1 || !items[0].DownloadAvailable {
		t.Fatal(items, err)
	}
	f.sql(t, "UPDATE operation_artifacts SET expires_at=0")
	item, err = f.service.GetTransfer(t.Context(), f.actor, f.transfer.ID)
	if err != nil || item.DownloadAvailable {
		t.Fatal("expired download exposed", item, err)
	}
}
