package handler

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/beat/backend/internal/agentcredential"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

const defaultAgentReportInterval = "5s"

type managedNodeResponse struct {
	nodeResponse
	AgentCredentialStatus string     `json:"agent_credential_status"`
	AgentTokenPrefix      string     `json:"agent_token_prefix"`
	AgentTokenCreatedAt   *time.Time `json:"agent_token_created_at"`
	AgentTokenLastUsedAt  *time.Time `json:"agent_token_last_used_at"`
	AgentTokenRevokedAt   *time.Time `json:"agent_token_revoked_at"`
	PrivateRemark         string     `json:"private_remark"`
}

type agentConfigResponse struct {
	ServerURL      string `json:"server_url"`
	AgentToken     string `json:"agent_token,omitempty"`
	NodeName       string `json:"node_name"`
	AdvertisedHost string `json:"advertised_host"`
	SSHPort        int    `json:"ssh_port"`
	ReportInterval string `json:"report_interval"`
}

type nodeCredentialResponse struct {
	Node        managedNodeResponse `json:"node"`
	AgentToken  string              `json:"agent_token"`
	AgentConfig agentConfigResponse `json:"agent_config"`
}

type managedNodeRequest struct {
	Name           string `json:"name"`
	Alias          string `json:"alias"`
	GroupID        string `json:"group_id"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	SSHPublicKey   string `json:"ssh_public_key"`
	ServerURL      string `json:"server_url"`
	ReportInterval string `json:"report_interval"`
}

func (h *NodeHandler) HandleListManagedNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := h.nodeStore.ListNodes(r.Context(), "")
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to list managed nodes")
		return
	}
	output := make([]managedNodeResponse, 0, len(nodes))
	for _, node := range nodes {
		response, err := h.buildManagedNodeResponse(r, node)
		if err != nil {
			JSONError(w, http.StatusInternalServerError, "failed to load managed node")
			return
		}
		output = append(output, response)
	}
	JSONResponse(w, http.StatusOK, output)
}

func (h *NodeHandler) HandleCreateManagedNode(w http.ResponseWriter, r *http.Request) {
	var body managedNodeRequest
	if err := ParseJSON(r, &body); err != nil || !validManagedNodeRequest(body) {
		JSONError(w, http.StatusBadRequest, "invalid managed node")
		return
	}
	token, err := agentcredential.Generate()
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to generate agent token")
		return
	}
	now := model.NowUTC()
	node, err := h.nodeStore.CreateManagedNode(r.Context(), store.ManagedNodeInput{
		Name: body.Name, Alias: body.Alias, GroupID: body.GroupID, Host: body.Host,
		Port: body.Port, SSHPublicKey: body.SSHPublicKey,
	}, store.AgentCredential{Hash: token.Hash, Prefix: token.Prefix, CreatedAt: now})
	if errors.Is(err, store.ErrNodeNameConflict) {
		JSONError(w, http.StatusConflict, "node name already exists")
		return
	}
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to create managed node")
		return
	}
	h.writeNodeCredential(w, r, http.StatusCreated, *node, token.Plaintext, body)
}

func (h *NodeHandler) HandleRotateAgentToken(w http.ResponseWriter, r *http.Request) {
	node, err := h.nodeStore.GetNode(r.Context(), r.PathValue("id"))
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to read node")
		return
	}
	if node == nil {
		JSONError(w, http.StatusNotFound, "node not found")
		return
	}
	var body managedNodeRequest
	if err := ParseJSON(r, &body); err != nil || !validServerURL(body.ServerURL) {
		JSONError(w, http.StatusBadRequest, "invalid agent configuration")
		return
	}
	token, err := agentcredential.Generate()
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to generate agent token")
		return
	}
	now := model.NowUTC()
	node, err = h.nodeStore.RotateAgentToken(r.Context(), node.ID, store.AgentCredential{
		Hash: token.Hash, Prefix: token.Prefix, CreatedAt: now,
	})
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to rotate agent token")
		return
	}
	body.Name, body.Host, body.Port = node.Name, node.Host, node.Port
	h.writeNodeCredential(w, r, http.StatusOK, *node, token.Plaintext, body)
}

func (h *NodeHandler) HandleRevokeAgentToken(w http.ResponseWriter, r *http.Request) {
	node, err := h.nodeStore.RevokeAgentToken(r.Context(), r.PathValue("id"), model.NowUTC())
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to revoke agent token")
		return
	}
	if node == nil {
		JSONError(w, http.StatusNotFound, "node not found")
		return
	}
	response, err := h.buildManagedNodeResponse(r, *node)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to load managed node")
		return
	}
	JSONResponse(w, http.StatusOK, response)
}

func (h *NodeHandler) HandleAgentInstallConfig(w http.ResponseWriter, r *http.Request) {
	node, err := h.nodeStore.GetNode(r.Context(), r.PathValue("id"))
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to read node")
		return
	}
	if node == nil {
		JSONError(w, http.StatusNotFound, "node not found")
		return
	}
	serverURL := r.URL.Query().Get("server_url")
	if !validServerURL(serverURL) {
		JSONError(w, http.StatusBadRequest, "invalid server_url")
		return
	}
	JSONResponse(w, http.StatusOK, buildAgentConfig(*node, serverURL, "", defaultAgentReportInterval))
}

func (h *NodeHandler) writeNodeCredential(
	w http.ResponseWriter,
	r *http.Request,
	status int,
	node model.Node,
	plaintext string,
	body managedNodeRequest,
) {
	response, err := h.buildManagedNodeResponse(r, node)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to load managed node")
		return
	}
	interval := body.ReportInterval
	if interval == "" {
		interval = defaultAgentReportInterval
	}
	JSONResponse(w, status, nodeCredentialResponse{
		Node: response, AgentToken: plaintext,
		AgentConfig: buildAgentConfig(node, body.ServerURL, plaintext, interval),
	})
}

func (h *NodeHandler) buildManagedNodeResponse(
	r *http.Request,
	node model.Node,
) (managedNodeResponse, error) {
	base, err := buildNodeResponse(r.Context(), node, h.mtsStore)
	if err != nil {
		return managedNodeResponse{}, err
	}
	return managedNodeResponse{
		nodeResponse: base, AgentCredentialStatus: node.AgentCredentialStatus(),
		AgentTokenPrefix: node.AgentTokenPrefix, AgentTokenCreatedAt: node.AgentTokenCreatedAt,
		AgentTokenLastUsedAt: node.AgentTokenLastUsedAt, AgentTokenRevokedAt: node.AgentTokenRevokedAt,
		PrivateRemark: node.PrivateRemark,
	}, nil
}

func buildAgentConfig(node model.Node, serverURL, token, interval string) agentConfigResponse {
	return agentConfigResponse{
		ServerURL: serverURL, AgentToken: token, NodeName: node.Name,
		AdvertisedHost: node.Host, SSHPort: node.Port, ReportInterval: interval,
	}
}

func validManagedNodeRequest(body managedNodeRequest) bool {
	if strings.TrimSpace(body.Name) == "" || strings.TrimSpace(body.Host) == "" ||
		body.Port < 1 || body.Port > 65535 || !validServerURL(body.ServerURL) {
		return false
	}
	if body.ReportInterval == "" {
		return true
	}
	interval, err := time.ParseDuration(body.ReportInterval)
	return err == nil && interval >= time.Second
}

func validServerURL(value string) bool {
	parsed, err := url.ParseRequestURI(value)
	return err == nil && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https")
}
