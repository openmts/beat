package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/beat/backend/internal/store"
)

func TestTrafficCalibrationRoutesRequireOwnerRecentAuth(t *testing.T) {
	sqlite, err := store.NewSQLiteStore("file:" + t.Name() + "?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	router := setupSessionRouterWithOptions(t,
		WithTrafficCalibrationStore(store.NewTrafficCalibrationStore(sqlite.DB)))
	const nodeID = "11111111-1111-1111-1111-111111111111"
	const eventID = "22222222-2222-2222-2222-222222222222"

	routes := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/v1/admin/nodes/" + nodeID + "/traffic-calibrations", ""},
		{http.MethodPost, "/api/v1/admin/nodes/" + nodeID + "/traffic-calibrations", `{}`},
		{http.MethodPost, "/api/v1/admin/nodes/" + nodeID + "/traffic-calibrations/" + eventID + "/revoke", `{}`},
		{http.MethodPost, "/api/v1/admin/nodes/" + nodeID + "/traffic-calibrations/" + eventID + "/replace", `{}`},
	}
	for _, route := range routes {
		anonymous := requestRouter(router, route.method, route.path, route.body, "", "http://example.com")
		if anonymous.Code != http.StatusUnauthorized && anonymous.Code != http.StatusForbidden {
			t.Fatalf("%s %s anonymous status = %d", route.method, route.path, anonymous.Code)
		}
	}

	cookie := bootstrapTerminalOwner(t, router)
	for _, route := range routes {
		fresh := requestRouterWithCookie(router, route.method, route.path, route.body, cookie, "http://example.com")
		if fresh.Code != http.StatusPreconditionRequired {
			t.Fatalf("%s %s status = %d body = %s", route.method, route.path, fresh.Code, fresh.Body.String())
		}
	}
}

func TestTrafficCalibrationRouteMarksPublicSummaryCalibrated(t *testing.T) {
	sqlite, err := store.NewSQLiteStore("file:" + t.Name() + "?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	router := setupSessionRouterWithOptions(t,
		reportMetricsFixtureOption(t),
		WithTrafficCalibrationStore(store.NewTrafficCalibrationStore(sqlite.DB)))
	cookie := bootstrapTerminalOwner(t, router)
	reauth := requestRouterWithCookie(router, http.MethodPost, "/api/v1/auth/reauthenticate",
		`{"password":"correct horse battery staple"}`, cookie, "http://example.com")
	if reauth.Code != http.StatusOK {
		t.Fatalf("reauth status = %d", reauth.Code)
	}
	created := requestRouterWithCookie(router, http.MethodPost, "/api/v1/nodes",
		`{"name":"calibrated","host":"127.0.0.1","port":22,"server_url":"https://beat.example"}`, cookie, "http://example.com")
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d body = %s", created.Code, created.Body.String())
	}
	nodeID := createdNodeID(t, created.Body.Bytes())
	calibrate := requestRouterWithCookie(router, http.MethodPost,
		"/api/v1/admin/nodes/"+nodeID+"/traffic-calibrations",
		`{"request_id":"aaaaaaaa-1111-1111-1111-111111111111","target_sent":"1000","target_received":"2000","reason":"provider snapshot"}`,
		cookie, "http://example.com")
	if calibrate.Code != http.StatusCreated {
		t.Fatalf("calibrate status = %d body = %s", calibrate.Code, calibrate.Body.String())
	}
	public := routerRequest(t, router, http.MethodGet, "/api/v1/nodes/"+nodeID, "", "")
	if public.Code != http.StatusOK || !strings.Contains(public.Body.String(), `"accounting_mode":"calibrated"`) {
		t.Fatalf("public summary not calibrated: %d %s", public.Code, public.Body.String())
	}
}

func createdNodeID(t *testing.T, body []byte) string {
	t.Helper()
	var decoded struct {
		Data struct {
			Node struct {
				ID string `json:"id"`
			} `json:"node"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode created node: %v (%s)", err, string(body))
	}
	return decoded.Data.Node.ID
}

func TestTrafficCalibrationRejectionsAreAudited(t *testing.T) {
	sqlite, err := store.NewSQLiteStore("file:" + t.Name() + "?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	router := setupSessionRouterWithOptions(t,
		reportMetricsFixtureOption(t),
		WithTrafficCalibrationStore(store.NewTrafficCalibrationStore(sqlite.DB)))
	cookie := bootstrapTerminalOwner(t, router)
	reauth := requestRouterWithCookie(router, http.MethodPost, "/api/v1/auth/reauthenticate",
		`{"password":"correct horse battery staple"}`, cookie, "http://example.com")
	if reauth.Code != http.StatusOK {
		t.Fatalf("reauth status = %d", reauth.Code)
	}
	created := requestRouterWithCookie(router, http.MethodPost, "/api/v1/nodes",
		`{"name":"rejected","host":"127.0.0.1","port":22,"server_url":"https://beat.example"}`, cookie, "http://example.com")
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d body = %s", created.Code, created.Body.String())
	}
	path := "/api/v1/admin/nodes/" + createdNodeID(t, created.Body.Bytes()) + "/traffic-calibrations"
	invalid := requestRouterWithCookie(router, http.MethodPost, path,
		`{"request_id":"aaaaaaaa-1111-1111-1111-111111111111","target_sent":"12x","target_received":"2000","reason":"bad target"}`,
		cookie, "http://example.com")
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid calibration status = %d body = %s", invalid.Code, invalid.Body.String())
	}
	requestID := "aaaaaaaa-1111-1111-1111-111111111112"
	first := requestRouterWithCookie(router, http.MethodPost, path,
		`{"request_id":"`+requestID+`","target_sent":"1000","target_received":"2000","reason":"provider snapshot"}`,
		cookie, "http://example.com")
	if first.Code != http.StatusCreated {
		t.Fatalf("first calibration status = %d body = %s", first.Code, first.Body.String())
	}
	conflict := requestRouterWithCookie(router, http.MethodPost, path,
		`{"request_id":"`+requestID+`","target_sent":"1000","target_received":"2999","reason":"provider snapshot"}`,
		cookie, "http://example.com")
	if conflict.Code != http.StatusConflict {
		t.Fatalf("conflicting calibration status = %d body = %s", conflict.Code, conflict.Body.String())
	}
	rows, err := sqlite.DB.Query(`SELECT outcome,detail_json FROM admin_audit_events
		WHERE action='traffic_calibration.reject' ORDER BY rowid`)
	if err != nil {
		t.Fatalf("query rejection audits: %v", err)
	}
	defer func() { _ = rows.Close() }()
	reasons := make([]string, 0, 2)
	for rows.Next() {
		var outcome, detail string
		if err := rows.Scan(&outcome, &detail); err != nil {
			t.Fatalf("scan rejection audit: %v", err)
		}
		if outcome != "failure" {
			t.Fatalf("rejection outcome = %s", outcome)
		}
		if strings.Contains(detail, "2999") || strings.Contains(detail, "bad target") {
			t.Fatalf("rejection audit leaked request content: %s", detail)
		}
		reasons = append(reasons, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read rejection audits: %v", err)
	}
	if len(reasons) != 2 || !strings.Contains(reasons[0], "reason:invalid") ||
		!strings.Contains(reasons[1], "reason:conflict") {
		t.Fatalf("rejection audits = %#v", reasons)
	}
}
