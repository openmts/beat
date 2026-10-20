package applicationsecrets

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/secretbox"
)

func TestSpoolFramesPinOldDataKeysUntilCleanup(t *testing.T) {
	database, directory := lifecycleTestDB(t)
	now := time.Now().UTC()
	service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	seedOperationResources(t, service)
	payload := []byte("x")
	digest := sha256.Sum256(payload)
	frame := secretbox.OperationFrame{ArtifactID: "artifact", Direction: "deploy", TotalBytes: 1, Digest: hex.EncodeToString(digest[:])}
	sealed, err := service.ring.SealOperationFrame(payload, frame)
	if err != nil {
		t.Fatal(err)
	}
	oldID, err := secretbox.OperationFrameKeyID(sealed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec(`INSERT INTO operation_artifact_chunks(id,artifact_id,chunk_index,storage_name,data_key_id,byte_size,digest,created_at)
 VALUES('frame','artifact',0,?,?,1,?,?)`, uuid.NewString(), oldID, frame.Digest, now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartRotation(t.Context()); err != nil {
		t.Fatal(err)
	}
	service.wg.Wait()
	now = now.Add(keyRetention + time.Second)
	if _, err := service.StartRotation(t.Context()); err != nil {
		t.Fatal(err)
	}
	service.wg.Wait()
	if service.ring.ciphers[oldID] == nil {
		t.Fatal("spool key retired while a frame still references it")
	}
	actual, err := service.ring.OpenOperationFrame(sealed, frame)
	if err != nil || string(actual) != "x" {
		t.Fatal(err)
	}
	if err := ValidateSnapshot(t.Context(), filepath.Join(directory, "beat.db"), filepath.Join(directory, "admin-data.key")); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec("DELETE FROM operation_artifact_chunks"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(keyRetention + time.Second)
	if _, err := service.StartRotation(t.Context()); err != nil {
		t.Fatal(err)
	}
	service.wg.Wait()
	if service.ring.ciphers[oldID] != nil {
		t.Fatal("unreferenced predecessor key was not retired")
	}
	if _, err := service.ring.OpenOperationFrame(sealed, frame); err != ErrUnhealthy {
		t.Fatal(err)
	}
	if _, err := service.ring.OpenOperationFrame(nil, frame); err != ErrCiphertext {
		t.Fatal(err)
	}
	if _, err := (&keyring{}).SealOperationFrame(nil, frame); err != ErrUnhealthy {
		t.Fatal(err)
	}
}
