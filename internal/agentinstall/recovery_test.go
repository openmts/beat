package agentinstall

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beat/backend/internal/agentjoin"
)

func TestLostResponseReusesDurableRequest(t *testing.T) {
	f := newFixture(t)
	var first agentjoin.Request
	f.respond = func(w http.ResponseWriter, r *http.Request) {
		var request agentjoin.Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		if f.requests.Load() == 1 {
			first = request
			state, err := readPrivate(filepath.Join(f.installer.paths.stateDir, "enrollment.json"))
			if err != nil || !strings.Contains(string(state), request.RequestID) {
				t.Error("request not durable before network")
			}
			assertMode(t, f.installer.paths.binary, executableFile)
			http.Error(w, "simulated lost response", http.StatusGatewayTimeout)
			return
		}
		if first != request {
			t.Error("retry generated different request or agent credentials")
		}
		_ = json.NewEncoder(w).Encode(agentjoin.Envelope[agentjoin.Result]{Code: http.StatusOK, Message: "success", Data: f.result})
	}
	if _, err := f.installer.Join(context.Background(), f.options); err == nil {
		t.Fatal("expected lost-response error")
	}
	if _, err := f.installer.Join(context.Background(), f.options); err != nil {
		t.Fatal(err)
	}
	if f.requests.Load() != 2 {
		t.Fatalf("want two attempts, got %d", f.requests.Load())
	}
}

func TestJoinRecoversAfterLocalWriteFailure(t *testing.T) {
	for _, file := range []string{"enrollment.json", "installation.json", "binary", "config", "unit"} {
		t.Run(file, func(t *testing.T) { testJoinWriteRecovery(t, file) })
	}
}

func testJoinWriteRecovery(t *testing.T, file string) {
	f := newFixture(t)
	path := filepath.Join(f.installer.paths.stateDir, file)
	switch file {
	case "binary":
		path = f.installer.paths.binary
	case "config":
		path = f.installer.paths.config
	case "unit":
		path = f.installer.paths.unit
	}
	f.installer.write = func(destination string, data []byte, mode os.FileMode) error {
		if destination == path {
			return errors.New("simulated disk failure")
		}
		return atomicWrite(destination, data, mode)
	}
	if _, err := f.installer.Join(context.Background(), f.options); err == nil {
		t.Fatal("expected write failure")
	}
	f.installer.write = atomicWrite
	if _, err := f.installer.Join(context.Background(), f.options); err != nil {
		t.Fatal(err)
	}
	if f.requests.Load() != 1 {
		t.Fatalf("want one enrollment, got %d", f.requests.Load())
	}
	f.assertInstalled(t)
}

func TestJoinRecoversAfterReceiptWriteFailure(t *testing.T) {
	f := newFixture(t)
	statePath := filepath.Join(f.installer.paths.stateDir, "enrollment.json")
	writes := 0
	f.installer.write = func(path string, data []byte, mode os.FileMode) error {
		if path == statePath {
			writes++
			if writes == 2 {
				return errors.New("receipt disk failure")
			}
		}
		return atomicWrite(path, data, mode)
	}
	if _, err := f.installer.Join(context.Background(), f.options); err == nil {
		t.Fatal("expected receipt failure")
	}
	if _, err := f.installer.Join(context.Background(), f.options); err != nil {
		t.Fatal(err)
	}
	if f.requests.Load() != 2 {
		t.Fatalf("want two safe enrollment attempts, got %d", f.requests.Load())
	}
}

func TestSystemdFailureRetainsConfigForServiceInstall(t *testing.T) {
	for _, command := range []string{"daemon-reload", "enable", "is-active"} {
		t.Run(command, func(t *testing.T) {
			f := newFixture(t)
			f.failCommand = command
			if _, err := f.installer.Join(context.Background(), f.options); err == nil {
				t.Fatal("expected systemctl failure")
			}
			assertMode(t, f.installer.paths.config, privateFile)
			f.failCommand = ""
			if err := f.installer.Install(context.Background(), ""); err != nil {
				t.Fatal(err)
			}
			if f.requests.Load() != 1 {
				t.Fatal("service install must not join again")
			}
			f.assertInstalled(t)
		})
	}
}

func TestJoinRejectsDifferentEnrollmentCommand(t *testing.T) {
	for _, field := range []string{"token", "server", "name", "host", "ssh-port"} {
		t.Run(field, func(t *testing.T) {
			f := newFixture(t)
			if _, err := f.installer.Join(context.Background(), f.options); err != nil {
				t.Fatal(err)
			}
			switch field {
			case "token":
				f.options.Token = strings.Replace(f.options.Token, "A", "B", 1)
			case "server":
				f.options.ServerURL = "https://other.example.com"
			case "name":
				f.options.Name = "different-node"
			case "host":
				f.options.Host = "192.0.2.9"
			case "ssh-port":
				f.options.SSHPort = 2233
			}
			if _, err := f.installer.Join(context.Background(), f.options); err == nil {
				t.Fatal("expected enrollment mismatch")
			}
			if f.requests.Load() != 1 {
				t.Fatal("mismatched command must not contact server")
			}
		})
	}
}

func TestJoinRejectsChangedConfigAndCorruptState(t *testing.T) {
	for _, kind := range []string{"config", "state", "version", "request", "result"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			if _, err := f.installer.Join(context.Background(), f.options); err != nil {
				t.Fatal(err)
			}
			statePath := filepath.Join(f.installer.paths.stateDir, "enrollment.json")
			var state enrollment
			if _, err := loadPrivateJSON(statePath, &state); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "config":
				mustWrite(t, f.installer.paths.config, []byte(`{}`), privateFile)
			case "state":
				mustWrite(t, statePath, []byte(`{"version":`), privateFile)
			case "version":
				state.Version++
				if err := f.installer.saveEnrollment(&state); err != nil {
					t.Fatal(err)
				}
			case "request":
				state.Request.RequestID = "!"
				if err := f.installer.saveEnrollment(&state); err != nil {
					t.Fatal(err)
				}
			case "result":
				state.Result.NodeID = ""
				if err := f.installer.saveEnrollment(&state); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := f.installer.Join(context.Background(), f.options); err == nil {
				t.Fatal("expected preserved-state error")
			}
			if f.requests.Load() != 1 {
				t.Fatal("corrupt local state must not contact server")
			}
		})
	}
}
