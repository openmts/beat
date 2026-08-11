package handler

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beat/backend/internal/store"
)

func TestRawEd25519PrivateKey_Public(t *testing.T) {
	_, priv, err := generateTestEd25519Key()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	raw := &rawEd25519PrivateKey{key: priv}
	pub := raw.Public()
	if pub == nil {
		t.Fatal("expected non-nil public key")
	}
}

func TestRawEd25519PrivateKey_Sign(t *testing.T) {
	_, priv, err := generateTestEd25519Key()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	raw := &rawEd25519PrivateKey{key: priv}
	digest := []byte("test data")
	sig, err := raw.Sign(nil, digest, crypto.Hash(0))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if len(sig) == 0 {
		t.Fatal("expected non-empty signature")
	}
}

func generateTestEd25519Key() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	return publicKey, privateKey, nil
}

func TestHandleDeleteSSHKey_NotFound(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()
	sshKeyStore := store.NewSSHKeyStore(s.DB)
	h := NewSSHKeyHandler(sshKeyStore)

	req := httptest.NewRequest(http.MethodDelete, "/api/ssh-keys/nonexistent", nil)
	req = req.WithContext(ctx)
	req.SetPathValue("id", "nonexistent")
	w := httptest.NewRecorder()

	h.HandleDeleteSSHKey(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", w.Code)
	}
}

