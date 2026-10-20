package store

import (
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func oidcFaultFixture(t *testing.T) (*OIDCStore, *SQLiteStore) {
	t.Helper()
	oidc, sqlite := setupOIDCStore(t)
	if err := oidc.CreateProvider(t.Context(), sampleProvider("p1", "issuer-one", "https://issuer.example.com")); err != nil {
		t.Fatal(err)
	}
	return oidc, sqlite
}

func oidcFaultTransaction(now time.Time) *model.OIDCTransaction {
	return &model.OIDCTransaction{
		ID: "cov-oidc-tx", StateHash: []byte("cov-oidc-state"), BrowserBindingHash: []byte("bbh"),
		NonceHash: []byte("nh"), CodeVerifierEncrypted: []byte("cve"),
		ProviderID: "p1", ProviderConfigurationVersion: 1, Purpose: model.OIDCPurposeLogin,
		RedirectURI: "https://beat.example.com/api/v1/auth/oidc/callback",
		ReturnPath:  "/admin", CreatedAt: now, ExpiresAt: now.Add(5 * time.Minute),
	}
}

// TestOIDCProviderListFaults 覆盖提供方清单的查询失败与行扫描失败。
func TestOIDCProviderListFaults(t *testing.T) {
	t.Run("scan failure", func(t *testing.T) {
		oidc, sqlite := oidcFaultFixture(t)
		if _, err := sqlite.DB.ExecContext(t.Context(),
			"UPDATE admin_oidc_providers SET sort_order = 'not-a-number'"); err != nil {
			t.Fatal(err)
		}
		if _, err := oidc.ListProviders(t.Context()); err == nil {
			t.Fatal("unscannable provider row was accepted")
		}
		if _, err := oidc.ListEnabledProviders(t.Context()); err == nil {
			t.Fatal("unscannable enabled provider row was accepted")
		}
	})
	t.Run("ledger unreadable", func(t *testing.T) {
		oidc, sqlite := oidcFaultFixture(t)
		if _, err := sqlite.DB.ExecContext(t.Context(),
			"ALTER TABLE admin_oidc_providers RENAME TO cov_oidc_providers_backup"); err != nil {
			t.Fatal(err)
		}
		if _, err := oidc.ListProviders(t.Context()); err == nil {
			t.Fatal("unreadable provider ledger was accepted")
		}
		if _, err := oidc.ListEnabledProviders(t.Context()); err == nil {
			t.Fatal("unreadable enabled provider ledger was accepted")
		}
	})
}

// TestOIDCProviderDeleteFaults 覆盖删除提供方的计数、写入与提交失败。
func TestOIDCProviderDeleteFaults(t *testing.T) {
	t.Run("identity ledger unreadable", func(t *testing.T) {
		oidc, sqlite := oidcFaultFixture(t)
		if _, err := sqlite.DB.ExecContext(t.Context(),
			"ALTER TABLE admin_external_identities RENAME TO cov_oidc_identities_backup"); err != nil {
			t.Fatal(err)
		}
		if err := oidc.DeleteProvider(t.Context(), "p1"); err == nil {
			t.Fatal("unreadable identity ledger was accepted")
		}
	})
	t.Run("delete blocked", func(t *testing.T) {
		oidc, sqlite := oidcFaultFixture(t)
		if _, err := sqlite.DB.ExecContext(t.Context(), `CREATE TRIGGER cov_oidc_delete
			BEFORE DELETE ON admin_oidc_providers BEGIN SELECT RAISE(ABORT,'delete blocked'); END`); err != nil {
			t.Fatal(err)
		}
		if err := oidc.DeleteProvider(t.Context(), "p1"); err == nil {
			t.Fatal("blocked provider delete was accepted")
		}
	})
	t.Run("commit blocked", func(t *testing.T) {
		oidc, sqlite := oidcFaultFixture(t)
		if _, err := sqlite.DB.ExecContext(t.Context(), `CREATE TABLE cov_oidc_delete_child(
			parent TEXT REFERENCES admin_oidc_providers(id) DEFERRABLE INITIALLY DEFERRED)`); err != nil {
			t.Fatal(err)
		}
		if _, err := sqlite.DB.ExecContext(t.Context(), `CREATE TRIGGER cov_oidc_delete_commit
			AFTER DELETE ON admin_oidc_providers BEGIN INSERT INTO cov_oidc_delete_child VALUES('missing'); END`); err != nil {
			t.Fatal(err)
		}
		if err := oidc.DeleteProvider(t.Context(), "p1"); err == nil {
			t.Fatal("deferred delete commit failure was accepted")
		}
	})
}

// TestOIDCIdentityListFaults 覆盖按用户列身份时的行扫描失败。
func TestOIDCIdentityListFaults(t *testing.T) {
	oidc, sqlite := oidcFaultFixture(t)
	createOIDCTestUser(t, t.Context(), sqlite, "oidc-user")
	if err := oidc.LinkIdentity(t.Context(), &model.ExternalIdentity{
		ID: "i1", ProviderID: "p1", UserID: "oidc-user", Subject: "subject-1",
		Email: "user@example.com", EmailVerified: true,
		PreferredUsername: "user", DisplayName: "User",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlite.DB.ExecContext(t.Context(),
		"UPDATE admin_external_identities SET created_at = 'not-a-timestamp'"); err != nil {
		t.Fatal(err)
	}
	if _, err := oidc.ListIdentitiesByUser(t.Context(), "oidc-user"); err == nil {
		t.Fatal("unscannable identity row was accepted")
	}
}

// TestOIDCTransactionConsumeFaults 覆盖消费事务的打开、更新与提交失败以及未知游标。
func TestOIDCTransactionConsumeFaults(t *testing.T) {
	now := time.Now().UTC()
	t.Run("closed database", func(t *testing.T) {
		closed := NewOIDCStore(closedSQLiteStore(t).DB)
		if _, err := closed.ConsumeTransaction(t.Context(), []byte("cov-oidc-state"), now); err == nil {
			t.Fatal("closed database consumed a transaction")
		}
	})
	t.Run("unknown state", func(t *testing.T) {
		oidc, _ := oidcFaultFixture(t)
		transaction, err := oidc.ConsumeTransaction(t.Context(), []byte("missing-state"), now)
		if err != nil || transaction != nil {
			t.Fatalf("unknown state = %v / %v", transaction, err)
		}
	})
	t.Run("corrupt row", func(t *testing.T) {
		oidc, sqlite := oidcFaultFixture(t)
		if _, err := sqlite.DB.ExecContext(t.Context(), `INSERT INTO admin_oidc_transactions(
			id, state_hash, browser_binding_hash, nonce_hash, code_verifier_encrypted,
			provider_id, provider_configuration_version, purpose, user_id, redirect_uri,
			return_path, created_at, expires_at, consumed_at
		) VALUES('cov-corrupt', x'0102', x'0304', x'0506', x'0708', 'p1', 1, 'login', NULL,
			'https://beat.example.com/api/v1/auth/oidc/callback', '/admin',
			'not-a-timestamp', 'not-a-timestamp', NULL)`); err != nil {
			t.Fatal(err)
		}
		if _, err := oidc.ConsumeTransaction(t.Context(), []byte{0x01, 0x02}, now); err == nil {
			t.Fatal("unscannable transaction row was consumed")
		}
	})
	t.Run("update blocked", func(t *testing.T) {
		oidc, sqlite := oidcFaultFixture(t)
		if err := oidc.CreateTransaction(t.Context(), oidcFaultTransaction(now)); err != nil {
			t.Fatal(err)
		}
		if _, err := sqlite.DB.ExecContext(t.Context(), `CREATE TRIGGER cov_oidc_tx_update
			BEFORE UPDATE ON admin_oidc_transactions BEGIN SELECT RAISE(ABORT,'update blocked'); END`); err != nil {
			t.Fatal(err)
		}
		if _, err := oidc.ConsumeTransaction(t.Context(), []byte("cov-oidc-state"), now); err == nil {
			t.Fatal("blocked transaction update was accepted")
		}
	})
	t.Run("commit blocked", func(t *testing.T) {
		oidc, sqlite := oidcFaultFixture(t)
		if err := oidc.CreateTransaction(t.Context(), oidcFaultTransaction(now)); err != nil {
			t.Fatal(err)
		}
		if _, err := sqlite.DB.ExecContext(t.Context(), `CREATE TABLE cov_oidc_tx_child(
			state BLOB REFERENCES admin_oidc_transactions(state_hash) DEFERRABLE INITIALLY DEFERRED)`); err != nil {
			t.Fatal(err)
		}
		if _, err := sqlite.DB.ExecContext(t.Context(), `CREATE TRIGGER cov_oidc_tx_commit
			AFTER UPDATE ON admin_oidc_transactions BEGIN INSERT INTO cov_oidc_tx_child VALUES(x'00'); END`); err != nil {
			t.Fatal(err)
		}
		if _, err := oidc.ConsumeTransaction(t.Context(), []byte("cov-oidc-state"), now); err == nil {
			t.Fatal("deferred transaction commit failure was accepted")
		}
	})
}
