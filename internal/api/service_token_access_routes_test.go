package api

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/serviceprincipal"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
)

const serviceTokenFixtureValue = model.ServiceTokenPrefix + "fixture"

type stubServiceTokenAuthenticator struct {
	principal *model.ServicePrincipal
	err       error
	calls     int
}

func (stub *stubServiceTokenAuthenticator) Authenticate(
	context.Context, string, netip.Addr,
) (*model.ServicePrincipal, error) {
	stub.calls++
	if stub.err != nil {
		return nil, stub.err
	}
	return stub.principal, nil
}

type recordingServiceTokenMetrics struct {
	authOutcomes []string
	denials      []string
	rateLimits   []string
	latencies    []string
}

func (metrics *recordingServiceTokenMetrics) ObserveServiceTokenAuth(outcome string) {
	metrics.authOutcomes = append(metrics.authOutcomes, outcome)
}

func (metrics *recordingServiceTokenMetrics) ObserveServiceTokenDenial(reason string) {
	metrics.denials = append(metrics.denials, reason)
}

func (metrics *recordingServiceTokenMetrics) ObserveServiceTokenLatency(scope string, _ time.Duration) {
	metrics.latencies = append(metrics.latencies, scope)
}

func (metrics *recordingServiceTokenMetrics) ObserveServiceTokenRateLimit(scope string) {
	metrics.rateLimits = append(metrics.rateLimits, scope)
}

var _ middleware.ServiceTokenMetrics = (*recordingServiceTokenMetrics)(nil)

func fixtureServicePrincipal(
	scopes []model.ServiceScope, resources ...model.ServiceResource,
) *model.ServicePrincipal {
	return fixtureServicePrincipalWithVisibility(model.ServiceVisibilityPrivate, scopes, resources...)
}

func fixturePublicServicePrincipal(
	scopes []model.ServiceScope, resources ...model.ServiceResource,
) *model.ServicePrincipal {
	return fixtureServicePrincipalWithVisibility(model.ServiceVisibilityPublic, scopes, resources...)
}

func fixtureServicePrincipalWithVisibility(
	visibility model.ServiceVisibility, scopes []model.ServiceScope, resources ...model.ServiceResource,
) *model.ServicePrincipal {
	return &model.ServicePrincipal{
		ID: "principal-1", Name: "reporting", TokenPrefix: serviceTokenFixtureValue,
		Visibility: visibility, Scopes: scopes, Resources: resources,
		ExpiresAt: time.Now().Add(time.Hour).UTC(),
	}
}

func serviceTokenOption(
	authenticator middleware.ServiceTokenAuthenticator,
	metrics middleware.ServiceTokenMetrics,
	limiter *serviceprincipal.Limiter,
) RouterOption {
	return serviceTokenOptionWithVisibility(authenticator, metrics, limiter, nil)
}

func serviceTokenOptionWithVisibility(
	authenticator middleware.ServiceTokenAuthenticator,
	metrics middleware.ServiceTokenMetrics,
	limiter *serviceprincipal.Limiter,
	visibility middleware.ServiceResourceVisibility,
) RouterOption {
	return WithServiceTokenAccess(ServiceTokenAccess{
		Authenticator: authenticator, Limiter: limiter, Metrics: metrics, Visibility: visibility,
	})
}

func serviceTokenBearer() string { return "Bearer " + serviceTokenFixtureValue }

func TestServiceTokenAccessAllowsScopedNodeInventory(t *testing.T) {
	authenticator := &stubServiceTokenAuthenticator{
		principal: fixtureServicePrincipal([]model.ServiceScope{model.ServiceScopeNodesRead}),
	}
	metrics := &recordingServiceTokenMetrics{}
	router := setupSessionRouterWithOptions(t, serviceTokenOption(authenticator, metrics, nil))

	response := requestRouter(router, http.MethodGet, "/api/v1/nodes/manage", "", serviceTokenBearer(), "")
	if response.Code != http.StatusOK {
		t.Fatalf("scoped inventory status = %d, body = %s", response.Code, response.Body.String())
	}
	if len(metrics.authOutcomes) != 1 || metrics.authOutcomes[0] != "authenticated" || len(metrics.denials) != 0 {
		t.Fatalf("metrics = %#v", metrics)
	}
}

func TestServiceTokenAccessDeniesMissingScope(t *testing.T) {
	authenticator := &stubServiceTokenAuthenticator{
		principal: fixtureServicePrincipal([]model.ServiceScope{model.ServiceScopeHealthRead}),
	}
	metrics := &recordingServiceTokenMetrics{}
	router := setupSessionRouterWithOptions(t, serviceTokenOption(authenticator, metrics, nil))

	response := requestRouter(router, http.MethodGet, "/api/v1/nodes/manage", "", serviceTokenBearer(), "")
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "service token is not permitted") {
		t.Fatalf("scope denial status = %d, body = %s", response.Code, response.Body.String())
	}
	if len(metrics.denials) != 1 || metrics.denials[0] != "scope" {
		t.Fatalf("denial metrics = %#v", metrics.denials)
	}
}

