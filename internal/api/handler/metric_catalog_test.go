package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/metricquery"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type metricQueryStub struct {
	err         error
	calls       int
	latestCalls int
	access      metricquery.Access
}

func (stub *metricQueryStub) Latest(_ context.Context, _ metricquery.LatestRequest, access metricquery.Access) (metricquery.LatestResponse, error) {
	stub.latestCalls++
	stub.access = access
	return metricquery.LatestResponse{Series: []metricquery.LatestSeries{}}, stub.err
}

func (stub *metricQueryStub) Query(_ context.Context, _ metricquery.Request, access metricquery.Access) (metricquery.Response, error) {
	stub.calls++
	stub.access = access
	return metricquery.Response{Series: []metricquery.Series{}}, stub.err
}

func catalogHandlerOwner(t *testing.T, sqlite *store.SQLiteStore) model.AdminPrincipal {
	t.Helper()
	now := model.NowUTC()
	until := now.Add(time.Hour)
	user := model.AdminUser{ID: "metric-owner", Username: "metric-owner", DisplayName: "Metric owner", Role: model.AdminRoleOwner, Enabled: true, PasswordHash: "test-hash", PasswordChangedAt: now, CreatedAt: now, UpdatedAt: now}
	admins := store.NewAdminStore(sqlite.DB)
	if err := admins.CreateUser(t.Context(), &user); err != nil {
		t.Fatal(err)
	}
	session := model.AdminSession{ID: "metric-session", UserID: user.ID, TokenHash: []byte("metric-session-hash"), CreatedAt: now, LastActivityAt: now, IdleExpiresAt: until, AbsoluteExpiresAt: until, ReauthenticatedUntil: &until}
	if err := admins.CreateSession(t.Context(), &session); err != nil {
		t.Fatal(err)
	}
	return model.AdminPrincipal{User: user, Session: session}
}

func metricPolicyRequest(t *testing.T, principal model.AdminPrincipal, body string) *http.Request {
	t.Helper()
	request := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(body))
	request.SetPathValue("key", "cpu")
	return request.WithContext(middleware.WithAdminPrincipal(request.Context(), principal))
}

func TestMetricCatalogHandlerSeparatesPublicAndAdminPolicyAndRequiresOwner(t *testing.T) {
	h, sqlite, _ := governedNodeReportFixture(t)
	principal := catalogHandlerOwner(t, sqlite)
	handler := NewMetricCatalogHandler(h.metricCatalog, nil, nil)
	body := `{"revision":1,"label":"Private CPU","description":"","default_aggregation":"avg","public":false,"enabled":true,"retention_days":null}`
	denied := httptest.NewRecorder()
	handler.HandleUpdatePolicy(denied, httptest.NewRequest(http.MethodPut, "/", strings.NewReader(body)))
	if denied.Code != http.StatusForbidden {
		t.Fatal("anonymous policy mutation accepted")
	}
	response := httptest.NewRecorder()
	handler.HandleUpdatePolicy(response, metricPolicyRequest(t, principal, body))
	if response.Code != http.StatusOK {
		t.Fatalf("policy status=%d/%s", response.Code, response.Body.String())
	}
	public, admin := httptest.NewRecorder(), httptest.NewRecorder()
	handler.HandlePublicCatalog(public, httptest.NewRequest(http.MethodGet, "/", nil))
	handler.HandleAdminCatalog(admin, httptest.NewRequest(http.MethodGet, "/", nil))
	if public.Code != http.StatusOK || strings.Contains(public.Body.String(), `"key":"cpu"`) {
		t.Fatal("private definition escaped public catalog")
	}
	if admin.Code != http.StatusOK || !strings.Contains(admin.Body.String(), `"retention_override_days":null`) {
		t.Fatal("administrator lost retention inheritance")
	}
	response = httptest.NewRecorder()
	handler.HandleUpdatePolicy(response, metricPolicyRequest(t, principal, body))
	if response.Code != http.StatusConflict {
		t.Fatal("stale policy was accepted")
	}
	request := metricPolicyRequest(t, principal, body)
	request.SetPathValue("key", "missing")
	response = httptest.NewRecorder()
	handler.HandleUpdatePolicy(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatal("missing policy accepted")
	}
}

