package releasesignature

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// 独立发布门禁提供已固定的公开签名fixture；普通单元测试不下载或签名。
func TestCosignOfflineIntegration(t *testing.T) {
	config, evidence := cosignFixture(t)
	v, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Verify(t.Context(), evidence); err != nil {
		t.Fatal("valid offline signed artifact rejected", err)
	}
	evidence.Payload[0] ^= 1
	if err := v.Verify(t.Context(), evidence); !errors.Is(err, ErrRejected) {
		t.Fatal("modified artifact accepted", err)
	}
	evidence.Payload[0] ^= 1
	config.CertificateIdentity = "https://untrusted.example/workflow"
	wrongIdentity, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := wrongIdentity.Verify(t.Context(), evidence); !errors.Is(err, ErrRejected) {
		t.Fatal("untrusted certificate identity accepted", err)
	}
	config, _ = cosignFixture(t)
	config.OIDCIssuer = "https://untrusted.example/issuer"
	wrongIssuer, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := wrongIssuer.Verify(t.Context(), evidence); !errors.Is(err, ErrRejected) {
		t.Fatal("untrusted issuer accepted", err)
	}
	assertVerifierClean(t, config.TemporaryDirectory)
}

func cosignFixture(t *testing.T) (Config, Evidence) {
	t.Helper()
	executable, fixture := os.Getenv("BEAT_COSIGN_TEST_EXECUTABLE"), os.Getenv("BEAT_COSIGN_TEST_FIXTURE")
	if executable == "" || fixture == "" {
		t.Skip("offline cosign integration fixture not configured")
	}
	read := func(name string) []byte {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(fixture, name))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	config := Config{Executable: executable, CertificateIdentity: string(read("identity.txt")), OIDCIssuer: "https://token.actions.githubusercontent.com", TrustedRoot: read("trusted-root.json"), TemporaryDirectory: t.TempDir()}
	evidence := Evidence{Payload: read("payload"), Bundle: read("bundle.json")}
	return config, evidence
}
