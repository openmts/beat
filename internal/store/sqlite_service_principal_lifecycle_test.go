package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestServicePrincipalRevokeIsIdempotentAndClassified(t *testing.T) {
	database := setupTestDB(t)
	store := NewServicePrincipalStore(database.DB)
	created := servicePrincipalFixture("principal-revoke")
	if err := store.Create(t.Context(), created, serviceActor()); err != nil {
		t.Fatal(err)
	}
	if err := store.Revoke(t.Context(), ServicePrincipalRevocation{ID: created.ID, Reason: "operator revoked", At: serviceNow.Add(time.Hour), Actor: serviceActor()}); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.ByHash(t.Context(), created.TokenHash)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RevokedAt == nil || loaded.RevokedReason != "operator revoked" || loaded.Revision != 2 {
		t.Fatal("revocation was not persisted", loaded)
	}
	if err := store.Revoke(t.Context(), ServicePrincipalRevocation{ID: created.ID, Reason: "again", At: serviceNow, Actor: serviceActor()}); !errors.Is(err, ErrServicePrincipalRevoked) {
		t.Fatal("second revocation was not classified", err)
	}
	if err := store.Revoke(t.Context(), ServicePrincipalRevocation{ID: "missing", Reason: "again", At: serviceNow, Actor: serviceActor()}); !errors.Is(err, ErrServicePrincipalNotFound) {
		t.Fatal("missing principal was not classified", err)
	}
	if err := store.Revoke(t.Context(), ServicePrincipalRevocation{Reason: "again", At: serviceNow, Actor: serviceActor()}); !errors.Is(err, ErrServicePrincipalNotFound) {
		t.Fatal("empty identity was not rejected", err)
	}
}

func TestServicePrincipalRotationHonoursOverlapWindow(t *testing.T) {
	database := setupTestDB(t)
	store := NewServicePrincipalStore(database.DB)
	previous := servicePrincipalFixture("principal-old")
	if err := store.Create(t.Context(), previous, serviceActor()); err != nil {
		t.Fatal(err)
	}
	next := servicePrincipalFixture("principal-new")
	next.TokenHash = []byte("abcdef0123456789abcdef0123456789")
	if err := store.Rotate(t.Context(), ServicePrincipalRotation{Previous: previous.ID, Next: next, Overlap: time.Hour, At: serviceNow, Actor: serviceActor()}); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.ByHash(t.Context(), previous.TokenHash)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SupersededAt == nil || loaded.RotationOverlapSeconds != 3600 {
		t.Fatal("rotation did not record the overlap window", loaded)
	}
	successor, err := store.ByHash(t.Context(), next.TokenHash)
	if err != nil || successor.RotatedFrom != previous.ID {
		t.Fatal("successor did not retain its predecessor", successor, err)
	}
	expired, err := store.Expire(t.Context(), serviceNow.Add(30*time.Minute))
	if err != nil || expired != 0 {
		t.Fatal("overlap window revoked early", expired, err)
	}
	expired, err = store.Expire(t.Context(), serviceNow.Add(2*time.Hour))
	if err != nil || expired != 1 {
		t.Fatal("expired overlap was not revoked", expired, err)
	}
	loaded, err = store.ByHash(t.Context(), previous.TokenHash)
	if err != nil || loaded.RevokedAt == nil || loaded.RevokedReason != "rotated" {
		t.Fatal("rotated predecessor was not revoked", loaded, err)
	}
	third := servicePrincipalFixture("principal-third")
	third.TokenHash = []byte("cccccccccccccccccccccccccccccccc")
	if err := store.Rotate(t.Context(), ServicePrincipalRotation{Previous: previous.ID, Next: third, Overlap: time.Hour, At: serviceNow, Actor: serviceActor()}); !errors.Is(err, ErrServicePrincipalRevoked) {
		t.Fatal("revoked predecessor was rotated again", err)
	}
}

