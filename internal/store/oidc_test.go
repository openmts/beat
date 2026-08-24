package store

import (
	"context"
	"crypto/rand"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func setupOIDCStore(t *testing.T) (*OIDCStore, *SQLiteStore) {
	t.Helper()
	sqlite := setupTestDB(t)
	return NewOIDCStore(sqlite.DB), sqlite
}

func createOIDCTestUser(t *testing.T, ctx context.Context, sqlite *SQLiteStore, id string) {
	t.Helper()
	now := time.Now().UTC()
	user := &model.AdminUser{
		ID: id, Username: "user-" + id, DisplayName: "User " + id,
		Role: model.AdminRoleAdmin, PasswordHash: "hash", Enabled: true,
		PasswordChangedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := NewAdminStore(sqlite.DB).CreateUser(ctx, user); err != nil {
		t.Fatalf("create admin user: %v", err)
	}
}

func sampleProvider(id, slug, issuer string) *model.OIDCProvider {
	return &model.OIDCProvider{
		ID: id, Slug: slug, DisplayName: "Provider " + slug, IssuerURL: issuer,
		ClientID: "client-" + id, ClientSecretEncrypted: []byte("encrypted"),
		Scopes: []string{"openid", "profile", "email"}, Enabled: true,
		ConfigurationVersion: 1, SortOrder: 1, CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
}

func TestOIDCProviderCRUD(t *testing.T) {
	oidc, _ := setupOIDCStore(t)
	ctx := context.Background()

	provider := sampleProvider("p1", "issuer-one", "https://issuer.example.com")
	if err := oidc.CreateProvider(ctx, provider); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	got, err := oidc.GetProviderByID(ctx, "p1")
	if err != nil || got == nil {
		t.Fatalf("get provider: %v / %v", got, err)
	}
	if got.ClientSecretConfigured() != true || len(got.Scopes) != 3 {
		t.Fatalf("provider = %#v", got)
	}

	got, err = oidc.GetProviderByIssuer(ctx, "https://issuer.example.com")
	if err != nil || got == nil {
		t.Fatalf("get provider by issuer: %v / %v", got, err)
	}

	got.Enabled = false
	got.SortOrder = 9
	if err := oidc.UpdateProvider(ctx, got); err != nil {
		t.Fatalf("update provider: %v", err)
	}
	refreshed, _ := oidc.GetProviderByID(ctx, "p1")
	if refreshed.Enabled || refreshed.SortOrder != 9 {
		t.Fatalf("updated provider = %#v", refreshed)
	}

	if err := oidc.DeleteProvider(ctx, "p1"); err != nil {
		t.Fatalf("delete provider: %v", err)
	}
	gone, _ := oidc.GetProviderByID(ctx, "p1")
	if gone != nil {
		t.Fatal("provider should be deleted")
	}
}

func TestOIDCProviderUniqueConstraints(t *testing.T) {
	oidc, _ := setupOIDCStore(t)
	ctx := context.Background()

	if err := oidc.CreateProvider(ctx, sampleProvider("p1", "dup", "https://issuer.example.com")); err != nil {
		t.Fatalf("create first: %v", err)
	}
	if err := oidc.CreateProvider(ctx, sampleProvider("p2", "dup", "https://other.example.com")); err == nil {
		t.Fatal("duplicate slug must fail")
	}
	if err := oidc.CreateProvider(ctx, sampleProvider("p3", "ok", "https://issuer.example.com")); err == nil {
		t.Fatal("duplicate issuer must fail")
	}
}

func TestOIDCProviderDeleteRejectedWithIdentities(t *testing.T) {
	oidc, sqlite := setupOIDCStore(t)
	ctx := context.Background()
	createOIDCTestUser(t, ctx, sqlite, "u-1")

	if err := oidc.CreateProvider(ctx, sampleProvider("p1", "issuer-one", "https://issuer.example.com")); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	now := time.Now().UTC()
	if err := oidc.LinkIdentity(ctx, &model.ExternalIdentity{
		ID: "id-1", ProviderID: "p1", UserID: "u-1", Subject: "sub-1",
		LastLoginAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("link identity: %v", err)
	}
	if err := oidc.DeleteProvider(ctx, "p1"); !errors.Is(err, ErrOIDCProviderHasIdentities) {
		t.Fatalf("delete with identities: got %v", err)
	}
}

func TestOIDCProviderConfigurationVersionIncrement(t *testing.T) {
	oidc, _ := setupOIDCStore(t)
	ctx := context.Background()

	if err := oidc.CreateProvider(ctx, sampleProvider("p1", "issuer-one", "https://issuer.example.com")); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	if err := oidc.IncrementProviderConfigurationVersion(ctx, "p1"); err != nil {
		t.Fatalf("increment: %v", err)
	}
	got, _ := oidc.GetProviderByID(ctx, "p1")
	if got.ConfigurationVersion != 2 {
		t.Fatalf("configuration version = %d, want 2", got.ConfigurationVersion)
	}
}

func TestOIDCExternalIdentityLifecycle(t *testing.T) {
	oidc, sqlite := setupOIDCStore(t)
	ctx := context.Background()
	createOIDCTestUser(t, ctx, sqlite, "u-1")
	createOIDCTestUser(t, ctx, sqlite, "u-2")

	if err := oidc.CreateProvider(ctx, sampleProvider("p1", "issuer-one", "https://issuer.example.com")); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	now := time.Now().UTC()
	identity := &model.ExternalIdentity{
		ID: "id-1", ProviderID: "p1", UserID: "u-1", Subject: "sub-abc",
		Email: "user@example.com", EmailVerified: true, PreferredUsername: "user",
		DisplayName: "User", LastLoginAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := oidc.LinkIdentity(ctx, identity); err != nil {
		t.Fatalf("link identity: %v", err)
	}

	// (provider_id, subject) 唯一
	if err := oidc.LinkIdentity(ctx, &model.ExternalIdentity{
		ID: "id-2", ProviderID: "p1", UserID: "u-2", Subject: "sub-abc",
		LastLoginAt: now, CreatedAt: now, UpdatedAt: now,
	}); err == nil {
		t.Fatal("duplicate (provider, subject) must fail")
	}
	// (provider_id, user_id) 唯一
	if err := oidc.LinkIdentity(ctx, &model.ExternalIdentity{
		ID: "id-3", ProviderID: "p1", UserID: "u-1", Subject: "sub-xyz",
		LastLoginAt: now, CreatedAt: now, UpdatedAt: now,
	}); err == nil {
		t.Fatal("duplicate (provider, user) must fail")
	}

	bySubject, err := oidc.GetIdentityByProviderSubject(ctx, "p1", "sub-abc")
	if err != nil || bySubject == nil {
		t.Fatalf("get by subject: %v / %v", bySubject, err)
	}

	listed, err := oidc.ListIdentitiesByUser(ctx, "u-1")
	if err != nil {
		t.Fatalf("list identities: %v", err)
	}
	if len(listed) != 1 || listed[0].ProviderName != "Provider issuer-one" {
		t.Fatalf("listed identities = %#v", listed)
	}

	if err := oidc.TouchIdentityLogin(ctx, "id-1", now.Add(time.Minute)); err != nil {
		t.Fatalf("touch login: %v", err)
	}

	removed, err := oidc.UnlinkIdentity(ctx, "id-1", "u-1")
	if err != nil || !removed {
		t.Fatalf("unlink: %v / %v", removed, err)
	}
	removed, _ = oidc.UnlinkIdentity(ctx, "id-1", "u-1")
	if removed {
		t.Fatal("second unlink must not remove anything")
	}
}

func TestOIDCTransactionConsumeAtomic(t *testing.T) {
	oidc, _ := setupOIDCStore(t)
	ctx := context.Background()

	if err := oidc.CreateProvider(ctx, sampleProvider("p1", "issuer-one", "https://issuer.example.com")); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	now := time.Now().UTC()
	stateHash := make([]byte, 32)
	_, _ = rand.Read(stateHash)
	transaction := &model.OIDCTransaction{
		ID: "t-1", StateHash: stateHash, BrowserBindingHash: []byte("bbh"),
		NonceHash: []byte("nh"), CodeVerifierEncrypted: []byte("cve"),
		ProviderID: "p1", ProviderConfigurationVersion: 1, Purpose: model.OIDCPurposeLogin,
		RedirectURI: "https://beat.example.com/api/v1/auth/oidc/callback",
		ReturnPath:  "/admin", CreatedAt: now, ExpiresAt: now.Add(5 * time.Minute),
	}
	if err := oidc.CreateTransaction(ctx, transaction); err != nil {
		t.Fatalf("create transaction: %v", err)
	}

	consumed, err := oidc.ConsumeTransaction(ctx, stateHash, now.Add(time.Minute))
	if err != nil || consumed == nil {
		t.Fatalf("consume transaction: %v / %v", consumed, err)
	}
	// 第二次消费必须返回 nil
	again, err := oidc.ConsumeTransaction(ctx, stateHash, now.Add(2*time.Minute))
	if err != nil || again != nil {
		t.Fatalf("second consume = %v / %v, want nil/nil", again, err)
	}
}

func TestOIDCTransactionExpiredNotConsumable(t *testing.T) {
	oidc, _ := setupOIDCStore(t)
	ctx := context.Background()

	if err := oidc.CreateProvider(ctx, sampleProvider("p1", "issuer-one", "https://issuer.example.com")); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	now := time.Now().UTC()
	stateHash := make([]byte, 32)
	_, _ = rand.Read(stateHash)
	if err := oidc.CreateTransaction(ctx, &model.OIDCTransaction{
		ID: "t-1", StateHash: stateHash, BrowserBindingHash: []byte("bbh"),
		NonceHash: []byte("nh"), CodeVerifierEncrypted: []byte("cve"),
		ProviderID: "p1", ProviderConfigurationVersion: 1, Purpose: model.OIDCPurposeLogin,
		RedirectURI: "https://beat.example.com/api/v1/auth/oidc/callback",
		ReturnPath:  "/admin", CreatedAt: now.Add(-10 * time.Minute), ExpiresAt: now.Add(-5 * time.Minute),
	}); err != nil {
		t.Fatalf("create transaction: %v", err)
	}
	if consumed, err := oidc.ConsumeTransaction(ctx, stateHash, now); err != nil || consumed != nil {
		t.Fatalf("expired consume = %v / %v, want nil/nil", consumed, err)
	}
}

func TestOIDCTransactionConcurrentConsumeSingleWinner(t *testing.T) {
	oidc, _ := setupOIDCStore(t)
	ctx := context.Background()

	if err := oidc.CreateProvider(ctx, sampleProvider("p1", "issuer-one", "https://issuer.example.com")); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	now := time.Now().UTC()
	stateHash := make([]byte, 32)
	_, _ = rand.Read(stateHash)
	if err := oidc.CreateTransaction(ctx, &model.OIDCTransaction{
		ID: "t-1", StateHash: stateHash, BrowserBindingHash: []byte("bbh"),
		NonceHash: []byte("nh"), CodeVerifierEncrypted: []byte("cve"),
		ProviderID: "p1", ProviderConfigurationVersion: 1, Purpose: model.OIDCPurposeLogin,
		RedirectURI: "https://beat.example.com/api/v1/auth/oidc/callback",
		ReturnPath:  "/admin", CreatedAt: now, ExpiresAt: now.Add(5 * time.Minute),
	}); err != nil {
		t.Fatalf("create transaction: %v", err)
	}

	const attempts = 8
	var wg sync.WaitGroup
	winners := make(chan int, attempts)
	for range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			transaction, _ := oidc.ConsumeTransaction(ctx, stateHash, now.Add(time.Minute))
			if transaction != nil {
				winners <- 1
			}
		}()
	}
	wg.Wait()
	close(winners)
	winnerCount := 0
	for range winners {
		winnerCount++
	}
	if winnerCount != 1 {
		t.Fatalf("concurrent consume winners = %d, want exactly 1", winnerCount)
	}
}

func TestOIDCTransactionExpiredCleanup(t *testing.T) {
	oidc, _ := setupOIDCStore(t)
	ctx := context.Background()

	if err := oidc.CreateProvider(ctx, sampleProvider("p1", "issuer-one", "https://issuer.example.com")); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	now := time.Now().UTC()
	for index, purpose := range []string{model.OIDCPurposeLogin, model.OIDCPurposeLink} {
		if err := oidc.CreateTransaction(ctx, &model.OIDCTransaction{
			ID: "t-" + purpose, StateHash: []byte("state" + purpose), BrowserBindingHash: []byte("bbh"),
			NonceHash: []byte("nh"), CodeVerifierEncrypted: []byte("cve"),
			ProviderID: "p1", ProviderConfigurationVersion: 1, Purpose: purpose,
			UserID: "u-" + string(rune('0'+index)), RedirectURI: "https://beat.example.com/cb",
			ReturnPath: "/admin", CreatedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Hour),
		}); err != nil {
			t.Fatalf("create expired transaction: %v", err)
		}
	}
	count, err := oidc.DeleteExpiredTransactions(ctx, now)
	if err != nil {
		t.Fatalf("delete expired: %v", err)
	}
	if count != 2 {
		t.Fatalf("deleted = %d, want 2", count)
	}
}

func TestOIDCMigrationIdempotent(t *testing.T) {
	sqlite := setupTestDB(t)
	ctx := context.Background()
	if err := applyOIDCIdentity(ctx, sqlite.DB); err != nil {
		t.Fatalf("second application of migration v5 failed: %v", err)
	}
	oidc := NewOIDCStore(sqlite.DB)
	if err := oidc.CreateProvider(ctx, sampleProvider("p1", "issuer-one", "https://issuer.example.com")); err != nil {
		t.Fatalf("create provider after idempotent migration: %v", err)
	}
}

func TestOIDCSessionAttributionColumns(t *testing.T) {
	sqlite := setupTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC()

	store := NewAdminStore(sqlite.DB)
	user := &model.AdminUser{
		ID: "u-1", Username: "owner", DisplayName: "Owner", Role: model.AdminRoleOwner,
		PasswordHash: "hash", Enabled: true, PasswordChangedAt: now,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateUser(ctx, user); err != nil {
		t.Fatalf("create admin user: %v", err)
	}
	session := &model.AdminSession{
		ID: "s-1", UserID: user.ID, TokenHash: []byte("hash"), TokenPrefix: "pre",
		CreatedAt: now, LastActivityAt: now, IdleExpiresAt: now.Add(time.Hour),
		AbsoluteExpiresAt: now.Add(24 * time.Hour), IPAddress: "127.0.0.1",
		UserAgent: "test", AuthenticationMethod: model.AuthMethodOIDC,
	}
	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatalf("create session: %v", err)
	}
	got, err := store.GetSessionByID(ctx, "s-1")
	if err != nil || got == nil {
		t.Fatalf("get session: %v / %v", got, err)
	}
	if got.AuthenticationMethod != model.AuthMethodOIDC {
		t.Fatalf("authentication method = %q, want oidc", got.AuthenticationMethod)
	}
}

func TestOIDCProviderListVariants(t *testing.T) {
	oidc, _ := setupOIDCStore(t)
	ctx := context.Background()
	enabled := sampleProvider("p1", "issuer-one", "https://issuer.example.com")
	if err := oidc.CreateProvider(ctx, enabled); err != nil {
		t.Fatalf("create enabled: %v", err)
	}
	disabled := sampleProvider("p2", "issuer-two", "https://issuer2.example.com")
	disabled.Enabled = false
	if err := oidc.CreateProvider(ctx, disabled); err != nil {
		t.Fatalf("create disabled: %v", err)
	}

	all, err := oidc.ListProviders(ctx)
	if err != nil || len(all) != 2 {
		t.Fatalf("list providers = %d / %v", len(all), err)
	}
	enabledList, err := oidc.ListEnabledProviders(ctx)
	if err != nil || len(enabledList) != 1 || enabledList[0].ID != "p1" {
		t.Fatalf("enabled providers = %v / %v", enabledList, err)
	}

	// 更新不存在的提供方
	if err := oidc.UpdateProvider(ctx, &model.OIDCProvider{ID: "missing", DisplayName: "x"}); !errors.Is(err, ErrOIDCProviderNotFound) {
		t.Fatalf("update missing = %v", err)
	}
	// 版本自增不存在的提供方
	if err := oidc.IncrementProviderConfigurationVersion(ctx, "missing"); !errors.Is(err, ErrOIDCProviderNotFound) {
		t.Fatalf("increment missing = %v", err)
	}
	// 删除不存在的提供方
	if err := oidc.DeleteProvider(ctx, "missing"); !errors.Is(err, ErrOIDCProviderNotFound) {
		t.Fatalf("delete missing = %v", err)
	}
}

func TestOIDCProviderScopesRoundTrip(t *testing.T) {
	oidc, _ := setupOIDCStore(t)
	ctx := context.Background()
	provider := sampleProvider("p1", "issuer-one", "https://issuer.example.com")
	provider.Scopes = []string{"openid", "profile"}
	if err := oidc.CreateProvider(ctx, provider); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	got, _ := oidc.GetProviderByID(ctx, "p1")
	if len(got.Scopes) != 2 || got.Scopes[0] != "openid" {
		t.Fatalf("scopes = %v", got.Scopes)
	}
	if got := decodeScopesJSON(""); len(got) != 1 || got[0] != "openid" {
		t.Fatalf("default scopes = %v", got)
	}
	if got := decodeScopesJSON("not-json"); len(got) != 1 || got[0] != "openid" {
		t.Fatalf("invalid scopes fallback = %v", got)
	}
	if got := encodeScopesJSON(nil); got != `["openid"]` {
		t.Fatalf("nil scopes encode = %q", got)
	}
}

func TestOIDCGetIdentityByIDAndTouchLogin(t *testing.T) {
	oidc, sqlite := setupOIDCStore(t)
	ctx := context.Background()
	createOIDCTestUser(t, ctx, sqlite, "u-1")
	if err := oidc.CreateProvider(ctx, sampleProvider("p1", "issuer-one", "https://issuer.example.com")); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	now := time.Now().UTC()
	if err := oidc.LinkIdentity(ctx, &model.ExternalIdentity{
		ID: "id-1", ProviderID: "p1", UserID: "u-1", Subject: "sub-1",
		LastLoginAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("link: %v", err)
	}
	got, err := oidc.GetIdentityByID(ctx, "id-1")
	if err != nil || got == nil || got.Subject != "sub-1" {
		t.Fatalf("get identity = %v / %v", got, err)
	}
	missing, err := oidc.GetIdentityByID(ctx, "missing")
	if err != nil || missing != nil {
		t.Fatalf("missing identity = %v / %v", missing, err)
	}
	if err := oidc.TouchIdentityLogin(ctx, "id-1", now.Add(time.Minute)); err != nil {
		t.Fatalf("touch login: %v", err)
	}
	// 未链接身份解锁返回 false
	removed, err := oidc.UnlinkIdentity(ctx, "id-1", "other-user")
	if err != nil || removed {
		t.Fatalf("unlink other user = %v / %v", removed, err)
	}
}

func TestOIDCTransactionCreateRejectsMissingProvider(t *testing.T) {
	oidc, _ := setupOIDCStore(t)
	now := time.Now().UTC()
	if err := oidc.CreateTransaction(context.Background(), &model.OIDCTransaction{
		ID: "t-missing", StateHash: []byte("sh"), BrowserBindingHash: []byte("bbh"),
		NonceHash: []byte("nh"), CodeVerifierEncrypted: []byte("cve"),
		ProviderID: "no-such-provider", ProviderConfigurationVersion: 1,
		Purpose: model.OIDCPurposeLogin, RedirectURI: "https://beat.test/cb",
		ReturnPath: "/admin", CreatedAt: now, ExpiresAt: now.Add(time.Minute),
	}); err == nil {
		t.Fatal("transaction with missing provider must fail")
	}
}

func TestOIDCStoreErrorPathsWithClosedDatabase(t *testing.T) {
	oidc, sqlite := setupOIDCStore(t)
	if err := sqlite.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}
	ctx := context.Background()
	now := time.Now().UTC()
	provider := sampleProvider("p1", "slug", "https://issuer.example.com")
	identity := &model.ExternalIdentity{ID: "id-1", ProviderID: "p1", UserID: "u-1", Subject: "s"}
	transaction := &model.OIDCTransaction{
		ID: "t-1", StateHash: []byte("sh"), BrowserBindingHash: []byte("b"),
		NonceHash: []byte("n"), CodeVerifierEncrypted: []byte("c"),
		ProviderID: "p1", ProviderConfigurationVersion: 1, Purpose: model.OIDCPurposeLogin,
		CreatedAt: now, ExpiresAt: now.Add(time.Minute),
	}
	for name, call := range map[string]func() error{
		"create provider":    func() error { return oidc.CreateProvider(ctx, provider) },
		"update provider":    func() error { return oidc.UpdateProvider(ctx, provider) },
		"get provider":       func() error { _, err := oidc.GetProviderByID(ctx, "p1"); return err },
		"list providers":     func() error { _, err := oidc.ListProviders(ctx); return err },
		"delete provider":    func() error { return oidc.DeleteProvider(ctx, "p1") },
		"increment version":  func() error { return oidc.IncrementProviderConfigurationVersion(ctx, "p1") },
		"link identity":      func() error { return oidc.LinkIdentity(ctx, identity) },
		"unlink identity":    func() error { _, err := oidc.UnlinkIdentity(ctx, "id-1", "u-1"); return err },
		"get identity":       func() error { _, err := oidc.GetIdentityByID(ctx, "id-1"); return err },
		"list identities":    func() error { _, err := oidc.ListIdentitiesByUser(ctx, "u-1"); return err },
		"touch identity":     func() error { return oidc.TouchIdentityLogin(ctx, "id-1", now) },
		"create transaction": func() error { return oidc.CreateTransaction(ctx, transaction) },
		"consume transaction": func() error {
			_, err := oidc.ConsumeTransaction(ctx, []byte("sh"), now)
			return err
		},
		"delete transactions": func() error {
			_, err := oidc.DeleteExpiredTransactions(ctx, now)
			return err
		},
	} {
		if err := call(); err == nil {
			t.Fatalf("%s on closed database must fail", name)
		}
	}
}
