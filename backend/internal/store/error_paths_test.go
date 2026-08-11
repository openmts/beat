package store

import (
	"context"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func closedSQLiteStore(t *testing.T) *SQLiteStore {
	t.Helper()
	s := setupTestDB(t)
	if err := s.Close(); err != nil {
		t.Fatalf("close sqlite store: %v", err)
	}
	return s
}

func TestGroupStoreClosedDatabase(t *testing.T) {
	s := closedSQLiteStore(t)
	ctx := context.Background()
	store := NewGroupStore(s.DB)

	tests := []struct {
		name string
		call func() error
	}{
		{"create", func() error { _, err := store.CreateGroup(ctx, "group"); return err }},
		{"list", func() error { _, err := store.ListGroups(ctx); return err }},
		{"get", func() error { _, err := store.GetGroup(ctx, "id"); return err }},
		{"update", func() error { return store.UpdateGroup(ctx, "id", "group") }},
		{"delete", func() error { return store.DeleteGroup(ctx, "id") }},
		{"default", func() error { _, err := store.GetDefaultGroup(ctx); return err }},
		{"set default", func() error { return store.SetDefaultGroup(ctx, "id") }},
		{"sort", func() error { return store.UpdateSortOrder(ctx, []string{"id"}) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); err == nil {
				t.Fatal("expected closed database error")
			}
		})
	}
}

func TestNodeStoreClosedDatabase(t *testing.T) {
	s := closedSQLiteStore(t)
	ctx := context.Background()
	store := NewNodeStore(s.DB)

	if _, err := store.ListNodes(ctx, ""); err == nil {
		t.Fatal("expected list error")
	}
	if _, err := store.GetNode(ctx, "id"); err == nil {
		t.Fatal("expected get error")
	}
	if _, err := store.GetNodeByName(ctx, "name"); err == nil {
		t.Fatal("expected get by name error")
	}
	if _, err := store.UpdateNode(ctx, "id", NodeUpdate{}); err == nil {
		t.Fatal("expected update error")
	}
	if err := store.DeleteNode(ctx, "id"); err == nil {
		t.Fatal("expected delete error")
	}
	if _, err := store.UpsertNode(ctx, "name", "host", 22); err == nil {
		t.Fatal("expected upsert error")
	}
	if _, err := store.ListOnlineNodes(ctx); err == nil {
		t.Fatal("expected online list error")
	}
	if _, err := store.defaultGroupID(ctx); err == nil {
		t.Fatal("expected default group lookup error")
	}
	metrics, err := store.GetNodeMetrics(ctx, "node", "cpu", time.Time{}, time.Now())
	if err != nil || metrics != nil {
		t.Fatalf("legacy metrics result = %v, %v; want nil, nil", metrics, err)
	}
}

func TestSSHKeyStoreClosedDatabase(t *testing.T) {
	s := closedSQLiteStore(t)
	ctx := context.Background()
	store := NewSSHKeyStore(s.DB)

	if _, err := store.ListSSHKeys(ctx); err == nil {
		t.Fatal("expected list error")
	}
	if _, err := store.CreateSSHKey(ctx, "key", "ed25519", "public", "private", "fingerprint"); err == nil {
		t.Fatal("expected create error")
	}
	if err := store.DeleteSSHKey(ctx, "id"); err == nil {
		t.Fatal("expected delete error")
	}
}

func TestAlertStoresClosedDatabase(t *testing.T) {
	s := closedSQLiteStore(t)
	ctx := context.Background()
	rules := NewAlertRuleStore(s.DB)
	channels := NewAlertChannelStore(s.DB)
	events := NewAlertEventStore(s.DB)
	rule := &model.AlertRule{}
	channel := &model.AlertChannel{}
	event := &model.AlertEvent{}

	ruleCalls := []struct {
		name string
		call func() error
	}{
		{"list", func() error { _, err := rules.ListAlertRules(ctx); return err }},
		{"create", func() error { _, err := rules.CreateAlertRule(ctx, rule); return err }},
		{"update", func() error { _, err := rules.UpdateAlertRule(ctx, "id", rule); return err }},
		{"delete", func() error { return rules.DeleteAlertRule(ctx, "id") }},
		{"enabled", func() error { _, err := rules.ListEnabledRules(ctx); return err }},
	}
	for _, tt := range ruleCalls {
		t.Run("rule_"+tt.name, func(t *testing.T) {
			if err := tt.call(); err == nil {
				t.Fatal("expected closed database error")
			}
		})
	}

	channelCalls := []struct {
		name string
		call func() error
	}{
		{"list", func() error { _, err := channels.ListAlertChannels(ctx); return err }},
		{"create", func() error { _, err := channels.CreateAlertChannel(ctx, channel); return err }},
		{"update", func() error { _, err := channels.UpdateAlertChannel(ctx, "id", channel); return err }},
		{"delete", func() error { return channels.DeleteAlertChannel(ctx, "id") }},
		{"enabled", func() error { _, err := channels.ListEnabledChannels(ctx); return err }},
	}
	for _, tt := range channelCalls {
		t.Run("channel_"+tt.name, func(t *testing.T) {
			if err := tt.call(); err == nil {
				t.Fatal("expected closed database error")
			}
		})
	}

	eventCalls := []struct {
		name string
		call func() error
	}{
		{"list", func() error { _, err := events.ListAlertEvents(ctx); return err }},
		{"create", func() error { return events.CreateEvent(ctx, event) }},
		{"active", func() error { _, err := events.GetActiveEvent(ctx, "rule", "node"); return err }},
		{"update", func() error { return events.UpdateEvent(ctx, event) }},
	}
	for _, tt := range eventCalls {
		t.Run("event_"+tt.name, func(t *testing.T) {
			if err := tt.call(); err == nil {
				t.Fatal("expected closed database error")
			}
		})
	}
}

