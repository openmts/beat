package nodelifecycle

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
)

type fixture struct {
	service *Service
	db      *sql.DB
	queue   *notificationqueue.Service
	actor   Actor
	now     time.Time
}

func lifecycleFixture(t *testing.T) *fixture {
	t.Helper()
	database, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	f := &fixture{db: database.DB, now: time.Date(2026, 1, 31, 12, 0, 0, 0, time.UTC), actor: Actor{UserID: "life-owner", SessionID: "life-session"}}
	secrets := testutil.NewSecrets(t, f.db)
	f.queue, err = notificationqueue.NewService(notificationqueue.Config{Database: f.db, Secrets: secrets, Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.queue.Close)
	f.service, err = NewService(Config{Database: f.db, Queue: f.queue, Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	admins := store.NewAdminStore(f.db)
	if err = admins.CreateUser(t.Context(), &model.AdminUser{ID: f.actor.UserID, Username: "life-owner", DisplayName: "Owner", Role: "owner", Enabled: true, PasswordHash: "fixture", PasswordChangedAt: f.now, CreatedAt: f.now, UpdatedAt: f.now}); err != nil {
		t.Fatal(err)
	}
	until := f.now.AddDate(20, 0, 0)
	if err = admins.CreateSession(t.Context(), &model.AdminSession{ID: f.actor.SessionID, UserID: f.actor.UserID, TokenHash: bytes.Repeat([]byte{3}, 32), TokenPrefix: "life", CreatedAt: f.now, LastActivityAt: f.now, IdleExpiresAt: until, AbsoluteExpiresAt: until, ReauthenticatedUntil: &until}); err != nil {
		t.Fatal(err)
	}
	f.exec(t, `INSERT INTO nodes(id,name,host,port,status,is_public,created_at,updated_at) VALUES('life-node','Life Node','host',22,'offline',1,?,?)`, f.now, f.now)
	return f
}

func (f *fixture) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := f.db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) save(t *testing.T, input model.LifecycleWrite) model.NodeLifecycle {
	t.Helper()
	value, err := f.service.SaveNode(t.Context(), f.actor, NodeChange{NodeID: "life-node", LifecycleWrite: input})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestManualLifecycleUpdatesAndPublicRedaction(t *testing.T) {
	f := lifecycleFixture(t)
	empty, err := f.service.Node(t.Context(), f.actor, "life-node")
	if err != nil || empty.Status != "untracked" {
		t.Fatal(empty, err)
	}
	input := validWrite()
	price := int64(1234500)
	input.PriceMicros = &price
	input.Currency = "USD"
	input.PrivateNotes = "PRIVATE_NOTES"
	input.ExpiresOn = "2026-02-28"
	value := f.save(t, input)
	if value.Status != "scheduled" || *value.DaysRemaining != 28 || !value.AnchorEndOfMonth {
		t.Fatal(value)
	}
	if public, err := f.service.PublicLifecycle(t.Context(), "life-node"); err != nil || public != nil {
		t.Fatal(public, err)
	}
	input.ExpiryVisibility = "public"
	f.save(t, input)
	public, err := f.service.PublicLifecycle(t.Context(), "life-node")
	if err != nil || public.ExpiresOn != input.ExpiresOn || public.PriceMicros != nil {
		t.Fatal(public, err)
	}
	input.ExpiryVisibility = "private"
	input.PriceVisibility = "public"
	f.save(t, input)
	public, err = f.service.PublicLifecycle(t.Context(), "life-node")
	if err != nil || public.ExpiresOn != "" || public.Status != "" || *public.PriceMicros != price {
		t.Fatal(public, err)
	}
	input.ExpiresOn = "2026-03-31"
	f.save(t, input)
	events, err := f.service.Events(t.Context(), f.actor, EventQuery{NodeID: "life-node"})
	if err != nil || len(events) != 1 || events[0].Kind != "node.renewed" || events[0].RoutingStatus != "not_routed" || events[0].MessageID == "" {
		t.Fatal(events, err)
	}
	f.save(t, input)
	events, err = f.service.Events(t.Context(), f.actor, EventQuery{NodeID: "life-node"})
	if err != nil || len(events) != 1 {
		t.Fatal("duplicate renewal", events, err)
	}
}

func TestSchedulerRemindersAndCalendarAdvanceAreDurable(t *testing.T) {
	f := lifecycleFixture(t)
	input := validWrite()
	input.ExpiresOn = "2026-01-31"
	input.RenewalMode = "calendar_auto_advance"
	input.ConfirmAutoAdvance = true
	f.save(t, input)
	if err := f.service.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	value, err := f.service.Node(t.Context(), f.actor, "life-node")
	if err != nil || value.ExpiresOn != "2026-02-28" || value.AnchorDay != 31 || value.Status != "scheduled" {
		t.Fatal(value, err)
	}
	events, err := f.service.Events(t.Context(), f.actor, EventQuery{NodeID: "life-node"})
	if err != nil || len(events) != 2 {
		t.Fatal(events, err)
	}
	var messages int
	if err = f.db.QueryRow(`SELECT COUNT(*) FROM notification_messages`).Scan(&messages); err != nil || messages != 2 {
		t.Fatal(messages, err)
	}
	if err = f.service.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = f.db.QueryRow(`SELECT COUNT(*) FROM notification_messages`).Scan(&messages); err != nil || messages != 2 {
		t.Fatal("repeat notification", messages, err)
	}
	f.now = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	if err = f.service.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	value, err = f.service.Node(t.Context(), f.actor, "life-node")
	if err != nil || value.ExpiresOn != "2026-03-31" {
		t.Fatal(value, err)
	}
	var status string
	if err = f.db.QueryRow(`SELECT status FROM nodes WHERE id='life-node'`).Scan(&status); err != nil || status != "offline" {
		t.Fatal("expiry affected monitoring", status, err)
	}
}

func TestLifecycleSettingsAndPermissions(t *testing.T) {
	f := lifecycleFixture(t)
	if _, err := NewService(Config{}); err != ErrUnavailable {
		t.Fatal(err)
	}
	settings, err := f.service.Settings(t.Context(), f.actor)
	if err != nil || settings.Timezone != "UTC" {
		t.Fatal(settings, err)
	}
	settings.PublicExpiry = true
	settings.LeadDays = []int{0, 3, 7}
	settings.PostDays = []int{}
	if _, err = f.service.SaveSettings(t.Context(), f.actor, settings); err != nil {
		t.Fatal(err)
	}
	if err = f.service.Ready(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.Settings(t.Context(), Actor{}); err != ErrForbidden {
		t.Fatal(err)
	}
	f.exec(t, `UPDATE admin_users SET role='admin' WHERE id=?`, f.actor.UserID)
	if _, err = f.service.SaveSettings(t.Context(), f.actor, settings); err != ErrForbidden {
		t.Fatal(err)
	}
	f.exec(t, `UPDATE admin_users SET role='owner' WHERE id=?`, f.actor.UserID)
	f.exec(t, `UPDATE admin_sessions SET reauthenticated_until=NULL WHERE id=?`, f.actor.SessionID)
	if _, err = f.service.SaveSettings(t.Context(), f.actor, settings); err != ErrForbidden {
		t.Fatal(err)
	}
	if _, err = f.service.Node(t.Context(), f.actor, "missing"); err != ErrNotFound {
		t.Fatal(err)
	}
	if _, err = f.service.Events(t.Context(), f.actor, EventQuery{Limit: 201}); err != ErrInvalid {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err = f.service.Run(ctx); err != context.Canceled {
		t.Fatal(err)
	}
}

func TestExistingAutomaticModeCanEditWithoutReconfirmingEnablement(t *testing.T) {
	f := lifecycleFixture(t)
	input := validWrite()
	input.ExpiresOn = "2026-02-28"
	input.RenewalMode = "calendar_auto_advance"
	if _, err := f.service.SaveNode(t.Context(), f.actor, NodeChange{NodeID: "life-node", LifecycleWrite: input}); err != ErrInvalid {
		t.Fatal(err)
	}
	input.ConfirmAutoAdvance = true
	f.save(t, input)
	input.ConfirmAutoAdvance = false
	input.Plan = "Updated plan"
	f.save(t, input)
	snapshot, err := f.service.Snapshot(t.Context())
	if err != nil || snapshot.Scheduled != 1 {
		t.Fatal(snapshot, err)
	}
	if err = f.service.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	snapshot, err = f.service.Snapshot(t.Context())
	if err != nil || snapshot.LastSuccessAt == nil {
		t.Fatal(snapshot, err)
	}
	if err = f.service.Prune(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = f.db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.Snapshot(t.Context()); err == nil {
		t.Fatal("closed snapshot")
	}
	if err = f.service.Prune(t.Context()); err == nil {
		t.Fatal("closed prune")
	}
}

func TestLifecycleObserverReportsTicksAndMaintenance(t *testing.T) {
	f := lifecycleFixture(t)
	observed := []error{}
	f.service.SetObserver(func(err error) { observed = append(observed, err) })
	if err := f.service.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(observed) != 2 || observed[0] != nil || observed[1] != nil {
		t.Fatal(observed)
	}
	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Tick(t.Context()); err == nil {
		t.Fatal("closed tick")
	}
	if len(observed) != 3 || observed[2] == nil {
		t.Fatal(observed)
	}
}
