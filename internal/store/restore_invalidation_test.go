package store

import (
	"database/sql"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestInvalidateRestoredAuthenticationRejectsIncompleteContext(t *testing.T) {
	sqlite := setupTestDB(t)
	cases := map[string]struct {
		database *sql.DB
		restore  RestoredAuthentication
	}{
		"nil database":   {database: nil, restore: RestoredAuthentication{BackupID: "backup-1", At: model.NowUTC()}},
		"missing backup": {database: sqlite.DB, restore: RestoredAuthentication{At: model.NowUTC()}},
		"missing time":   {database: sqlite.DB, restore: RestoredAuthentication{BackupID: "backup-1"}},
	}
	for name, item := range cases {
		t.Run(name, func(t *testing.T) {
			if err := InvalidateRestoredAuthentication(t.Context(), item.database, item.restore); err == nil {
				t.Fatal("incomplete restore context was accepted")
			}
		})
	}
}

func TestInvalidateRestoredAuthenticationRevokesSessionsAndIdentityState(t *testing.T) {
	sqlite := setupTestDB(t)
	admin := NewAdminStore(sqlite.DB)
	user := testAdminUser("restore-owner", model.AdminRoleOwner)
	if err := admin.CreateUser(t.Context(), &user); err != nil {
		t.Fatal(err)
	}
	now := model.NowUTC()
	expiry := now.Add(time.Hour)
	session := model.AdminSession{ID: "restore-session", UserID: user.ID, TokenHash: []byte("hash"), TokenPrefix: "restore",
		CreatedAt: now, LastActivityAt: now, IdleExpiresAt: expiry, AbsoluteExpiresAt: expiry}
	if err := admin.CreateSession(t.Context(), &session); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlite.DB.ExecContext(t.Context(), `INSERT INTO admin_oidc_providers
		(id,slug,display_name,issuer_url,client_id,scopes_json,enabled,configuration_version,sort_order,created_at,updated_at)
		VALUES('provider-1','provider-1','Provider','https://issuer.example','client','["openid"]',1,1,0,?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlite.DB.ExecContext(t.Context(), `INSERT INTO admin_oidc_transactions
		(id,state_hash,browser_binding_hash,nonce_hash,code_verifier_encrypted,provider_id,provider_configuration_version,purpose,user_id,redirect_uri,return_path,created_at,expires_at)
		VALUES('tx','state',x'01',x'02',x'03','provider-1',1,'login',NULL,'https://beat.example/callback','/admin',?,?)`, now, expiry); err != nil {
		t.Fatal(err)
	}
	at := now.Add(time.Minute)
	if err := InvalidateRestoredAuthentication(t.Context(), sqlite.DB, RestoredAuthentication{BackupID: "backup-1", At: at}); err != nil {
		t.Fatalf("invalidate restored authentication: %v", err)
	}
	var revoked sql.NullTime
	if err := sqlite.DB.QueryRowContext(t.Context(), "SELECT revoked_at FROM admin_sessions WHERE id='restore-session'").Scan(&revoked); err != nil {
		t.Fatal(err)
	}
	if !revoked.Valid {
		t.Fatal("restored session stayed active")
	}
	var transactions int
	if err := sqlite.DB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM admin_oidc_transactions").Scan(&transactions); err != nil {
		t.Fatal(err)
	}
	if transactions != 0 {
		t.Fatalf("identity transactions remaining = %d", transactions)
	}
	var audits int
	if err := sqlite.DB.QueryRowContext(t.Context(),
		"SELECT COUNT(*) FROM admin_audit_events WHERE action='security.restore_sessions_revoked' AND resource_id='backup-1'").Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Fatalf("restore audit rows = %d", audits)
	}
}
