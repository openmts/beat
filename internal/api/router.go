package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/notification"
	"github.com/beat/backend/internal/store"
)

type Router struct {
	mux               *http.ServeMux
	nodeStore         *store.NodeStore
	groupStore        *store.GroupStore
	sshKeyStore       *store.SSHKeyStore
	alertRuleStore    *store.AlertRuleStore
	alertChannelStore *store.AlertChannelStore
	alertEventStore   *store.AlertEventStore
	mtsStore          *store.MTSStore
	networkTaskStore  *store.NetworkTaskStore
	siteSettingsStore *store.SiteSettingsStore
	delivery          *notification.Service
	trafficReports    handler.TrafficReportOperations
	maintenance       handler.MaintenanceOperations
	backups           handler.BackupOperations
	security          *adminauth.Service
	adminToken        string
	legacyAgentToken  string
	terminal          handler.TerminalOperations
	adminAuthEnabled  bool
	agentAuthEnabled  bool
}

type RouterOption func(*Router)

func WithAuthTokens(adminToken, agentToken string) RouterOption {
	return func(router *Router) {
		router.adminToken = adminToken
		router.legacyAgentToken = agentToken
		router.adminAuthEnabled = true
		router.agentAuthEnabled = true
	}
}

func WithAdminToken(adminToken string) RouterOption {
	return func(router *Router) {
		router.adminToken = adminToken
		router.adminAuthEnabled = true
	}
}

func WithAgentAuthentication(legacyAgentToken string) RouterOption {
	return func(router *Router) {
		router.legacyAgentToken = legacyAgentToken
		router.agentAuthEnabled = true
	}
}

func WithTerminalOperations(terminal handler.TerminalOperations) RouterOption {
	return func(router *Router) {
		router.terminal = terminal
	}
}

func WithNetworkTaskStore(networkTaskStore *store.NetworkTaskStore) RouterOption {
	return func(router *Router) {
		router.networkTaskStore = networkTaskStore
	}
}

func WithSiteSettingsStore(siteSettingsStore *store.SiteSettingsStore) RouterOption {
	return func(router *Router) {
		router.siteSettingsStore = siteSettingsStore
	}
}

func WithNotificationService(delivery *notification.Service) RouterOption {
	return func(router *Router) {
		router.delivery = delivery
	}
}

func WithTrafficReportService(service handler.TrafficReportOperations) RouterOption {
	return func(router *Router) {
		router.trafficReports = service
	}
}

func WithMaintenanceService(service handler.MaintenanceOperations) RouterOption {
	return func(router *Router) {
		router.maintenance = service
	}
}

func WithBackupService(service handler.BackupOperations) RouterOption {
	return func(router *Router) {
		router.backups = service
	}
}

func WithAdminSecurity(service *adminauth.Service) RouterOption {
	return func(router *Router) {
		router.security = service
	}
}

func NewRouter(
	nodeStore *store.NodeStore,
	groupStore *store.GroupStore,
	sshKeyStore *store.SSHKeyStore,
	alertRuleStore *store.AlertRuleStore,
	alertChannelStore *store.AlertChannelStore,
	alertEventStore *store.AlertEventStore,
	mtsStore *store.MTSStore,
	options ...RouterOption,
) *Router {
	r := &Router{
		mux:               http.NewServeMux(),
		nodeStore:         nodeStore,
		groupStore:        groupStore,
		sshKeyStore:       sshKeyStore,
		alertRuleStore:    alertRuleStore,
		alertChannelStore: alertChannelStore,
		alertEventStore:   alertEventStore,
		mtsStore:          mtsStore,
	}
	for _, option := range options {
		option(r)
	}
	if r.delivery == nil {
		r.delivery = notification.NewService()
	}

	r.setupRoutes()

	return r
}

func (r *Router) setupRoutes() {
	api := http.NewServeMux()
	r.registerAuthAndSettingsRoutes(api)
	r.registerNodeAndGroupRoutes(api)
	r.registerOperationsRoutes(api)
	r.registerNetworkRoutes(api)

	r.mux.Handle("/api/v1/", middleware.SecurityHeaders(
		middleware.CORS(middleware.ContentTypeJSON(api)),
	))
}

func (r *Router) admin(next http.Handler) http.Handler {
	if r.security != nil {
		return r.sessionAdmin(next)
	}
	if !r.adminAuthEnabled {
		return next
	}
	return middleware.BearerAuth(r.adminToken)(next)
}

func (r *Router) agent(next http.Handler, resolver middleware.LegacyNodeNameResolver) http.Handler {
	if !r.agentAuthEnabled {
		return next
	}
	return middleware.AgentAuth(r.nodeStore, r.legacyAgentToken, resolver)(next)
}

func (r *Router) websocketAdmin(next http.Handler) http.Handler {
	if r.security != nil {
		return r.sessionWebSocketAdmin(next)
	}
	if !r.adminAuthEnabled {
		return next
	}
	return middleware.WebSocketBearerAuth(r.adminToken)(next)
}

func queryNodeName(parameter string) middleware.LegacyNodeNameResolver {
	return func(r *http.Request) (string, bool) {
		value := r.URL.Query().Get(parameter)
		return value, value != ""
	}
}

func bodyNodeName(field string) middleware.LegacyNodeNameResolver {
	return func(r *http.Request) (string, bool) {
		const maximumBody = 1 << 20
		content, err := io.ReadAll(io.LimitReader(r.Body, maximumBody+1))
		if err != nil || len(content) > maximumBody {
			return "", false
		}
		r.Body = io.NopCloser(bytes.NewReader(content))
		var values map[string]json.RawMessage
		if err := json.Unmarshal(content, &values); err != nil {
			return "", false
		}
		var name string
		if err := json.Unmarshal(values[field], &name); err != nil || name == "" {
			return "", false
		}
		return name, true
	}
}

func (r *Router) ServeHandler() http.Handler {
	return r.mux
}
