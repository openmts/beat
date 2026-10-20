package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

var calibrationFixedNow = time.Date(2026, time.October, 15, 12, 0, 0, 0, time.UTC)

func calibrationHandlerFixture(t *testing.T) (*TrafficCalibrationHandler, model.Node, *store.TrafficCalibrationStore) {
	t.Helper()
	sqlite, mts := setupNodeTestDB(t)
	nodeStore := store.NewNodeStore(sqlite.DB)
	node, err := nodeStore.UpsertNode(t.Context(), "calibrated", "192.0.2.1", 22)
	if err != nil || node == nil {
		t.Fatalf("upsert node: %v", err)
	}
	ledger := store.NewTrafficCalibrationStore(sqlite.DB)
	handler := NewTrafficCalibrationHandler(nodeStore, mts, ledger).WithClock(func() time.Time { return calibrationFixedNow })
	return handler, *node, ledger
}

func calibrationRequest(t *testing.T, handler *TrafficCalibrationHandler, method, nodeID, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, "/api/v1/admin/nodes/"+nodeID+"/traffic-calibrations", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.SetPathValue("id", nodeID)
	request = request.WithContext(middleware.WithAdminPrincipal(request.Context(),
		model.AdminPrincipal{User: model.AdminUser{ID: "user-1", Username: "owner"}}))
	recorder := httptest.NewRecorder()
	handler.HandleCreate(recorder, request)
	return recorder
}

func TestTrafficCalibrationHandlerCreateAndHistory(t *testing.T) {
	handler, node, ledger := calibrationHandlerFixture(t)
	body := `{"request_id":"11111111-1111-1111-1111-111111111111","target_sent":"1000",` +
		`"target_received":"2000","reason":"provider reported opening balance"}`
	created := calibrationRequest(t, handler, http.MethodPost, node.ID, body)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d body = %s", created.Code, created.Body.String())
	}
	response := decodeCalibrationResponse(t, created.Body.Bytes())
	if response.Event.Kind != model.TrafficCalibrationSet || response.Event.TargetSent == nil ||
		*response.Event.TargetSent != "1000" || response.Replayed {
		t.Fatalf("created event = %#v", response.Event)
	}
	if response.Event.EffectiveAt == nil || !response.Event.EffectiveAt.Equal(calibrationFixedNow) {
		t.Fatalf("auto effective = %v", response.Event.EffectiveAt)
	}

	replay := calibrationRequest(t, handler, http.MethodPost, node.ID, body)
	if replay.Code != http.StatusCreated || !decodeCalibrationResponse(t, replay.Body.Bytes()).Replayed {
		t.Fatalf("replay status = %d body = %s", replay.Code, replay.Body.String())
	}
	conflict := calibrationRequest(t, handler, http.MethodPost, node.ID,
		`{"request_id":"11111111-1111-1111-1111-111111111111","target_sent":"1","target_received":"2","reason":"changed"}`)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("conflict status = %d body = %s", conflict.Code, conflict.Body.String())
	}

	historyRequest := httptest.NewRequest(http.MethodGet,
		"/api/v1/admin/nodes/"+node.ID+"/traffic-calibrations?limit=10", nil)
	historyRequest.SetPathValue("id", node.ID)
	history := httptest.NewRecorder()
	handler.HandleHistory(history, historyRequest)
	if history.Code != http.StatusOK {
		t.Fatalf("history status = %d body = %s", history.Code, history.Body.String())
	}
	var page struct {
		Data trafficCalibrationHistoryResponse `json:"data"`
	}
	if err := json.Unmarshal(history.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(page.Data.Events) != 1 {
		t.Fatalf("history events = %#v", page.Data.Events)
	}
	stored, err := ledger.History(t.Context(), store.TrafficCalibrationHistoryRequest{NodeID: node.ID})
	if err != nil || len(stored.Events) != 1 {
		t.Fatalf("stored history = %#v %v", stored, err)
	}
}

