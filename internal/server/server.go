package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"beat/internal/auth"
	"beat/internal/config"
	"beat/internal/domain"
	"beat/internal/platform/sqlite"
	"beat/internal/platform/tsdb"
	beatv1 "beat/internal/protocol/beatv1"
	"beat/internal/version"
	"beat/web"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"gopkg.in/yaml.v3"
)

type Server struct {
	cfg               config.Config
	logger            *slog.Logger
	store             *sqlite.Store
	tsdb              tsdb.Backend
	router            http.Handler
	startedAt         time.Time
	http              *http.Server
	settingsMu        sync.RWMutex
	settings          SystemSettings
	settingsUpdatedAt time.Time
	authManager       *auth.Manager
	loginLimiter      *loginLimiter
	grpcServer        *grpc.Server
	agentSessions     *agentSessionRegistry
	upgradeDispatchMu sync.Mutex

	agentBinaryDir string
}

func New(ctx context.Context, cfg config.Config, logger *slog.Logger) (*Server, error) {
	store, err := sqlite.Open(cfg.DatabasePath)
	if err != nil {
		return nil, err
	}
	if err := store.Migrate(ctx); err != nil {
		_ = store.Close()
		return nil, err
	}
	authManager, err := auth.NewManager(ctx, store)
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("initialize admin authentication: %w", err)
	}
	defaults := defaultSystemSettings(cfg)
	systemSettings, settingsUpdatedAt, err := loadSystemSettings(ctx, store, defaults)
	if err != nil {
		logger.Warn("system_settings_load_failed", "error", err)
		systemSettings = defaults
		settingsUpdatedAt = time.Time{}
	}
	cfg.MTSDataPath = systemSettings.Storage.MTSDataPath
	cfg.MTSSync = systemSettings.Storage.WALSync
	cfg.Retention = time.Duration(systemSettings.Storage.RetentionDays) * 24 * time.Hour
	timeSeries, err := tsdb.OpenMTS(ctx, cfg.MTSDataPath, cfg.Retention, cfg.MTSSync, logger)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	s := &Server{
		cfg:               cfg,
		logger:            logger,
		store:             store,
		tsdb:              timeSeries,
		startedAt:         time.Now().UTC(),
		settings:          systemSettings,
		settingsUpdatedAt: settingsUpdatedAt,
		authManager:       authManager,
		loginLimiter:      newLoginLimiter(),
		agentSessions:     newAgentSessionRegistry(),
		agentBinaryDir:    resolveAgentBinaryDir(cfg.AgentBinaryDir),
	}
	s.grpcServer = grpc.NewServer(
		grpc.MaxRecvMsgSize(8<<20),
		grpc.MaxSendMsgSize(8<<20),
		grpc.KeepaliveParams(keepalive.ServerParameters{Time: 30 * time.Second, Timeout: 10 * time.Second}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{MinTime: 10 * time.Second, PermitWithoutStream: true}),
	)
	beatv1.RegisterAgentControlServer(s.grpcServer, &agentControlService{server: s})
	httpHandler := s.routes()
	s.router = h2c.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor == 2 && strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
			s.grpcServer.ServeHTTP(w, r)
			return
		}
		httpHandler.ServeHTTP(w, r)
	}), &http2.Server{})
	s.http = &http.Server{Addr: cfg.Listen, Handler: s.router, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 90 * time.Second}
	return s, nil
}

func (s *Server) Handler() http.Handler { return s.router }

