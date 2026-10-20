package store

import "testing"

func TestSnapshotSchemaValidation(t *testing.T) {
	for _, scenario := range []struct {
		name, statement string
		valid           bool
	}{
		{"current", "", true},
		{"future", "INSERT INTO schema_migrations VALUES(9999,'future',CURRENT_TIMESTAMP)", false},
		{"gap", "DELETE FROM schema_migrations WHERE version=2", false},
		{"empty", "DELETE FROM schema_migrations", false},
		{"wrong name", "UPDATE schema_migrations SET name='wrong' WHERE version=1", false},
		{"missing table", "DROP TABLE schema_migrations", false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			database := setupTestDB(t)
			if scenario.statement != "" {
				if _, err := database.DB.Exec(scenario.statement); err != nil {
					t.Fatal(err)
				}
			}
			err := ValidateSnapshotSchema(t.Context(), database.DB)
			if (err == nil) != scenario.valid {
				t.Fatalf("schema validation: %v", err)
			}
		})
	}
}

func TestSnapshotSchemaRejectsMalformedEntryAndClosedStore(t *testing.T) {
	database := setupTestDB(t)
	for _, statement := range []string{"DROP TABLE schema_migrations", "CREATE TABLE schema_migrations(version TEXT,name TEXT)", "INSERT INTO schema_migrations VALUES('bad','bad')"} {
		if _, err := database.DB.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := ValidateSnapshotSchema(t.Context(), database.DB); err == nil {
		t.Fatal("invalid schema entry accepted")
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSnapshotSchema(t.Context(), database.DB); err == nil {
		t.Fatal("closed database accepted")
	}
}
