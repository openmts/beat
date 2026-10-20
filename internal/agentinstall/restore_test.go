package agentinstall

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func receiptWithoutConfig(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t)
	if _, err := f.installer.Join(context.Background(), f.options); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(f.installer.paths.config); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestServiceInstallRestoresMissingDefaultConfigFromReceipt(t *testing.T) {
	f := receiptWithoutConfig(t)
	f.installer.client = nil
	if err := f.installer.Install(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	f.assertInstalled(t)
	if f.requests.Load() != 1 {
		t.Fatal("local receipt recovery must not enroll again")
	}
}

func TestServiceInstallDoesNotRestoreCustomConfigPath(t *testing.T) {
	f := receiptWithoutConfig(t)
	custom := filepath.Join(f.installer.paths.stateDir, "custom.json")
	if err := f.installer.Install(context.Background(), custom); err == nil {
		t.Fatal("custom config path must not use default receipt")
	}
	if _, err := os.Stat(custom); !os.IsNotExist(err) {
		t.Fatalf("custom config unexpectedly created: %v", err)
	}
	if f.requests.Load() != 1 {
		t.Fatal("custom path failure must not contact server")
	}
}

func TestServiceInstallRejectsDamagedReceipt(t *testing.T) {
	cases := map[string]func(*enrollment){
		"version": func(state *enrollment) { state.Version++ },
		"pending": func(state *enrollment) { state.Result = nil },
		"digest":  func(state *enrollment) { state.JoinHash = "invalid" },
		"server":  func(state *enrollment) { state.ServerURL = "https://invalid/path" },
		"request": func(state *enrollment) { state.Request.AgentToken = "invalid" },
		"result":  func(state *enrollment) { state.Result.NodeID = "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := receiptWithoutConfig(t)
			var state enrollment
			if _, err := loadPrivateJSON(filepath.Join(f.installer.paths.stateDir, "enrollment.json"), &state); err != nil {
				t.Fatal(err)
			}
			mutate(&state)
			if err := f.installer.saveEnrollment(&state); err != nil {
				t.Fatal(err)
			}
			if err := f.installer.Install(context.Background(), ""); err == nil {
				t.Fatal("damaged receipt must not restore config")
			}
			if _, err := os.Stat(f.installer.paths.config); !os.IsNotExist(err) {
				t.Fatalf("config unexpectedly restored: %v", err)
			}
		})
	}
}

func TestServiceInstallHandlesReceiptReadAndWriteFailures(t *testing.T) {
	f := receiptWithoutConfig(t)
	f.installer.write = func(string, []byte, os.FileMode) error { return errors.New("disk failure") }
	if err := f.installer.Install(context.Background(), ""); err == nil {
		t.Fatal("expected config restore failure")
	}
	mustWrite(t, filepath.Join(f.installer.paths.stateDir, "enrollment.json"), []byte("malformed JSON"), privateFile)
	if err := f.installer.Install(context.Background(), ""); err == nil {
		t.Fatal("expected malformed receipt error")
	}
}