func (s *Server) Run(ctx context.Context) error {
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.tsdb.Close(closeCtx); err != nil {
			s.logger.Warn("mts_close_failed", "error", err)
		}
		if err := s.store.Close(); err != nil {
			s.logger.Warn("sqlite_close_failed", "error", err)
		}
	}()
	if s.cfg.DemoMode {
		if err := s.seedDemo(ctx); err != nil {
			return err
		}
		go s.runDemoLoop(ctx)
	}
	go s.runMaintenance(ctx)
	go s.runAgentUpgradeMaintenance(ctx)
	go func() {
		<-ctx.Done()
		grpcStopped := make(chan struct{})
		go func() {
			s.grpcServer.GracefulStop()
			close(grpcStopped)
		}()
		select {
		case <-grpcStopped:
		case <-time.After(5 * time.Second):
			s.grpcServer.Stop()
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = s.http.Shutdown(shutdownCtx)
	}()
	s.logger.Info("beat_server_started", "version", version.Version, "listen", s.cfg.Listen, "demo", s.cfg.DemoMode, "mts", s.cfg.MTSDataPath, "agent_binary_dir", s.agentBinaryDir)
	var err error
	if s.cfg.TLSCertPath != "" {
		err = s.http.ListenAndServeTLS(s.cfg.TLSCertPath, s.cfg.TLSKeyPath)
	} else {
		err = s.http.ListenAndServe()
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(s.securityHeaders)
	r.Use(s.rejectCrossOriginMutation)
	r.Use(s.requestLogger)
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := s.store.Ping(r.Context()); err != nil {
			writeError(w, http.StatusServiceUnavailable, "not_ready", "关系型存储尚未就绪")
			return
		}
		stats, err := s.tsdb.Stats(r.Context())
		if err != nil || !stats.Ready {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"code":    "not_ready",
				"message": "时序存储尚未就绪",
				"storage": stats,
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ready", "driver": stats.Driver, "storage": stats})
	})
	r.Get("/downloads/beat-agent", s.handleAgentDownload)
	r.Get("/downloads/beat-agent/{arch}", s.handleAgentDownload)
	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/login", s.handleAuthLogin)
		r.Post("/auth/logout", s.handleAuthLogout)
		r.Get("/auth/me", s.handleAuthMe)
		r.Get("/meta", s.handleMeta)
		r.Route("/public", func(r chi.Router) {
			r.Get("/bootstrap", s.handlePublicBootstrap)
			r.Get("/theme", s.handlePublicTheme)
			r.Get("/nodes", s.handlePublicNodes)
			r.Get("/nodes/{id}", s.handlePublicNode)
			r.Get("/nodes/{id}/metrics", s.handleNodeMetrics)
			r.Get("/nodes/{id}/network-probes", s.handleNodeNetworkProbes)
			r.Get("/groups", s.handlePublicGroups)
		})
		r.Group(func(r chi.Router) {
			r.Use(s.requireAdmin)
			r.Use(s.auditAdminMutation)
			r.Put("/auth/credentials", s.handleUpdateCredentials)
			r.Get("/nodes", s.handleNodes)
			r.Get("/nodes/{id}", s.handleNode)
			r.Get("/nodes/{id}/metrics", s.handleNodeMetrics)
			r.Get("/nodes/{id}/network-probes", s.handleNodeNetworkProbes)
			r.Get("/groups", s.handleGroups)
			r.Get("/overview", s.handleOverview)
			r.Patch("/nodes/{id}", s.handleUpdateNode)
			r.Delete("/nodes/{id}", s.handleUninstallNode)
			r.Post("/groups", s.handleCreateGroup)
			r.Patch("/groups/{id}", s.handleUpdateGroup)
			r.Delete("/groups/{id}", s.handleDeleteGroup)
			r.Put("/groups/{id}/members", s.handleSetGroupMembers)
			r.Get("/agents", s.handleAgents)
			r.Post("/agent-tokens", s.handleCreateAgentToken)
			r.Get("/agent-upgrades/overview", s.handleAgentUpgradeOverview)
			r.Post("/agent-upgrades", s.handleCreateAgentUpgrade)
			r.Get("/agent-upgrades/{id}", s.handleAgentUpgradeBatch)
			r.Post("/agent-upgrades/{id}/retry", s.handleRetryAgentUpgrade)
			r.Get("/tasks", s.handleTasks)
			r.Post("/tasks", s.handleCreateTask)
			r.Patch("/tasks/{id}", s.handleUpdateTask)
			r.Delete("/tasks/{id}", s.handleDeleteTask)
			r.Get("/task-runs", s.handleTaskRuns)
			r.Get("/task-runs/{id}", s.handleTaskRun)
			r.Get("/settings", s.handleSettings)
			r.Put("/settings", s.handleUpdateSettings)
			r.Get("/admin/theme/draft", s.handleAdminThemeDraft)
			r.Put("/admin/theme/draft", s.handleUpdateThemeDraft)
			r.Post("/admin/theme/publish", s.handlePublishThemeDraft)
			r.Get("/admin/theme/revisions", s.handleThemeRevisions)
			r.Get("/admin/theme/catalog", s.handleThemeCatalog)
		})
	})
	r.NotFound(s.serveSPA)
	return r
}

func (s *Server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			s.logger.Info("http_request", "method", r.Method, "path", r.URL.Path, "status", ww.Status(), "duration_ms", time.Since(start).Milliseconds())
		}
	})
}

func (s *Server) serveSPA(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeError(w, http.StatusNotFound, "not_found", "接口不存在")
		return
	}
	file := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if file == "" || file == "." {
		file = "index.html"
	}
	root := web.FS()
	if _, err := fs.Stat(root, file); err != nil {
		file = "index.html"
	}
	data, err := fs.ReadFile(root, file)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	switch path.Ext(file) {
	case ".js":
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	case ".css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case ".svg":
		w.Header().Set("Content-Type", "image/svg+xml")
	case ".html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(data)
}

func (s *Server) authorizeAgent(ctx context.Context, token, agentID string, now time.Time) (bool, error) {
	if token == "" {
		return false, nil
	}
	if constantTimeEqual(token, s.cfg.AgentToken) {
		return true, nil
	}
	return s.store.AuthorizeEnrollmentToken(ctx, token, agentID, now)
}

