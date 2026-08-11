package api

import (
	"net/http"
	"testing"
)

func TestNodeSortRouteRequiresAdmin(t *testing.T) {
	router := setupAuthenticatedRouter(t)
	unauthorized := routerRequest(t, router, http.MethodPut, "/api/v1/nodes/sort",
		`{"ids":["node"]}`, "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized sort status = %d", unauthorized.Code)
	}
	authorized := routerRequest(t, router, http.MethodPut, "/api/v1/nodes/sort",
		`{"ids":["missing"]}`, "admin-secret")
	if authorized.Code != http.StatusBadRequest {
		t.Fatalf("authorized invalid sort status = %d, body = %s", authorized.Code, authorized.Body.String())
	}
}