func TestServiceTokenAccessRejectsUnknownTokenAndUnavailableStorage(t *testing.T) {
	t.Run("unknown token", func(t *testing.T) {
		authenticator := &stubServiceTokenAuthenticator{err: serviceprincipal.ErrUnauthorized}
		metrics := &recordingServiceTokenMetrics{}
		router := setupSessionRouterWithOptions(t, serviceTokenOption(authenticator, metrics, nil))

		response := requestRouter(router, http.MethodGet, "/api/v1/nodes/manage", "", serviceTokenBearer(), "")
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("unknown token status = %d", response.Code)
		}
		if len(metrics.authOutcomes) != 1 || metrics.authOutcomes[0] != "denied" {
			t.Fatalf("outcome metrics = %#v", metrics.authOutcomes)
		}
	})
	t.Run("unavailable store", func(t *testing.T) {
		authenticator := &stubServiceTokenAuthenticator{err: serviceprincipal.ErrUnavailable}
		metrics := &recordingServiceTokenMetrics{}
		router := setupSessionRouterWithOptions(t, serviceTokenOption(authenticator, metrics, nil))

		response := requestRouter(router, http.MethodGet, "/api/v1/nodes/manage", "", serviceTokenBearer(), "")
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("unavailable status = %d", response.Code)
		}
		if len(metrics.authOutcomes) != 1 || metrics.authOutcomes[0] != "unavailable" {
			t.Fatalf("outcome metrics = %#v", metrics.authOutcomes)
		}
	})
}

func TestServiceTokenAccessRateLimitsAuthenticatedTokens(t *testing.T) {
	authenticator := &stubServiceTokenAuthenticator{
		principal: fixtureServicePrincipal([]model.ServiceScope{model.ServiceScopeNodesRead}),
	}
	metrics := &recordingServiceTokenMetrics{}
	limiter := serviceprincipal.NewLimiter(serviceprincipal.RateLimits{
		Token:  serviceprincipal.RateLimit{Burst: 1, Interval: 2 * time.Second},
		Source: serviceprincipal.RateLimit{Burst: 50, Interval: time.Hour},
	})
	router := setupSessionRouterWithOptions(t, serviceTokenOption(authenticator, metrics, limiter))

	first := requestRouter(router, http.MethodGet, "/api/v1/nodes/manage", "", serviceTokenBearer(), "")
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, body = %s", first.Code, first.Body.String())
	}
	second := requestRouter(router, http.MethodGet, "/api/v1/nodes/manage", "", serviceTokenBearer(), "")
	if second.Code != http.StatusTooManyRequests || second.Header().Get("Retry-After") != "2" {
		t.Fatalf("limited status = %d, retry after = %q", second.Code, second.Header().Get("Retry-After"))
	}
	if len(metrics.rateLimits) != 1 || metrics.rateLimits[0] != "token" {
		t.Fatalf("rate limit metrics = %#v", metrics.rateLimits)
	}
}

func TestServiceTokenAccessKeepsAnonymousAndBrowserPaths(t *testing.T) {
	authenticator := &stubServiceTokenAuthenticator{err: errors.New("service tokens must not be consulted")}
	metrics := &recordingServiceTokenMetrics{}
	router := setupSessionRouterWithOptions(t, serviceTokenOption(authenticator, metrics, nil))

	anonymous := requestRouter(router, http.MethodGet, "/api/v1/nodes", "", "", "")
	if anonymous.Code != http.StatusOK {
		t.Fatalf("anonymous public status = %d", anonymous.Code)
	}
	browser := requestRouter(router, http.MethodGet, "/api/v1/nodes/manage", "", "Bearer admin-secret", "")
	if browser.Code != http.StatusOK {
		t.Fatalf("legacy admin status = %d, body = %s", browser.Code, browser.Body.String())
	}
	if authenticator.calls != 0 || len(metrics.authOutcomes) != 0 {
		t.Fatalf("browser paths consulted service tokens: calls = %d, metrics = %#v",
			authenticator.calls, metrics.authOutcomes)
	}
}

