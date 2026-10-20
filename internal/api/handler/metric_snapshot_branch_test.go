package handler

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func TestSnapshotHandlerUnavailableAndDecodeBranches(t *testing.T) {
	fixture := newCollaborationFixture(t)
	empty := &DashboardCollaborationHandler{}
	cases := map[string]struct {
		handler http.HandlerFunc
		method  string
		input   collaborationRequest
		want    int
	}{
		"list without store": {empty.HandleListSnapshots, http.MethodGet, collaborationRequest{}, http.StatusServiceUnavailable},
		"create without store": {empty.HandleCreateSnapshot, http.MethodPost,
			collaborationRequest{body: map[string]any{"dashboard_id": fixture.dashboard.ID}}, http.StatusServiceUnavailable},
		"revoke without store": {empty.HandleRevokeSnapshot, http.MethodPost, collaborationRequest{id: "id"}, http.StatusServiceUnavailable},
		"public without store": {empty.HandlePublicSnapshot, http.MethodGet, collaborationRequest{token: strings.Repeat("a", 64)},
			http.StatusServiceUnavailable},
		"malformed body": {fixture.collaboration.HandleCreateSnapshot, http.MethodPost,
			collaborationRequest{body: "not-an-object"}, http.StatusBadRequest},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			input := test.input
			input.handle = test.handler
			response := fixture.call(t, test.method, input)
			if response.Code != test.want {
				t.Fatalf("status = %d body = %s", response.Code, response.Body)
			}
		})
	}
	if response := fixture.call(t, http.MethodGet, collaborationRequest{handle: empty.HandleListSnapshots,
		anonymous: true}); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous list = %d", response.Code)
	}
	if response := fixture.call(t, http.MethodPost, collaborationRequest{handle: empty.HandleRevokeSnapshot,
		id: "id", anonymous: true}); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous revoke = %d", response.Code)
	}
}

func TestSnapshotListSurfacesStorageFailure(t *testing.T) {
	fixture := newCollaborationFixture(t)
	if _, err := fixture.db.DB.ExecContext(t.Context(), `DROP TABLE dashboard_snapshots`); err != nil {
		t.Fatal(err)
	}
	response := fixture.call(t, http.MethodGet, collaborationRequest{handle: fixture.collaboration.HandleListSnapshots})
	if response.Code < 400 {
		t.Fatalf("broken store list = %d body = %s", response.Code, response.Body)
	}
	if response := fixture.call(t, http.MethodPost, collaborationRequest{
		id: "missing-snapshot", handle: fixture.collaboration.HandleRevokeSnapshot}); response.Code < 400 {
		t.Fatalf("broken revoke = %d body = %s", response.Code, response.Body)
	}
}

func TestSnapshotSourceScopeBranches(t *testing.T) {
	fixture := newCollaborationFixture(t)
	owner := fixture.call(t, http.MethodPost, collaborationRequest{handle: fixture.collaboration.HandleCreateSnapshot,
		body: map[string]any{"dashboard_id": model.PublicDashboardID, "name": "public scope"}})
	if owner.Code != http.StatusCreated {
		t.Fatalf("public scope create = %d body = %s", owner.Code, owner.Body)
	}
	node := fixture.call(t, http.MethodPost, collaborationRequest{handle: fixture.collaboration.HandleCreateSnapshot,
		body: map[string]any{"dashboard_id": "node-edge-1", "name": "node scope"}})
	if node.Code != http.StatusCreated {
		t.Fatalf("node scope create = %d body = %s", node.Code, node.Body)
	}
	// 管理员默认看板需要 owner 且最近重认证；普通管理员被拒。
	_, adminPrincipal := fixture.sessionActor(t, "snapshot-admin", model.AdminRoleAdmin, true)
	denied := fixture.call(t, http.MethodPost, collaborationRequest{handle: fixture.collaboration.HandleCreateSnapshot,
		actor: &adminPrincipal, body: map[string]any{"dashboard_id": model.AdminDashboardID, "name": "admin scope"}})
	if denied.Code != http.StatusForbidden {
		t.Fatalf("admin scope create = %d body = %s", denied.Code, denied.Body)
	}
	// 所有者提交过长标识会得到参数错误而不是 500。
	request := newAdminRequest(t, &fixture.principal, http.MethodPost, "/api/v1/admin/metrics/snapshots", "")
	if _, err := fixture.collaboration.snapshotSource(request, strings.Repeat("x", 81),
		store.DashboardActor{}); !errors.Is(err, errCollaborationBadRequest) {
		t.Fatalf("long id error = %v", err)
	}
}
