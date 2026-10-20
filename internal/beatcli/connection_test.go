package beatcli

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConnectionValidation(t *testing.T) {
	transport := &stubTransport{handler: func(*http.Request) (*http.Response, error) { return okBody(), nil }}
	noServer := runCommand(t, transport, nil, append(tokenArgs(t), "health")...)
	if noServer.err == nil || !strings.Contains(noServer.err.Error(), ServerEnv) {
		t.Fatalf("expected a server error, got %v", noServer.err)
	}
	noToken := runCommand(t, transport, nil, append(serverArgs(), "health")...)
	if noToken.err == nil || !strings.Contains(noToken.err.Error(), ServiceTokenEnv) {
		t.Fatalf("expected a token error, got %v", noToken.err)
	}
	zeroTimeout := runCommand(t, transport, nil, append(append(serverArgs(), tokenArgs(t)...), "--timeout", "0s", "health")...)
	if zeroTimeout.err == nil || !strings.Contains(zeroTimeout.err.Error(), "--timeout") {
		t.Fatalf("expected a timeout error, got %v", zeroTimeout.err)
	}
	caWithTransport := runCommand(t, transport, nil, append(append(serverArgs(), tokenArgs(t)...), "--ca-file", writeTokenFile(t, 0o600, "pem"), "health")...)
	if caWithTransport.err == nil || !strings.Contains(caWithTransport.err.Error(), "--ca-file") {
		t.Fatalf("expected a CA conflict error, got %v", caWithTransport.err)
	}
	invalidCA := runCommand(t, nil, nil, append(append(serverArgs(), tokenArgs(t)...), "--ca-file", writeTokenFile(t, 0o600, "not a pem"), "health")...)
	if invalidCA.err == nil || !strings.Contains(invalidCA.err.Error(), "PEM") {
		t.Fatalf("expected a CA parse error, got %v", invalidCA.err)
	}
}

func TestSubcommandsRequireConnection(t *testing.T) {
	transport := &stubTransport{handler: func(*http.Request) (*http.Response, error) { return okBody(), nil }}
	noEnv := func(string) string { return "" }
	missingServer := [][]string{
		append(tokenArgs(t), "nodes", "list"),
		append(tokenArgs(t), "nodes", "metrics", "n1"),
		append(tokenArgs(t), "backups", "list"),
		append(tokenArgs(t), "backups", "create"),
		append(tokenArgs(t), "backups", "download", "b1", "--output", "-"),
		append(tokenArgs(t), "audit", "list"),
	}
	for _, args := range missingServer {
		if result := runCommand(t, transport, noEnv, args...); result.err == nil {
			t.Fatalf("expected a server error for %v", args)
		}
	}
	missingToken := [][]string{
		append(serverArgs(), "nodes", "list"),
		append(serverArgs(), "nodes", "metrics", "n1"),
		append(serverArgs(), "backups", "list"),
		append(serverArgs(), "backups", "create"),
		append(serverArgs(), "audit", "list"),
	}
	for _, args := range missingToken {
		if result := runCommand(t, transport, noEnv, args...); result.err == nil {
			t.Fatalf("expected a token error for %v", args)
		}
	}
}

func TestDefaultTransportLoadsCustomCA(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, testCertificatePEM(t), 0o600); err != nil {
		t.Fatalf("write CA: %v", err)
	}
	args := append([]string{"--" + serverFlag, "http://127.0.0.1:9"}, tokenArgs(t)...)
	args = append(args, "--ca-file", path, "health")
	result := runCommand(t, nil, nil, args...)
	if result.err == nil {
		t.Fatal("expected the request to fail without a listener")
	}
	if strings.Contains(result.err.Error(), "CA bundle") || strings.Contains(result.err.Error(), "PEM") {
		t.Fatalf("expected the CA bundle to load, got %v", result.err)
	}
}

func TestServerEnvironmentFallbackAndIPv6(t *testing.T) {
	transport := &stubTransport{handler: func(*http.Request) (*http.Response, error) { return okBody(), nil }}
	env := func(name string) string {
		if name == ServerEnv {
			return "http://[::1]:8080"
		}
		return ""
	}
	result := runCommand(t, transport, env, append(tokenArgs(t), "health")...)
	if result.err != nil {
		t.Fatalf("unexpected error: %v", result.err)
	}
	if got := transport.requests[0].URL.String(); got != "http://[::1]:8080"+runtimePath {
		t.Fatalf("expected IPv6 URL, got %q", got)
	}
}

func TestTokenFromEnvironmentAndStandardInput(t *testing.T) {
	transport := &stubTransport{handler: func(*http.Request) (*http.Response, error) { return okBody(), nil }}
	env := func(name string) string {
		if name == ServiceTokenEnv {
			return "beat_service_v1_env"
		}
		return ""
	}
	if result := runCommand(t, transport, env, append(serverArgs(), "health")...); result.err != nil {
		t.Fatalf("unexpected error: %v", result.err)
	}
	if got := transport.requests[0].Header.Get("Authorization"); got != "Bearer beat_service_v1_env" {
		t.Fatalf("expected env token, got %q", got)
	}
}

func TestCertificatePoolParsesGeneratedCertificate(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "ca.pem")
	if err := os.WriteFile(path, testCertificatePEM(t), 0o600); err != nil {
		t.Fatalf("write CA: %v", err)
	}
	if _, err := certificatePool(path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := certificatePool(filepath.Join(directory, "absent.pem")); err == nil {
		t.Fatal("expected a missing CA error")
	}
}

func testCertificatePEM(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "beat-cli-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
