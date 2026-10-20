package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/beat/backend/internal/store"
)

func TestCredentialMutationDoesNotDependOnMetricStorage(t *testing.T) {
	database, metrics := setupNodeTestDB(t)
	nodes := store.NewNodeStore(database.DB)
	handler := NewNodeHandler(nodes, metrics)
	if err := metrics.Close(); err != nil {
		t.Fatal(err)
	}
	response := managedNodeRequestForTest(handler.HandleCreateManagedNode, http.MethodPost, "/nodes",
		`{"name":"isolated","host":"localhost","port":22,"server_url":"https://beat.example"}`, "")
	if response.Code != http.StatusCreated {
		t.Fatalf("create status: %d", response.Code)
	}
	var envelope struct {
		Data nodeCredentialResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	first := envelope.Data.AgentToken
	nodeID := envelope.Data.Node.ID
	assertCredentialMetadata(t, response.Body.Bytes())
	assertTokenAccepted(t, nodes, first)
	response = managedNodeRequestForTest(handler.HandleRotateAgentToken, http.MethodPost, "/rotate",
		`{"server_url":"https://beat.example"}`, nodeID)
	if response.Code != http.StatusOK {
		t.Fatalf("rotate status: %d", response.Code)
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	assertTokenAccepted(t, nodes, envelope.Data.AgentToken)
	assertTokenRejected(t, nodes, first)
	response = managedNodeRequestForTest(handler.HandleRevokeAgentToken, http.MethodPost, "/revoke", "", nodeID)
	if response.Code != http.StatusOK {
		t.Fatalf("revoke status: %d", response.Code)
	}
	assertTokenRejected(t, nodes, envelope.Data.AgentToken)
}

func assertCredentialMetadata(t *testing.T, body []byte) {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	var data struct {
		Node map[string]json.RawMessage `json:"node"`
	}
	if err := json.Unmarshal(raw["data"], &data); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"metrics", "traffic", "telemetry_at", "agent_token_hash"} {
		if _, present := data.Node[key]; present {
			t.Fatalf("credential metadata includes %s", key)
		}
	}
}

func assertTokenAccepted(t *testing.T, nodes *store.NodeStore, token string) {
	t.Helper()
	node, err := nodes.AuthenticateAgentToken(t.Context(), token)
	if err != nil || node == nil {
		t.Fatal("returned credential is unusable", err)
	}
}

func assertTokenRejected(t *testing.T, nodes *store.NodeStore, token string) {
	t.Helper()
	node, err := nodes.AuthenticateAgentToken(t.Context(), token)
	if err != nil || node != nil {
		t.Fatal("invalidated credential remains usable", err)
	}
}

func TestCredentialHandlersRejectMissingMutationResult(t *testing.T) {
	database, metrics := setupNodeTestDB(t)
	nodes := store.NewNodeStore(database.DB)
	handler := NewNodeHandler(nodes, metrics)
	node, err := nodes.UpsertNode(t.Context(), "disappearing", "localhost", 22)
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.DB.Exec(`CREATE TRIGGER remove_rotated AFTER UPDATE OF agent_token_hash ON nodes BEGIN DELETE FROM nodes WHERE id = NEW.id; END`)
	if err != nil {
		t.Fatal(err)
	}
	response := managedNodeRequestForTest(handler.HandleRotateAgentToken, http.MethodPost, "/rotate", `{"server_url":"https://beat.example"}`, node.ID)
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing rotation result status: %d", response.Code)
	}
	existing, err := nodes.GetNode(t.Context(), node.ID)
	if err != nil || existing == nil {
		t.Fatal("missing result transaction did not roll back", err)
	}
	_, err = database.DB.Exec(`CREATE TRIGGER remove_created AFTER INSERT ON nodes BEGIN DELETE FROM nodes WHERE id = NEW.id; END`)
	if err != nil {
		t.Fatal(err)
	}
	response = managedNodeRequestForTest(handler.HandleCreateManagedNode, http.MethodPost, "/nodes", `{"name":"missing","host":"localhost","port":22,"server_url":"https://beat.example"}`, "")
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("missing creation result status: %d", response.Code)
	}
}

func TestCredentialRotationWriteFailureIsReportedBeforeIssuance(t *testing.T) {
	database, metrics := setupNodeTestDB(t)
	nodes := store.NewNodeStore(database.DB)
	handler := NewNodeHandler(nodes, metrics)
	node, err := nodes.UpsertNode(t.Context(), "write-failure", "localhost", 22)
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.DB.Exec(`CREATE TRIGGER fail_rotation BEFORE UPDATE OF agent_token_hash ON nodes BEGIN SELECT RAISE(ABORT, 'injected failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	response := managedNodeRequestForTest(handler.HandleRotateAgentToken, http.MethodPost, "/rotate", `{"server_url":"https://beat.example"}`, node.ID)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("failed rotation status: %d", response.Code)
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data) != 0 {
		t.Fatal("failed write exposed credential data")
	}
}