func TestServiceTokenAccessEnforcesNodeResourceConstraint(t *testing.T) {
	authenticator := &stubServiceTokenAuthenticator{principal: fixtureServicePrincipal(
		[]model.ServiceScope{model.ServiceScopeMetricsRead},
		model.ServiceResource{Kind: model.ServiceResourceNode, ID: "node-a"},
	)}
	metrics := &recordingServiceTokenMetrics{}
	router := setupSessionRouterWithOptions(t, serviceTokenOption(authenticator, metrics, nil))

	denied := requestRouter(router, http.MethodGet, "/api/v1/nodes/node-b/metrics", "", serviceTokenBearer(), "")
	if denied.Code != http.StatusForbidden {
		t.Fatalf("unrelated node status = %d, body = %s", denied.Code, denied.Body.String())
	}
	if len(metrics.denials) != 1 || metrics.denials[0] != "resource" {
		t.Fatalf("denial metrics = %#v", metrics.denials)
	}
	allowed := requestRouter(router, http.MethodGet, "/api/v1/nodes/node-a/metrics", "", serviceTokenBearer(), "")
	if allowed.Code == http.StatusForbidden || allowed.Code == http.StatusUnauthorized {
		t.Fatalf("constrained node was blocked: %d, body = %s", allowed.Code, allowed.Body.String())
	}
}

