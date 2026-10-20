package store

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

const servicePrincipalMigration = 16

var serviceNow = time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)

func serviceActor() ServicePrincipalActor {
	return ServicePrincipalActor{UserID: "owner-one", SessionID: "session-abcdef123456", RequestID: "request-one",
		IPAddress: "192.0.2.10", UserAgent: "beat-test"}
}

func servicePrincipalFixture(id string) model.ServicePrincipal {
	return model.ServicePrincipal{
		ID:          id,
		Name:        "nightly backup",
		Purpose:     "automation",
		TokenPrefix: model.ServiceTokenPrefix + "abcdefgh",
		TokenHash:   []byte("0123456789abcdef0123456789abcdef"),
		Visibility:  model.ServiceVisibilityPrivate,
		SourceCIDRs: []string{"192.0.2.0/24"},
		Scopes:      []model.ServiceScope{model.ServiceScopeBackupsCreate, model.ServiceScopeNodesRead},
		Resources:   []model.ServiceResource{{Kind: model.ServiceResourceNode, ID: "node-one"}},
		ExpiresAt:   serviceNow.Add(24 * time.Hour),
		Revision:    1,
		CreatedBy:   "owner-one",
		CreatedAt:   serviceNow,
	}
}

func TestServicePrincipalMigrationUpgradesV15AndStaysIdempotent(t *testing.T) {
	database := preServicePrincipalMigrationFixture(t)
	for range 2 {
		if err := applyMigrations(t.Context(), database.DB); err != nil {
			t.Fatal(err)
		}
	}
	var version, entries, scopes, resources int
	if err := database.DB.QueryRow("SELECT MAX(version),COUNT(*) FROM schema_migrations").Scan(&version, &entries); err != nil ||
		version != currentSchemaVersion || entries != currentSchemaVersion {
		t.Fatal("v16 was not registered exactly once", version, entries, err)
	}
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM service_token_scopes").Scan(&scopes); err != nil {
		t.Fatal(err)
	}
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM service_token_resources").Scan(&resources); err != nil {
		t.Fatal(err)
	}
	if scopes != 0 || resources != 0 {
		t.Fatal("migration activated token constraints", scopes, resources)
	}
	if err := ValidateSnapshotSchema(t.Context(), database.DB); err != nil {
		t.Fatal(err)
	}
}

func TestServicePrincipalMigrationFailureRollsBackLedgerAndTables(t *testing.T) {
	database := preServicePrincipalMigrationFixture(t)
	if _, err := database.DB.Exec("CREATE TABLE service_token_scopes(existing TEXT)"); err != nil {
		t.Fatal(err)
	}
	if err := applyMigrations(t.Context(), database.DB); err == nil {
		t.Fatal("conflicting v16 schema was accepted")
	}
	var version, added int
	if err := database.DB.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil || version != servicePrincipalMigration-1 {
		t.Fatal("failed migration advanced ledger", version, err)
	}
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name IN('service_principals','service_token_resources')").Scan(&added); err != nil || added != 0 {
		t.Fatal("failed migration left partial tables", added, err)
	}
}

func TestServicePrincipalRoundTripAndLookup(t *testing.T) {
	database := setupTestDB(t)
	store := NewServicePrincipalStore(database.DB)
	created := servicePrincipalFixture("principal-one")
	if err := store.Create(t.Context(), created, serviceActor()); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.ByHash(t.Context(), created.TokenHash)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Name != created.Name || loaded.Visibility != model.ServiceVisibilityPrivate {
		t.Fatal("stored principal was not retained", loaded)
	}
	if len(loaded.Scopes) != 2 || loaded.Scopes[0] != model.ServiceScopeBackupsCreate || loaded.Scopes[1] != model.ServiceScopeNodesRead {
		t.Fatal("scopes were not retained in canonical order", loaded.Scopes)
	}
	if len(loaded.Resources) != 1 || loaded.Resources[0].ID != "node-one" {
		t.Fatal("resource constraints were not retained", loaded.Resources)
	}
	if len(loaded.SourceCIDRs) != 1 || loaded.SourceCIDRs[0] != "192.0.2.0/24" {
		t.Fatal("source restrictions were not retained", loaded.SourceCIDRs)
	}
	if _, err := store.ByHash(t.Context(), []byte("ffffffffffffffffffffffffffffffff")); !errors.Is(err, ErrServicePrincipalNotFound) {
		t.Fatal("identical hash for a missing token returned a value")
	}
	if _, err := store.ByHash(t.Context(), []byte("short")); !errors.Is(err, ErrServicePrincipalNotFound) {
		t.Fatal("malformed digest was accepted")
	}
	listed, err := store.List(t.Context())
	if err != nil || len(listed) != 1 || listed[0].ID != "principal-one" {
		t.Fatal("list did not return the created principal", listed, err)
	}
	if listed[0].TokenPrefix != created.TokenPrefix {
		t.Fatal("list did not return token metadata", listed[0].TokenPrefix)
	}
	if err := store.Create(t.Context(), created, serviceActor()); err == nil {
		t.Fatal("duplicate token hash was accepted")
	}
}

func TestServicePrincipalCreateValidatesRecords(t *testing.T) {
	database := setupTestDB(t)
	store := NewServicePrincipalStore(database.DB)
	cases := map[string]func(*model.ServicePrincipal){
		"empty id": func(value *model.ServicePrincipal) { value.ID = "" },
		"bad name": func(value *model.ServicePrincipal) { value.Name = "" },
		"long purpose": func(value *model.ServicePrincipal) {
			value.Purpose = string(make([]byte, model.MaximumServiceTokenPurpose+1))
		},
		"bad prefix":     func(value *model.ServicePrincipal) { value.TokenPrefix = "other" },
		"short hash":     func(value *model.ServicePrincipal) { value.TokenHash = []byte("short") },
		"bad visibility": func(value *model.ServicePrincipal) { value.Visibility = "internal" },
		"empty scopes":   func(value *model.ServicePrincipal) { value.Scopes = nil },
		"bad scope":      func(value *model.ServicePrincipal) { value.Scopes = []model.ServiceScope{"nodes:delete"} },
		"bad resource": func(value *model.ServicePrincipal) {
			value.Resources = []model.ServiceResource{{Kind: "cluster", ID: "one"}}
		},
		"empty resource": func(value *model.ServicePrincipal) {
			value.Resources = []model.ServiceResource{{Kind: model.ServiceResourceNode}}
		},
		"zero expiry":   func(value *model.ServicePrincipal) { value.ExpiresAt = time.Time{} },
		"zero created":  func(value *model.ServicePrincipal) { value.CreatedAt = time.Time{} },
		"empty creator": func(value *model.ServicePrincipal) { value.CreatedBy = "" },
		"bad revision":  func(value *model.ServicePrincipal) { value.Revision = 0 },
	}
	for name, mutate := range cases {
		value := servicePrincipalFixture("principal-" + name)
		value.TokenHash = []byte(fmt.Sprintf("%032d", len(name)))
		mutate(&value)
		if err := store.Create(t.Context(), value, serviceActor()); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
}

func preServicePrincipalMigrationFixture(t *testing.T) *SQLiteStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pre-v16.db")
	if err := secureSQLiteFile(path); err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("sqlite", sqliteConnectionString(path))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.Exec("CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,applied_at DATETIME NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	for _, migration := range sqliteMigrations {
		if migration.version >= servicePrincipalMigration {
			break
		}
		if err := applyMigration(t.Context(), database, migration); err != nil {
			t.Fatal(err)
		}
	}
	return &SQLiteStore{DB: database, path: path}
}
