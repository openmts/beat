package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/serviceprincipalhttp"
	"github.com/beat/backend/internal/store"
)

// 下列桩内嵌接口，用于在注册阶段得到非 nil 依赖；注册不会调用任何方法。
type registrationOperationsStub struct {
	handler.RemoteOperations
	handler.RemoteTransfers
}

type registrationDiagnosticsStub struct{ handler.Diagnostics }

type registrationAcceleratorsStub struct{ handler.AcceleratorService }

type registrationRetentionStub struct {
	handler.AgentRetentionOperations
}

type registrationOverviewStub struct {
	handler.MigrationOverviewSource
}

type registrationMetricsStub struct{ handler.EnrollmentMetrics }

type registrationNotificationsStub struct{ handler.AlertNotifications }

type registrationLimiterStub struct{ IngressLimiter }

func servedPattern(mux *http.ServeMux, method, path string) string {
	_, pattern := mux.Handler(httptest.NewRequest(method, "http://example.com"+path, nil))
	return pattern
}

func requireRegistered(t *testing.T, mux *http.ServeMux, method, path string) {
	t.Helper()
	if pattern := servedPattern(mux, method, path); pattern == "" {
		t.Fatalf("%s %s is not registered", method, path)
	}
}

func TestCommercialRouterOptionsAssignFields(t *testing.T) {
	router := &Router{}
	WithRemoteOperations(registrationOperationsStub{})(router)
	WithDiagnostics(registrationDiagnosticsStub{})(router)
	WithAlertEvaluation(handler.AlertEvaluationHandlerConfig{
		Rules:  &store.AlertEvaluationRuleStore{},
		States: &store.AlertEvaluationStore{},
	})(router)
	WithAccelerators(registrationAcceleratorsStub{})(router)
	WithAgentRetentionService(registrationRetentionStub{})(router)
	WithMigrationOverview(registrationOverviewStub{})(router)
	WithAgentEnrollmentMetrics(registrationMetricsStub{})(router)
	WithAlertNotifications(registrationNotificationsStub{})(router)
	WithIngressLimiter(registrationLimiterStub{})(router)
	WithServicePrincipals(serviceprincipalhttp.New(nil))(router)

	if router.remoteOperations == nil || router.diagnostics == nil || router.alertEvaluation == nil ||
		router.accelerators == nil || router.agentRetention == nil || router.migrationOverview == nil ||
		router.enrollmentMetrics == nil || router.alertNotifications == nil || router.ingressLimiter == nil ||
		router.servicePrincipals == nil {
		t.Fatalf("options were not applied: %#v", router)
	}
}

func TestRegisterCommercialRoutesMountsAllSurfaces(t *testing.T) {
	router := &Router{
		remoteOperations: registrationOperationsStub{},
		diagnostics:      registrationDiagnosticsStub{},
		alertEvaluation: &handler.AlertEvaluationHandlerConfig{
			Rules:  &store.AlertEvaluationRuleStore{},
			States: &store.AlertEvaluationStore{},
		},
	}
	mux := http.NewServeMux()
	router.registerCommercialRoutes(mux)
	requireRegistered(t, mux, http.MethodGet, "/api/v1/admin/operations/status")
	requireRegistered(t, mux, http.MethodGet, "/api/v1/admin/operations/transfers")
	requireRegistered(t, mux, http.MethodGet, "/api/v1/admin/diagnostics/policy")
	requireRegistered(t, mux, http.MethodGet, "/api/v1/alerts/rules")
}

func TestRegisterAcceleratorAndRetentionSurfaces(t *testing.T) {
	router := &Router{
		accelerators:      registrationAcceleratorsStub{},
		agentRetention:    registrationRetentionStub{},
		migrationOverview: registrationOverviewStub{},
		servicePrincipals: serviceprincipalhttp.New(nil),
	}
	mux := http.NewServeMux()
	router.registerAcceleratorRoutes(mux)
	router.registerAgentRetentionRoutes(mux)
	router.registerMigrationOverviewRoutes(mux)
	router.registerServicePrincipalRoutes(mux)
	requireRegistered(t, mux, http.MethodGet, "/api/v1/admin/nodes/node-1/accelerators")
	requireRegistered(t, mux, http.MethodGet, "/api/v1/settings/agent-release-retention")
	requireRegistered(t, mux, http.MethodGet, "/api/v1/admin/migration")
	requireRegistered(t, mux, http.MethodGet, "/api/v1/admin/service-tokens")
}

func TestRegisterMetricErasureRoutesWithSignal(t *testing.T) {
	router := &Router{metricErasure: &store.MetricErasureStore{}, metricErasureWake: func() {}}
	mux := http.NewServeMux()
	router.registerMetricErasureRoutes(mux)
	requireRegistered(t, mux, http.MethodGet, "/api/v1/admin/metric-erasure/jobs")
}

func TestRegisterMetricCatalogRoutesWithoutSession(t *testing.T) {
	router := &Router{metricCatalog: &store.MetricCatalogStore{}}
	mux := http.NewServeMux()
	router.registerMetricCatalogRoutes(mux)
	requireRegistered(t, mux, http.MethodGet, "/api/v1/metrics/catalog")
	requireRegistered(t, mux, http.MethodPut, "/api/v1/admin/metrics/catalog/cpu")
}

func TestRegisterNotificationRoutesWithSession(t *testing.T) {
	router := &Router{notificationQueue: &notificationqueue.Service{}, security: &adminauth.Service{}}
	mux := http.NewServeMux()
	router.registerNotificationRoutes(mux)
	requireRegistered(t, mux, http.MethodGet, "/api/v1/notifications/settings")
	requireRegistered(t, mux, http.MethodPost, "/api/v1/alerts/channels")
}

func TestRegisterAuthAndSettingsServesDeploymentStatus(t *testing.T) {
	router := &Router{}
	mux := http.NewServeMux()
	router.registerAuthAndSettingsRoutes(mux)
	requireRegistered(t, mux, http.MethodGet, "/api/v1/admin/settings/deployment")
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(
		http.MethodGet, "http://example.com/api/v1/admin/settings/deployment", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("deployment status route = %d", recorder.Code)
	}
}

func TestRegisterPublicTelemetryRoutesWithoutTelemetryStore(t *testing.T) {
	router := &Router{publicTelemetry: &store.PublicTelemetryStore{}}
	mux := http.NewServeMux()
	router.registerPublicTelemetryRoutes(mux)
	requireRegistered(t, mux, http.MethodGet, "/api/v1/admin/settings/public-telemetry")
	if pattern := servedPattern(mux, http.MethodPost, "/api/v1/public/interactions"); pattern != "" {
		t.Fatalf("anonymous interactions must stay disabled, got %q", pattern)
	}
}
