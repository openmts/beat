package releasesignature

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func verifierFixture(t *testing.T, body string) Config {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fixed shell fixture requires Unix")
	}
	root := t.TempDir()
	path := filepath.Join(root, "verifier")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return Config{Executable: path, CertificateIdentity: "https://workflow.example/release", OIDCIssuer: "https://issuer.example", TrustedRoot: []byte(`{"fixture":true}`), TemporaryDirectory: root}
}

func testEvidence() Evidence {
	return Evidence{Payload: []byte("private-payload-fixture"), Bundle: []byte(`{"bundle":true}`)}
}

func TestVerifyFixedPolicyPrivateFilesAndEnvironment(t *testing.T) {
	t.Setenv("BEAT_SIGNATURE_SECRET", "must-not-reach-child")
	config := verifierFixture(t, `test "$1" = verify-blob
test "$2" = --offline
test "$3" = --new-bundle-format
test "$4" = --trusted-root
test -s "$5"
test "$6" = --bundle
test -s "$7"
test "$8" = --certificate-identity
test "$9" = https://workflow.example/release
test "${10}" = --certificate-oidc-issuer
test "${11}" = https://issuer.example
test -s "${12}"
test -z "${BEAT_SIGNATURE_SECRET:-}"
test "$(stat -c %a .)" = 700
test "$(stat -c %a payload)" = 600
test "$(stat -c %a bundle.json)" = 600
test "$(stat -c %a trusted-root.json)" = 600`)
	v, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Verify(t.Context(), testEvidence()); err != nil {
		t.Fatal(err)
	}
	assertVerifierClean(t, config.TemporaryDirectory)
	if strings.Contains(fmt.Sprintf("%#v", testEvidence()), "private-payload") {
		t.Fatal("evidence logging leaked payload")
	}
}

func assertVerifierClean(t *testing.T, root string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(root, ".beat-signature-*"))
	if err != nil || len(matches) != 0 {
		t.Fatal("verification left private work files", err)
	}
}

func TestVerifyRejectsFailureTimeoutAndExcessiveOutput(t *testing.T) {
	for _, test := range []struct {
		name, body string
		timeout    time.Duration
		want       error
	}{
		{"invalid signature", "echo private-payload-fixture >&2; exit 3", time.Second, ErrRejected},
		{"timeout", "exec /bin/sleep 3", 20 * time.Millisecond, context.DeadlineExceeded},
		{"output limit", "head -c 70000 /dev/zero", time.Second, ErrOutputLimit},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := verifierFixture(t, test.body)
			config.Timeout = test.timeout
			v, err := New(config)
			if err != nil {
				t.Fatal(err)
			}
			err = v.Verify(t.Context(), testEvidence())
			if !errors.Is(err, test.want) || strings.Contains(err.Error(), "private-payload") {
				t.Fatal("wrong bounded failure", err)
			}
			assertVerifierClean(t, config.TemporaryDirectory)
		})
	}
}

func TestVerifyRejectsInvalidInputAndReplacedExecutable(t *testing.T) {
	config := verifierFixture(t, "exit 0")
	v, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, evidence := range []Evidence{{}, {Payload: []byte("x")}, {Payload: make([]byte, MaximumPayloadBytes+1), Bundle: []byte("x")}, {Payload: []byte("x"), Bundle: make([]byte, MaximumBundleBytes+1)}} {
		if err := v.Verify(t.Context(), evidence); !errors.Is(err, ErrRejected) {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := v.Verify(ctx, testEvidence()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := os.Rename(config.Executable, config.Executable+".previous"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.Executable, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := v.Verify(t.Context(), testEvidence()); !errors.Is(err, ErrUnavailable) {
		t.Fatal("replaced verifier accepted", err)
	}
}

func TestVerifyUnavailableDirectoryAndExclusiveWrites(t *testing.T) {
	config := verifierFixture(t, "exit 0")
	private := filepath.Join(config.TemporaryDirectory, "work")
	if err := os.Mkdir(private, 0o700); err != nil {
		t.Fatal(err)
	}
	config.TemporaryDirectory = private
	v, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(private); err != nil {
		t.Fatal(err)
	}
	if err := v.Verify(t.Context(), testEvidence()); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if err := writePrivate(config.Executable, []byte("replacement")); err == nil {
		t.Fatal("exclusive private write replaced an existing file")
	}
}
