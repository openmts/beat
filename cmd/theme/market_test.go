package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/theme"
)

type publishFixture struct{ directory, private, public, entries, catalog, signature string }

func newPublishFixture(t *testing.T) publishFixture {
	t.Helper()
	dir := t.TempDir()
	fixture := publishFixture{directory: dir, private: filepath.Join(dir, "signing.key"), public: filepath.Join(dir, "trust.pub"),
		entries: filepath.Join(dir, "entries.json"), catalog: filepath.Join(dir, "catalog.json"), signature: filepath.Join(dir, "catalog.sig")}
	args := []string{"beat-theme", "market", "keygen", "--private-key", fixture.private, "--public-key", fixture.public}
	if err := newCommand(io.Discard).Run(t.Context(), args); err != nil {
		t.Fatal(err)
	}
	raw, err := theme.BuildArchive(theme.PackOptions{Source: commandSource(t)})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "slate.zip"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.entries, []byte(`[{"archive":"slate.zip","package_url":"https://example.com/slate.zip"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (fixture publishFixture) args() []string {
	return []string{"beat-theme", "market", "publish", "--private-key", fixture.private, "--entries", fixture.entries,
		"--output", fixture.catalog, "--signature", fixture.signature, "--expires-in", "24h"}
}

func TestMarketPublisherCLIProducesVerifiablePrivateArtifacts(t *testing.T) {
	fixture := newPublishFixture(t)
	var output bytes.Buffer
	if err := newCommand(&output).Run(t.Context(), fixture.args()); err != nil {
		t.Fatal(err)
	}
	publicText, err := os.ReadFile(fixture.public)
	if err != nil {
		t.Fatal(err)
	}
	public, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(publicText)))
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(fixture.catalog)
	if err != nil {
		t.Fatal(err)
	}
	signatureText, err := os.ReadFile(fixture.signature)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(signatureText)))
	if err != nil || !ed25519.Verify(public, body, signature) {
		t.Fatalf("invalid published signature: %v", err)
	}
	var catalog model.ThemeCatalog
	if err := json.Unmarshal(body, &catalog); err != nil {
		t.Fatal(err)
	}
	identity, err := theme.MarketIdentity(string(publicText))
	if err != nil || catalog.SourceID != identity || len(catalog.Themes) != 1 || !strings.Contains(output.String(), identity) {
		t.Fatalf("invalid catalog: %+v %v", catalog, err)
	}
	verifyPublisherArtifacts(t, fixture)
}

func verifyPublisherArtifacts(t *testing.T, fixture publishFixture) {
	t.Helper()
	for _, filename := range []string{fixture.private, fixture.public, fixture.catalog, fixture.signature} {
		info, err := os.Stat(filename)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("unsafe mode: %s %v", filename, err)
		}
	}
	var output bytes.Buffer
	args := []string{"beat-theme", "market", "identity", "--public-key", fixture.public}
	if err := newCommand(&output).Run(t.Context(), args); err != nil || !strings.HasPrefix(output.String(), "sha256:") {
		t.Fatalf("identity: %s %v", output.String(), err)
	}
	private, err := os.ReadFile(fixture.private)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), strings.TrimSpace(string(private))) {
		t.Fatal("private key was printed")
	}
}

func TestMarketCommandsRejectInvalidArgumentsAndProtectExistingFiles(t *testing.T) {
	fixture := newPublishFixture(t)
	for _, args := range [][]string{
		{"beat-theme", "market", "identity", "--public-key", fixture.public, "extra"},
		{"beat-theme", "market", "identity", "--public-key", filepath.Join(fixture.directory, "missing")},
		{"beat-theme", "market", "identity", "--public-key", fixture.entries},
		{"beat-theme", "market", "keygen", "--private-key", fixture.private, "--public-key", fixture.public},
		{"beat-theme", "market", "keygen", "--private-key", fixture.private, "--public-key", fixture.public, "extra"},
		append(fixture.args(), "extra"), append(fixture.args(), "--expires-in", "200h"),
	} {
		if err := newCommand(io.Discard).Run(t.Context(), args); err == nil {
			t.Fatalf("invalid command accepted: %v", args)
		}
	}
	partial := filepath.Join(fixture.directory, "new.key")
	if err := newCommand(io.Discard).Run(t.Context(), []string{"beat-theme", "market", "keygen", "--private-key", partial, "--public-key", fixture.public}); err == nil {
		t.Fatal("existing trust key overwritten")
	}
	if _, err := os.Stat(partial); !os.IsNotExist(err) {
		t.Fatalf("partial signing key retained: %v", err)
	}
}

func TestMarketCommandRejectsUnsafeInputsAndHandlesOutputFailure(t *testing.T) {
	fixture := newPublishFixture(t)
	if err := os.Chmod(fixture.private, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := newCommand(io.Discard).Run(t.Context(), fixture.args()); err == nil {
		t.Fatal("readable private key accepted")
	}
	if err := os.Chmod(fixture.private, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := newCommand(failingWriter{}).Run(t.Context(), fixture.args()); err == nil {
		t.Fatal("output failure ignored")
	}
	for _, content := range []string{`null`, `[{"extra":1}]`, `[] {}`, `[{"archive":"missing","package_url":"https://example.com/x"}]`, `invalid`} {
		if err := os.WriteFile(fixture.entries, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := readMarketInputs(fixture.entries); err == nil {
			t.Fatalf("invalid input accepted: %s", content)
		}
	}
	if err := os.WriteFile(fixture.private, []byte("invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readMarketPrivateKey(fixture.private); err == nil {
		t.Fatal("invalid private key accepted")
	}
}
