package handler

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type acceleratorStub struct {
	err        error
	scope      *store.AcceleratorPolicyScope
	actor      *store.AcceleratorActor
	nodeHidden bool
	visibility *bool
}

func (s acceleratorStub) Node(context.Context, store.AcceleratorActor, string) (model.NodeAcceleratorsAdmin, error) {
	return model.NodeAcceleratorsAdmin{PublicVisible: !s.nodeHidden, State: "disabled"}, s.err
}

func (s acceleratorStub) Policy(_ context.Context, _ store.AcceleratorActor, scope store.AcceleratorPolicyScope) (model.AcceleratorPolicy, error) {
	if s.scope != nil {
		*s.scope = scope
	}
	return model.DefaultAcceleratorPolicy(), s.err
}

func (s acceleratorStub) SavePolicy(_ context.Context, actor store.AcceleratorActor, change store.AcceleratorPolicyChange) (model.AcceleratorPolicy, error) {
	if s.scope != nil {
		*s.scope = change.Scope
	}
	if s.actor != nil {
		*s.actor = actor
	}
	return model.DefaultAcceleratorPolicy(), s.err
}

func (s acceleratorStub) AgentPolicy(context.Context, string, bool) (model.AcceleratorPolicy, error) {
	return model.DefaultAcceleratorPolicy(), s.err
}

func (s acceleratorStub) PublishReport(context.Context, string, string) error { return s.err }

func (s acceleratorStub) PublicVisible(context.Context, string) (bool, error) {
	return !s.nodeHidden, s.err
}

func (s acceleratorStub) SetVisibility(_ context.Context, _ store.AcceleratorActor, _ string, visible bool) error {
	if s.visibility != nil {
		*s.visibility = visible
	}
	return s.err
}

func (s acceleratorStub) PublicAccelerators(context.Context, string) ([]model.AcceleratorDevicePublic, error) {
	return []model.AcceleratorDevicePublic{{DeviceID: strings.Repeat("a", 32), Ordinal: 1, Vendor: "nvidia", Model: "Fixture", Capabilities: []string{"gpu_utilization_percent"}, Metrics: map[string]float64{"gpu_utilization_percent": 42}, CadenceMS: 5000}}, s.err
}

func TestAcceleratorHandlersRequireSessionAndUseNamedScope(t *testing.T) {
	var scope store.AcceleratorPolicyScope
	var actor store.AcceleratorActor
	handler := NewAcceleratorHandler(acceleratorStub{scope: &scope, actor: &actor})
	for _, action := range []http.HandlerFunc{handler.HandleNode, handler.HandlePolicy, handler.HandleSavePolicy} {
		anonymous := httptest.NewRequest(http.MethodGet, "/", nil)
		response := httptest.NewRecorder()
		action(response, anonymous)
		if response.Code != 401 {
			t.Fatal(response.Code)
		}
		request := geoIPRequest(`{}`)
		request.Header.Set("User-Agent", "fixture-agent")
		response = httptest.NewRecorder()
		action(response, request)
		if response.Code != 200 || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(response.Code)
		}
	}
	if scope.Kind != "node" || scope.ID != "node" || actor.UserAgent != "fixture-agent" {
		t.Fatal(scope, actor)
	}
	request := geoIPRequest(`{}`)
	request.SetPathValue("id", "")
	response := httptest.NewRecorder()
	handler.HandlePolicy(response, request)
	if response.Code != 200 || scope.Kind != "enrollment_policy" || scope.ID != "default" {
		t.Fatal(response.Code, scope)
	}
	response = httptest.NewRecorder()
	handler.HandleSavePolicy(response, geoIPRequest(`{"command":"forbidden"}`))
	if response.Code != 400 {
		t.Fatal(response.Code)
	}
}

func TestAcceleratorHandlersReturnBoundedErrorCodes(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
	}{{store.ErrAcceleratorForbidden, 403}, {store.ErrAdminMutationRecent, 428}, {store.ErrAcceleratorPolicy, 409}, {model.ErrAgentReportInvalid, 400}, {sql.ErrNoRows, 404}, {errors.New("PRIVATE_DRIVER_PATH"), 503}} {
		response := httptest.NewRecorder()
		NewAcceleratorHandler(acceleratorStub{err: test.err}).HandleNode(response, geoIPRequest(`{}`))
		if response.Code != test.status || strings.Contains(response.Body.String(), "PRIVATE") {
			t.Fatal(response.Code, response.Body.String())
		}
	}
}

