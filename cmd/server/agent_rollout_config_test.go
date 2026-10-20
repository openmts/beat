package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentrollout"
)

func TestRolloutConfigurationIsExplicitAndBounded(t *testing.T) {
	config, err := readRolloutConfiguration("")
	if err != nil || config.Enabled {
		t.Fatal("default rollout configuration enabled updates", err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	for _, input := range []string{"null", "{}", `{`, `{"schema_version":2}`, `{"schema_version":1,"shell":"unsafe"}`, `{"schema_version":1} {}`} {
		if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := readRolloutConfiguration(path); !errors.Is(err, errRolloutConfiguration) {
			t.Fatal("malformed startup configuration accepted", err)
		}
	}
	prefix := `{"schema_version":1,"enabled":false}`
	for _, size := range []int{maximumRolloutConfigurationBytes, maximumRolloutConfigurationBytes + 1} {
		if err := os.WriteFile(path, []byte(prefix+strings.Repeat(" ", size-len(prefix))), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := readRolloutConfiguration(path)
		if (err == nil) != (size == maximumRolloutConfigurationBytes) {
			t.Fatal("incorrect configuration size boundary", size, err)
		}
	}
}

func rolloutStartupFixture(t *testing.T, directory string) (string, rolloutConfiguration) {
	t.Helper()
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{71}, ed25519.SeedSize))
	now := time.Now().UTC()
	config := rolloutConfiguration{SchemaVersion: rolloutConfigurationVersion, Enabled: true,
		Keys: []rolloutTrustKey{{ID: "fixture-root", PublicKey: key.Public().(ed25519.PublicKey), NotBefore: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)}},
		Sigstore: rolloutSignatureTrust{Executable: filepath.Join(directory, "fixture-verifier"), TrustedRootFile: filepath.Join(directory, "fixture-root.json"),
			ReleaseWorkflow: "https://github.com/example/beat/.github/workflows/release.yml", OIDCIssuer: "https://token.actions.githubusercontent.com"}}
	if err := os.WriteFile(config.Sigstore.Executable, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.Sigstore.TrustedRootFile, []byte(`{"fixture":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(directory, "rollout.json"), config
}

func writeRolloutStartup(t *testing.T, path string, config rolloutConfiguration) {
	t.Helper()
	content, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BEAT_AGENT_ROLLOUT_CONFIG", path)
}

func TestRolloutSignatureModeIsExplicitAndUnambiguous(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	now := time.Now().UTC()
	public, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	key := `{"id":"offline-root","public_key":"` + base64.StdEncoding.EncodeToString(public) + `","not_before":"` +
		now.Format(time.RFC3339) + `","expires_at":"` + now.Add(time.Hour).Format(time.RFC3339) + `"}`
	sigstore := `{"executable":"/usr/bin/cosign","trusted_root_file":"/etc/trusted-root.json","certificate_identity":"https://example/identity","release_workflow":"https://example/workflow","oidc_issuer":"https://example/issuer"}`
	for _, testCase := range []struct {
		name    string
		body    string
		mode    string
		pinned  bool
		invalid bool
	}{
		{name: "缺省仍是 sigstore", body: `{"schema_version":1,"enabled":true,"sigstore":` + sigstore + `}`, mode: rolloutSignatureModeSigstore},
		{name: "显式 sigstore", body: `{"schema_version":1,"enabled":true,"signature_mode":"sigstore","sigstore":` + sigstore + `}`, mode: rolloutSignatureModeSigstore},
		{name: "固定公钥模式无需 sigstore", body: `{"schema_version":1,"enabled":true,"signature_mode":"pinned_ed25519","keys":[` + key + `]}`, mode: rolloutSignatureModePinned, pinned: true},
		{name: "固定公钥模式必须有公钥", body: `{"schema_version":1,"enabled":true,"signature_mode":"pinned_ed25519"}`, invalid: true},
		{name: "固定公钥模式不接受 sigstore 块", body: `{"schema_version":1,"enabled":true,"signature_mode":"pinned_ed25519","keys":[` + key + `],"sigstore":` + sigstore + `}`, invalid: true},
		{name: "未知模式被拒", body: `{"schema_version":1,"enabled":true,"signature_mode":"trust-me"}`, invalid: true},
	} {
		if err := os.WriteFile(path, []byte(testCase.body), 0o600); err != nil {
			t.Fatal(err)
		}
		config, err := readRolloutConfiguration(path)
		if testCase.invalid {
			if !errors.Is(err, errRolloutConfiguration) {
				t.Fatal(testCase.name, "accepted", err)
			}
			continue
		}
		if err != nil || config.SignatureMode != testCase.mode {
			t.Fatal(testCase.name, config.SignatureMode, err)
		}
		if !testCase.pinned {
			continue
		}
		// 固定公钥模式必须能在没有 Sigstore 策略文件的情况下完成启动信任登记。
		options := agentrollout.Config{}
		if err := configureRolloutTrust(&options, config, t.TempDir()); err != nil || !options.PinnedOnly || len(options.Keys) != 1 || options.Sigstore != nil {
			t.Fatal("pinned trust options", options.PinnedOnly, len(options.Keys), err)
		}
	}
}