func TestTrafficCalibrationHandlerValidationAndBoundaries(t *testing.T) {
	handler, node, _ := calibrationHandlerFixture(t)
	cases := []struct {
		name string
		body string
		want int
	}{
		{"invalid target", `{"request_id":"22222222-2222-2222-2222-222222222222","target_sent":"1.5","target_received":"2","reason":"bad"}`,
			http.StatusBadRequest},
		{"missing reason", `{"request_id":"33333333-3333-3333-3333-333333333333","target_sent":"1","target_received":"2","reason":""}`,
			http.StatusBadRequest},
		{"bad request id", `{"request_id":"nope","target_sent":"1","target_received":"2","reason":"bad"}`,
			http.StatusBadRequest},
		{"future instant", `{"request_id":"44444444-4444-4444-4444-444444444444","effective_at":"2026-10-20T00:00:00Z","target_sent":"1","target_received":"2","reason":"future"}`,
			http.StatusBadRequest},
		{"sub-second instant", `{"request_id":"55555555-5555-5555-5555-555555555555","effective_at":"2026-10-10T00:00:00.5Z","target_sent":"1","target_received":"2","reason":"precise"}`,
			http.StatusBadRequest},
		{"before cycle", `{"request_id":"66666666-6666-6666-6666-666666666666","effective_at":"2026-09-01T00:00:00Z","target_sent":"1","target_received":"2","reason":"past"}`,
			http.StatusBadRequest},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := calibrationRequest(t, handler, http.MethodPost, node.ID, testCase.body)
			if recorder.Code != testCase.want {
				t.Fatalf("status = %d, want %d (%s)", recorder.Code, testCase.want, recorder.Body.String())
			}
		})
	}
	explicit := calibrationRequest(t, handler, http.MethodPost, node.ID,
		`{"request_id":"77777777-7777-7777-7777-777777777777","effective_at":"2026-10-10T00:00:00Z","target_sent":"10","target_received":"20","reason":"explicit"}`)
	if explicit.Code != http.StatusCreated {
		t.Fatalf("explicit effective status = %d body = %s", explicit.Code, explicit.Body.String())
	}
	if got := decodeCalibrationResponse(t, explicit.Body.Bytes()).Event.EffectiveAt; got == nil ||
		!got.Equal(time.Date(2026, time.October, 10, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("explicit effective = %v", got)
	}
	missing := calibrationRequest(t, handler, http.MethodPost, "99999999-9999-9999-9999-999999999999",
		`{"request_id":"88888888-8888-8888-8888-888888888888","target_sent":"1","target_received":"2","reason":"x"}`)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing node status = %d", missing.Code)
	}
}

func TestTrafficCalibrationHandlerRevokeAndReplace(t *testing.T) {
	handler, node, _ := calibrationHandlerFixture(t)
	first := calibrationRequest(t, handler, http.MethodPost, node.ID,
		`{"request_id":"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa","target_sent":"10","target_received":"20","reason":"first"}`)
	if first.Code != http.StatusCreated {
		t.Fatalf("first status = %d", first.Code)
	}
	eventID := decodeCalibrationResponse(t, first.Body.Bytes()).Event.ID

	replace := calibrationRequestWithPath(t, handler, node.ID, eventID, "replace",
		`{"request_id":"bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb","target_sent":"30","target_received":"40","reason":"correction"}`)
	if replace.Code != http.StatusOK {
		t.Fatalf("replace status = %d body = %s", replace.Code, replace.Body.String())
	}
	replaced := decodeCalibrationResponse(t, replace.Body.Bytes())
	if replaced.Revoked == nil || replaced.Event.ID == eventID {
		t.Fatalf("replace response = %#v", replaced)
	}

	revoke := calibrationRequestWithPath(t, handler, node.ID, replaced.Event.ID, "revoke",
		`{"request_id":"cccccccc-cccc-cccc-cccc-cccccccccccc"}`)
	if revoke.Code != http.StatusOK || decodeCalibrationResponse(t, revoke.Body.Bytes()).Revoked == nil {
		t.Fatalf("revoke status = %d body = %s", revoke.Code, revoke.Body.String())
	}
	stale := calibrationRequestWithPath(t, handler, node.ID, replaced.Event.ID, "revoke",
		`{"request_id":"dddddddd-dddd-dddd-dddd-dddddddddddd"}`)
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale revoke status = %d body = %s", stale.Code, stale.Body.String())
	}
}

func TestTrafficCalibrationHandlerUnavailable(t *testing.T) {
	handler := NewTrafficCalibrationHandler(nil, nil, nil)
	recorder := calibrationRequest(t, handler, http.MethodPost,
		"11111111-1111-1111-1111-111111111111", `{}`)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable status = %d", recorder.Code)
	}
}

func calibrationRequestWithPath(t *testing.T, handler *TrafficCalibrationHandler, nodeID, eventID, action, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost,
		"/api/v1/admin/nodes/"+nodeID+"/traffic-calibrations/"+eventID+"/"+action, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.SetPathValue("id", nodeID)
	request.SetPathValue("calibration_id", eventID)
	request = request.WithContext(middleware.WithAdminPrincipal(request.Context(),
		model.AdminPrincipal{User: model.AdminUser{ID: "user-1", Username: "owner"}}))
	recorder := httptest.NewRecorder()
	if action == "revoke" {
		handler.HandleRevoke(recorder, request)
	} else {
		handler.HandleReplace(recorder, request)
	}
	return recorder
}

func decodeCalibrationResponse(t *testing.T, body []byte) trafficCalibrationResponse {
	t.Helper()
	var envelope struct {
		Data trafficCalibrationResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode calibration response: %v (%s)", err, string(body))
	}
	return envelope.Data
}
