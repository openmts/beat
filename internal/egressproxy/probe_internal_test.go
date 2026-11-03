package egressproxy

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beat/backend/internal/secretbox"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
)

func newProbeService(t *testing.T) (*Service, *sql.DB) {
	t.Helper()
	database, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	secrets := testutil.NewSecrets(t, database.DB)
	service, err := NewService(store.NewEgressProxyStore(database.DB), secrets)
	if err != nil {
		t.Fatal(err)
	}
	return service, database.DB
}

func enableProbeService(t *testing.T, service *Service) {
	t.Helper()
	if _, err := service.Update(t.Context(), Update{Enabled: true, ProxyURL: "http://127.0.0.1:7890", ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
}

func TestProbeUsesFixedTargetAndReturnsLatency(t *testing.T) {
	service, _ := newProbeService(t)
	enableProbeService(t, service)
	var target string
	service.probeClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		target = request.URL.String()
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("<h1>Example Domain</h1>")), Header: make(http.Header)}, nil
	})}
	result, err := service.Probe(t.Context())
	if err != nil || target != probeTarget || result.CheckedAt.IsZero() || result.LatencyMS < 0 {
		t.Fatalf("probe = %+v, %v, target %q", result, err, target)
	}
}

func TestProbeFailsClosedForDisabledSettingsAndTransportFailures(t *testing.T) {
	service, _ := newProbeService(t)
	if _, err := service.Probe(t.Context()); !errors.Is(err, ErrDisabled) {
		t.Fatalf("disabled probe = %v", err)
	}
	enableProbeService(t, service)
	for _, testCase := range []struct {
		name string
		do   func(*http.Request) (*http.Response, error)
		want error
	}{
		{"timeout", func(*http.Request) (*http.Response, error) { return nil, context.DeadlineExceeded }, ErrProbeTimeout},
		{"transport", func(*http.Request) (*http.Response, error) { return nil, errors.New("dial failed") }, ErrProbeFailed},
		{"status", func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("down")), Header: make(http.Header)}, nil
		}, ErrProbeFailed},
		{"unexpected body", func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("proxy login page")), Header: make(http.Header)}, nil
		}, ErrProbeFailed},
	} {
		service.probeClient = &http.Client{Transport: roundTripFunc(testCase.do)}
		if _, err := service.Probe(t.Context()); !errors.Is(err, testCase.want) {
			t.Fatalf("%s probe = %v", testCase.name, err)
		}
	}
}

func TestProbeDefaultClientFailsFastAndRejectsRedirects(t *testing.T) {
	service, _ := newProbeService(t)
	enableProbeService(t, service)
	client := service.newProbeClient()
	request := httptest.NewRequest(http.MethodGet, probeTarget, nil)
	if err := client.CheckRedirect(request, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("redirect policy = %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	proxyAddress := listener.Addr().String()
	if err = listener.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Update(t.Context(), Update{Enabled: true, ProxyURL: "http://" + proxyAddress, ExpectedRevision: 2}); err != nil {
		t.Fatal(err)
	}
	service.probeClient = nil
	if _, err := service.Probe(t.Context()); !errors.Is(err, ErrProbeFailed) {
		t.Fatalf("closed local proxy probe = %v", err)
	}
}

func TestProbeAndSettingsFailClosedWhenStoreIsUnavailable(t *testing.T) {
	service, database := newProbeService(t)
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Get(t.Context()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("get = %v", err)
	}
	if _, err := service.Update(t.Context(), Update{Enabled: true, ProxyURL: "http://127.0.0.1:7890", ExpectedRevision: 1}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("update = %v", err)
	}
	if _, err := service.Proxy(httptest.NewRequest(http.MethodGet, probeTarget, nil)); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("proxy = %v", err)
	}
	if _, err := service.IsProxyAddress(t.Context(), "127.0.0.1:7890"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("proxy address = %v", err)
	}
	if _, err := service.Probe(t.Context()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("probe = %v", err)
	}
}

func TestUpdateFailsClosedOnEncryptionAndStoreErrors(t *testing.T) {
	database, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	baseSecrets := testutil.NewSecrets(t, database.DB)
	service, err := NewService(store.NewEgressProxyStore(database.DB), sealFailureSecrets{ResourceService: baseSecrets})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Update(t.Context(), Update{Enabled: true, ProxyURL: "http://127.0.0.1:7890", ExpectedRevision: 1}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("seal failure = %v", err)
	}

	service, err = NewService(store.NewEgressProxyStore(database.DB), baseSecrets)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = database.DB.Exec(`CREATE TRIGGER reject_egress_proxy_update BEFORE UPDATE ON egress_proxy_settings
		BEGIN SELECT RAISE(ABORT, 'rejected'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Update(t.Context(), Update{Enabled: true, ProxyURL: "http://127.0.0.1:7890", ExpectedRevision: 1}); err == nil || errors.Is(err, ErrConflict) {
		t.Fatalf("store failure = %v", err)
	}
}

type sealFailureSecrets struct {
	secretbox.ResourceService
}

func (secrets sealFailureSecrets) WithWrite(_ context.Context, write func(secretbox.ResourceAccess) error) error {
	return write(sealFailureAccess{open: secrets.OpenResource})
}

type sealFailureAccess struct {
	open func(context.Context, []byte, secretbox.ResourceAAD) ([]byte, error)
}

func (sealFailureAccess) Seal([]byte, secretbox.ResourceAAD) ([]byte, error) {
	return nil, errors.New("seal failed")
}

func (access sealFailureAccess) Open(ciphertext []byte, aad secretbox.ResourceAAD) ([]byte, error) {
	return access.open(context.Background(), ciphertext, aad)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
