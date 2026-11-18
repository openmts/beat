package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"beat/internal/domain"
	"beat/internal/platform/sqlite"
	beatv1 "beat/internal/protocol/beatv1"
	"beat/internal/version"

	"github.com/go-chi/chi/v5"
)

const (
	agentUpgradeMaxConcurrent = 10
	agentUpgradeStaleAfter    = 10 * time.Minute
)

func (s *Server) agentUpgradeOverview(ctx context.Context) (domain.AgentUpgradeOverview, error) {
	now := time.Now().UTC()
	agents, err := s.store.ListAgents(ctx, now)
	if err != nil {
		return domain.AgentUpgradeOverview{}, err
	}
	nodes, err := s.store.ListNodes(ctx, "", now)
	if err != nil {
		return domain.AgentUpgradeOverview{}, err
	}
	active, err := s.store.GetActiveAgentUpgradeBatch(ctx)
	if err != nil {
		return domain.AgentUpgradeOverview{}, err
	}
	recent, err := s.store.ListAgentUpgradeBatches(ctx, 8)
	if err != nil {
		return domain.AgentUpgradeOverview{}, err
	}
	snapshot := s.agentReleaseSnapshot(ctx)
	artifacts := snapshot.Artifacts
	artifactByArch := make(map[string]domain.AgentUpgradeArtifact, len(artifacts))
	for _, artifact := range artifacts {
		artifactByArch[artifact.Arch] = artifact
	}
	nodeByID := make(map[string]domain.Node, len(nodes))
	for _, node := range nodes {
		nodeByID[node.ID] = node
	}
	overview := domain.AgentUpgradeOverview{
		TargetVersion: snapshot.Version,
		ReleaseTag:    snapshot.ReleaseTag,
		ReleaseURL:    snapshot.ReleaseURL,
		Repository:    snapshot.Repository,
		Artifacts:     artifacts,
		TotalNodes:    len(agents),
		ActiveBatch:   active,
		RecentBatches: recent,
	}
	for _, agent := range agents {
		node, exists := nodeByID[agent.NodeID]
		if !exists {
			overview.Unsupported++
			continue
		}
		artifact, exists := artifactByArch[node.Arch]
		if !exists || !artifact.Available {
			overview.Unsupported++
			continue
		}
		if strings.TrimSpace(node.AgentVersion) == "" {
			overview.Unsupported++
			continue
		}
		if version.Compare(node.AgentVersion, snapshot.Version) >= 0 {
			overview.UpToDate++
			continue
		}
		overview.Upgradable++
		if node.Status == domain.NodeOffline {
			overview.Offline++
		} else {
			overview.Online++
		}
	}
	return overview, nil
}

func (s *Server) handleAgentUpgradeOverview(w http.ResponseWriter, r *http.Request) {
	overview, err := s.agentUpgradeOverview(r.Context())
	if err != nil {
		s.logger.Error("agent_upgrade_overview_failed", "error", err)
		writeError(w, http.StatusInternalServerError, "storage_error", "读取 Agent 升级状态失败")
		return
	}
	writeJSON(w, http.StatusOK, overview)
}

