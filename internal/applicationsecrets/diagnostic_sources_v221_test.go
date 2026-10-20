package applicationsecrets

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/beat/backend/internal/secretbox"
)

// 早期 schema 没有跳身份表时必须直接跳过，建表后必须登记成部署级密钥来源，
// 并且复合键（job#hop）要能被解析回 AAD。
func TestDiagnosticHopSourcesFollowSchemaAndKeyContract(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	ctx := context.Background()

	sources, err := diagnosticSourcesForDatabase(ctx, database)
	if err != nil || sources != nil {
		t.Fatalf("absent namespace must be skipped: %v %v", sources, err)
	}

	if _, err := database.Exec(`CREATE TABLE diagnostic_hop_identities(job_id TEXT, hop_index INTEGER, identity_json BLOB)`); err != nil {
		t.Fatal(err)
	}
	sources, err = diagnosticSourcesForDatabase(ctx, database)
	if err != nil || len(sources) != 1 || sources[0].name != "diagnostic_hop" {
		t.Fatalf("unexpected sources: %v %v", sources, err)
	}

	if _, err := database.Exec(`INSERT INTO diagnostic_hop_identities(job_id, hop_index, identity_json) VALUES('job-7', 2, x'0102')`); err != nil {
		t.Fatal(err)
	}
	rows, err := database.Query(sources[0].query, "", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		t.Fatalf("expected one registered hop: %v", rows.Err())
	}
	resource, err := scanDiagnosticHop(rows)
	if err != nil {
		t.Fatal(err)
	}
	if resource.id != "job-7#2" || resource.required {
		t.Fatalf("unexpected resource: %+v", resource)
	}
	if resource.aad != secretbox.DiagnosticHopAAD("job-7", 2) {
		t.Fatalf("unexpected aad: %+v", resource.aad)
	}
	if string(resource.ciphertext) != "\x01\x02" {
		t.Fatalf("unexpected ciphertext: %q", resource.ciphertext)
	}
}

// 复合键必须是 job#hop 且 hop 在 1..64 之间；其它形状一律判为不健康。
func TestSplitHopKeyRejectsMalformedKeys(t *testing.T) {
	jobID, index, err := splitHopKey("job-7#64")
	if err != nil || jobID != "job-7" || index != 64 {
		t.Fatalf("valid key rejected: %q %d %v", jobID, index, err)
	}
	for _, key := range []string{"", "no-separator", "job-7#", "job-7#abc", "job-7#0", "job-7#65"} {
		if _, _, err := splitHopKey(key); !errors.Is(err, ErrUnhealthy) {
			t.Fatalf("expected %q to be rejected, got %v", key, err)
		}
	}
}
