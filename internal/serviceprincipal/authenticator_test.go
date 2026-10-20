package serviceprincipal

import (
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func authenticatorFixture(t *testing.T) (*Authenticator, *fakeStore, model.ServicePrincipal, string) {
	t.Helper()
	token, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	value := model.ServicePrincipal{
		ID: "principal-one", Name: "backup", TokenPrefix: token.Prefix, TokenHash: token.Hash,
		Scopes:    []model.ServiceScope{model.ServiceScopeBackupsCreate},
		ExpiresAt: serviceNow().Add(24 * time.Hour),
	}
	storage := newFakeStore(value)
	authenticator := NewAuthenticator(storage)
	authenticator.now = func() time.Time { return serviceNow() }
	return authenticator, storage, value, token.Plaintext
}

func TestAuthenticateAcceptsValidToken(t *testing.T) {
	authenticator, storage, _, plaintext := authenticatorFixture(t)
	principal, err := authenticator.Authenticate(t.Context(), plaintext, netip.AddrFrom4([4]byte{192, 0, 2, 10}))
	if err != nil {
		t.Fatal(err)
	}
	if principal.ID != "principal-one" || principal.TokenHash == nil {
		t.Fatal("authenticated principal is incomplete", principal)
	}
	if len(storage.touched) != 1 {
		t.Fatal("first authentication did not record use", storage.touched)
	}
}

func TestAuthenticateRejectsUniformly(t *testing.T) {
	authenticator, storage, value, plaintext := authenticatorFixture(t)
	token, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	revoked := serviceNow().Add(-time.Minute)
	expired := model.ServicePrincipal{ID: "principal-expired", TokenHash: token.Hash, ExpiresAt: serviceNow()}
	superseded := model.ServicePrincipal{ID: "principal-old", TokenHash: token.Hash, ExpiresAt: serviceNow().Add(time.Hour),
		SupersededAt: &revoked, RotationOverlapSeconds: 60}
	cases := map[string]struct {
		token  string
		source netip.Addr
		stored *model.ServicePrincipal
	}{
		"malformed":      {"not-a-token", netip.AddrFrom4([4]byte{192, 0, 2, 10}), nil},
		"wrong digest":   {token.Plaintext, netip.AddrFrom4([4]byte{192, 0, 2, 10}), &value},
		"revoked":        {token.Plaintext, netip.AddrFrom4([4]byte{192, 0, 2, 10}), &model.ServicePrincipal{ID: "principal-revoked", TokenHash: token.Hash, ExpiresAt: serviceNow().Add(time.Hour), RevokedAt: &revoked}},
		"expired":        {token.Plaintext, netip.AddrFrom4([4]byte{192, 0, 2, 10}), &expired},
		"superseded":     {token.Plaintext, netip.AddrFrom4([4]byte{192, 0, 2, 10}), &superseded},
		"source refused": {plaintext, netip.AddrFrom4([4]byte{198, 51, 100, 7}), nil},
	}
	for name, test := range cases {
		storage.values = map[string]model.ServicePrincipal{}
		if test.stored != nil {
			storage.values[test.stored.ID] = *test.stored
		} else if name != "malformed" {
			storage.values[value.ID] = value
		}
		if name == "source refused" {
			stored := value
			stored.SourceCIDRs = []string{"192.0.2.0/24"}
			storage.values = map[string]model.ServicePrincipal{stored.ID: stored}
		}
		if _, err := authenticator.Authenticate(t.Context(), test.token, test.source); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("%s returned %v", name, err)
		}
	}
}

func TestAuthenticateDistinguishesStorageFailure(t *testing.T) {
	authenticator, storage, _, plaintext := authenticatorFixture(t)
	storage.listErr = errors.New("database unavailable")
	if _, err := authenticator.Authenticate(t.Context(), plaintext, netip.AddrFrom4([4]byte{192, 0, 2, 10})); !errors.Is(err, ErrUnavailable) {
		t.Fatal("storage failure was reported as an authorization decision", err)
	}
}

func TestAuthenticateThrottlesUsageRecording(t *testing.T) {
	authenticator, storage, principal, plaintext := authenticatorFixture(t)
	recent := serviceNow().Add(-time.Minute)
	stored := principal
	stored.LastUsedAt = &recent
	storage.values = map[string]model.ServicePrincipal{stored.ID: stored}
	if _, err := authenticator.Authenticate(t.Context(), plaintext, netip.AddrFrom4([4]byte{192, 0, 2, 10})); err != nil {
		t.Fatal(err)
	}
	if len(storage.touched) != 0 {
		t.Fatal("recent usage was recorded again", storage.touched)
	}
	old := serviceNow().Add(-time.Hour)
	stored.LastUsedAt = &old
	storage.values = map[string]model.ServicePrincipal{stored.ID: stored}
	if _, err := authenticator.Authenticate(t.Context(), plaintext, netip.AddrFrom4([4]byte{192, 0, 2, 10})); err != nil {
		t.Fatal(err)
	}
	if len(storage.touched) != 1 {
		t.Fatal("stale usage was not recorded", storage.touched)
	}
	storage.touchErr = errors.New("write failed")
	storage.touched = nil
	if _, err := authenticator.Authenticate(t.Context(), plaintext, netip.AddrFrom4([4]byte{192, 0, 2, 10})); err != nil {
		t.Fatal("usage recording failure rejected an authenticated request", err)
	}
}

func TestAuthenticateRejectsUnknownToken(t *testing.T) {
	authenticator, storage, _, plaintext := authenticatorFixture(t)
	storage.values = map[string]model.ServicePrincipal{}
	if _, err := authenticator.Authenticate(t.Context(), plaintext, netip.AddrFrom4([4]byte{192, 0, 2, 10})); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("unknown token was accepted", err)
	}
	storage.values = map[string]model.ServicePrincipal{}
	storage.listErr = store.ErrServicePrincipalNotFound
	if _, err := authenticator.Authenticate(t.Context(), plaintext, netip.AddrFrom4([4]byte{192, 0, 2, 10})); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("missing principal was not classified as unauthorized", err)
	}
}
