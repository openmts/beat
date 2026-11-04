package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTLSSettingsFromEnv(t *testing.T) {
	tests := []struct {
		name    string
		values  map[string]string
		want    tlsSettings
		wantErr bool
	}{
		{name: "default disabled"},
		{name: "enabled with files", values: map[string]string{
			envTLSEnabled: "true", envTLSCertFile: " cert.pem ", envTLSKeyFile: "key.pem",
		}, want: tlsSettings{enabled: true, certFile: "cert.pem", keyFile: "key.pem"}},
		{name: "explicit false", values: map[string]string{envTLSEnabled: "false"}, want: tlsSettings{}},
		{name: "invalid boolean", values: map[string]string{envTLSEnabled: "sometimes"}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			settings, err := tlsSettingsFromEnv(func(key string) string { return test.values[key] })
			if test.wantErr {
				if err == nil || !strings.Contains(err.Error(), envTLSEnabled) {
					t.Fatalf("error = %v, want invalid %s error", err, envTLSEnabled)
				}
				return
			}
			if err != nil || settings != test.want {
				t.Fatalf("settings = %#v, error = %v, want %#v", settings, err, test.want)
			}
		})
	}
}

func TestLoadServerTLSConfigRejectsUnsafeConfiguration(t *testing.T) {
	certFile, keyFile, _ := writeServerTestCertificate(t)
	otherCertFile, otherKeyFile, _ := writeServerTestCertificate(t)
	tests := []struct {
		name     string
		settings tlsSettings
		want     string
	}{
		{name: "files without enable", settings: tlsSettings{certFile: certFile, keyFile: keyFile}, want: "require TLS to be enabled"},
		{name: "missing key", settings: tlsSettings{enabled: true, certFile: certFile}, want: "certificate or key file is missing"},
		{name: "missing certificate file", settings: tlsSettings{enabled: true, certFile: filepath.Join(t.TempDir(), "missing.pem"), keyFile: keyFile}, want: "inspect TLS file"},
		{name: "certificate directory", settings: tlsSettings{enabled: true, certFile: t.TempDir(), keyFile: keyFile}, want: "regular file"},
		{name: "mismatched key", settings: tlsSettings{enabled: true, certFile: certFile, keyFile: otherKeyFile}, want: "load TLS certificate and key"},
		{name: "different key", settings: tlsSettings{enabled: true, certFile: otherCertFile, keyFile: keyFile}, want: "load TLS certificate and key"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config, err := loadServerTLSConfig(test.settings)
			if err == nil || !strings.Contains(err.Error(), test.want) || config != nil {
				t.Fatalf("config = %#v, error = %v, want %q", config, err, test.want)
			}
		})
	}
}

func TestLoadServerTLSConfigRejectsLoosePermissions(t *testing.T) {
	certFile, keyFile, _ := writeServerTestCertificate(t)
	if err := os.Chmod(certFile, 0o666); err != nil {
		t.Fatalf("chmod certificate: %v", err)
	}
	if _, err := loadServerTLSConfig(tlsSettings{enabled: true, certFile: certFile, keyFile: keyFile}); err == nil || !strings.Contains(err.Error(), "writable") {
		t.Fatalf("certificate permission error = %v", err)
	}
	if err := os.Chmod(certFile, 0o600); err != nil {
		t.Fatalf("restore certificate: %v", err)
	}
	if err := os.Chmod(keyFile, 0o640); err != nil {
		t.Fatalf("chmod key: %v", err)
	}
	if _, err := loadServerTLSConfig(tlsSettings{enabled: true, certFile: certFile, keyFile: keyFile}); err == nil || !strings.Contains(err.Error(), "accessible by group or others") {
		t.Fatalf("key permission error = %v", err)
	}
}

func TestRunRejectsInvalidTLSBeforeDataInitialization(t *testing.T) {
	err := run(t.Context(), serverRunConfig{
		dbPath: "/dev/null/beat.db", mtsPath: t.TempDir(), listenAddr: "127.0.0.1:0", staticDir: t.TempDir(),
		tlsEnabled: true, tlsCertFile: "missing.pem", tlsKeyFile: "missing.key",
	})
	if err == nil || !strings.Contains(err.Error(), "configure tls") {
		t.Fatalf("error = %v, want TLS configuration error", err)
	}
}

func TestServeHTTPSTLSAndGracefulShutdown(t *testing.T) {
	certFile, keyFile, roots := writeServerTestCertificate(t)
	tlsConfig, err := loadServerTLSConfig(tlsSettings{enabled: true, certFile: certFile, keyFile: keyFile})
	if err != nil {
		t.Fatalf("load TLS config: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	address := availableServerAddress(t)
	done := make(chan error, 1)
	go func() {
		done <- serveHTTP(ctx, httpServerConfig{
			address: address, tlsConfig: tlsConfig,
			handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }),
		})
	}()

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			Proxy:             nil,
			ForceAttemptHTTP2: true,
			TLSClientConfig:   &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12},
		},
	}
	response := waitForHTTPSHealth(t, client, "https://"+address+"/healthz")
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusNoContent)
	}
	if response.TLS == nil || response.TLS.Version < tls.VersionTLS12 {
		t.Fatalf("TLS state = %#v, want TLS 1.2 or newer", response.TLS)
	}
	if response.ProtoMajor != 2 {
		t.Fatalf("protocol = %s, want HTTP/2", response.Proto)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close response: %v", err)
	}

	plain := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}}
	if plainResponse, err := plain.Get("http://" + address + "/healthz"); err == nil {
		status := plainResponse.StatusCode
		if closeErr := plainResponse.Body.Close(); closeErr != nil {
			t.Fatalf("close plain HTTP response: %v", closeErr)
		}
		if status < http.StatusBadRequest {
			t.Fatalf("plain HTTP request status = %d, want rejection", status)
		}
	}
	legacyConn, err := tls.Dial("tcp", address, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS10, MaxVersion: tls.VersionTLS11})
	if err == nil {
		_ = legacyConn.Close()
		t.Fatal("TLS 1.1 connection was accepted")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve HTTPS: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("HTTPS lifecycle did not stop")
	}
}

func waitForHTTPSHealth(t *testing.T, client *http.Client, target string) *http.Response {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get(target)
		if err == nil {
			return response
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("HTTPS server did not become healthy")
	return nil
}

func writeServerTestCertificate(t *testing.T) (string, string, *x509.CertPool) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, DNSNames: []string{"localhost"},
		BasicConstraintsValid: true, IsCA: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	directory := t.TempDir()
	certFile, keyFile := filepath.Join(directory, "cert.pem"), filepath.Join(directory, "key.pem")
	writeServerTestTLSFile(t, certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	writeServerTestTLSFile(t, keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	return certFile, keyFile, roots
}

func writeServerTestTLSFile(t *testing.T, path string, content []byte) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatalf("create TLS test file: %v", err)
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		t.Fatalf("write TLS test file: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close TLS test file: %v", err)
	}
}
