package handler

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/nodelifecycle"
	"github.com/beat/backend/internal/store"
)

type lifecycleStub struct{ err error }

func (s lifecycleStub) Settings(context.Context, nodelifecycle.Actor) (model.LifecycleSettings, error) {
	return nodelifecycle.DefaultSettings(), s.err
}

func (s lifecycleStub) SaveSettings(context.Context, nodelifecycle.Actor, model.LifecycleSettings) (model.LifecycleSettings, error) {
	return nodelifecycle.DefaultSettings(), s.err
}

func (s lifecycleStub) Node(context.Context, nodelifecycle.Actor, string) (model.NodeLifecycle, error) {
	return model.NodeLifecycle{}, s.err
}

func (s lifecycleStub) SaveNode(_ context.Context, _ nodelifecycle.Actor, input nodelifecycle.NodeChange) (model.NodeLifecycle, error) {
	return model.NodeLifecycle{LifecycleWrite: input.LifecycleWrite}, s.err
}

func (s lifecycleStub) Events(context.Context, nodelifecycle.Actor, nodelifecycle.EventQuery) ([]model.LifecycleEvent, error) {
	return []model.LifecycleEvent{}, s.err
}

func TestNodeLifecycleHandlersSessionAndJSON(t *testing.T) {
	handler := NewNodeLifecycleHandler(lifecycleStub{})
	for _, action := range []http.HandlerFunc{handler.HandleSettings, handler.HandleSaveSettings, handler.HandleNode, handler.HandleSaveNode, handler.HandleEvents} {
		anonymous := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{}`))
		response := httptest.NewRecorder()
		action(response, anonymous)
		if response.Code != 401 {
			t.Fatal(response.Code)
		}
		response = httptest.NewRecorder()
		action(response, geoIPRequest(`{}`))
		if response.Code != 200 || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(response.Code, response.Body.String())
		}
	}
	response := httptest.NewRecorder()
	handler.HandleSaveNode(response, geoIPRequest(`{"price_micros":"9223372036854775807","currency":"USD"}`))
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"price_micros":"9223372036854775807"`) {
		t.Fatal(response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	handler.HandleSaveNode(response, geoIPRequest(`{"price_micros":9223372036854775807}`))
	if response.Code != 400 {
		t.Fatal("unsafe JSON number", response.Code)
	}
}

func TestNodeLifecycleHandlerErrorsAndStrictBodies(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
	}{{nodelifecycle.ErrInvalid, 400}, {nodelifecycle.ErrForbidden, 403}, {nodelifecycle.ErrNotFound, 404}, {nodelifecycle.ErrConflict, 409}, {errors.New("PRIVATE_NOTES"), 503}} {
		response := httptest.NewRecorder()
		NewNodeLifecycleHandler(lifecycleStub{err: test.err}).HandleNode(response, geoIPRequest(`{}`))
		if response.Code != test.status || strings.Contains(response.Body.String(), "PRIVATE") {
			t.Fatal(response.Code, response.Body.String())
		}
	}
	handler := NewNodeLifecycleHandler(lifecycleStub{})
	for _, action := range []http.HandlerFunc{handler.HandleSaveNode, handler.HandleSaveSettings} {
		response := httptest.NewRecorder()
		action(response, geoIPRequest(`{"unknown":1}`))
		if response.Code != 400 {
			t.Fatal(response.Code)
		}
	}
	for _, limit := range []string{"bad", "1"} {
		request := geoIPRequest(`{}`)
		request.URL.RawQuery = "limit=" + limit
		response := httptest.NewRecorder()
		handler.HandleEvents(response, request)
		expected := 200
		if limit == "bad" {
			expected = 400
		}
		if response.Code != expected {
			t.Fatal(response.Code)
		}
	}
}

type lifecycleViewStub struct{ err error }

func (s lifecycleViewStub) PublicLifecycle(context.Context, string) (*model.LifecyclePublic, error) {
	price := int64(math.MaxInt64)
	return &model.LifecyclePublic{PriceMicros: &price, Currency: "USD"}, s.err
}

func TestLifecycleViewsSurviveRESTAndWebsocketRefresh(t *testing.T) {
	handler, database, node, _ := geoIPReportFixture(t)
	public := true
	node, err := handler.nodeStore.UpdateNode(t.Context(), node.ID, store.NodeUpdate{GroupID: node.GroupID, IsPublic: &public})
	if err != nil {
		t.Fatal(err)
	}
	handler.WithLifecycleViews(lifecycleViewStub{})
	response, err := handler.buildPublicNodeResponse(t.Context(), *node, model.DefaultSiteSettings())
	if err != nil || response.Lifecycle == nil || *response.Lifecycle.PriceMicros != math.MaxInt64 {
		t.Fatal(response, err)
	}
	metrics := NewMetricsHandler(store.NewNodeStore(database.DB), handler.mtsStore).WithLifecycleViews(lifecycleViewStub{})
	snapshot, err := metrics.snapshot(t.Context())
	if err != nil || len(snapshot.Nodes) != 1 || snapshot.Nodes[0].Lifecycle == nil {
		t.Fatal(snapshot, err)
	}
	handler.WithLifecycleViews(lifecycleViewStub{err: errors.New("unavailable")})
	if _, err = handler.buildPublicNodeResponse(t.Context(), *node, model.DefaultSiteSettings()); err == nil {
		t.Fatal("read failure hidden")
	}
}
