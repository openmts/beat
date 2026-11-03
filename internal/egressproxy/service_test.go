package egressproxy_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beat/backend/internal/applicationsecrets"
	"github.com/beat/backend/internal/egressproxy"
	"github.com/beat/backend/internal/secretbox"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
)

type fixture struct {
	store   *store.SQLiteStore
	secrets *applicationsecrets.Service
	service *egressproxy.Service
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	database, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	secrets := testutil.NewSecrets(t, database.DB)
	service, err := egressproxy.NewService(store.NewEgressProxyStore(database.DB), secrets)
	if err != nil {
		t.Fatal(err)
	}
	return fixture{store: database, secrets: secrets, service: service}
}

type cancelAfterWriteSecrets struct {
	secretbox.ResourceService
	cancel context.CancelFunc
}

func (secrets cancelAfterWriteSecrets) WithWrite(ctx context.Context, write func(secretbox.ResourceAccess) error) error {
	err := secrets.ResourceService.WithWrite(ctx, write)
	secrets.cancel()
	return err
}

func TestEgressProxyUpdateReturnsCommittedSettingsAfterRequestCancellation(t *testing.T) {
	database, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	secrets := testutil.NewSecrets(t, database.DB)
	ctx, cancel := context.WithCancel(t.Context())
	service, err := egressproxy.NewService(store.NewEgressProxyStore(database.DB), cancelAfterWriteSecrets{ResourceService: secrets, cancel: cancel})
	if err != nil {
		t.Fatal(err)
	}
	password := "proxy-secret"
	updated, err := service.Update(ctx, egressproxy.Update{
		Enabled: true, ProxyURL: "http://127.0.0.1:7890", Username: "proxy-user",
		Password: &password, ExpectedRevision: 1,
	})
	if err != nil || !updated.Enabled || !updated.HasPassword || updated.Revision != 2 {
		t.Fatalf("committed update = %+v, %v", updated, err)
	}
	var revision int64
	if err := database.DB.QueryRow(`SELECT revision FROM egress_proxy_settings WHERE id=1`).Scan(&revision); err != nil || revision != 2 {
		t.Fatalf("stored revision = %d, %v", revision, err)
	}
}

func TestEgressProxyLifecycleEncryptsCredentialsAndResolvesRequests(t *testing.T) {
	f := newFixture(t)
	initial, err := f.service.Get(t.Context())
	if err != nil || initial.Enabled || initial.Revision != 1 {
		t.Fatalf("initial: %+v %v", initial, err)
	}
	password := "proxy-secret"
	updated, err := f.service.Update(t.Context(), egressproxy.Update{
		Enabled: true, ProxyURL: "http://127.0.0.1:7890", Username: "proxy-user",
		Password: &password, ExpectedRevision: initial.Revision,
	})
	if err != nil || !updated.Enabled || !updated.HasPassword || updated.Revision != 2 || updated.Username != "proxy-user" {
		t.Fatalf("updated: %+v %v", updated, err)
	}
	var encrypted []byte
	if err := f.store.DB.QueryRow("SELECT proxy_secret_encrypted FROM egress_proxy_settings WHERE id=1").Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, []byte(password)) || bytes.Contains(encrypted, []byte("127.0.0.1")) {
		t.Fatal("proxy secret was stored in plaintext")
	}
	request := httptest.NewRequest("GET", "https://api.github.com/repos/P3TERX/GeoLite.mmdb/releases/latest", nil)
	proxyURL, err := f.service.Proxy(request)
	if err != nil || proxyURL.String() != "http://proxy-user:proxy-secret@127.0.0.1:7890" {
		t.Fatalf("proxy URL: %v %v", proxyURL, err)
	}
	if allowed, err := f.service.IsProxyAddress(t.Context(), "127.0.0.1:7890"); err != nil || !allowed {
		t.Fatalf("configured proxy address was rejected: %v %v", allowed, err)
	}
	if allowed, err := f.service.IsProxyAddress(t.Context(), "127.0.0.1:7891"); err != nil || allowed {
		t.Fatalf("foreign proxy address was allowed: %v %v", allowed, err)
	}

	updated, err = f.service.Update(t.Context(), egressproxy.Update{
		Enabled: true, ProxyURL: "socks5://127.0.0.1:1080", Username: "proxy-user", ExpectedRevision: updated.Revision,
	})
	if err != nil || !updated.HasPassword {
		t.Fatalf("password was not preserved: %+v %v", updated, err)
	}
	proxyURL, err = f.service.Proxy(request)
	if err != nil || proxyURL.String() != "socks5://proxy-user:proxy-secret@127.0.0.1:1080" {
		t.Fatalf("preserved proxy URL: %v %v", proxyURL, err)
	}

	updated, err = f.service.Update(t.Context(), egressproxy.Update{
		Enabled: false, ProxyURL: "socks5://127.0.0.1:1080", Username: "proxy-user",
		ClearPassword: true, ExpectedRevision: updated.Revision,
	})
	if err != nil || updated.Enabled || updated.HasPassword {
		t.Fatalf("disabled settings: %+v %v", updated, err)
	}
	proxyURL, err = f.service.Proxy(request)
	if err != nil || proxyURL != nil {
		t.Fatalf("disabled proxy still active: %v %v", proxyURL, err)
	}
}

