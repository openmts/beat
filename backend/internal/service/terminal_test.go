package service

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/sshclient"
	"github.com/beat/backend/internal/store"
)

type fakeTerminalConnector struct {
	mu      sync.Mutex
	opened  []string
	execute map[string]string
	err     map[string]error
	openErr error
}

func (f *fakeTerminalConnector) OpenTerminal(
	_ context.Context,
	request sshclient.Request,
	stream io.ReadWriteCloser,
) error {
	f.mu.Lock()
	f.opened = append(f.opened, request.Node.ID+":"+request.PrivateKey)
	f.mu.Unlock()
	if f.openErr != nil {
		return f.openErr
	}
	_, err := stream.Write([]byte("connected"))
	return err
}

func (f *fakeTerminalConnector) Execute(
	_ context.Context,
	request sshclient.Request,
	command string,
) (string, error) {
	if err := f.err[request.Node.ID]; err != nil {
		return "", err
	}
	return f.execute[request.Node.ID] + ":" + command, nil
}

type memoryStream struct {
	strings.Builder
}

func (s *memoryStream) Read(_ []byte) (int, error) { return 0, io.EOF }

func (s *memoryStream) Close() error { return nil }

func setupTerminalService(t *testing.T, privateKey string) (*TerminalService, *model.Node, *fakeTerminalConnector) {
	t.Helper()
	sqliteStore, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() { _ = sqliteStore.Close() })

	nodes := store.NewNodeStore(sqliteStore.DB)
	keys := store.NewSSHKeyStore(sqliteStore.DB)
	node, err := nodes.UpsertNode(context.Background(), "node-one", "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	key, err := keys.CreateSSHKey(context.Background(), "key-one", "ed25519", "ssh-ed25519 public", privateKey, "fp")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	_, err = nodes.UpdateNode(context.Background(), node.ID, store.NodeUpdate{
		GroupID: node.GroupID, SSHPublicKey: key.PublicKey,
	})
	if err != nil {
		t.Fatalf("assign key: %v", err)
	}
	node.SSHPublicKey = key.PublicKey

	connector := &fakeTerminalConnector{
		execute: map[string]string{node.ID: "output"},
		err:     make(map[string]error),
	}
	return NewTerminalService(nodes, keys, connector), node, connector
}

func TestTerminalServiceOpenTerminal(t *testing.T) {
	svc, node, connector := setupTerminalService(t, "private")
	stream := &memoryStream{}

	if err := svc.OpenTerminal(context.Background(), node.ID, stream); err != nil {
		t.Fatalf("open terminal: %v", err)
	}
	if stream.String() != "connected" {
		t.Fatalf("stream = %q, want connected", stream.String())
	}
	if len(connector.opened) != 1 || connector.opened[0] != node.ID+":private" {
		t.Fatalf("opened = %v", connector.opened)
	}
}

func TestTerminalServiceCredentialErrors(t *testing.T) {
	svc, node, _ := setupTerminalService(t, "")

	if err := svc.OpenTerminal(context.Background(), "missing", &memoryStream{}); !errors.Is(err, ErrTerminalNodeNotFound) {
		t.Fatalf("missing node error = %v", err)
	}
	if err := svc.OpenTerminal(context.Background(), node.ID, &memoryStream{}); !errors.Is(err, ErrTerminalPrivateKeyUnavailable) {
		t.Fatalf("public-only key error = %v", err)
	}
}

func TestTerminalServiceAdditionalErrors(t *testing.T) {
	svc, node, connector := setupTerminalService(t, "private")
	connector.openErr = errors.New("open failed")
	if err := svc.OpenTerminal(context.Background(), node.ID, &memoryStream{}); err == nil ||
		!strings.Contains(err.Error(), "open ssh terminal") {
		t.Fatalf("connector error = %v", err)
	}

	unassigned, err := svc.nodes.UpsertNode(context.Background(), "unassigned", "127.0.0.2", 22)
	if err != nil {
		t.Fatalf("create unassigned node: %v", err)
	}
	if err := svc.OpenTerminal(context.Background(), unassigned.ID, &memoryStream{}); !errors.Is(err, ErrTerminalKeyNotAssigned) {
		t.Fatalf("unassigned key error = %v", err)
	}

	_, err = svc.nodes.UpdateNode(context.Background(), unassigned.ID, store.NodeUpdate{
		GroupID: unassigned.GroupID, SSHPublicKey: "ssh-ed25519 missing",
	})
	if err != nil {
		t.Fatalf("assign missing key: %v", err)
	}
	if err := svc.OpenTerminal(context.Background(), unassigned.ID, &memoryStream{}); !errors.Is(err, ErrTerminalManagedKeyNotFound) {
		t.Fatalf("missing managed key error = %v", err)
	}
}

func TestTerminalServiceExecuteBatch(t *testing.T) {
	svc, node, connector := setupTerminalService(t, "private")
	connector.err[node.ID] = errors.New("remote failure")

	results := svc.ExecuteBatch(context.Background(), []string{node.ID, "missing"}, "uptime")
	if len(results) != 2 {
		t.Fatalf("results length = %d, want 2", len(results))
	}
	if results[0].NodeID != node.ID || results[0].Error != "remote failure" {
		t.Fatalf("first result = %#v", results[0])
	}
	if results[1].NodeID != "missing" || results[1].Error == "" {
		t.Fatalf("second result = %#v", results[1])
	}
}

func TestTerminalServiceExecute(t *testing.T) {
	svc, node, _ := setupTerminalService(t, "private")

	result := svc.ExecuteBatch(context.Background(), []string{node.ID}, "hostname")
	if len(result) != 1 || result[0].Output != "output:hostname" || result[0].NodeName != "node-one" {
		t.Fatalf("result = %#v", result)
	}
}