func TestServiceTokenAccessCoversBackupScopes(t *testing.T) {
	authenticator := &stubServiceTokenAuthenticator{principal: fixtureServicePrincipal(
		[]model.ServiceScope{model.ServiceScopeBackupsReadMetadata, model.ServiceScopeBackupsCreate},
	)}
	metrics := &recordingServiceTokenMetrics{}
	router := setupSessionRouterWithOptions(t, WithBackupService(optionBackup{}),
		serviceTokenOption(authenticator, metrics, nil))

	list := requestRouter(router, http.MethodGet, "/api/v1/admin/backups", "", serviceTokenBearer(), "")
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"backup"`) {
		t.Fatalf("backup list status = %d, body = %s", list.Code, list.Body.String())
	}
	create := requestRouter(router, http.MethodPost, "/api/v1/admin/backups", "", serviceTokenBearer(), "")
	if create.Code != http.StatusAccepted {
		t.Fatalf("backup create status = %d, body = %s", create.Code, create.Body.String())
	}
	download := requestRouter(router, http.MethodGet, "/api/v1/admin/backups/backup/download", "", serviceTokenBearer(), "")
	if download.Code != http.StatusForbidden {
		t.Fatalf("download without scope status = %d", download.Code)
	}
}

func TestServiceTokenAccessCoversAuditReadWithoutSessionPrefix(t *testing.T) {
	router, adminStore := setupServiceTokenAuditRouter(t,
		fixtureServicePrincipal([]model.ServiceScope{model.ServiceScopeAuditRead}))
	event := model.AdminAuditEvent{
		ID: "event-1", RequestID: "request-1", ActorID: "user-1", ActorUsername: "owner",
		Action: "service_token.create", ResourceType: "service_token", ResourceID: "token-1",
		Outcome: model.AuditOutcomeSuccess, DetailJSON: "{}", SessionPrefix: "sessionsuffix",
		CreatedAt: time.Now().UTC(),
	}
	if err := adminStore.CreateAuditEvent(t.Context(), &event); err != nil {
		t.Fatalf("seed audit event: %v", err)
	}

	response := requestRouter(router, http.MethodGet, "/api/v1/admin/audit-events?limit=10", "", serviceTokenBearer(), "")
	if response.Code != http.StatusOK {
		t.Fatalf("audit read status = %d, body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "token-1") {
		t.Fatalf("audit read missed seeded event: %s", response.Body.String())
	}
	if strings.Contains(response.Body.String(), "sessionsuffix") {
		t.Fatalf("service audit view leaked session prefix: %s", response.Body.String())
	}
}

func TestServiceTokenAccessCoversNetworkTaskRead(t *testing.T) {
	authenticator := &stubServiceTokenAuthenticator{
		principal: fixtureServicePrincipal([]model.ServiceScope{model.ServiceScopeNetworkTasksRead}),
	}
	router := setupServiceTokenNetworkRouter(t, serviceTokenOption(authenticator, &recordingServiceTokenMetrics{}, nil))
	response := requestRouter(router, http.MethodGet, "/api/v1/network/tasks", "", serviceTokenBearer(), "")
	if response.Code != http.StatusOK {
		t.Fatalf("network tasks status = %d, body = %s", response.Code, response.Body.String())
	}
}

func setupServiceTokenNetworkRouter(t *testing.T, option RouterOption) *Router {
	t.Helper()
	sqliteStore, err := store.NewSQLiteStore("file:" + t.Name() + "?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("create SQLite store: %v", err)
	}
	t.Cleanup(func() { _ = sqliteStore.Close() })
	mtsStore, err := store.NewMTSStore(filepath.Join(t.TempDir(), "mts"))
	if err != nil {
		t.Fatalf("create MTS store: %v", err)
	}
	t.Cleanup(func() { _ = mtsStore.Close() })
	return NewRouter(
		store.NewNodeStore(sqliteStore.DB), store.NewGroupStore(sqliteStore.DB), store.NewSSHKeyStore(sqliteStore.DB),
		store.NewAlertRuleStore(sqliteStore.DB), store.NewAlertChannelStore(sqliteStore.DB),
		store.NewAlertEventStore(sqliteStore.DB), mtsStore,
		WithAdminToken("admin-secret"),
		WithNetworkTaskStore(store.NewNetworkTaskStore(sqliteStore.DB)),
		option,
	)
}

func setupServiceTokenAuditRouter(t *testing.T, principal *model.ServicePrincipal) (*Router, *store.AdminStore) {
	t.Helper()
	sqliteStore, err := store.NewSQLiteStore("file:" + t.Name() + "?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqliteStore.Close() })
	randomData := make([]byte, 8192)
	for index := range randomData {
		randomData[index] = byte(index)
	}
	random := bytes.NewReader(randomData)
	adminStore := store.NewAdminStore(sqliteStore.DB)
	security, err := adminauth.NewService(adminauth.ServiceConfig{
		Store: adminStore, Secrets: testutil.NewSecrets(t, sqliteStore.DB), LegacyToken: "admin-secret", Random: random,
		Passwords: adminauth.NewPasswordHasher(adminauth.PasswordParams{
			MemoryKiB: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
		}, random),
	})
	if err != nil {
		t.Fatalf("new authentication service: %v", err)
	}
	access := serviceTokenOption(&stubServiceTokenAuthenticator{principal: principal}, &recordingServiceTokenMetrics{}, nil)
	router := NewRouter(
		store.NewNodeStore(sqliteStore.DB), store.NewGroupStore(sqliteStore.DB), store.NewSSHKeyStore(sqliteStore.DB),
		store.NewAlertRuleStore(sqliteStore.DB), store.NewAlertChannelStore(sqliteStore.DB),
		store.NewAlertEventStore(sqliteStore.DB), nil,
		WithAdminToken("admin-secret"), WithAdminSecurity(security), access,
	)
	return router, adminStore
}

func TestServiceTokenAccessAppliesPublicVisibility(t *testing.T) {
	authenticator := &stubServiceTokenAuthenticator{principal: fixturePublicServicePrincipal(
		[]model.ServiceScope{model.ServiceScopeNodesRead, model.ServiceScopeMetricsRead},
	)}
	metrics := &recordingServiceTokenMetrics{}
	visibility := func(_ context.Context, kind model.ServiceResourceKind, id string) (bool, error) {
		if kind != model.ServiceResourceNode {
			return false, nil
		}
		return id == "node-a", nil
	}
	router := setupSessionRouterWithOptions(t,
		serviceTokenOptionWithVisibility(authenticator, metrics, nil, visibility))

	publicNode := requestRouter(router, http.MethodGet, "/api/v1/nodes/node-a/metrics", "", serviceTokenBearer(), "")
	if publicNode.Code == http.StatusForbidden || publicNode.Code == http.StatusServiceUnavailable {
		t.Fatalf("public node read blocked: %d, body = %s", publicNode.Code, publicNode.Body.String())
	}
	privateNode := requestRouter(router, http.MethodGet, "/api/v1/nodes/node-b/metrics", "", serviceTokenBearer(), "")
	if privateNode.Code != http.StatusForbidden {
		t.Fatalf("private node status = %d, body = %s", privateNode.Code, privateNode.Body.String())
	}
	privateList := requestRouter(router, http.MethodGet, "/api/v1/nodes/manage", "", serviceTokenBearer(), "")
	if privateList.Code != http.StatusForbidden {
		t.Fatalf("private endpoint status = %d, body = %s", privateList.Code, privateList.Body.String())
	}
	if len(metrics.denials) != 2 || metrics.denials[0] != "visibility" || metrics.denials[1] != "visibility" {
		t.Fatalf("visibility denial metrics = %#v", metrics.denials)
	}
}

func TestServiceTokenAccessFailsClosedWhenVisibilityLookupFails(t *testing.T) {
	authenticator := &stubServiceTokenAuthenticator{principal: fixturePublicServicePrincipal(
		[]model.ServiceScope{model.ServiceScopeMetricsRead},
	)}
	metrics := &recordingServiceTokenMetrics{}
	visibility := func(context.Context, model.ServiceResourceKind, string) (bool, error) {
		return false, errors.New("visibility lookup failed")
	}
	router := setupSessionRouterWithOptions(t,
		serviceTokenOptionWithVisibility(authenticator, metrics, nil, visibility))
	response := requestRouter(router, http.MethodGet, "/api/v1/nodes/node-a/metrics", "", serviceTokenBearer(), "")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("failed visibility lookup status = %d, body = %s", response.Code, response.Body.String())
	}
	if len(metrics.authOutcomes) != 2 || metrics.authOutcomes[1] != "unavailable" {
		t.Fatalf("outcome metrics = %#v", metrics.authOutcomes)
	}
}