func constantTimeEqual(left, right string) bool {
	if len(left) != len(right) || left == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

type OverviewSummary struct {
	NodeCount    int     `json:"nodeCount"`
	OnlineCount  int     `json:"onlineCount"`
	OfflineCount int     `json:"offlineCount"`
	WarningCount int     `json:"warningCount"`
	AvgCPU       float64 `json:"avgCpu"`
	AvgMemory    float64 `json:"avgMemory"`
	AvgDisk      float64 `json:"avgDisk"`
	NetworkRx    float64 `json:"networkRx"`
	NetworkTx    float64 `json:"networkTx"`
}

type OverviewResponse struct {
	GeneratedAt time.Time           `json:"generatedAt"`
	Range       string              `json:"range"`
	Summary     OverviewSummary     `json:"summary"`
	Fleet       []domain.FleetPoint `json:"fleet"`
	Nodes       []domain.Node       `json:"nodes"`
	TopCPU      []domain.Node       `json:"topCpu"`
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	window, step := parseRange(r.URL.Query().Get("range"), now)
	nodes, err := s.store.ListNodes(r.Context(), "", now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", "读取节点失败")
		return
	}
	allMetrics, err := s.tsdb.QueryAll(r.Context(), now.Add(-window), now)
	if err != nil {
		s.logger.Warn("query_fleet_metrics_failed", "error", err)
	}
	summary := summarize(nodes)
	resp := OverviewResponse{
		GeneratedAt: now,
		Range:       r.URL.Query().Get("range"),
		Summary:     summary,
		Fleet:       fleetSeries(allMetrics, window, step),
		Nodes:       nodes,
		TopCPU:      topCPU(nodes, 5),
	}
	if resp.Range == "" {
		resp.Range = "24h"
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleNodes(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	nodes, err := s.store.ListNodes(r.Context(), r.URL.Query().Get("q"), now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", "读取节点失败")
		return
	}
	counts := map[string]int{"all": len(nodes), "online": 0, "warning": 0, "offline": 0}
	for _, node := range nodes {
		counts[string(node.Status)]++
	}
	status := r.URL.Query().Get("status")
	if status != "" && status != "all" {
		filtered := make([]domain.Node, 0, len(nodes))
		for _, node := range nodes {
			if string(node.Status) == status {
				filtered = append(filtered, node)
			}
		}
		nodes = filtered
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": nodes, "total": len(nodes), "counts": counts})
}

func (s *Server) handleGroups(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	groups, err := s.store.ListGroups(r.Context(), now)
	if err != nil {
		s.logger.Error("list_node_groups_failed", "error", err)
		writeError(w, http.StatusInternalServerError, "storage_error", "读取节点分组失败")
		return
	}
	nodes, err := s.store.ListNodes(r.Context(), "", now)
	if err != nil {
		s.logger.Error("list_group_nodes_failed", "error", err)
		writeError(w, http.StatusInternalServerError, "storage_error", "读取节点失败")
		return
	}
	ungrouped := 0
	for _, node := range nodes {
		if node.GroupID == "" {
			ungrouped++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":          groups,
		"total":          len(groups),
		"ungroupedCount": ungrouped,
	})
}

type groupRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Color       string `json:"color"`
}

func validateGroupInput(input groupRequest) (groupRequest, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.Color = strings.TrimSpace(input.Color)
	if input.Name == "" {
		return groupRequest{}, errors.New("分组名称不能为空")
	}
	if len([]rune(input.Name)) > 48 {
		return groupRequest{}, errors.New("分组名称不能超过 48 个字符")
	}
	if len([]rune(input.Description)) > 200 {
		return groupRequest{}, errors.New("分组描述不能超过 200 个字符")
	}
	if input.Color == "" {
		input.Color = "blue"
	}
	switch input.Color {
	case "blue", "cyan", "violet", "emerald", "amber", "rose":
	default:
		return groupRequest{}, errors.New("不支持的分组主题色")
	}
	return input, nil
}

func (s *Server) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var input groupRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "分组配置格式无效")
		return
	}
	input, err := validateGroupInput(input)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	group, err := s.store.CreateGroup(r.Context(), uuid.NewString(), input.Name, input.Description, input.Color, time.Now().UTC())
	if errors.Is(err, sqlite.ErrGroupNameConflict) {
		writeError(w, http.StatusConflict, "group_name_conflict", "分组名称已存在")
		return
	}
	if err != nil {
		s.logger.Error("create_node_group_failed", "error", err)
		writeError(w, http.StatusInternalServerError, "storage_error", "分组创建失败")
		return
	}
	writeJSON(w, http.StatusCreated, group)
}

