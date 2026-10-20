package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/observability"
	"github.com/beat/backend/internal/serviceprincipal"
	"github.com/beat/backend/internal/store"
)

func TestInitializeServicePrincipalsRevokesElapsedTokens(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	database, err := store.NewSQLiteStore(filepath.Join(directory, "tokens.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	principals := store.NewServicePrincipalStore(database.DB)
	actor := store.ServicePrincipalActor{UserID: "owner-one", SessionID: "session-abcdef", RequestID: "request-one"}
	expired := model.ServicePrincipal{
		ID: "principal-expired", Name: "expired", TokenPrefix: model.ServiceTokenPrefix + "abcdefgh",
		TokenHash: make([]byte, 32), Visibility: model.ServiceVisibilityPrivate,
		Scopes:    []model.ServiceScope{model.ServiceScopeHealthRead},
		ExpiresAt: time.Now().UTC().Add(-time.Hour), Revision: 1, CreatedBy: "owner-one", CreatedAt: time.Now().UTC().Add(-2 * time.Hour),
	}
	if err := principals.Create(t.Context(), expired, actor); err != nil {
		t.Fatal(err)
	}
	services := &serverServices{}
	stores := &serverStores{sqlite: database}
	if err := services.initializeServicePrincipals(t.Context(), stores); err != nil {
		t.Fatal(err)
	}
	if services.servicePrincipals == nil {
		t.Fatal("service token handler was not initialized")
	}
	var revoked int
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM service_principals WHERE revoked_reason = 'expired'").Scan(&revoked); err != nil {
		t.Fatal(err)
	}
	if revoked != 1 {
		t.Fatal("elapsed token was not revoked at startup", revoked)
	}
}

func TestInitializeServicePrincipalsRequiresDependencies(t *testing.T) {
	if err := (&serverServices{}).initializeServicePrincipals(t.Context(), nil); err == nil {
		t.Fatal("missing dependencies were accepted")
	}
}

func TestServiceTokenAccessSkipsUninitializedServices(t *testing.T) {
	metrics := observability.NewRegistry()
	for name, services := range map[string]*serverServices{"nil services": nil, "empty services": {}} {
		access := serviceTokenAccess(services, metrics, nil)
		if access.Authenticator != nil || access.Limiter != nil || access.Metrics != nil {
			t.Fatalf("%s enabled the data plane: %#v", name, access)
		}
	}
}

func TestServiceTokenAccessCarriesAuthenticatorLimiterAndMetrics(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	database, err := store.NewSQLiteStore(filepath.Join(directory, "tokens.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	services := &serverServices{}
	if err := services.initializeServicePrincipals(t.Context(), &serverStores{sqlite: database}); err != nil {
		t.Fatal(err)
	}
	metrics := observability.NewRegistry()
	access := serviceTokenAccess(services, metrics, nil)
	if access.Authenticator == nil || access.Limiter == nil || access.Metrics != metrics {
		t.Fatalf("data plane was not wired: %#v", access)
	}
	if access.FailureLimiter == nil || access.FailureLimiter == access.Limiter {
		t.Fatalf("failed-authentication admission was not wired separately: %#v", access)
	}
	if access.Audit != nil {
		t.Fatalf("audit recorder was wired without an admin service: %#v", access.Audit)
	}
}

func TestFailedServiceTokenRateLimitsCapSourceBurst(t *testing.T) {
	limits := failedServiceTokenRateLimits()
	if limits.Source.Burst != failedServiceTokenSourceBurst || limits.Source.Interval != time.Second {
		t.Fatalf("failed authentication source limits = %+v", limits.Source)
	}
	definitions := serviceprincipal.DefaultRateLimits()
	if limits.Token != definitions.Token {
		t.Fatalf("token limits changed: %+v", limits.Token)
	}
}

func TestNodeResourceVisibilityFollowsPublicFlag(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	database, err := store.NewSQLiteStore(filepath.Join(directory, "visibility.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	nodes := store.NewNodeStore(database.DB)
	publicNode, err := nodes.UpsertNode(t.Context(), "public-node", "127.0.0.1", 22)
	if err != nil {
		t.Fatal(err)
	}
	isPublic := true
	if _, err := nodes.UpdateNode(t.Context(), publicNode.ID, store.NodeUpdate{GroupID: publicNode.GroupID, IsPublic: &isPublic}); err != nil {
		t.Fatal(err)
	}
	privateNode, err := nodes.UpsertNode(t.Context(), "private-node", "127.0.0.2", 22)
	if err != nil {
		t.Fatal(err)
	}
	notPublic := false
	if _, err := nodes.UpdateNode(t.Context(), privateNode.ID, store.NodeUpdate{GroupID: privateNode.GroupID, IsPublic: &notPublic}); err != nil {
		t.Fatal(err)
	}
	visibility := nodeResourceVisibility(nodes)
	cases := []struct {
		name string
		kind model.ServiceResourceKind
		id   string
		want bool
	}{
		{name: "public node", kind: model.ServiceResourceNode, id: publicNode.ID, want: true},
		{name: "private node", kind: model.ServiceResourceNode, id: privateNode.ID},
		{name: "unknown node", kind: model.ServiceResourceNode, id: "missing-node"},
		{name: "other kind", kind: model.ServiceResourceChannel, id: publicNode.ID},
		{name: "empty id", kind: model.ServiceResourceNode},
	}
	for _, testCase := range cases {
		public, err := visibility(t.Context(), testCase.kind, testCase.id)
		if err != nil || public != testCase.want {
			t.Fatalf("%s: public = %v, err = %v", testCase.name, public, err)
		}
	}
	if public, err := nodeResourceVisibility(nil)(t.Context(), model.ServiceResourceNode, publicNode.ID); err != nil || public {
		t.Fatalf("nil store visibility = %v, err = %v", public, err)
	}
}

func TestRegisterServiceTokenMetricsExposesCounts(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	database, err := store.NewSQLiteStore(filepath.Join(directory, "counts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	principals := store.NewServicePrincipalStore(database.DB)
	active := model.ServicePrincipal{
		ID: "principal-active", Name: "active", TokenPrefix: model.ServiceTokenPrefix + "active01",
		TokenHash: make([]byte, 32), Visibility: model.ServiceVisibilityPrivate,
		Scopes:    []model.ServiceScope{model.ServiceScopeMetricsRead},
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour), Revision: 1, CreatedBy: "owner-one", CreatedAt: time.Now().UTC(),
	}
	if err := principals.Create(t.Context(), active, store.ServicePrincipalActor{UserID: "owner-one"}); err != nil {
		t.Fatal(err)
	}
	services := &serverServices{}
	if err := services.initializeServicePrincipals(t.Context(), &serverStores{sqlite: database}); err != nil {
		t.Fatal(err)
	}
	metrics := observability.NewRegistry()
	registerServiceTokenMetrics(metrics, services)
	output := metrics.Prometheus()
	for _, expected := range []string{
		`beat_service_tokens{state="active"} 1`,
		`beat_service_tokens{state="expiring"} 1`,
		`beat_service_tokens{state="revoked"} 0`,
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("metrics missing %q:\n%s", expected, output)
		}
	}
	registerServiceTokenMetrics(nil, nil)
}
