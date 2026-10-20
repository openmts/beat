package backup

import (
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// makeFutureSchemaArchive 模拟外部新版本归档，不要求当前 writer 接受损坏的本地账本。
func makeFutureSchemaArchive(t *testing.T, archive string) {
	t.Helper()
	validated, err := validateArchive(t.Context(), archive, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(validated.root) })
	path := filepath.Join(validated.root, sqliteEntry)
	uri := (&url.URL{Scheme: "file", Path: path, RawQuery: "mode=rw"}).String()
	database, err := sql.Open("sqlite", uri)
	if err != nil {
		t.Fatal(err)
	}
	_, updateErr := database.Exec("INSERT INTO schema_migrations(version,name,applied_at) VALUES(9999,'future',CURRENT_TIMESTAMP)")
	closeErr := database.Close()
	if updateErr != nil || closeErr != nil {
		t.Fatal(updateErr, closeErr)
	}
	size, digest, err := fileDigest(path)
	if err != nil {
		t.Fatal(err)
	}
	manifest := validated.manifest
	manifest.PayloadSizes[sqliteEntry], manifest.Checksums[sqliteEntry] = size, digest
	if err := writeMetadataFiles(validated.root, manifest); err != nil {
		t.Fatal(err)
	}
	payloads := make([]string, 0, len(manifest.PayloadSizes))
	for name := range manifest.PayloadSizes {
		payloads = append(payloads, name)
	}
	sort.Strings(payloads)
	if err := writeArchive(archive+".future-partial", archive, validated.root, payloads); err != nil {
		t.Fatal(err)
	}
}