func TestAdminStoreClosedDatabase(t *testing.T) {
	s := closedSQLiteStore(t)
	ctx := t.Context()
	adminStore := NewAdminStore(s.DB)
	now := model.NowUTC()
	user := testAdminUser("owner", model.AdminRoleOwner)
	session := model.AdminSession{ID: "session", UserID: user.ID, TokenHash: []byte("hash")}
	event := model.AdminAuditEvent{ID: "event"}
	calls := []func() error{
		func() error { _, err := adminStore.CountUsers(ctx); return err },
		func() error { return adminStore.CreateUser(ctx, &user) },
		func() error { _, err := adminStore.GetUserByUsername(ctx, user.Username); return err },
		func() error { _, err := adminStore.GetUserByID(ctx, user.ID); return err },
		func() error { _, err := adminStore.ListUsers(ctx); return err },
		func() error { return adminStore.UpdateUser(ctx, user.ID, "Owner", model.AdminRoleOwner, true) },
		func() error { return adminStore.DeleteUser(ctx, user.ID) },
		func() error { return adminStore.UpdatePassword(ctx, user.ID, "hash", now) },
		func() error { return adminStore.UpdateTOTP(ctx, user.ID, nil, nil) },
		func() error { return adminStore.TouchUserLogin(ctx, user.ID, now) },
		func() error { return adminStore.Bootstrap(ctx, &user, &session) },
		func() error { return adminStore.CreateSession(ctx, &session) },
		func() error { _, err := adminStore.GetSessionByTokenHash(ctx, session.TokenHash); return err },
		func() error { _, err := adminStore.GetSessionByID(ctx, session.ID); return err },
		func() error { _, err := adminStore.ListSessions(ctx, user.ID); return err },
		func() error { return adminStore.TouchSession(ctx, session.ID, now, now) },
		func() error { return adminStore.MarkSessionReauthenticated(ctx, session.ID, now) },
		func() error { return adminStore.RevokeSession(ctx, session.ID, now) },
		func() error { _, err := adminStore.RevokeOtherSessions(ctx, user.ID, session.ID, now); return err },
		func() error { return adminStore.RevokeUserSessions(ctx, user.ID, session.ID, now) },
		func() error { _, err := adminStore.DeleteExpiredSessions(ctx, now); return err },
		func() error { return adminStore.CreateAuditEvent(ctx, &event) },
		func() error { _, err := adminStore.ListAuditEvents(ctx, model.AuditFilter{}); return err },
		func() error { _, err := adminStore.CleanupAuditEventsBefore(ctx, now); return err },
	}
	assertStoreCallsFail(t, calls)
}

func TestBackupStoreClosedDatabase(t *testing.T) {
	s := closedSQLiteStore(t)
	ctx := t.Context()
	backupStore := NewBackupStore(s.DB)
	record := model.BackupRecord{ID: "backup"}
	calls := []func() error{
		func() error { return backupStore.Create(ctx, &record) },
		func() error { return backupStore.Update(ctx, &record) },
		func() error { _, err := backupStore.Get(ctx, record.ID); return err },
		func() error { _, err := backupStore.List(ctx); return err },
		func() error { return backupStore.Delete(ctx, record.ID) },
	}
	assertStoreCallsFail(t, calls)
}

func assertStoreCallsFail(t *testing.T, calls []func() error) {
	t.Helper()
	for index, call := range calls {
		if err := call(); err == nil {
			t.Fatalf("closed store call %d returned nil error", index)
		}
	}
}
