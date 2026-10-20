package agentinstall

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/agentcredential"
	"github.com/beat/backend/internal/agentjoin"
)

func TestExpiredUnusedTokenCanBeRefreshed(t *testing.T) {
	testPendingTokenRefresh(t, false)
}

func TestLostResponseCanBeRecoveredWithFreshTokenAfterExpiry(t *testing.T) {
	testPendingTokenRefresh(t, true)
}

func testPendingTokenRefresh(t *testing.T, committed bool) {
	f := newFixture(t)
	oldToken := f.options.Token
	var request agentjoin.Request
	credentials, nodes := 0, 0
	generator := f.installer.credential
	f.installer.credential = func() (agentcredential.Token, error) { credentials++; return generator() }
	f.respond = func(w http.ResponseWriter, r *http.Request) {
		var current agentjoin.Request
		if err := json.NewDecoder(r.Body).Decode(&current); err != nil {
			t.Error(err)
			return
		}
		if request.RequestID == "" {
			request = current
		} else if request != current {
			t.Error("token refresh changed stable enrollment identity")
		}
		if r.Header.Get("Authorization") == "Bearer "+oldToken {
			if committed && f.requests.Load() == 1 {
				nodes++
				w.WriteHeader(http.StatusGatewayTimeout)
			} else {
				w.WriteHeader(http.StatusUnauthorized)
			}
			return
		}
		assertPendingTokenDigest(t, f)
		if !committed {
			nodes++
		}
		_ = json.NewEncoder(w).Encode(agentjoin.Envelope[agentjoin.Result]{Code: http.StatusOK, Message: "success", Data: f.result})
	}
	if _, err := f.installer.Join(context.Background(), f.options); err == nil {
		t.Fatal("expected initial enrollment failure")
	}
	if committed {
		if _, err := f.installer.Join(context.Background(), f.options); err == nil {
			t.Fatal("expected expired old token")
		}
	}
	f.options.Token = freshToken(t)
	if _, err := f.installer.Join(context.Background(), f.options); err != nil {
		t.Fatal(err)
	}
	if credentials != 1 || nodes != 1 {
		t.Fatalf("want one credential and one node; got %d credentials, %d nodes", credentials, nodes)
	}
	f.assertInstalled(t)
}

func TestRevokedIdentityIsPreservedForAdministratorRecovery(t *testing.T) {
	f := newFixture(t)
	f.respond = func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusGatewayTimeout) }
	if _, err := f.installer.Join(context.Background(), f.options); err == nil {
		t.Fatal("expected lost response")
	}
	var original enrollment
	statePath := filepath.Join(f.installer.paths.stateDir, "enrollment.json")
	if _, err := loadPrivateJSON(statePath, &original); err != nil {
		t.Fatal(err)
	}
	f.options.Token = freshToken(t)
	f.respond = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(agentjoin.Envelope[any]{Code: http.StatusForbidden, Message: agentjoin.AgentIdentityUnavailableMessage})
	}
	if _, err := f.installer.Join(context.Background(), f.options); !errors.Is(err, errAgentIdentityUnavailable) {
		t.Fatalf("want administrator-recovery error, got %v", err)
	}
	var preserved enrollment
	if _, err := loadPrivateJSON(statePath, &preserved); err != nil {
		t.Fatal(err)
	}
	if preserved.Request != original.Request || preserved.Result != nil {
		t.Fatal("revoked identity was replaced or completed")
	}
}

func TestTokenRefreshMustBeDurableBeforeNetwork(t *testing.T) {
	f := newFixture(t)
	f.respond = func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) }
	if _, err := f.installer.Join(context.Background(), f.options); err == nil {
		t.Fatal("expected expired token")
	}
	f.options.Token = freshToken(t)
	f.installer.write = func(string, []byte, os.FileMode) error { return errors.New("state disk failure") }
	if _, err := f.installer.Join(context.Background(), f.options); err == nil {
		t.Fatal("expected state save error")
	}
	if f.requests.Load() != 1 {
		t.Fatal("token used before refreshing state was durable")
	}
}

func freshToken(t *testing.T) string {
	t.Helper()
	token, err := agentjoin.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	return token.Plaintext
}

func assertPendingTokenDigest(t *testing.T, f *fixture) {
	t.Helper()
	var state enrollment
	if _, err := loadPrivateJSON(filepath.Join(f.installer.paths.stateDir, "enrollment.json"), &state); err != nil {
		t.Error(err)
		return
	}
	if state.JoinHash != tokenHash(f.options.Token) {
		t.Error("new token digest was not persisted before network")
	}
}
