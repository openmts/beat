package remoteoperations

import (
	"context"
	"testing"

	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/secretbox"
)

type cryptoFaultSecrets struct {
	base                 secretbox.ResourceService
	seal, open           string
	frameSeal, frameOpen bool
}

type cryptoFaultAccess struct {
	secretbox.ResourceAccess
	fault cryptoFaultSecrets
}

func (fault cryptoFaultSecrets) WithWrite(ctx context.Context, write func(secretbox.ResourceAccess) error) error {
	return fault.base.WithWrite(ctx, func(access secretbox.ResourceAccess) error {
		return write(cryptoFaultAccess{ResourceAccess: access, fault: fault})
	})
}

func (fault cryptoFaultSecrets) OpenResource(ctx context.Context, value []byte, aad secretbox.ResourceAAD) ([]byte, error) {
	return fault.base.OpenResource(ctx, value, aad)
}

func (access cryptoFaultAccess) Seal(value []byte, aad secretbox.ResourceAAD) ([]byte, error) {
	if aad.Namespace+":"+aad.Field == access.fault.seal {
		return nil, ErrUnavailable
	}
	return access.ResourceAccess.Seal(value, aad)
}

func (access cryptoFaultAccess) Open(value []byte, aad secretbox.ResourceAAD) ([]byte, error) {
	if aad.Namespace+":"+aad.Field == access.fault.open {
		return nil, ErrUnavailable
	}
	return access.ResourceAccess.Open(value, aad)
}

func (access cryptoFaultAccess) OperationFingerprint(value []byte) (string, error) {
	return access.ResourceAccess.(secretbox.OperationFingerprinter).OperationFingerprint(value)
}

func (access cryptoFaultAccess) VerifyOperationFingerprint(value []byte, digest string) bool {
	return access.ResourceAccess.(secretbox.OperationFingerprinter).VerifyOperationFingerprint(value, digest)
}

func (access cryptoFaultAccess) SealOperationFrame(value []byte, frame secretbox.OperationFrame) ([]byte, error) {
	if access.fault.frameSeal {
		return nil, ErrUnavailable
	}
	return access.ResourceAccess.(secretbox.OperationSpoolAccess).SealOperationFrame(value, frame)
}

func (access cryptoFaultAccess) OpenOperationFrame(value []byte, frame secretbox.OperationFrame) ([]byte, error) {
	if access.fault.frameOpen {
		return nil, ErrUnavailable
	}
	return access.ResourceAccess.(secretbox.OperationSpoolAccess).OpenOperationFrame(value, frame)
}

func installCryptoFault(t *testing.T, f fixture, fault cryptoFaultSecrets) {
	t.Helper()
	fault.base = f.service.secrets
	f.service.secrets = fault
	queue, err := notificationqueue.NewService(notificationqueue.Config{Database: f.database.DB, Secrets: fault})
	if err != nil {
		t.Fatal(err)
	}
	f.service.queue = queue
}

func TestResultCryptoFailuresRollBackStateAndNotification(t *testing.T) {
	for _, field := range []string{"operation_attempt:stdout", "operation_attempt:stderr", "notification_message:payload"} {
		t.Run(field, func(t *testing.T) {
			f := testFixture(t)
			f.task(t, "crypto")
			attempt := f.lease(t)
			if err := f.service.Start(t.Context(), attempt); err != nil {
				t.Fatal(err)
			}
			installCryptoFault(t, f, cryptoFaultSecrets{seal: field})
			if err := f.service.Complete(t.Context(), Completion{Attempt: attempt, Result: Result{State: "succeeded", Stdout: []byte("private")}}); err == nil {
				t.Fatal("encryption failure acknowledged")
			}
			var state string
			if err := f.database.DB.QueryRow("SELECT state FROM operation_attempts WHERE id=?", attempt.ID).Scan(&state); err != nil || state != "running" {
				t.Fatal(state, err)
			}
		})
	}
}

