package store

import (
	"database/sql"
	"errors"
	"testing"
)

func TestAcceleratorVisibilityDefaultsTrueAndPersists(t *testing.T) {
	fixture := newAcceleratorFixture(t)
	visible, err := fixture.inventory.PublicVisible(t.Context(), fixture.node.ID)
	if err != nil || !visible {
		t.Fatalf("default visibility = %v, err = %v, want true", visible, err)
	}
	if err = fixture.inventory.SetVisibility(t.Context(), fixture.actor, fixture.node.ID, false); err != nil {
		t.Fatal(err)
	}
	visible, err = fixture.inventory.PublicVisible(t.Context(), fixture.node.ID)
	if err != nil || visible {
		t.Fatalf("saved visibility = %v, err = %v, want false", visible, err)
	}
	var audits int
	if err = fixture.database.DB.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM admin_audit_events WHERE action = 'accelerator.visibility.update' AND resource_id = ?`,
		fixture.node.ID).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Fatalf("audit rows = %d, want 1", audits)
	}
}

func TestAcceleratorVisibilityRejectsMissingNode(t *testing.T) {
	fixture := newAcceleratorFixture(t)
	err := fixture.inventory.SetVisibility(t.Context(), fixture.actor, "missing-node", true)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing node err = %v, want sql.ErrNoRows", err)
	}
}

func TestAcceleratorPublicVisibilityMigrationAppliesDefaults(t *testing.T) {
	database := setupTestDB(t)
	var columns int
	if err := database.DB.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM pragma_table_info('site_settings') WHERE name = 'show_accelerators' AND dflt_value = '1'`).Scan(&columns); err != nil {
		t.Fatal(err)
	}
	if columns != 1 {
		t.Fatalf("show_accelerators column = %d, want 1", columns)
	}
	var visibilityDefault int
	if err := database.DB.QueryRowContext(t.Context(),
		`SELECT show_accelerators FROM site_settings WHERE id = 1`).Scan(&visibilityDefault); err != nil {
		t.Fatal(err)
	}
	if visibilityDefault != 1 {
		t.Fatalf("default show_accelerators = %d, want 1", visibilityDefault)
	}
	var tables int
	if err := database.DB.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'node_accelerator_visibility'`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 1 {
		t.Fatalf("visibility table count = %d, want 1", tables)
	}
}
