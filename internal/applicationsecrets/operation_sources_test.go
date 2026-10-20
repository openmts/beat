package applicationsecrets

import (
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/secretbox"
)

func TestOperationResourcesRotateAndValidateSnapshots(t *testing.T) {
	database, directory := lifecycleTestDB(t)
	service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	seedOperationResources(t, service)
	sources, err := operationSourcesForDatabase(t.Context(), database.DB)
	if err != nil || len(sources) != 6 {
		t.Fatal(len(sources), err)
	}
	for _, source := range sources {
		resources, err := source.readBatch(t.Context(), database.DB, "")
		if err != nil || len(resources) != 1 {
			t.Fatalf("source %s: %v", source.name, err)
		}
		if _, err := service.ring.Open(resources[0].ciphertext, resources[0].aad); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.StartRotation(t.Context()); err != nil {
		t.Fatal(err)
	}
	service.wg.Wait()
	for _, source := range sources {
		resources, err := source.readBatch(t.Context(), database.DB, "")
		if err != nil {
			t.Fatal(err)
		}
		id, err := secretbox.EnvelopeDataKeyID(resources[0].ciphertext)
		if err != nil || id != service.ring.active {
			t.Fatalf("source %s did not rotate: %v", source.name, err)
		}
	}
	if err := ValidateSnapshot(t.Context(), filepath.Join(directory, "beat.db"), filepath.Join(directory, "admin-data.key")); err != nil {
		t.Fatal(err)
	}
}

func seedOperationResources(t *testing.T, service *Service) {
	t.Helper()
	for _, statement := range []string{
		`INSERT INTO nodes(id,name,host,port,created_at,updated_at) VALUES('op-node','op-node','127.0.0.1',22,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`,
		`INSERT INTO operation_policies(id,name,configuration_json,version,creator_id,created_at,updated_at) VALUES('policy','Policy','{}',1,'owner',1,1)`,
		`INSERT INTO operation_snippets(id,name,visibility,owner_id,current_version,created_at,updated_at) VALUES('snippet','Snippet','private','owner',1,1,1)`,
	} {
		if _, err := service.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	for _, resource := range []struct {
		query string
		aad   secretbox.ResourceAAD
	}{
		{`INSERT INTO operation_snippet_versions(id,snippet_id,version,shell_family,content_encrypted,fingerprint,created_at) VALUES('version','snippet',1,'sh',?,'digest',1)`, secretbox.OperationSnippetAAD("snippet", 1, "sh")},
		{`INSERT INTO operation_tasks(id,dedupe_key,request_fingerprint,kind,payload_encrypted,policy_id,policy_snapshot,actor_id,timeout_seconds,output_bytes,expires_at,created_at,updated_at) VALUES('task','dedupe','digest','shell',?,'policy','{}','owner',1,1,1,1,1)`, secretbox.OperationTaskAAD("task")},
		{`INSERT INTO operation_artifacts(id,storage_name,metadata_encrypted,byte_size,digest,scan_status,state,actor_id,expires_at,created_at,updated_at) VALUES('artifact','random',?,1,'digest','pending','uploading','owner',1,1,1)`, secretbox.OperationArtifactAAD("artifact")},
		{`INSERT INTO operation_transfers(id,direction,path_encrypted,root_id,byte_size,digest,mode,state,actor_id,expires_at,created_at,updated_at) VALUES('transfer','deploy',?,'inbox',1,'digest',384,'queued','owner',1,1,1)`, secretbox.OperationTransferAAD("transfer")},
	} {
		sealed, err := service.ring.Seal([]byte("private-operation-value"), resource.aad)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.db.Exec(resource.query, sealed); err != nil {
			t.Fatal(err)
		}
	}
	seedOperationOutput(t, service)
}

func seedOperationOutput(t *testing.T, service *Service) {
	t.Helper()
	if _, err := service.db.Exec(`INSERT INTO operation_task_targets(id,task_id,node_id,node_name,state,created_at,updated_at) VALUES('target','task','op-node','Node','running',1,1)`); err != nil {
		t.Fatal(err)
	}
	stdout, err := service.ring.Seal([]byte("private stdout"), secretbox.OperationOutputAAD("attempt", "stdout"))
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := service.ring.Seal([]byte("private stderr"), secretbox.OperationOutputAAD("attempt", "stderr"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.db.Exec(`INSERT INTO operation_attempts(id,target_id,attempt_number,nonce_hash,state,capability_digest,lease_expires_at,created_at,stdout_encrypted,stderr_encrypted)
		VALUES('attempt','target',1,zeroblob(32),'running','digest',1,1,?,?)`, stdout, stderr); err != nil {
		t.Fatal(err)
	}
}

func TestIncompleteOperationSchemaFailsClosed(t *testing.T) {
	database, _ := lifecycleTestDB(t)
	if _, err := database.DB.Exec("ALTER TABLE operation_transfers RENAME TO fixture_missing_operations_transfers"); err != nil {
		t.Fatal(err)
	}
	if _, err := operationSourcesForDatabase(t.Context(), database.DB); err != ErrUnhealthy {
		t.Fatal(err)
	}
}

func TestOperationFingerprintUsesCurrentAndRetainedKeys(t *testing.T) {
	database, directory := lifecycleTestDB(t)
	service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	fingerprint, err := service.ring.OperationFingerprint([]byte("content"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartRotation(t.Context()); err != nil {
		t.Fatal(err)
	}
	service.wg.Wait()
	if !service.ring.VerifyOperationFingerprint([]byte("content"), fingerprint) {
		t.Fatal("retained key lost")
	}
	if service.ring.VerifyOperationFingerprint([]byte("other"), fingerprint) || service.ring.VerifyOperationFingerprint(nil, "bad") || service.ring.VerifyOperationFingerprint(nil, "missing:hash") {
		t.Fatal("invalid fingerprint accepted")
	}
	if _, err := (&keyring{}).OperationFingerprint(nil); err != ErrUnhealthy {
		t.Fatal(err)
	}
}