func TestOutputCryptoFailureDoesNotExposeEitherStream(t *testing.T) {
	for _, field := range []string{"operation_attempt:stdout", "operation_attempt:stderr"} {
		t.Run(field, func(t *testing.T) {
			f := testFixture(t)
			id := f.task(t, "output")
			attempt := f.lease(t)
			if err := f.service.Start(t.Context(), attempt); err != nil {
				t.Fatal(err)
			}
			result := Completion{Attempt: attempt, Result: Result{State: "succeeded", Stdout: []byte("out"), Stderr: []byte("err")}}
			if err := f.service.Complete(t.Context(), result); err != nil {
				t.Fatal(err)
			}
			task, err := f.service.GetTask(t.Context(), f.actor, id)
			if err != nil {
				t.Fatal(err)
			}
			installCryptoFault(t, f, cryptoFaultSecrets{open: field})
			if output, err := f.service.TargetOutput(t.Context(), f.actor, TargetRequest{TaskID: id, TargetID: task.Targets[0].ID}); err == nil || len(output.Stdout) != 0 || len(output.Stderr) != 0 {
				t.Fatal("output escaped failed authentication")
			}
			if err := f.service.Complete(t.Context(), result); err == nil {
				t.Fatal("unverified duplicate acknowledged")
			}
		})
	}
}

func TestTransferCryptoFailuresKeepArtifactsUnavailable(t *testing.T) {
	for _, fault := range []cryptoFaultSecrets{{seal: "operation_artifact:metadata"}, {seal: "operation_transfer:path"}} {
		f, _ := fileFixture(t)
		installCryptoFault(t, f, fault)
		if _, err := f.service.CreateTransfer(t.Context(), f.actor, transferInput(f, "deploy", []byte("x"))); err == nil {
			t.Fatal("unsealed transfer created")
		}
	}
	f, _ := fileFixture(t)
	transfer, err := f.service.CreateTransfer(t.Context(), f.actor, transferInput(f, "deploy", []byte("x")))
	if err != nil {
		t.Fatal(err)
	}
	installCryptoFault(t, f, cryptoFaultSecrets{frameSeal: true})
	if err := f.service.UploadChunk(t.Context(), f.actor, ChunkInput{TransferID: transfer.ID, Content: []byte("x"), Digest: fileDigest([]byte("x"))}); err == nil {
		t.Fatal("unsealed frame acknowledged")
	}
}

func TestSnippetAndPayloadOpenFailuresFailClosed(t *testing.T) {
	for _, field := range []string{"operation_snippet:content", "operation_task:payload"} {
		t.Run(field, func(t *testing.T) {
			f := testFixture(t)
			id := f.task(t, "content")
			snippet, err := f.service.SaveSnippet(t.Context(), f.actor, SnippetInput{Name: "Draft", Visibility: "private", ShellFamily: "sh", Content: []byte("content")})
			if err != nil {
				t.Fatal(err)
			}
			installCryptoFault(t, f, cryptoFaultSecrets{open: field})
			if field == "operation_task:payload" {
				if _, err := f.service.TaskContent(t.Context(), f.actor, id); err == nil {
					t.Fatal("task plaintext exposed")
				}
				if _, err := f.service.Lease(t.Context(), LeaseRequest{Proof: NodeProof{Token: f.token}, CapabilityDigest: f.digest}); err == nil {
					t.Fatal("unauthenticated task dispatched")
				}
			} else if _, err := f.service.SnippetContent(t.Context(), f.actor, snippet); err == nil {
				t.Fatal("snippet plaintext exposed")
			}
		})
	}
}

func TestMalformedArtifactMetadataCannotStartDownload(t *testing.T) {
	f := transferFault(t, "download")
	err := f.service.secrets.WithWrite(t.Context(), func(access secretbox.ResourceAccess) error {
		var id string
		if err := f.database.DB.QueryRow("SELECT artifact_id FROM operation_transfers WHERE id=?", f.transfer.ID).Scan(&id); err != nil {
			return err
		}
		sealed, err := access.Seal([]byte("invalid metadata"), secretbox.OperationArtifactAAD(id))
		if err != nil {
			return err
		}
		_, err = f.database.DB.Exec("UPDATE operation_artifacts SET metadata_encrypted=? WHERE id=?", sealed, id)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.service.DownloadTransfer(t.Context(), f.actor, DownloadInput{ID: f.transfer.ID, Consume: discardDownload}); err != ErrUnavailable {
		t.Fatal(err)
	}
}