func TestEgressProxyRejectsInvalidInputAndRevisionConflicts(t *testing.T) {
	f := newFixture(t)
	longPassword := strings.Repeat("p", 2049)
	for _, input := range []egressproxy.Update{
		{Enabled: true, ExpectedRevision: 1},
		{Enabled: true, ProxyURL: "ftp://127.0.0.1:21", ExpectedRevision: 1},
		{Enabled: true, ProxyURL: "http://127.0.0.1", ExpectedRevision: 1},
		{Enabled: true, ProxyURL: "http://:8080", ExpectedRevision: 1},
		{Enabled: true, ProxyURL: "http://user:pass@127.0.0.1:8080", ExpectedRevision: 1},
		{Enabled: true, ProxyURL: "http://127.0.0.1:8080/path", ExpectedRevision: 1},
		{Enabled: true, ProxyURL: "http://127.0.0.1:8080?q=1", ExpectedRevision: 1},
		{Enabled: true, ProxyURL: "http://127.0.0.1:8080/\\", ExpectedRevision: 1},
		{Enabled: true, ProxyURL: "http://" + strings.Repeat("a", 2050) + ":80", ExpectedRevision: 1},
		{Enabled: true, ProxyURL: "http://127.0.0.1:8080", Password: &longPassword, ExpectedRevision: 1},
		{Enabled: false, ProxyURL: "http://127.0.0.1:8080", Username: "bad\nuser", ExpectedRevision: 1},
	} {
		if _, err := f.service.Update(t.Context(), input); !errors.Is(err, egressproxy.ErrInvalid) {
			t.Fatalf("input %+v returned %v", input, err)
		}
	}
	_, err := f.service.Update(t.Context(), egressproxy.Update{Enabled: true, ProxyURL: "http://127.0.0.1:8080", ExpectedRevision: 2})
	if !errors.Is(err, egressproxy.ErrConflict) {
		t.Fatalf("revision conflict returned %v", err)
	}
}

func TestEgressProxyAcceptsSupportedURLsAndRejectsUnavailableStore(t *testing.T) {
	for _, value := range []string{
		"http://127.0.0.1:8080", "https://proxy.example:8443", "socks5://proxy.example:1080", "socks5h://proxy.example:1080",
	} {
		if _, err := egressproxy.ValidateURL(value, true); err != nil {
			t.Fatalf("validate %q: %v", value, err)
		}
	}
	if _, err := egressproxy.ValidateURL("", false); err != nil {
		t.Fatal(err)
	}
	if _, err := egressproxy.NewService(nil, nil); !errors.Is(err, egressproxy.ErrUnavailable) {
		t.Fatalf("nil service returned %v", err)
	}
}

func TestEgressProxyTransportAndInvalidHelpers(t *testing.T) {
	f := newFixture(t)
	base := &http.Transport{DisableKeepAlives: true}
	transport := f.service.Transport(base)
	if transport == base || transport.Proxy == nil || !transport.DisableKeepAlives {
		t.Fatal("transport was not copied with the dynamic proxy")
	}
	if f.service.Transport(nil).Proxy == nil {
		t.Fatal("default transport was not wrapped")
	}
	if _, err := f.service.Proxy(nil); !errors.Is(err, egressproxy.ErrInvalid) || !egressproxy.IsInvalid(err) {
		t.Fatalf("nil request error = %v", err)
	}
	if _, err := f.service.Update(t.Context(), egressproxy.Update{}); !errors.Is(err, egressproxy.ErrInvalid) {
		t.Fatalf("zero revision error = %v", err)
	}
	if allowed, err := f.service.IsProxyAddress(t.Context(), "bad-address"); err != nil || allowed {
		t.Fatalf("disabled or malformed address = %v %v", allowed, err)
	}
}