func (s *Server) handleUpdateGroup(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var input groupRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "分组配置格式无效")
		return
	}
	input, err := validateGroupInput(input)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	group, err := s.store.UpdateGroup(r.Context(), chi.URLParam(r, "id"), input.Name, input.Description, input.Color, time.Now().UTC())
	if errors.Is(err, sqlite.ErrGroupNotFound) {
		writeError(w, http.StatusNotFound, "group_not_found", "分组不存在")
		return
	}
	if errors.Is(err, sqlite.ErrGroupNameConflict) {
		writeError(w, http.StatusConflict, "group_name_conflict", "分组名称已存在")
		return
	}
	if err != nil {
		s.logger.Error("update_node_group_failed", "error", err)
		writeError(w, http.StatusInternalServerError, "storage_error", "分组保存失败")
		return
	}
	writeJSON(w, http.StatusOK, group)
}

func (s *Server) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	group, err := s.store.DeleteGroup(r.Context(), chi.URLParam(r, "id"), time.Now().UTC())
	if errors.Is(err, sqlite.ErrGroupNotFound) {
		writeError(w, http.StatusNotFound, "group_not_found", "分组不存在")
		return
	}
	if err != nil {
		s.logger.Error("delete_node_group_failed", "error", err)
		writeError(w, http.StatusInternalServerError, "storage_error", "分组删除失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "group": group})
}

type setGroupMembersRequest struct {
	NodeIDs []string `json:"nodeIds"`
}

func (s *Server) handleSetGroupMembers(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var input setGroupMembersRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "分组成员格式无效")
		return
	}
	if len(input.NodeIDs) > 5000 {
		writeError(w, http.StatusBadRequest, "invalid_request", "单个分组不能超过 5000 个节点")
		return
	}
	groupID := chi.URLParam(r, "id")
	if err := s.store.SetGroupMembers(r.Context(), groupID, input.NodeIDs, time.Now().UTC()); errors.Is(err, sqlite.ErrGroupNotFound) {
		writeError(w, http.StatusNotFound, "group_not_found", "分组不存在")
		return
	} else if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusBadRequest, "node_not_found", "分组中包含不存在的节点")
		return
	} else if err != nil {
		s.logger.Error("set_node_group_members_failed", "error", err, "group_id", groupID)
		writeError(w, http.StatusInternalServerError, "storage_error", "分组成员保存失败")
		return
	}
	group, err := s.store.GetGroup(r.Context(), groupID, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", "读取分组失败")
		return
	}
	writeJSON(w, http.StatusOK, group)
}

func (s *Server) handleNode(w http.ResponseWriter, r *http.Request) {
	node, err := s.store.GetNode(r.Context(), chi.URLParam(r, "id"), time.Now().UTC())
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "节点不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", "读取节点失败")
		return
	}
	writeJSON(w, http.StatusOK, node)
}

type updateNodeRequest struct {
	Hostname string            `json:"hostname"`
	Labels   map[string]string `json:"labels"`
	GroupID  string            `json:"groupId"`
}

func (s *Server) handleUpdateNode(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var input updateNodeRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "节点配置格式无效")
		return
	}
	hostname := cleanCommandValue(input.Hostname, "", 128)
	if hostname == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "节点名称不能为空")
		return
	}
	labels, err := validateNodeLabels(input.Labels)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	node, err := s.store.UpdateNodeConfig(r.Context(), chi.URLParam(r, "id"), hostname, labels, strings.TrimSpace(input.GroupID), time.Now().UTC())
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "节点不存在")
		return
	}
	if errors.Is(err, sqlite.ErrGroupNotFound) {
		writeError(w, http.StatusBadRequest, "group_not_found", "节点分组不存在")
		return
	}
	if err != nil {
		s.logger.Error("update_node_config_failed", "error", err, "node_id", chi.URLParam(r, "id"))
		writeError(w, http.StatusInternalServerError, "storage_error", "节点配置保存失败")
		return
	}
	writeJSON(w, http.StatusOK, node)
}

func (s *Server) handleUninstallNode(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "id")
	node, task, err := s.store.UninstallNode(r.Context(), nodeID, time.Now().UTC())
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "节点不存在")
		return
	}
	if err != nil {
		s.logger.Error("uninstall_node_failed", "error", err, "node_id", nodeID)
		writeError(w, http.StatusInternalServerError, "storage_error", "节点卸载失败")
		return
	}
	metricsDeleted := true
	if err := s.tsdb.DeleteNode(r.Context(), node.ID); err != nil {
		metricsDeleted = false
		s.logger.Warn("delete_node_metrics_failed", "error", err, "node_id", node.ID)
	}
	pushCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	if err := s.pushPendingAgentTasks(pushCtx, node.AgentID); err != nil {
		s.logger.Warn("push_uninstall_task_failed", "agent_id", node.AgentID, "task_id", task.ID, "error", err)
	}
	cancel()
	s.logger.Info("node_uninstall_queued", "node_id", node.ID, "agent_id", node.AgentID, "task_id", task.ID)
	writeJSON(w, http.StatusAccepted, map[string]any{
		"deleted":        true,
		"nodeId":         node.ID,
		"hostname":       node.Hostname,
		"agentId":        node.AgentID,
		"task":           task,
		"metricsDeleted": metricsDeleted,
	})
}