func TestServicePrincipalRotationRejectsInvalidRequests(t *testing.T) {
	database := setupTestDB(t)
	store := NewServicePrincipalStore(database.DB)
	active := servicePrincipalFixture("principal-active")
	if err := store.Create(t.Context(), active, serviceActor()); err != nil {
		t.Fatal(err)
	}
	next := servicePrincipalFixture("principal-next")
	next.TokenHash = []byte("abcdef0123456789abcdef0123456789")
	if err := store.Rotate(t.Context(), ServicePrincipalRotation{Previous: active.ID, Next: next, Overlap: model.MaximumServiceTokenRotationOverlap + time.Second, At: serviceNow, Actor: serviceActor()}); !errors.Is(err, ErrServicePrincipalRotation) {
		t.Fatal("excessive overlap was accepted", err)
	}
	if err := store.Rotate(t.Context(), ServicePrincipalRotation{Previous: active.ID, Next: next, Overlap: -time.Second, At: serviceNow, Actor: serviceActor()}); !errors.Is(err, ErrServicePrincipalRotation) {
		t.Fatal("negative overlap was accepted", err)
	}
	if err := store.Rotate(t.Context(), ServicePrincipalRotation{Previous: "missing", Next: next, At: serviceNow, Actor: serviceActor()}); !errors.Is(err, ErrServicePrincipalNotFound) {
		t.Fatal("missing predecessor was not classified", err)
	}
	if _, err := store.ByHash(t.Context(), next.TokenHash); !errors.Is(err, ErrServicePrincipalNotFound) {
		t.Fatal("failed rotation persisted the successor")
	}
	broken := next
	broken.Name = ""
	if err := store.Rotate(t.Context(), ServicePrincipalRotation{Previous: active.ID, Next: broken, At: serviceNow, Actor: serviceActor()}); err == nil {
		t.Fatal("invalid successor policy was accepted")
	}
}

