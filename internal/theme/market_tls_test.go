package theme

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"
)

type marketTLSFixture struct {
	client                       *MarketClient
	server                       *httptest.Server
	mu                           sync.Mutex
	lookups, dials, hosts, names []string
	privateAfterFirst            bool
}

func newMarketTLSFixture(t *testing.T, redirectCount int) *marketTLSFixture {
	t.Helper()
	fixture := &marketTLSFixture{}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixture.mu.Lock()
		fixture.hosts = append(fixture.hosts, r.Host)
		fixture.names = append(fixture.names, r.TLS.ServerName)
		fixture.mu.Unlock()
		index, _ := strconv.Atoi(r.URL.Path[1:])
		if index < redirectCount {
			http.Redirect(w, r, "https://package.example.test/"+strconv.Itoa(index+1), http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("verified TLS and fixed destination"))
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{marketTLSCertificate(t)}}
	server.StartTLS()
	t.Cleanup(server.Close)
	fixture.server, fixture.client = server, NewMarketClient(false, time.Now)
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	fixture.client.SetRootCAs(pool)
	transport := fixture.client.client.Transport.(*http.Transport)
	transport.DialContext = checkedMarketDial(marketNetwork{lookup: fixture.lookup, dial: fixture.dial}, false)
	return fixture
}

func marketTLSCertificate(t *testing.T) tls.Certificate {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{"catalog.example.test", "package.example.test"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, public, private)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: private}
}

func (fixture *marketTLSFixture) lookup(_ context.Context, hostname string) ([]net.IPAddr, error) {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	fixture.lookups = append(fixture.lookups, hostname)
	if fixture.privateAfterFirst && len(fixture.lookups) > 1 {
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	}
	return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
}

func (fixture *marketTLSFixture) dial(ctx context.Context, network, destination string) (net.Conn, error) {
	fixture.mu.Lock()
	fixture.dials = append(fixture.dials, destination)
	fixture.mu.Unlock()
	if destination != "8.8.8.8:443" {
		return nil, errors.New("destination was not the verified numeric IP")
	}
	// 测试只连接自己创建的 loopback fixture，绝不拨公网占位地址。
	return (&net.Dialer{}).DialContext(ctx, network, fixture.server.Listener.Addr().String())
}

func TestMarketTransportPinsEachRedirectAndPreservesVerifiedTLSIdentity(t *testing.T) {
	fixture := newMarketTLSFixture(t, 1)
	raw, _, err := fixture.client.DownloadPackage(t.Context(), "https://catalog.example.test/0")
	if err != nil || string(raw) != "verified TLS and fixed destination" {
		t.Fatalf("TLS download: %q %v", raw, err)
	}
	wantHosts := []string{"catalog.example.test", "package.example.test"}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if !reflect.DeepEqual(wantHosts, fixture.lookups) || !reflect.DeepEqual(wantHosts, fixture.hosts) || !reflect.DeepEqual(wantHosts, fixture.names) {
		t.Fatalf("lost DNS/Host/SNI binding: lookup=%v hosts=%v names=%v", fixture.lookups, fixture.hosts, fixture.names)
	}
	if !reflect.DeepEqual([]string{"8.8.8.8:443", "8.8.8.8:443"}, fixture.dials) {
		t.Fatalf("re-resolved after validation: %v", fixture.dials)
	}
}

func TestMarketTransportRevalidatesRedirectDNSAndTLS(t *testing.T) {
	t.Run("rebound private address", func(t *testing.T) {
		fixture := newMarketTLSFixture(t, 1)
		fixture.privateAfterFirst = true
		if _, _, err := fixture.client.DownloadPackage(t.Context(), "https://catalog.example.test/0"); !errors.Is(err, ErrMarketInvalid) {
			t.Fatal(err)
		}
		fixture.mu.Lock()
		defer fixture.mu.Unlock()
		if len(fixture.dials) != 1 || len(fixture.hosts) != 1 {
			t.Fatalf("private redirect was dialed: %v", fixture.dials)
		}
	})
	t.Run("wrong certificate hostname", func(t *testing.T) {
		fixture := newMarketTLSFixture(t, 0)
		if _, _, err := fixture.client.DownloadPackage(t.Context(), "https://untrusted.example.test/0"); !errors.Is(err, ErrMarketUnavailable) {
			t.Fatal(err)
		}
	})
	for _, count := range []int{3, 4} {
		t.Run(strconv.Itoa(count)+" redirects", func(t *testing.T) {
			fixture := newMarketTLSFixture(t, count)
			_, _, err := fixture.client.DownloadPackage(t.Context(), "https://catalog.example.test/0")
			if (count == 3) != (err == nil) {
				t.Fatalf("redirect bound: %v", err)
			}
		})
	}
}
