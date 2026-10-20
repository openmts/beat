package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/secretbox"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
)

func TestReportUsesCanonicalProxySourceForGeoIP(t *testing.T) {
	for _, trusted := range []bool{false, true} {
		t.Run(map[bool]string{true: "trusted", false: "untrusted"}[trusted], func(t *testing.T) {
			handler, database, node, secrets := geoIPReportFixture(t)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/report", strings.NewReader(testutil.VersionedAgentReport(t, `{"host":"9.9.9.9","port":22,"metrics":{"cpu":1}}`)))
			request = request.WithContext(middleware.WithAgentIdentity(request.Context(), testutil.AgentTestIdentity(*node)))
			request.RemoteAddr = "192.0.2.10:5000"
			request.Header.Set("X-Forwarded-For", "8.8.4.4")
			cidrs := ""
			if trusted {
				cidrs = "192.0.2.0/24"
			}
			proxies, err := middleware.ParseTrustedProxies(cidrs)
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			middleware.RequestContext(proxies)(http.HandlerFunc(handler.HandleNodeReport)).ServeHTTP(response, request)
			if response.Code != http.StatusCreated {
				t.Fatalf("report: %d %s", response.Code, response.Body.String())
			}
			var ciphertext []byte
			if err = database.DB.QueryRow(`SELECT source_encrypted FROM node_locations WHERE id=?`, node.ID).Scan(&ciphertext); err != nil {
				t.Fatal(err)
			}
			plain, err := secrets.OpenResource(t.Context(), ciphertext, secretbox.GeoIPSourceAAD(node.ID, "observed"))
			expected := "192.0.2.10"
			if trusted {
				expected = "8.8.4.4"
			}
			if err != nil || string(plain) != expected {
				t.Fatal("untrusted host/header used", string(plain), err)
			}
			clear(plain)
			if node.AgentCredentialStatus() != model.AgentCredentialActive {
				t.Fatal("fixture identity")
			}
		})
	}
}

func geoIPReportFixture(t *testing.T) (*NodeHandler, *store.SQLiteStore, *model.Node, secretbox.ResourceService) {
	t.Helper()
	database, mts := setupNodeTestDB(t)

	secrets := testutil.NewSecrets(t, database.DB)
	nodes := store.NewNodeStore(database.DB).WithGeoIPSecrets(secrets)
	node, err := nodes.CreateManagedNode(t.Context(), store.ManagedNodeInput{Name: "geoip-agent", Host: "untrusted.example", Port: 22}, store.AgentCredential{Hash: []byte(strings.Repeat("a", 32)), Prefix: "geo", CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewNodeHandler(nodes, mts)
	return handler, database, node, secrets
}

type locationViewStub struct{ err error }

func (s locationViewStub) PublicLocation(context.Context, string) (*model.LocationPublic, error) {
	return &model.LocationPublic{Scope: "public", CountryCode: "US"}, s.err
}

func TestLocationViewsSurviveRESTAndWebsocketRefresh(t *testing.T) {
	database, mts := setupNodeTestDB(t)
	nodes := store.NewNodeStore(database.DB)
	node, err := nodes.UpsertNode(t.Context(), "public-location", "private-host", 22)
	if err != nil {
		t.Fatal(err)
	}
	public := true
	node, err = nodes.UpdateNode(t.Context(), node.ID, store.NodeUpdate{GroupID: node.GroupID, IsPublic: &public})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewNodeHandler(nodes, mts).WithLocationViews(locationViewStub{})
	response, err := handler.buildPublicNodeResponse(t.Context(), *node, model.DefaultSiteSettings())
	if err != nil || response.Location == nil || response.Location.CountryCode != "US" {
		t.Fatal(response, err)
	}
	metrics := NewMetricsHandler(nodes, mts).WithLocationViews(locationViewStub{})
	snapshot, err := metrics.snapshot(t.Context())
	if err != nil || len(snapshot.Nodes) != 1 || snapshot.Nodes[0].Location == nil {
		t.Fatal(snapshot, err)
	}
	handler.WithLocationViews(locationViewStub{err: errors.New("unavailable")})
	if _, err = handler.buildPublicNodeResponse(t.Context(), *node, model.DefaultSiteSettings()); err == nil {
		t.Fatal("location read failure hidden")
	}
}