func TestMetricQueryHandlerStatusPrivacyAndRequestBounds(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
	}{{nil, 200}, {metricquery.ErrInvalid, 400}, {metricquery.ErrArithmetic, 422}, {metricquery.ErrCapacity, 429}, {errors.New("disk failed"), 503}} {
		stub := &metricQueryStub{err: test.err}
		handler := NewMetricCatalogHandler(nil, stub, nil)
		for _, public := range []bool{true, false} {
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`))
			response := httptest.NewRecorder()
			if public {
				handler.HandlePublicQuery(response, request)
			} else {
				handler.HandleAdminQuery(response, request)
			}
			if response.Code != test.status || stub.access.PublicOnly != public || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("query response or audience changed")
			}
			if test.status == 429 && response.Header().Get("Retry-After") == "" {
				t.Fatal("capacity rejection lacks retry guidance")
			}
		}
	}
	for _, test := range []struct {
		err    error
		status int
	}{{nil, 200}, {metricquery.ErrInvalid, 400}, {metricquery.ErrArithmetic, 422}, {metricquery.ErrCapacity, 429}, {errors.New("disk failed"), 503}} {
		handler := NewMetricCatalogHandler(nil, nil, nil)
		for _, public := range []bool{true, false} {
			stub := &metricQueryStub{err: test.err}
			handler.queries = stub
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"series":[{"ref_id":"A","node_id":"node-1","metric":"cpu_usage"}]}`))
			response := httptest.NewRecorder()
			if public {
				handler.HandlePublicLatest(response, request)
			} else {
				handler.HandleAdminLatest(response, request)
			}
			if response.Code != test.status || stub.access.PublicOnly != public || stub.latestCalls != 1 || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("latest response or audience changed", response.Code, test.status, public)
			}
		}
	}
	for _, body := range []string{`{"unknown":true}`, `{} {}`, `{`, strings.Repeat(" ", maximumMetricRequestBytes+1)} {
		stub := &metricQueryStub{}
		handler := NewMetricCatalogHandler(nil, stub, nil)
		response := httptest.NewRecorder()
		handler.HandlePublicQuery(response, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
		if response.Code < 400 || stub.calls != 0 {
			t.Fatal("invalid body reached query service")
		}
	}
}

func TestMetricCatalogHandlerReportsUnavailableDependenciesAndInvalidPolicy(t *testing.T) {
	empty := NewMetricCatalogHandler(nil, nil, nil)
	for _, operation := range []http.HandlerFunc{empty.HandlePublicCatalog, empty.HandlePublicQuery} {
		response := httptest.NewRecorder()
		operation(response, httptest.NewRequest(http.MethodGet, "/", nil))
		if response.Code != 503 {
			t.Fatal("missing dependency became empty success")
		}
	}
	h, sqlite, _ := governedNodeReportFixture(t)
	principal := catalogHandlerOwner(t, sqlite)
	handler := NewMetricCatalogHandler(h.metricCatalog, nil, nil)
	for _, body := range []string{`{`, `{"revision":1,"label":"","enabled":true}`} {
		response := httptest.NewRecorder()
		handler.HandleUpdatePolicy(response, metricPolicyRequest(t, principal, body))
		if response.Code != 400 {
			t.Fatal("invalid policy accepted")
		}
	}
	response := httptest.NewRecorder()
	empty.HandleUpdatePolicy(response, metricPolicyRequest(t, principal, `{}`))
	if response.Code != 503 {
		t.Fatal("missing catalog was not reported")
	}
	if err := sqlite.Close(); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	handler.HandlePublicCatalog(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != 503 {
		t.Fatal("closed catalog became empty success")
	}
	response = httptest.NewRecorder()
	handler.HandleUpdatePolicy(response, metricPolicyRequest(t, principal, `{"revision":1}`))
	if response.Code != 500 {
		t.Fatal("write storage error ignored")
	}
}

func TestMetricCatalogCustomPolicyDispatchesDurableStopTasks(t *testing.T) {
	h, sqlite, node := governedNodeReportFixture(t)
	principal := catalogHandlerOwner(t, sqlite)
	bindReportedMetrics(t, sqlite, reportMetricBindings{nodeID: node.ID, names: []string{"queue"}})
	definition, err := h.metricCatalog.Get(t.Context(), "custom_queue")
	if err != nil {
		t.Fatal(err)
	}
	policy := model.MetricPolicyUpdate{Revision: definition.Revision, Label: definition.Label, DefaultAggregation: "avg", Enabled: false, Public: true}
	encoded, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	request := metricPolicyRequest(t, principal, string(encoded))
	request.SetPathValue("key", "custom_queue")
	dispatcher := &fakeCustomMetricDispatcher{err: errors.New("connection failed")}
	handler := NewMetricCatalogHandler(h.metricCatalog, nil, dispatcher)
	response := httptest.NewRecorder()
	handler.HandleUpdatePolicy(response, request)
	if response.Code != 200 || dispatcher.count() != 1 {
		t.Fatal("durable policy stop was not dispatched")
	}
	pending, err := store.NewAgentTaskStore(sqlite.DB).GetPendingTasks(t.Context(), node.ID)
	if err != nil || len(pending) != 1 || !strings.Contains(pending[0].Payload, `"metrics":[]`) {
		t.Fatal("failed dispatch lost durable stop")
	}
}

func TestMetricCatalogHandlerRejectsRevokedSessionAtTransactionBoundary(t *testing.T) {
	h, sqlite, _ := governedNodeReportFixture(t)
	principal := catalogHandlerOwner(t, sqlite)
	if err := store.NewAdminStore(sqlite.DB).RevokeSession(t.Context(), principal.Session.ID, model.NowUTC()); err != nil {
		t.Fatal(err)
	}
	request := metricPolicyRequest(t, principal, `{"revision":1,"label":"Rejected","description":"","default_aggregation":"avg","public":true,"enabled":true,"retention_days":null}`)
	response := httptest.NewRecorder()
	NewMetricCatalogHandler(h.metricCatalog, nil, nil).HandleUpdatePolicy(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatal("stale handler principal committed a policy")
	}
}