func (s *Server) handleCreateAgentUpgrade(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var input struct {
		IncludeOffline *bool `json:"includeOffline"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid_request", "升级请求格式无效")
		return
	}
	includeOffline := true
	if input.IncludeOffline != nil {
		includeOffline = *input.IncludeOffline
	}
	now := time.Now().UTC()
	agents, err := s.store.ListAgents(r.Context(), now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", "读取 Agent 列表失败")
		return
	}
	nodes, err := s.store.ListNodes(r.Context(), "", now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", "读取节点列表失败")
		return
	}
	snapshot := s.agentReleaseSnapshot(r.Context())
	artifactByArch := make(map[string]domain.AgentUpgradeArtifact)
	for _, artifact := range snapshot.Artifacts {
		artifactByArch[artifact.Arch] = artifact
	}
	nodeByID := make(map[string]domain.Node, len(nodes))
	for _, node := range nodes {
		nodeByID[node.ID] = node
	}
	items := make([]domain.AgentUpgradeItem, 0, len(agents))
	skipped := 0
	for _, agent := range agents {
		node, exists := nodeByID[agent.NodeID]
		if !exists || strings.TrimSpace(node.AgentVersion) == "" {
			skipped++
			continue
		}
		if version.Compare(node.AgentVersion, snapshot.Version) >= 0 {
			continue
		}
		artifact, exists := artifactByArch[node.Arch]
		if !exists || !artifact.Available {
			skipped++
			continue
		}
		if node.Status == domain.NodeOffline && !includeOffline {
			skipped++
			continue
		}
		items = append(items, domain.AgentUpgradeItem{
			AgentID: agent.ID, NodeID: node.ID, NodeName: node.Hostname, Arch: node.Arch,
			FromVersion: node.AgentVersion, ToVersion: snapshot.Version,
			ArtifactSHA256: artifact.SHA256, ArtifactSize: artifact.Size, ArtifactURL: artifact.DownloadURL,
		})
	}
	if len(items) == 0 {
		writeError(w, http.StatusConflict, "no_upgradable_agents", "当前没有需要升级的 Agent")
		return
	}
	batch, err := s.store.CreateAgentUpgradeBatch(r.Context(), snapshot.Version, items, now)
	if errors.Is(err, sqlite.ErrAgentUpgradeInProgress) {
		writeError(w, http.StatusConflict, "upgrade_in_progress", "已有 Agent 升级任务正在进行")
		return
	}
	if err != nil {
		s.logger.Error("create_agent_upgrade_failed", "error", err)
		writeError(w, http.StatusInternalServerError, "storage_error", "创建 Agent 升级任务失败")
		return
	}
	s.logger.Info("agent_upgrade_batch_created", "batch_id", batch.ID, "target_version", batch.TargetVersion, "total", batch.Total, "skipped", skipped)
	go s.dispatchAgentUpgrades(context.Background(), batch.ID)
	writeJSON(w, http.StatusAccepted, map[string]any{"batch": batch, "skipped": skipped})
}

func (s *Server) handleAgentUpgradeBatch(w http.ResponseWriter, r *http.Request) {
	batch, err := s.store.GetAgentUpgradeBatch(r.Context(), chi.URLParam(r, "id"))
	if errors.Is(err, sqlite.ErrAgentUpgradeNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Agent 升级任务不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", "读取 Agent 升级任务失败")
		return
	}
	writeJSON(w, http.StatusOK, batch)
}

func (s *Server) handleRetryAgentUpgrade(w http.ResponseWriter, r *http.Request) {
	batch, err := s.store.RetryAgentUpgradeBatch(r.Context(), chi.URLParam(r, "id"), time.Now().UTC())
	if errors.Is(err, sqlite.ErrAgentUpgradeNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Agent 升级任务不存在")
		return
	}
	if err != nil {
		s.logger.Error("retry_agent_upgrade_failed", "error", err)
		writeError(w, http.StatusInternalServerError, "storage_error", "重试 Agent 升级任务失败")
		return
	}
	go s.dispatchAgentUpgrades(context.Background(), batch.ID)
	writeJSON(w, http.StatusAccepted, batch)
}

func (s *Server) dispatchAgentUpgrades(ctx context.Context, batchID string) {
	s.upgradeDispatchMu.Lock()
	defer s.upgradeDispatchMu.Unlock()
	batch, err := s.store.GetAgentUpgradeBatch(ctx, batchID)
	if err != nil || batch.Status != "running" {
		return
	}
	capacity := agentUpgradeMaxConcurrent - batch.Delivered
	if capacity <= 0 {
		return
	}
	items, err := s.store.ListQueuedAgentUpgradeItems(ctx, batchID, "", 100)
	if err != nil {
		s.logger.Warn("list_agent_upgrade_items_failed", "batch_id", batchID, "error", err)
		return
	}
	dispatched := 0
	for _, item := range items {
		if dispatched >= capacity {
			return
		}
		session := s.agentSessions.get(item.AgentID)
		if session == nil {
			continue
		}
		delivered, err := s.store.MarkAgentUpgradeDelivered(ctx, item.ID, time.Now().UTC())
		if err != nil || !delivered {
			continue
		}
		if strings.TrimSpace(item.ArtifactURL) == "" {
			_, _ = s.store.UpdateAgentUpgradeProgress(ctx, item.AgentID, item.ID, "failed", "升级包未关联 GitHub Release 地址", time.Now().UTC())
			continue
		}
		if !session.enqueueAgentUpgrade(upgradeMessage(item)) {
			_, _ = s.store.UpdateAgentUpgradeProgress(ctx, item.AgentID, item.ID, "failed", "Agent 会话发送队列已满", time.Now().UTC())
			continue
		}
		dispatched++
		s.logger.Info("agent_upgrade_delivered", "batch_id", batchID, "item_id", item.ID, "agent_id", item.AgentID, "from_version", item.FromVersion, "target_version", item.ToVersion)
	}
}

func upgradeMessage(item domain.AgentUpgradeItem) *beatv1.AgentUpgrade {
	return &beatv1.AgentUpgrade{
		OperationId: item.ID, TargetVersion: item.ToVersion, Arch: item.Arch,
		DownloadUrl: item.ArtifactURL, Sha256: item.ArtifactSHA256,
		Size: item.ArtifactSize, TimeoutSeconds: 300,
	}
}

func (s *Server) reconcileAgentUpgrade(agentID, currentVersion string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	completed, err := s.store.ReconcileAgentUpgradeVersion(ctx, agentID, currentVersion, time.Now().UTC())
	if err != nil {
		s.logger.Warn("reconcile_agent_upgrade_failed", "agent_id", agentID, "error", err)
		return
	}
	if len(completed) > 0 {
		s.logger.Info("agent_upgrade_succeeded", "agent_id", agentID, "version", currentVersion, "items", len(completed))
	}
	if active, err := s.store.GetActiveAgentUpgradeBatch(ctx); err == nil && active != nil {
		go s.dispatchAgentUpgrades(context.Background(), active.ID)
	}
}

func (s *Server) runAgentUpgradeMaintenance(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now().UTC()
			affected, err := s.store.FailStaleAgentUpgradeItems(ctx, now.Add(-agentUpgradeStaleAfter), now)
			if err != nil {
				s.logger.Warn("agent_upgrade_stale_cleanup_failed", "error", err)
				continue
			}
			if affected > 0 {
				s.logger.Warn("agent_upgrade_items_timed_out", "count", affected)
			}
			if active, err := s.store.GetActiveAgentUpgradeBatch(ctx); err == nil && active != nil {
				s.dispatchAgentUpgrades(ctx, active.ID)
			}
		}
	}
}