func TestEgressProxyFailsClosedOnCorruptPayloadAndCancellation(t *testing.T) {
	f := newFixture(t)
	if _, err := f.store.DB.Exec(`UPDATE egress_proxy_settings SET enabled=1,proxy_secret_encrypted=X'00'`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Get(t.Context()); !errors.Is(err, egressproxy.ErrUnavailable) {
		t.Fatalf("corrupt ciphertext = %v", err)
	}

	f = newFixture(t)
	var malformed []byte
	if err := f.secrets.WithWrite(t.Context(), func(access secretbox.ResourceAccess) error {
		var sealErr error
		malformed, sealErr = access.Seal([]byte(`{`), secretbox.EgressProxyAAD(1))
		return sealErr
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB.Exec(`UPDATE egress_proxy_settings SET enabled=1,proxy_secret_encrypted=?`, malformed); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Get(t.Context()); !errors.Is(err, egressproxy.ErrUnavailable) {
		t.Fatalf("malformed payload = %v", err)
	}

	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := f.service.Get(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v", err)
	}
}

func TestEgressProxyProxyAddressAndDefaultTransportEdges(t *testing.T) {
	f := newFixture(t)
	updated, err := f.service.Update(t.Context(), egressproxy.Update{
		Enabled: true, ProxyURL: "http://proxy.example:7890", ExpectedRevision: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if allowed, err := f.service.IsProxyAddress(t.Context(), "bad-address"); err != nil || allowed {
		t.Fatalf("malformed dial address = %v %v", allowed, err)
	}
	if allowed, err := f.service.IsProxyAddress(t.Context(), "proxy.example:7890"); err != nil || !allowed {
		t.Fatalf("proxy address = %v %v", allowed, err)
	}

	var malformed []byte
	if err = f.secrets.WithWrite(t.Context(), func(access secretbox.ResourceAccess) error {
		var sealErr error
		malformed, sealErr = access.Seal([]byte(`{"proxy_url":"http://%"}`), secretbox.EgressProxyAAD(updated.Revision+1))
		return sealErr
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.DB.Exec(`UPDATE egress_proxy_settings SET enabled=1,revision=?,proxy_secret_encrypted=?`, updated.Revision+1, malformed); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.Proxy(httptest.NewRequest("GET", "https://example.com", nil)); !errors.Is(err, egressproxy.ErrUnavailable) {
		t.Fatalf("invalid stored proxy = %v", err)
	}
	if allowed, err := f.service.IsProxyAddress(t.Context(), "proxy.example:7890"); !errors.Is(err, egressproxy.ErrUnavailable) || allowed {
		t.Fatalf("invalid stored address = %v %v", allowed, err)
	}

	previous := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("fixture") })
	t.Cleanup(func() { http.DefaultTransport = previous })
	transport := f.service.Transport(nil)
	if transport == nil || transport.Proxy == nil {
		t.Fatal("non-default standard transport path was not covered")
	}
	f.service.Transport(&http.Transport{})
}

func TestEgressProxyEnabledWithoutSecretFailsClosed(t *testing.T) {
	f := newFixture(t)
	if _, err := f.store.DB.Exec(`UPDATE egress_proxy_settings SET enabled=1, proxy_secret_encrypted=NULL`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Get(t.Context()); !errors.Is(err, egressproxy.ErrUnavailable) {
		t.Fatalf("get = %v", err)
	}
	if _, err := f.service.Proxy(httptest.NewRequest(http.MethodGet, "https://example.com", nil)); !errors.Is(err, egressproxy.ErrUnavailable) {
		t.Fatalf("proxy = %v", err)
	}
	if allowed, err := f.service.IsProxyAddress(t.Context(), "127.0.0.1:7890"); !errors.Is(err, egressproxy.ErrUnavailable) || allowed {
		t.Fatalf("proxy address = %v, %v", allowed, err)
	}
	if _, err := f.service.Probe(t.Context()); !errors.Is(err, egressproxy.ErrUnavailable) {
		t.Fatalf("probe = %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