func TestServicePrincipalUseAndBulkRevocation(t *testing.T) {
	database := setupTestDB(t)
	store := NewServicePrincipalStore(database.DB)
	first := servicePrincipalFixture("principal-first")
	second := servicePrincipalFixture("principal-second")
	second.TokenHash = []byte("abcdef0123456789abcdef0123456789")
	if err := store.Create(t.Context(), first, serviceActor()); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(t.Context(), second, serviceActor()); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordUse(t.Context(), first.ID, serviceNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordUse(t.Context(), first.ID, serviceNow.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.ByHash(t.Context(), first.TokenHash)
	if err != nil || loaded.LastUsedAt == nil || !loaded.LastUsedAt.Equal(serviceNow.Add(time.Hour)) {
		t.Fatal("last-used time moved backwards", loaded, err)
	}
	if err := store.RecordUse(t.Context(), "", serviceNow); !errors.Is(err, ErrServicePrincipalNotFound) {
		t.Fatal("empty identity recorded use", err)
	}
	revoked, err := store.RevokeAll(t.Context(), ServicePrincipalBulkRevocation{Reason: "restore", At: serviceNow.Add(2 * time.Hour), Actor: serviceActor()})
	if err != nil || revoked != 2 {
		t.Fatal("bulk revocation did not revoke active tokens", revoked, err)
	}
	revoked, err = store.RevokeAll(t.Context(), ServicePrincipalBulkRevocation{Reason: "restore", At: serviceNow.Add(3 * time.Hour), Actor: serviceActor()})
	if err != nil || revoked != 0 {
		t.Fatal("bulk revocation was not idempotent", revoked, err)
	}
}

func TestServicePrincipalExpireRevokesElapsedTokens(t *testing.T) {
	database := setupTestDB(t)
	store := NewServicePrincipalStore(database.DB)
	soon := servicePrincipalFixture("principal-soon")
	soon.ExpiresAt = serviceNow.Add(time.Minute)
	soon.TokenHash = []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	late := servicePrincipalFixture("principal-late")
	late.ExpiresAt = serviceNow.Add(48 * time.Hour)
	late.TokenHash = []byte("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	for _, value := range []model.ServicePrincipal{soon, late} {
		if err := store.Create(t.Context(), value, serviceActor()); err != nil {
			t.Fatal(err)
		}
	}
	revoked, err := store.Expire(t.Context(), serviceNow.Add(2*time.Minute))
	if err != nil || revoked != 1 {
		t.Fatal("elapsed token was not revoked", revoked, err)
	}
	loaded, err := store.ByHash(t.Context(), soon.TokenHash)
	if err != nil || loaded.RevokedAt == nil || loaded.RevokedReason != "expired" {
		t.Fatal("elapsed token was not marked expired", loaded, err)
	}
	revoked, err = store.Expire(t.Context(), serviceNow.Add(3*time.Minute))
	if err != nil || revoked != 0 {
		t.Fatal("expiry was not idempotent", revoked, err)
	}
}

func TestServicePrincipalStoreReportsClosedDatabase(t *testing.T) {
	database := setupTestDB(t)
	store := NewServicePrincipalStore(database.DB)
	value := servicePrincipalFixture("principal-closed")
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(t.Context(), value, serviceActor()); err == nil {
		t.Fatal("create on a closed database succeeded")
	}
	if _, err := store.ByHash(t.Context(), value.TokenHash); err == nil {
		t.Fatal("lookup on a closed database succeeded")
	}
	if _, err := store.List(t.Context()); err == nil {
		t.Fatal("list on a closed database succeeded")
	}
	if err := store.Revoke(t.Context(), ServicePrincipalRevocation{ID: value.ID, Reason: "closed", At: serviceNow, Actor: serviceActor()}); err == nil {
		t.Fatal("revoke on a closed database succeeded")
	}
	if err := store.Rotate(t.Context(), ServicePrincipalRotation{Previous: value.ID, Next: value, At: serviceNow, Actor: serviceActor()}); err == nil {
		t.Fatal("rotate on a closed database succeeded")
	}
	if err := store.RecordUse(t.Context(), value.ID, serviceNow); err == nil {
		t.Fatal("record use on a closed database succeeded")
	}
	if _, err := store.RevokeAll(t.Context(), ServicePrincipalBulkRevocation{Reason: "closed", At: serviceNow, Actor: serviceActor()}); err == nil {
		t.Fatal("bulk revoke on a closed database succeeded")
	}
	if _, err := store.Expire(t.Context(), serviceNow); err == nil {
		t.Fatal("expire on a closed database succeeded")
	}
}

func TestServicePrincipalAuditsOwnerMutationsWithoutSecrets(t *testing.T) {
	database := setupTestDB(t)
	store := NewServicePrincipalStore(database.DB)
	created := servicePrincipalFixture("principal-audit")
	if err := store.Create(t.Context(), created, serviceActor()); err != nil {
		t.Fatal(err)
	}
	next := servicePrincipalFixture("principal-audit-next")
	next.TokenHash = []byte("1234567890abcdef1234567890abcdef")
	if err := store.Rotate(t.Context(), ServicePrincipalRotation{Previous: created.ID, Next: next, Overlap: time.Hour, At: serviceNow, Actor: serviceActor()}); err != nil {
		t.Fatal(err)
	}
	if err := store.Revoke(t.Context(), ServicePrincipalRevocation{ID: next.ID, Reason: "no longer needed", At: serviceNow.Add(time.Hour), Actor: serviceActor()}); err != nil {
		t.Fatal(err)
	}
	rows, err := database.DB.Query("SELECT action, actor_id, session_prefix, detail_json FROM admin_audit_events ORDER BY action")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	actions := map[string]string{}
	for rows.Next() {
		var action, actor, prefix, detail string
		if err := rows.Scan(&action, &actor, &prefix, &detail); err != nil {
			t.Fatal(err)
		}
		if actor != "owner-one" || prefix != "session-" {
			t.Fatalf("audit lost actor evidence %s %s", actor, prefix)
		}
		if strings.Contains(detail, "0123456789abcdef0123456789abcdef") || strings.Contains(detail, "1234567890abcdef1234567890abcdef") {
			t.Fatalf("audit leaked a token hash: %s", detail)
		}
		actions[action] = detail
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"service_token.create", "service_token.rotate", "service_token.revoke"} {
		if _, ok := actions[action]; !ok {
			t.Fatalf("missing audit action %s in %v", action, actions)
		}
	}
	if !strings.Contains(actions["service_token.revoke"], "no longer needed") {
		t.Fatal("revocation reason was not audited", actions["service_token.revoke"])
	}
	if !strings.Contains(actions["service_token.rotate"], created.ID) {
		t.Fatal("rotation audit did not record the predecessor", actions["service_token.rotate"])
	}
}
