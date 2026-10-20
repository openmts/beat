package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
)

func calibrationNodeRequest(
	t *testing.T,
	handler *TrafficCalibrationHandler,
	method string,
	nodeID string,
	body string,
	call func(http.ResponseWriter, *http.Request),
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, "/api/v1/admin/nodes/"+nodeID+"/traffic-calibrations", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.SetPathValue("id", nodeID)
	request.SetPathValue("calibration_id", "00000000-0000-0000-0000-000000000000")
	recorder := httptest.NewRecorder()
	call(recorder, request)
	return recorder
}

func TestTrafficCalibrationHandlerLoadAndDecodeBranches(t *testing.T) {
	handler, node, _ := calibrationHandlerFixture(t)
	validSet := `{"request_id":"11111111-1111-1111-1111-111111111111","target_sent":"1000",` +
		`"target_received":"2000","reason":"branch coverage"}`

	cases := []struct {
		name   string
		method string
		nodeID string
		body   string
		call   func(http.ResponseWriter, *http.Request)
		want   int
	}{
		{"unknown node history", http.MethodGet, "missing-node", "", handler.HandleHistory, http.StatusNotFound},
		{"invalid limit", http.MethodGet, node.ID, "", handler.HandleHistory, http.StatusBadRequest},
		{"unknown node create", http.MethodPost, "missing-node", validSet, handler.HandleCreate, http.StatusNotFound},
		{"malformed create", http.MethodPost, node.ID, "{", handler.HandleCreate, http.StatusBadRequest},
		{"bad bytes create", http.MethodPost, node.ID,
			`{"request_id":"22222222-2222-2222-2222-222222222222","target_sent":"nope","target_received":"1"}`,
			handler.HandleCreate, http.StatusBadRequest},
		{"unknown node revoke", http.MethodPost, "missing-node", `{"request_id":"x"}`, handler.HandleRevoke, http.StatusNotFound},
		{"malformed revoke", http.MethodPost, node.ID, "{", handler.HandleRevoke, http.StatusBadRequest},
		// 撤销/替换都以“当前生效事件”为前置条件：事件不匹配时是冲突而不是 404 或 400。
		{"unknown event revoke", http.MethodPost, node.ID, `{"request_id":"33333333-3333-3333-3333-333333333333"}`,
			handler.HandleRevoke, http.StatusConflict},
		{"unknown node replace", http.MethodPost, "missing-node", validSet, handler.HandleReplace, http.StatusNotFound},
		{"malformed replace", http.MethodPost, node.ID, "{", handler.HandleReplace, http.StatusBadRequest},
		{"unknown event replace", http.MethodPost, node.ID, validSet, handler.HandleReplace, http.StatusConflict},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			path := "/api/v1/admin/nodes/" + test.nodeID + "/traffic-calibrations"
			if test.name == "invalid limit" {
				path += "?limit=abc"
			}
			request := httptest.NewRequest(test.method, path, strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			request.SetPathValue("id", test.nodeID)
			request.SetPathValue("calibration_id", "00000000-0000-0000-0000-000000000000")
			// 追加式账本记录操作者与会话，路由中间件负责注入主体；本层用例显式提供。
			request = request.WithContext(middleware.WithAdminPrincipal(request.Context(),
				model.AdminPrincipal{User: model.AdminUser{ID: "user-1", Username: "owner"},
					Session: model.AdminSession{ID: "session-1"}}))
			recorder := httptest.NewRecorder()
			test.call(recorder, request)
			if recorder.Code != test.want {
				t.Fatalf("status = %d want %d body = %s", recorder.Code, test.want, recorder.Body.String())
			}
		})
	}

	var nilHandler *TrafficCalibrationHandler
	missing := httptest.NewRequest(http.MethodGet, "/api/v1/admin/nodes/node-1/traffic-calibrations", nil)
	missing.SetPathValue("id", "node-1")
	unavailable := httptest.NewRecorder()
	nilHandler.HandleHistory(unavailable, missing)
	if unavailable.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil handler status = %d", unavailable.Code)
	}
	empty := &TrafficCalibrationHandler{}
	emptyResponse := httptest.NewRecorder()
	empty.HandleHistory(emptyResponse, missing)
	if emptyResponse.Code != http.StatusServiceUnavailable {
		t.Fatalf("empty handler status = %d", emptyResponse.Code)
	}
	if response := calibrationNodeRequest(t, handler, http.MethodGet, node.ID, "",
		handler.HandleHistory); response.Code != http.StatusOK {
		t.Fatalf("history status = %d body = %s", response.Code, response.Body.String())
	}
}

func TestCalibrationHistoryLimitAndRespondError(t *testing.T) {
	if limit, err := calibrationHistoryLimit(""); err != nil || limit != 0 {
		t.Fatalf("empty limit = %d / %v", limit, err)
	}
	if limit, err := calibrationHistoryLimit("25"); err != nil || limit != 25 {
		t.Fatalf("limit = %d / %v", limit, err)
	}
	for _, raw := range []string{"abc", "0", "-3"} {
		if _, err := calibrationHistoryLimit(raw); !errors.Is(err, model.ErrTrafficCalibrationInvalid) {
			t.Fatalf("limit %q error = %v", raw, err)
		}
	}
	handler := &TrafficCalibrationHandler{}
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"invalid", model.ErrTrafficCalibrationInvalid, http.StatusBadRequest},
		{"conflict", model.ErrTrafficCalibrationConflict, http.StatusConflict},
		{"missing", model.ErrTrafficCalibrationMissing, http.StatusNotFound},
		{"unexpected", errors.New("boom"), http.StatusInternalServerError},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			if !handler.respondError(recorder, test.err) {
				t.Fatal("respondError must report handled errors")
			}
			if recorder.Code != test.want {
				t.Fatalf("status = %d want %d", recorder.Code, test.want)
			}
		})
	}
	if recorder := httptest.NewRecorder(); handler.respondError(recorder, nil) {
		t.Fatal("nil error must not be handled")
	}
}