func validateNodeLabels(labels map[string]string) (map[string]string, error) {
	if len(labels) > 32 {
		return nil, errors.New("节点标签不能超过 32 个")
	}
	result := make(map[string]string, len(labels))
	for rawKey, rawValue := range labels {
		key := strings.TrimSpace(rawKey)
		value := strings.TrimSpace(rawValue)
		if key == "" {
			return nil, errors.New("节点标签键不能为空")
		}
		if len([]rune(key)) > 64 || len([]rune(value)) > 256 {
			return nil, errors.New("节点标签键或值过长")
		}
		result[key] = value
	}
	return result, nil
}

func (s *Server) handleNodeMetrics(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	window, step := parseRange(r.URL.Query().Get("range"), now)
	nodeID := chi.URLParam(r, "id")
	if _, err := s.store.GetNode(r.Context(), nodeID, now); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "节点不存在")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", "读取节点失败")
		return
	}
	points, err := s.tsdb.Query(r.Context(), nodeID, now.Add(-window), now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", "读取指标失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"nodeId":      nodeID,
		"from":        now.Add(-window),
		"to":          now,
		"stepSeconds": int(step.Seconds()),
		"points":      downsampleNode(points, step),
	})
}

func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	agents, err := s.store.ListAgents(r.Context(), time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", "读取代理失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":           agents,
		"total":           len(agents),
		"tokenTtlSeconds": int(s.cfg.EnrollmentTokenTTL.Seconds()),
		"architectures":   s.agentArchitectures(s.configuredServerURL(r)),
	})
}

type createAgentTokenRequest struct {
	Name   string `json:"name"`
	Labels string `json:"labels"`
}

func (s *Server) handleCreateAgentToken(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var input createAgentTokenRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "接入配置格式无效")
		return
	}
	name := cleanCommandValue(input.Name, "edge-node", 128)
	labels := cleanCommandValue(input.Labels, "env=production", 1024)
	token, err := newEnrollmentToken()
	if err != nil {
		s.logger.Error("generate_enrollment_token_failed", "error", err)
		writeError(w, http.StatusInternalServerError, "token_generation_failed", "无法生成接入令牌")
		return
	}
	now := time.Now().UTC()
	expiresAt := now.Add(s.cfg.EnrollmentTokenTTL)
	if err := s.store.CreateEnrollmentToken(r.Context(), uuid.NewString(), token, name, now, expiresAt); err != nil {
		s.logger.Error("store_enrollment_token_failed", "error", err)
		writeError(w, http.StatusInternalServerError, "storage_error", "接入令牌保存失败")
		return
	}
	serverURL := s.configuredServerURL(r)
	writeJSON(w, http.StatusCreated, map[string]any{
		"token":         token,
		"name":          name,
		"labels":        labels,
		"createdAt":     now,
		"expiresAt":     expiresAt,
		"ttlSeconds":    int(s.cfg.EnrollmentTokenTTL.Seconds()),
		"accessCommand": buildAgentAccessCommand(serverURL, token, name, labels),
	})
}

func (s *Server) handleAgentDownload(w http.ResponseWriter, r *http.Request) {
	arch := chi.URLParam(r, "arch")
	if arch == "" {
		arch = runtime.GOARCH
	}
	if !supportedAgentArch(arch) {
		writeError(w, http.StatusNotFound, "unsupported_architecture", "不支持的 Agent 架构")
		return
	}
	binaryPath := s.agentBinaryPathFor(arch)
	if binaryPath == "" {
		writeError(w, http.StatusServiceUnavailable, "agent_binary_unavailable", "当前架构的 Agent 二进制不可用")
		return
	}
	file, err := os.Open(binaryPath)
	if err != nil {
		s.logger.Warn("open_agent_binary_failed", "path", binaryPath, "error", err)
		writeError(w, http.StatusServiceUnavailable, "agent_binary_unavailable", "当前架构的 Agent 二进制不可用")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		writeError(w, http.StatusServiceUnavailable, "agent_binary_unavailable", "Agent 二进制不可用")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="beat-agent"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if checksum, checksumErr := fileSHA256(binaryPath); checksumErr == nil {
		w.Header().Set("X-Beat-SHA256", checksum)
		w.Header().Set("ETag", `"sha256-`+checksum+`"`)
		w.Header().Set("Digest", "sha-256="+base64.StdEncoding.EncodeToString(mustHexDecode(checksum)))
	} else {
		s.logger.Warn("agent_binary_checksum_failed", "path", binaryPath, "error", checksumErr)
	}
	http.ServeContent(w, r, "beat-agent", info.ModTime(), file)
}

func resolveAgentBinaryDir(configured string) string {
	if configured = strings.TrimSpace(configured); configured != "" {
		absolute, err := filepath.Abs(configured)
		if err == nil {
			return absolute
		}
		return configured
	}
	candidates := make([]string, 0, 3)
	if executable, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Dir(executable))
	}
	candidates = append(candidates, filepath.Join(".", "bin"), ".")
	for _, candidate := range candidates {
		absolute, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if hasAgentBinary(absolute) {
			return absolute
		}
	}
	if len(candidates) > 0 {
		if absolute, err := filepath.Abs(candidates[0]); err == nil {
			return absolute
		}
	}
	return ""
}