func TestAcceleratorViewsAndReportPolicyPreserveTrustedIdentity(t *testing.T) {
	handler, database, node, _ := geoIPReportFixture(t)
	handler.WithAccelerators(acceleratorStub{})
	response, err := handler.buildPublicNodeResponse(t.Context(), *node, model.DefaultSiteSettings())
	if err != nil || len(response.Accelerators) != 1 {
		t.Fatal(response, err)
	}
	metrics := NewMetricsHandler(store.NewNodeStore(database.DB), handler.mtsStore).WithAccelerators(acceleratorStub{})
	snapshot, err := metrics.snapshot(t.Context())
	if err != nil || len(snapshot.Nodes) != 1 || len(snapshot.Nodes[0].Accelerators) != 1 {
		t.Fatal(snapshot, err)
	}
	policy, err := handler.acceleratorReportPolicy(t.Context(), model.AgentIdentity{NodeID: node.ID, Mode: model.AgentCredentialActive}, model.AgentReport{})
	if err != nil || policy == nil {
		t.Fatal(policy, err)
	}
	policy, err = handler.acceleratorReportPolicy(t.Context(), model.AgentIdentity{Mode: model.AgentCredentialLegacy}, model.AgentReport{})
	if err != nil || policy != nil {
		t.Fatal(policy, err)
	}
	handler.WithAccelerators(acceleratorStub{err: store.ErrAcceleratorUnavailable})
	if _, err = handler.buildPublicNodeResponse(t.Context(), *node, model.DefaultSiteSettings()); err == nil {
		t.Fatal("GPU read failure hidden")
	}
	if _, err = handler.acceleratorReportPolicy(t.Context(), model.AgentIdentity{NodeID: node.ID, Mode: model.AgentCredentialActive}, model.AgentReport{}); err == nil {
		t.Fatal("policy read failure hidden")
	}
}

func TestAcceleratorPublicNodeHonorsGlobalAndNodeVisibility(t *testing.T) {
	handler, _, node, _ := geoIPReportFixture(t)
	settings := model.DefaultSiteSettings()
	handler.WithAccelerators(acceleratorStub{})

	settings.ShowAccelerators = false
	response, err := handler.buildPublicNodeResponse(t.Context(), *node, settings)
	if err != nil || response.AcceleratorVisible || len(response.Accelerators) != 0 {
		t.Fatalf("global hidden response = %+v, err = %v", response, err)
	}

	settings.ShowAccelerators = true
	response, err = handler.buildPublicNodeResponse(t.Context(), *node, settings)
	if err != nil || !response.AcceleratorVisible || len(response.Accelerators) != 1 {
		t.Fatalf("visible response = %+v, err = %v", response, err)
	}

	handler.WithAccelerators(acceleratorStub{nodeHidden: true})
	response, err = handler.buildPublicNodeResponse(t.Context(), *node, settings)
	if err != nil || response.AcceleratorVisible || len(response.Accelerators) != 0 {
		t.Fatalf("node hidden response = %+v, err = %v", response, err)
	}
}

func TestAcceleratorVisibilityHandlerSavesSelectedNode(t *testing.T) {
	var visible bool
	handler := NewAcceleratorHandler(acceleratorStub{visibility: &visible})
	request := geoIPRequest(`{"visible":false}`)
	request.SetPathValue("id", "node-fixture")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.HandleSaveVisibility(response, request)
	if response.Code != http.StatusOK || visible {
		t.Fatalf("save visibility status = %d, visible = %v, body = %s", response.Code, visible, response.Body.String())
	}
	invalid := geoIPRequest(`{"visible":`)
	invalid.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	handler.HandleSaveVisibility(response, invalid)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid body status = %d", response.Code)
	}
	anonymous := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"visible":true}`))
	response = httptest.NewRecorder()
	handler.HandleSaveVisibility(response, anonymous)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d", response.Code)
	}
}