func TestHandleDeleteSSHKey_MissingID(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()
	sshKeyStore := store.NewSSHKeyStore(s.DB)
	h := NewSSHKeyHandler(sshKeyStore)

	req := httptest.NewRequest(http.MethodDelete, "/api/ssh-keys/", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.HandleDeleteSSHKey(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", w.Code)
	}
}

func TestHandleListSSHKeys(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()
	sshKeyStore := store.NewSSHKeyStore(s.DB)
	h := NewSSHKeyHandler(sshKeyStore)

	_, _ = sshKeyStore.CreateSSHKey(ctx, "key1", "rsa", "pub1", "priv1", "fp1")
	_, _ = sshKeyStore.CreateSSHKey(ctx, "key2", "ed25519", "pub2", "priv2", "fp2")

	req := httptest.NewRequest(http.MethodGet, "/api/ssh-keys", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.HandleListSSHKeys(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestHandleCreateSSHKey(t *testing.T) {
	t.Run("creates ssh key", func(t *testing.T) {
		s := setupTestDB(t)
		ctx := context.Background()
		sshKeyStore := store.NewSSHKeyStore(s.DB)
		h := NewSSHKeyHandler(sshKeyStore)

		body := `{"name": "test-key", "key_type": "rsa", "public_key": "ssh-rsa AAA...", "private_key": "-----BEGIN RSA PRIVATE KEY-----"}`
		req := httptest.NewRequest(http.MethodPost, "/api/ssh-keys", strings.NewReader(body))
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.HandleCreateSSHKey(w, req)

		if w.Code != http.StatusCreated {
			t.Errorf("expected status 201, got %d", w.Code)
		}
	})

	t.Run("creates public-only ssh key", func(t *testing.T) {
		s := setupTestDB(t)
		sshKeyStore := store.NewSSHKeyStore(s.DB)
		h := NewSSHKeyHandler(sshKeyStore)

		body := `{"name":"public-only","key_type":"ed25519","public_key":"ssh-ed25519 AAAApublic"}`
		req := httptest.NewRequest(http.MethodPost, "/api/ssh-keys", strings.NewReader(body))
		w := httptest.NewRecorder()

		h.HandleCreateSSHKey(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d: %s", w.Code, http.StatusCreated, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "private_key") {
			t.Fatalf("response exposes private key field: %s", w.Body.String())
		}
	})

	t.Run("missing fields returns 400", func(t *testing.T) {
		s := setupTestDB(t)
		ctx := context.Background()
		sshKeyStore := store.NewSSHKeyStore(s.DB)
		h := NewSSHKeyHandler(sshKeyStore)

		body := `{"name": "test-key"}`
		req := httptest.NewRequest(http.MethodPost, "/api/ssh-keys", strings.NewReader(body))
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.HandleCreateSSHKey(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("invalid JSON returns 400", func(t *testing.T) {
		s := setupTestDB(t)
		ctx := context.Background()
		sshKeyStore := store.NewSSHKeyStore(s.DB)
		h := NewSSHKeyHandler(sshKeyStore)

		body := `not-json`
		req := httptest.NewRequest(http.MethodPost, "/api/ssh-keys", strings.NewReader(body))
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.HandleCreateSSHKey(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})
}

func TestHandleGenerateSSHKey(t *testing.T) {
	t.Run("generates RSA key", func(t *testing.T) {
		s := setupTestDB(t)
		ctx := context.Background()
		sshKeyStore := store.NewSSHKeyStore(s.DB)
		h := NewSSHKeyHandler(sshKeyStore)

		body := `{"name": "rsa-key", "key_type": "rsa"}`
		req := httptest.NewRequest(http.MethodPost, "/api/ssh-keys/generate", strings.NewReader(body))
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.HandleGenerateSSHKey(w, req)

		if w.Code != http.StatusCreated {
			t.Errorf("expected status 201, got %d", w.Code)
		}
	})

	t.Run("generates Ed25519 key", func(t *testing.T) {
		s := setupTestDB(t)
		ctx := context.Background()
		sshKeyStore := store.NewSSHKeyStore(s.DB)
		h := NewSSHKeyHandler(sshKeyStore)

		body := `{"name": "ed25519-key", "key_type": "ed25519"}`
		req := httptest.NewRequest(http.MethodPost, "/api/ssh-keys/generate", strings.NewReader(body))
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.HandleGenerateSSHKey(w, req)

		if w.Code != http.StatusCreated {
			t.Errorf("expected status 201, got %d", w.Code)
		}
	})

	t.Run("defaults to RSA when key_type is empty", func(t *testing.T) {
		s := setupTestDB(t)
		ctx := context.Background()
		sshKeyStore := store.NewSSHKeyStore(s.DB)
		h := NewSSHKeyHandler(sshKeyStore)

		body := `{"name": "default-key"}`
		req := httptest.NewRequest(http.MethodPost, "/api/ssh-keys/generate", strings.NewReader(body))
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.HandleGenerateSSHKey(w, req)

		if w.Code != http.StatusCreated {
			t.Errorf("expected status 201, got %d", w.Code)
		}
	})

	t.Run("unsupported key type returns 400", func(t *testing.T) {
		s := setupTestDB(t)
		ctx := context.Background()
		sshKeyStore := store.NewSSHKeyStore(s.DB)
		h := NewSSHKeyHandler(sshKeyStore)

		body := `{"name": "bad-key", "key_type": "dsa"}`
		req := httptest.NewRequest(http.MethodPost, "/api/ssh-keys/generate", strings.NewReader(body))
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.HandleGenerateSSHKey(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("missing name returns 400", func(t *testing.T) {
		s := setupTestDB(t)
		ctx := context.Background()
		sshKeyStore := store.NewSSHKeyStore(s.DB)
		h := NewSSHKeyHandler(sshKeyStore)

		body := `{"key_type": "rsa"}`
		req := httptest.NewRequest(http.MethodPost, "/api/ssh-keys/generate", strings.NewReader(body))
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.HandleGenerateSSHKey(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})
}

func TestHandleDeleteSSHKey(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()
	sshKeyStore := store.NewSSHKeyStore(s.DB)
	h := NewSSHKeyHandler(sshKeyStore)

	k, err := sshKeyStore.CreateSSHKey(ctx, "to-delete", "rsa", "pub", "priv", "fp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/ssh-keys/"+k.ID, nil)
	req = req.WithContext(ctx)
	req.SetPathValue("id", k.ID)
	w := httptest.NewRecorder()

	h.HandleDeleteSSHKey(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", w.Code)
	}
}
