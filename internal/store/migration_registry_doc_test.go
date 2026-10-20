package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// B8 的教训：commercial-parity-implementation-sequence.md 是迁移号与备份格式的唯一登记册，
// 但它曾经停在「预览 v14」，而线上早已是 v28，导致后续批次无法判断哪个号还能用。
// 这里把登记册与代码清单钉在一起：改迁移号却不同步文档，测试直接失败。
func TestMigrationRegistryDocumentMatchesInventory(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "beat", "commercial-parity-implementation-sequence.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration registry: %v", err)
	}
	document := string(raw)

	current := fmt.Sprintf("`currentSchemaVersion = %d`", currentSchemaVersion)
	if !strings.Contains(document, current) {
		t.Errorf("registry does not record %s", current)
	}
	next := fmt.Sprintf("schema `v%d`", currentSchemaVersion+1)
	if !strings.Contains(document, next) {
		t.Errorf("registry does not record the next free schema number %s", next)
	}
	if len(sqliteMigrations) != currentSchemaVersion {
		t.Fatalf("inventory has %d entries for schema version %d", len(sqliteMigrations), currentSchemaVersion)
	}
	for index, migration := range sqliteMigrations {
		if migration.version != index+1 {
			t.Fatalf("inventory is not consecutive at %d: version %d", index+1, migration.version)
		}
		if !strings.Contains(document, "`"+migration.name+"`") {
			t.Errorf("registry does not record v%d migration name %q", migration.version, migration.name)
		}
	}
}
