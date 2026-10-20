package geoip_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/geoip"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/secretbox"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
)

type fixture struct {
	service *geoip.Service
	db      *store.SQLiteStore
	actor   geoip.Actor
	now     time.Time
	secrets secretbox.ResourceService
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	database, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	f := &fixture{db: database, now: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC), actor: geoip.Actor{UserID: "geo-owner", SessionID: "geo-session"}}
	secrets := testutil.NewSecrets(t, database.DB)
	f.secrets = secrets
	f.service, err = geoip.NewService(geoip.Config{Database: database.DB, Secrets: secrets, Directory: filepath.Join(t.TempDir(), "geoip"), Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.service.Close() })
	admins := store.NewAdminStore(database.DB)
	if err = admins.CreateUser(t.Context(), &model.AdminUser{ID: f.actor.UserID, Username: "geo-owner", DisplayName: "Owner", Role: "owner", Enabled: true, PasswordHash: "fixture", PasswordChangedAt: f.now, CreatedAt: f.now, UpdatedAt: f.now}); err != nil {
		t.Fatal(err)
	}
	until := f.now.Add(time.Hour)
	if err = admins.CreateSession(t.Context(), &model.AdminSession{ID: f.actor.SessionID, UserID: f.actor.UserID, TokenHash: bytes.Repeat([]byte{9}, 32), TokenPrefix: "geo", CreatedAt: f.now, LastActivityAt: f.now, IdleExpiresAt: until, AbsoluteExpiresAt: until, ReauthenticatedUntil: &until}); err != nil {
		t.Fatal(err)
	}
	f.exec(t, `INSERT INTO nodes(id,name,host,port,status,ssh_public_key,cpu_model,os,platform,os_version,kernel,arch,virtualization,agent_version,sort_order,tags,is_public,public_remark,private_remark,traffic_limit,traffic_limit_type,traffic_reset_day,created_at,updated_at) VALUES('geo-node','Geo Node','untrusted-host',22,'offline','','','','','','','','','',0,'[]',1,'','',0,'sum',1,?,?)`, f.now, f.now)
	return f
}

func (f *fixture) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := f.db.DB.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) observe(t *testing.T, source string) {
	t.Helper()
	err := f.secrets.WithWrite(t.Context(), func(access secretbox.ResourceAccess) error {
		tx, err := f.db.DB.BeginTx(t.Context(), nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		if err = geoip.RecordObserved(t.Context(), tx, geoip.ObservedSource{NodeID: "geo-node", Source: source, Access: access, Now: f.now}); err != nil {
			return err
		}
		return tx.Commit()
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDefaultsManualPrivacyAndPurge(t *testing.T) {
	f := newFixture(t)
	settings, err := f.service.Settings(t.Context(), f.actor)
	if err != nil || settings.Provider != "disabled" || settings.RemoteConsent {
		t.Fatalf("default: %+v %v", settings, err)
	}
	location, err := f.service.Location(t.Context(), f.actor, "geo-node")
	if err != nil || location.Mode != "observed" {
		t.Fatalf("location: %+v %v", location, err)
	}
	input := geoip.LocationMutation{NodeID: "geo-node", LocationWrite: model.LocationWrite{Mode: "manual", Scope: "public", CountryCode: "JP", Visibility: "private"}}
	if _, err = f.service.SaveLocation(t.Context(), f.actor, input); err != nil {
		t.Fatal(err)
	}
	public, err := f.service.PublicLocation(t.Context(), "geo-node")
	if err != nil || public != nil {
		t.Fatalf("private leaked: %+v %v", public, err)
	}
	input.Visibility = "public"
	if _, err = f.service.SaveLocation(t.Context(), f.actor, input); err != nil {
		t.Fatal(err)
	}
	public, err = f.service.PublicLocation(t.Context(), "geo-node")
	if err != nil || public.CountryCode != "JP" {
		t.Fatalf("public: %+v %v", public, err)
	}
	f.observe(t, "8.8.8.8")
	location, err = f.service.Location(t.Context(), f.actor, "geo-node")
	if err != nil || location.CountryCode != "JP" || location.HasSource {
		t.Fatal("manual overwritten", err)
	}
	if err = f.service.Refresh(t.Context(), f.actor, "geo-node"); err != geoip.ErrInvalid {
		t.Fatal(err)
	}
	input.Mode = "disabled"
	input.Purge = true
	if _, err = f.service.SaveLocation(t.Context(), f.actor, input); err != nil {
		t.Fatal(err)
	}
	public, err = f.service.PublicLocation(t.Context(), "geo-node")
	if err != nil || public != nil {
		t.Fatal("disabled public", err)
	}
}

func TestObservedEncryptionAndLocalResolution(t *testing.T) {
	f := newFixture(t)
	f.observe(t, "8.8.8.8")
	var encrypted []byte
	var fingerprint string
	var due sql.NullTime
	if err := f.db.DB.QueryRow(`SELECT source_encrypted,source_fingerprint,next_refresh_at FROM node_locations WHERE id='geo-node'`).Scan(&encrypted, &fingerprint, &due); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, []byte("8.8.8.8")) || strings.Contains(fingerprint, "8.8.8.8") || due.Valid {
		t.Fatal("source not private or scheduled while disabled")
	}
	settings, err := f.service.InstallMMDB(t.Context(), f.actor, geoip.MMDBUpload{Body: bytes.NewReader(testutil.CountryMMDB("US", 1700000000)), ExpectedRevision: 1})
	if err != nil || settings.Provider != "mmdb" {
		t.Fatalf("install: %+v %v", settings, err)
	}
	if err = f.service.Process(t.Context()); err != nil {
		t.Fatal(err)
	}
	location, err := f.service.Location(t.Context(), f.actor, "geo-node")
	if err != nil || location.CountryCode != "US" || location.Scope != "public" {
		t.Fatalf("resolve: %+v %v", location, err)
	}
	encoded, err := json.Marshal(location)
	if err != nil || bytes.Contains(encoded, []byte("8.8.8.8")) {
		t.Fatal("source leaked", err)
	}
	f.observe(t, "10.0.0.1")
	if err = f.service.Process(t.Context()); err != nil {
		t.Fatal(err)
	}
	location, err = f.service.Location(t.Context(), f.actor, "geo-node")
	if err != nil || location.Scope != "private" || location.CountryCode != "" {
		t.Fatalf("private resolve: %+v %v", location, err)
	}
}
