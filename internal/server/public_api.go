package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"beat/internal/domain"

	"github.com/go-chi/chi/v5"
)

type PublicNode struct {
	ID                       string            `json:"id"`
	Hostname                 string            `json:"hostname"`
	GroupID                  string            `json:"groupId"`
	GroupName                string            `json:"groupName"`
	GroupColor               string            `json:"groupColor"`
	Address                  string            `json:"address"`
	OS                       string            `json:"os"`
	Kernel                   string            `json:"kernel"`
	Arch                     string            `json:"arch"`
	Status                   domain.NodeStatus `json:"status"`
	Labels                   map[string]string `json:"labels"`
	CPUCores                 int               `json:"cpuCores"`
	CPUModel                 string            `json:"cpuModel"`
	TotalMemoryBytes         uint64            `json:"totalMemoryBytes"`
	TotalDiskBytes           uint64            `json:"totalDiskBytes"`
	CollectorIntervalSeconds int               `json:"collectorIntervalSeconds"`
	FirstSeen                time.Time         `json:"firstSeen"`
	LastSeen                 time.Time         `json:"lastSeen"`
	Metrics                  domain.Metrics    `json:"metrics"`
}

type PublicGroup struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Color        string `json:"color"`
	NodeCount    int    `json:"nodeCount"`
	OnlineCount  int    `json:"onlineCount"`
	WarningCount int    `json:"warningCount"`
	OfflineCount int    `json:"offlineCount"`
}

func publicNode(node domain.Node) PublicNode {
	labels := node.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	return PublicNode{
		ID: node.ID, Hostname: node.Hostname, GroupID: node.GroupID, GroupName: node.GroupName,
		GroupColor: node.GroupColor, Address: node.Address, OS: node.OS, Kernel: node.Kernel,
		Arch: node.Arch, Status: node.Status, Labels: labels, CPUCores: node.CPUCores,
		CPUModel: node.CPUModel, TotalMemoryBytes: node.TotalMemoryBytes,
		TotalDiskBytes: node.TotalDiskBytes, CollectorIntervalSeconds: node.CollectorIntervalSeconds,
		FirstSeen: node.FirstSeen, LastSeen: node.LastSeen, Metrics: node.Metrics,
	}
}

func publicGroup(group domain.NodeGroup) PublicGroup {
	return PublicGroup{
		ID: group.ID, Name: group.Name, Description: group.Description, Color: group.Color,
		NodeCount: group.NodeCount, OnlineCount: group.OnlineCount,
		WarningCount: group.WarningCount, OfflineCount: group.OfflineCount,
	}
}

func (s *Server) publicNodeCounts(ctx context.Context) (map[string]int, error) {
	nodes, err := s.store.ListNodes(ctx, "", time.Now().UTC())
	if err != nil {
		return nil, err
	}
	counts := map[string]int{"all": len(nodes), "online": 0, "warning": 0, "offline": 0}
	for _, node := range nodes {
		counts[string(node.Status)]++
	}
	return counts, nil
}

func (s *Server) handlePublicBootstrap(w http.ResponseWriter, r *http.Request) {
	theme, err := s.publishedTheme(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "theme_error", "读取前台主题失败")
		return
	}
	counts, err := s.publicNodeCounts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", "读取节点统计失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"site":        s.currentSettings().Site,
		"theme":       theme.Document,
		"revision":    theme.Revision,
		"publishedAt": theme.PublishedAt,
		"counts":      counts,
		"generatedAt": time.Now().UTC(),
	})
}

func (s *Server) handlePublicNodes(w http.ResponseWriter, r *http.Request) {
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
	items := make([]PublicNode, 0, len(nodes))
	for _, node := range nodes {
		items = append(items, publicNode(node))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items), "counts": counts})
}

func (s *Server) handlePublicNode(w http.ResponseWriter, r *http.Request) {
	node, err := s.store.GetNode(r.Context(), chi.URLParam(r, "id"), time.Now().UTC())
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "节点不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", "读取节点失败")
		return
	}
	writeJSON(w, http.StatusOK, publicNode(node))
}

func (s *Server) handlePublicGroups(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	groups, err := s.store.ListGroups(r.Context(), now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", "读取节点分组失败")
		return
	}
	nodes, err := s.store.ListNodes(r.Context(), "", now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", "读取节点失败")
		return
	}
	ungrouped := 0
	for _, node := range nodes {
		if node.GroupID == "" {
			ungrouped++
		}
	}
	items := make([]PublicGroup, 0, len(groups))
	for _, group := range groups {
		items = append(items, publicGroup(group))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items), "ungroupedCount": ungrouped})
}
