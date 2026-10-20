package applicationsecrets

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/secretbox"
	"github.com/beat/backend/internal/store"
)

func TestAllResourcesMigrateAndPlaintextPagesAreRemoved(t *testing.T) {
	database, directory := lifecycleTestDB(t)
	root := bytes.Repeat([]byte{7}, rootKeyBytes)
	seedLegacyTOTP(t, database, root)
	privateKey := seedLegacyConsumers(t, database, root)
	if err := publishNewPrivateFile(filepath.Join(directory, "admin-data.key"), root); err != nil {
		t.Fatal(err)
	}
	service, err := Open(t.Context(), Config{Database: database.DB, DataDir: directory})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	for _, source := range resourceSources(false) {
		rows, err := source.readBatch(t.Context(), database.DB, "")
		if err != nil || len(rows) == 0 {
			t.Fatalf("missing source %s: %v", source.name, err)
		}
		for _, row := range rows {
			if len(row.ciphertext) == 0 {
				continue
			}
			plaintext, err := service.ring.Open(row.ciphertext, row.aad)
			if err != nil || len(plaintext) == 0 {
				t.Fatalf("resource %s was not migrated: %v", source.name, err)
			}
			clear(plaintext)
			row.aad.RowID += "-other"
			if _, err := service.ring.Open(row.ciphertext, row.aad); err == nil {
				t.Fatal("cross-row substitution accepted")
			}
		}
	}
	if legacy, err := hasLegacySSH(t.Context(), database.DB); err != nil || legacy {
		t.Fatalf("plaintext column remains: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(directory, "beat.db"))
	if err != nil || bytes.Contains(raw, []byte(privateKey)) {
		t.Fatalf("plaintext key remains on disk: %v", err)
	}
	if _, err := service.StartRotation(t.Context()); err != nil {
		t.Fatal(err)
	}
	service.wg.Wait()
	if err := ValidateSnapshot(t.Context(), filepath.Join(directory, "beat.db"), filepath.Join(directory, "admin-data.key")); err != nil {
		t.Fatal(err)
	}
}

func seedLegacyConsumers(t *testing.T, database *store.SQLiteStore, root []byte) string {
	t.Helper()
	manager, err := secretbox.NewFromKey(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	provider := &model.OIDCProvider{ID: "provider", Slug: "provider", DisplayName: "Provider", IssuerURL: "https://issuer.example", ClientID: "client",
		Scopes: []string{"openid"}, Enabled: true, ConfigurationVersion: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	provider.ClientSecretEncrypted, err = manager.EncryptEnvelope([]byte("provider-secret"), secretbox.OIDCProviderAAD(provider.ID, 1))
	if err != nil {
		t.Fatal(err)
	}
	providers := store.NewOIDCStore(database.DB)
	if err := providers.CreateProvider(t.Context(), provider); err != nil {
		t.Fatal(err)
	}
	verifier, err := manager.EncryptEnvelope([]byte("pkce-verifier"), secretbox.OIDCTransactionAAD("transaction", provider.ID))
	if err != nil {
		t.Fatal(err)
	}
	if err := providers.CreateTransaction(t.Context(), &model.OIDCTransaction{ID: "transaction", ProviderID: provider.ID, ProviderConfigurationVersion: 1,
		StateHash: []byte("state"), BrowserBindingHash: []byte("binding"), NonceHash: []byte("nonce"), CodeVerifierEncrypted: verifier,
		Purpose: model.OIDCPurposeLogin, RedirectURI: "https://beat.example/callback", ReturnPath: "/admin", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(key, "")
	if err != nil {
		t.Fatal(err)
	}
	privateKey := string(pem.EncodeToMemory(block))
	for _, row := range []struct{ id, value string }{{"ssh", privateKey}, {"public-only", ""}} {
		if _, err := database.DB.Exec(`INSERT INTO ssh_keys(id,name,key_type,public_key,private_key,fingerprint,created_at) VALUES(?,?,'ed25519','public',?,'fingerprint',CURRENT_TIMESTAMP)`, row.id, row.id, row.value); err != nil {
			t.Fatal(err)
		}
	}
	return privateKey
}
