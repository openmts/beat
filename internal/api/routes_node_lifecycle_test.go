package api

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/beat/backend/internal/geoip"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/nodelifecycle"
)

type geoIPRouteService struct{}

func (geoIPRouteService) Settings(context.Context, geoip.Actor) (model.GeoIPSettings, error) {
	return model.GeoIPSettings{}, nil
}

func (geoIPRouteService) SaveSettings(context.Context, geoip.Actor, model.GeoIPSettingsWrite) (model.GeoIPSettings, error) {
	return model.GeoIPSettings{}, nil
}

func (geoIPRouteService) Location(context.Context, geoip.Actor, string) (model.NodeLocation, error) {
	return model.NodeLocation{}, nil
}

func (geoIPRouteService) SaveLocation(context.Context, geoip.Actor, geoip.LocationMutation) (model.NodeLocation, error) {
	return model.NodeLocation{}, nil
}

func (geoIPRouteService) Refresh(context.Context, geoip.Actor, string) error { return nil }

func (geoIPRouteService) InstallMMDB(context.Context, geoip.Actor, geoip.MMDBUpload) (model.GeoIPSettings, error) {
	return model.GeoIPSettings{}, nil
}

type lifecycleRouteService struct{}

func (lifecycleRouteService) Settings(context.Context, nodelifecycle.Actor) (model.LifecycleSettings, error) {
	return model.LifecycleSettings{}, nil
}

func (lifecycleRouteService) SaveSettings(
	context.Context,
	nodelifecycle.Actor,
	model.LifecycleSettings,
) (model.LifecycleSettings, error) {
	return model.LifecycleSettings{}, nil
}

func (lifecycleRouteService) Node(context.Context, nodelifecycle.Actor, string) (model.NodeLifecycle, error) {
	return model.NodeLifecycle{}, nil
}

func (lifecycleRouteService) SaveNode(
	context.Context,
	nodelifecycle.Actor,
	nodelifecycle.NodeChange,
) (model.NodeLifecycle, error) {
	return model.NodeLifecycle{}, nil
}

func (lifecycleRouteService) Events(
	context.Context,
	nodelifecycle.Actor,
	nodelifecycle.EventQuery,
) ([]model.LifecycleEvent, error) {
	return nil, nil
}

type fakeLocationViews struct{}

func (*fakeLocationViews) PublicLocation(context.Context, string) (*model.LocationPublic, error) {
	return nil, nil
}

type fakeLifecycleViews struct{}

func (*fakeLifecycleViews) PublicLifecycle(context.Context, string) (*model.LifecyclePublic, error) {
	return nil, nil
}

func TestNodeLifecycleRoutesRegisterAdminSurface(t *testing.T) {
	router := &Router{}
	WithGeoIP(geoIPRouteService{}, nil)(router)
	WithNodeLifecycle(lifecycleRouteService{}, nil)(router)
	mux := http.NewServeMux()
	router.registerNodeLifecycleRoutes(mux)
	cases := []struct{ method, path, pattern string }{
		{http.MethodGet, "/api/v1/admin/settings/geoip", "GET /api/v1/admin/settings/geoip"},
		{http.MethodGet, "/api/v1/admin/nodes/node-1/location", "GET /api/v1/admin/nodes/{id}/location"},
		{http.MethodPut, "/api/v1/admin/settings/geoip", "PUT /api/v1/admin/settings/geoip"},
		{http.MethodPut, "/api/v1/admin/nodes/node-1/location", "PUT /api/v1/admin/nodes/{id}/location"},
		{http.MethodPost, "/api/v1/admin/nodes/node-1/location/refresh", "POST /api/v1/admin/nodes/{id}/location/refresh"},
		{http.MethodPost, "/api/v1/admin/settings/geoip/mmdb", "POST /api/v1/admin/settings/geoip/mmdb"},
		{http.MethodPost, "/api/v1/admin/settings/geoip/mmdb/update", "POST /api/v1/admin/settings/geoip/mmdb/update"},
		{http.MethodPost, "/api/v1/admin/settings/geoip/mmdb/rollback", "POST /api/v1/admin/settings/geoip/mmdb/rollback"},
		{http.MethodGet, "/api/v1/admin/settings/lifecycle", "GET /api/v1/admin/settings/lifecycle"},
		{http.MethodPut, "/api/v1/admin/settings/lifecycle", "PUT /api/v1/admin/settings/lifecycle"},
		{http.MethodGet, "/api/v1/admin/nodes/node-1/lifecycle", "GET /api/v1/admin/nodes/{id}/lifecycle"},
		{http.MethodPut, "/api/v1/admin/nodes/node-1/lifecycle", "PUT /api/v1/admin/nodes/{id}/lifecycle"},
		{http.MethodGet, "/api/v1/admin/nodes/node-1/lifecycle/events", "GET /api/v1/admin/nodes/{id}/lifecycle/events"},
	}
	for _, test := range cases {
		_, pattern := mux.Handler(&http.Request{Method: test.method, URL: &url.URL{Path: test.path}})
		if pattern != test.pattern {
			t.Fatalf("%s %s registered as %q, want %q", test.method, test.path, pattern, test.pattern)
		}
	}
}

func TestNodeLifecycleRoutesStayClosedWithoutServices(t *testing.T) {
	router := &Router{}
	mux := http.NewServeMux()
	router.registerNodeLifecycleRoutes(mux)
	for _, path := range []string{"/api/v1/admin/settings/geoip", "/api/v1/admin/settings/lifecycle"} {
		if _, pattern := mux.Handler(&http.Request{Method: http.MethodGet, URL: &url.URL{Path: path}}); pattern != "" {
			t.Fatalf("%s registered without a service: %q", path, pattern)
		}
	}
	if router.geoIP != nil || router.nodeLifecycle != nil {
		t.Fatalf("zero router already had services: %#v", router)
	}
}

func TestRouterOptionsCarryGeoIPAndLifecycleViews(t *testing.T) {
	router := &Router{}
	locationViews, lifecycleViews := &fakeLocationViews{}, &fakeLifecycleViews{}
	WithGeoIP(geoIPRouteService{}, locationViews)(router)
	WithNodeLifecycle(lifecycleRouteService{}, lifecycleViews)(router)
	if router.geoIP == nil || router.locationViews != locationViews {
		t.Fatalf("GeoIP option = %#v", router)
	}
	if router.nodeLifecycle == nil || router.lifecycleViews != lifecycleViews {
		t.Fatalf("lifecycle option = %#v", router)
	}
}
