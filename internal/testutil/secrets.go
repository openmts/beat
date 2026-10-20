//go:build linux

package testutil

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/beat/backend/internal/applicationsecrets"
)

// NewSecrets 给测试提供真实数据库密钥注册表，避免生产路径兼容测试密文。
func NewSecrets(t testing.TB, database *sql.DB) *applicationsecrets.Service {
	t.Helper()
	return OpenSecrets(t, database, t.TempDir())
}

func OpenSecrets(t testing.TB, database *sql.DB, directory string) *applicationsecrets.Service {
	t.Helper()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	service, err := applicationsecrets.Open(context.Background(), applicationsecrets.Config{Database: database, DataDir: directory})
	if err != nil {
		t.Fatalf("initialize application secret fixture: %v", err)
	}
	t.Cleanup(service.Close)
	return service
}