func hasAgentBinary(dir string) bool {
	for _, arch := range []string{"amd64", "arm64", runtime.GOARCH} {
		for _, name := range []string{"beat-agent-linux-" + arch, "beat-agent-" + arch} {
			if info, err := os.Stat(filepath.Join(dir, name)); err == nil && !info.IsDir() {
				return true
			}
		}
	}
	if info, err := os.Stat(filepath.Join(dir, "beat-agent")); err == nil && !info.IsDir() {
		return true
	}
	return false
}

func (s *Server) agentBinaryPathFor(arch string) string {
	if s.agentBinaryDir == "" {
		return ""
	}
	candidates := []string{
		filepath.Join(s.agentBinaryDir, "beat-agent-linux-"+arch),
		filepath.Join(s.agentBinaryDir, "beat-agent-"+arch),
	}
	if arch == runtime.GOARCH {
		candidates = append(candidates, filepath.Join(s.agentBinaryDir, "beat-agent"))
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

func supportedAgentArch(arch string) bool {
	return arch == "amd64" || arch == "arm64"
}

func (s *Server) agentArchitectures(serverURL string) []map[string]any {
	labels := map[string]string{
		"amd64": "x86_64 / AMD64",
		"arm64": "ARM64 / AArch64",
	}
	result := make([]map[string]any, 0, 2)
	for _, arch := range []string{"amd64", "arm64"} {
		downloadURL := strings.TrimRight(serverURL, "/") + "/downloads/beat-agent/" + arch
		checksum := ""
		if binaryPath := s.agentBinaryPathFor(arch); binaryPath != "" {
			checksum, _ = fileSHA256(binaryPath)
		}
		result = append(result, map[string]any{
			"id":              arch,
			"label":           labels[arch],
			"available":       checksum != "",
			"downloadUrl":     downloadURL,
			"checksumSha256":  checksum,
			"downloadCommand": buildAgentDownloadCommand(downloadURL, arch, checksum),
		})
	}
	return result
}

func newEnrollmentToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "beat_enroll_" + base64.RawURLEncoding.EncodeToString(buf), nil
}

func buildAgentDownloadCommand(downloadURL, arch, checksum string) string {
	verify := ""
	if checksum = strings.TrimSpace(checksum); checksum != "" {
		verify = fmt.Sprintf("\nprintf '%%s  %%s\\n' %s \"$tmp\" | sha256sum -c -", shellQuote(checksum))
	}
	return fmt.Sprintf(`set -euo pipefail
case "$(uname -m)" in
  x86_64) host_arch=amd64 ;;
  aarch64|arm64) host_arch=arm64 ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac
[ "$host_arch" = %s ] || { echo "installer target is %s, current host is $host_arch" >&2; exit 1; }
tmp=$(mktemp /tmp/beat-agent.XXXXXX)
trap 'rm -f "$tmp"' EXIT
curl -fsSL %s -o "$tmp"%s
chmod 0755 "$tmp"
sudo "$tmp" install
echo "Agent installed: $(/usr/local/bin/beat-agent --version)"
`, shellQuote(arch), shellQuote(arch), shellQuote(downloadURL), verify)
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func mustHexDecode(value string) []byte {
	decoded, _ := hex.DecodeString(value)
	return decoded
}

func buildAgentAccessCommand(serverURL, token, name, labels string) string {
	configuration, _ := yaml.Marshal(struct {
		ServerURL string            `yaml:"server"`
		Token     string            `yaml:"token"`
		Interval  string            `yaml:"interval"`
		Name      string            `yaml:"name"`
		Labels    map[string]string `yaml:"labels"`
		StateFile string            `yaml:"state_file"`
		DiskPath  string            `yaml:"disk_path"`
	}{
		ServerURL: serverURL,
		Token:     token,
		Interval:  "15s",
		Name:      name,
		Labels:    parseAgentLabelPairs(labels),
		StateFile: "/etc/beat/agent-id",
		DiskPath:  "/",
	})
	return fmt.Sprintf(`set -euo pipefail
sudo install -d -m 0750 /etc/beat
sudo install -m 0600 /dev/null /etc/beat/agent.yaml
sudo tee /etc/beat/agent.yaml >/dev/null <<'EOF'
%sEOF
sudo chmod 0600 /etc/beat/agent.yaml
sudo rm -f /etc/beat/beat-agent.env
sudo systemctl daemon-reload
sudo systemctl enable beat-agent
sudo systemctl restart beat-agent
sudo systemctl --no-pager --full status beat-agent | sed -n '1,12p' || true
`, configuration)
}

func parseAgentLabelPairs(raw string) map[string]string {
	labels := map[string]string{}
	for _, pair := range strings.Split(raw, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if ok && strings.TrimSpace(key) != "" {
			labels[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return labels
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func cleanCommandValue(value, fallback string, maxLength int) string {
	value = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' || r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, value))
	if value == "" {
		return fallback
	}
	runes := []rune(value)
	if len(runes) > maxLength {
		value = string(runes[:maxLength])
	}
	return value
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	s.writeSettings(w, r)
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var input struct {
		Settings SystemSettings `json:"settings"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_settings", "系统设置格式无效")
		return
	}
	settings := cleanSystemSettings(input.Settings)
	if err := validateSystemSettings(settings); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_settings", err.Error())
		return
	}
	now := time.Now().UTC()
	payload, err := json.Marshal(persistedSystemSettings{Settings: settings, UpdatedAt: now})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "settings_encode_failed", "系统设置序列化失败")
		return
	}
	if err := s.store.SetSetting(r.Context(), systemSettingsKey, string(payload)); err != nil {
		s.logger.Error("save_system_settings_failed", "error", err)
		writeError(w, http.StatusInternalServerError, "storage_error", "系统设置保存失败")
		return
	}
	s.settingsMu.Lock()
	s.settings = settings
	s.settingsUpdatedAt = now
	s.settingsMu.Unlock()
	s.logger.Info("system_settings_updated", "restart_required", s.mtsRestartRequired(), "mts_path", settings.Storage.MTSDataPath, "retention_days", settings.Storage.RetentionDays)
	s.writeSettings(w, r)
}

func (s *Server) writeSettings(w http.ResponseWriter, r *http.Request) {
	stats, err := s.tsdb.Stats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", "读取存储状态失败")
		return
	}
	settings, updatedAt := s.settingsSnapshot()
	var updatedAtValue any
	if !updatedAt.IsZero() {
		updatedAtValue = updatedAt
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"server": map[string]any{
			"version":       version.Version,
			"startedAt":     s.startedAt,
			"uptimeSeconds": int(time.Since(s.startedAt).Seconds()),
			"listen":        s.cfg.Listen,
			"demoMode":      s.cfg.DemoMode,
		},
		"storage": map[string]any{
			"sqlitePath":    s.cfg.DatabasePath,
			"dataDir":       s.cfg.DataDir,
			"mtsPath":       s.cfg.MTSDataPath,
			"retention":     s.cfg.Retention.String(),
			"retentionDays": int(s.cfg.Retention.Hours() / 24),
			"walSync":       s.cfg.MTSSync,
			"mts":           stats,
		},
		"configuration":   settings,
		"updatedAt":       updatedAtValue,
		"restartRequired": s.mtsRestartRequired(),
		"collector": map[string]any{
			"intervalSeconds":           15,
			"offlineAfterSeconds":       45,
			"defaultRange":              "24h",
			"enrollmentTokenTTLSeconds": int(s.cfg.EnrollmentTokenTTL.Seconds()),
		},
	})
}

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	nodes, _ := s.store.ListNodes(r.Context(), "", time.Now().UTC())
	settings := s.currentSettings()
	writeJSON(w, http.StatusOK, map[string]any{
		"version":                   version.Version,
		"serverTime":                time.Now().UTC(),
		"demoMode":                  s.cfg.DemoMode,
		"nodeCount":                 len(nodes),
		"site":                      settings.Site,
		"agentArchitectures":        s.agentArchitectures(s.configuredServerURL(r)),
		"enrollmentTokenTtlSeconds": int(s.cfg.EnrollmentTokenTTL.Seconds()),
	})
}

func parseRange(value string, now time.Time) (time.Duration, time.Duration) {
	switch value {
	case "1h":
		return time.Hour, time.Minute
	case "6h":
		return 6 * time.Hour, 5 * time.Minute
	case "7d":
		return 7 * 24 * time.Hour, time.Hour
	default:
		return 24 * time.Hour, 15 * time.Minute
	}
}

func summarize(nodes []domain.Node) OverviewSummary {
	summary := OverviewSummary{NodeCount: len(nodes)}
	var cpu, memory, disk, rx, tx float64
	for _, node := range nodes {
		switch node.Status {
		case domain.NodeOnline:
			summary.OnlineCount++
		case domain.NodeWarning:
			summary.WarningCount++
		case domain.NodeOffline:
			summary.OfflineCount++
		}
		if node.Status != domain.NodeOffline {
			cpu += node.Metrics.CPUUsage
			memory += node.Metrics.MemoryUsage
			disk += node.Metrics.DiskUsage
			rx += node.Metrics.NetworkRxBps
			tx += node.Metrics.NetworkTxBps
		}
	}
	online := summary.OnlineCount + summary.WarningCount
	if online > 0 {
		summary.AvgCPU = round(cpu/float64(online), 1)
		summary.AvgMemory = round(memory/float64(online), 1)
		summary.AvgDisk = round(disk/float64(online), 1)
	}
	summary.NetworkRx = round(rx, 0)
	summary.NetworkTx = round(tx, 0)
	return summary
}

func topCPU(nodes []domain.Node, limit int) []domain.Node {
	sorted := append([]domain.Node(nil), nodes...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Metrics.CPUUsage > sorted[j].Metrics.CPUUsage })
	if len(sorted) > limit {
		sorted = sorted[:limit]
	}
	return sorted
}

func fleetSeries(series map[string][]domain.Metrics, window, step time.Duration) []domain.FleetPoint {
	now := time.Now().UTC().Truncate(step)
	start := now.Add(-window)
	buckets := map[int64][]domain.Metrics{}
	for _, points := range series {
		for _, point := range points {
			bucket := point.Timestamp.Truncate(step).Unix()
			buckets[bucket] = append(buckets[bucket], point)
		}
	}
	result := make([]domain.FleetPoint, 0)
	for ts := start.Unix(); ts <= now.Unix(); ts += int64(step.Seconds()) {
		points := buckets[ts]
		if len(points) == 0 {
			continue
		}
		var cpu, memory, disk, rx, tx float64
		for _, point := range points {
			cpu += point.CPUUsage
			memory += point.MemoryUsage
			disk += point.DiskUsage
			rx += point.NetworkRxBps
			tx += point.NetworkTxBps
		}
		n := float64(len(points))
		result = append(result, domain.FleetPoint{
			Timestamp: time.Unix(ts, 0).UTC(), CPUUsage: round(cpu/n, 2),
			MemoryUsage: round(memory/n, 2), DiskUsage: round(disk/n, 2),
			NetworkRx: round(rx, 0), NetworkTx: round(tx, 0),
		})
	}
	return result
}

func downsampleNode(points []domain.Metrics, step time.Duration) []domain.SeriesPoint {
	if len(points) == 0 {
		return []domain.SeriesPoint{}
	}
	type aggregate struct {
		count int
		point domain.SeriesPoint
	}
	buckets := map[int64]*aggregate{}
	for _, point := range points {
		key := point.Timestamp.Truncate(step).Unix()
		bucket := buckets[key]
		if bucket == nil {
			bucket = &aggregate{point: domain.SeriesPoint{Timestamp: time.Unix(key, 0).UTC()}}
			buckets[key] = bucket
		}
		bucket.count++
		bucket.point.CPUUsage += point.CPUUsage
		bucket.point.CPUUser += point.CPUUser
		bucket.point.CPUSystem += point.CPUSystem
		bucket.point.MemoryUsage += point.MemoryUsage
		bucket.point.DiskUsage += point.DiskUsage
		bucket.point.NetworkRxBps += point.NetworkRxBps
		bucket.point.NetworkTxBps += point.NetworkTxBps
		bucket.point.Load1 += point.Load1
		bucket.point.Load5 += point.Load5
		bucket.point.Load15 += point.Load15
		bucket.point.Processes += float64(point.Processes)
		bucket.point.TCPConnections += float64(point.TCPConnections)
	}
	keys := make([]int64, 0, len(buckets))
	for key := range buckets {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	result := make([]domain.SeriesPoint, 0, len(keys))
	for _, key := range keys {
		bucket := buckets[key]
		n := float64(bucket.count)
		p := bucket.point
		p.CPUUsage = round(p.CPUUsage/n, 2)
		p.CPUUser = round(p.CPUUser/n, 2)
		p.CPUSystem = round(p.CPUSystem/n, 2)
		p.MemoryUsage = round(p.MemoryUsage/n, 2)
		p.DiskUsage = round(p.DiskUsage/n, 2)
		p.NetworkRxBps = round(p.NetworkRxBps/n, 0)
		p.NetworkTxBps = round(p.NetworkTxBps/n, 0)
		p.Load1 = round(p.Load1/n, 2)
		p.Load5 = round(p.Load5/n, 2)
		p.Load15 = round(p.Load15/n, 2)
		p.Processes = round(p.Processes/n, 0)
		p.TCPConnections = round(p.TCPConnections/n, 0)
		result = append(result, p)
	}
	return result
}

func publicServerURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := r.Header.Get("X-Forwarded-Proto"); forwarded != "" {
		scheme = forwarded
	}
	return scheme + "://" + r.Host
}

func round(value float64, digits int) float64 {
	factor := math.Pow10(digits)
	return math.Round(value*factor) / factor
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"code": code, "message": message})
}
