//go:build !linux && !windows

package agentoperations

import (
	"context"
	"errors"
	"testing"
)

func TestUnsupportedPlatformRejectsOperationsWithoutAdvertisingCapabilities(t *testing.T) {
	if operationPlatformSupported() || defaultShell() != "" || shellSupported("sh") || len(supportedShells()) != 0 {
		t.Fatal("unsupported execution advertised")
	}
	config := normalizeConfig(Config{Enabled: true, Commands: CommandConfig{Enabled: true, AllowAdHoc: true}})
	if _, err := capabilities(config); !errors.Is(err, ErrBoundary) {
		t.Fatal(err)
	}
	config.Enabled = false
	config.FileRoots = []FileRoot{{Read: true, Write: true}}
	policy, err := capabilities(config)
	if err != nil || policy.Enabled || len(policy.Kinds) != 0 || len(policy.Roots) != 0 || len(policy.SupportedShells) != 0 {
		t.Fatal(policy, err)
	}
	runner, err := NewRunner(RunnerConfig{Local: config})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Close(); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{helperArgument}, {guardianArgument}} {
		handled, status := HandleHelper(args)
		if !handled || status == 0 {
			t.Fatal(handled, status)
		}
	}
	if handled, _ := HandleHelper(nil); handled {
		t.Fatal("monitoring was treated as operation helper")
	}
	if _, err := resolveIdentity(CommandConfig{}); err != ErrBoundary {
		t.Fatal(err)
	}
	if err := validateBoundary(settings{}); err != ErrBoundary {
		t.Fatal(err)
	}
	engine := &engine{}
	if code, err := engine.execute(context.Background(), execution{}, nil); code != -1 || err != ErrBoundary {
		t.Fatal(code, err)
	}
	if err := engine.Recover("id"); err != ErrBoundary {
		t.Fatal(err)
	}
}

func TestUnsupportedPlatformRejectsAllFileMutations(t *testing.T) {
	for _, err := range []error{privateDirectory("", true), privateFile(""), replacePrivate("", ""), syncDirectory(""), secureCreatedDirectory(""), validateOperationDirectory(""), checkSensitiveFile(""), secureCreatedFile(nil), setTransferMode(nil, 0)} {
		if err != ErrBoundary {
			t.Fatal(err)
		}
	}
	if unlock, err := lockState(""); unlock != nil || err != ErrBoundary {
		t.Fatal(err)
	}
	if root, err := openTransferRoot(""); root != nil || err != ErrBoundary {
		t.Fatal(err)
	}
	root := &transferRoot{}
	if err := root.Close(); err != ErrBoundary {
		t.Fatal(err)
	}
	if file, err := root.openSource(""); file != nil || err != ErrBoundary {
		t.Fatal(err)
	}
	if dest, err := root.destination(""); dest != nil || err != ErrBoundary {
		t.Fatal(err)
	}
	dest := &transferDestination{}
	for _, err := range []error{dest.Close(), dest.publish("", false), dest.removeTemporary("")} {
		if err != ErrBoundary {
			t.Fatal(err)
		}
	}
	if file, err := dest.temporary("", false); file != nil || err != ErrBoundary {
		t.Fatal(err)
	}
	if file, err := dest.current(); file != nil || err != ErrBoundary {
		t.Fatal(err)
	}
	if _, err := transferFileIdentity(nil); err != ErrBoundary {
		t.Fatal(err)
	}
	if _, err := transferFileVersion(nil); err != ErrBoundary {
		t.Fatal(err)
	}
	if nativeNotExist(ErrBoundary) || transferModeMatches(nil, 0) {
		t.Fatal("unsupported file success")
	}
	if len(splitPath("/a/b")) != 3 || len(platformDeniedRoots()) != 1 {
		t.Fatal("invalid path metadata")
	}
}
