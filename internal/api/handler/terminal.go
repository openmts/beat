package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/websocket"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/service"
)

const (
	maxBatchNodes   = 50
	maxCommandBytes = 4096
	batchTimeout    = 30 * time.Second
)

type TerminalOperations interface {
	OpenTerminal(context.Context, string, io.ReadWriteCloser) error
	ExecuteBatch(context.Context, []string, string) []service.BatchResult
}

type TerminalHandler struct {
	operations TerminalOperations
}

func NewTerminalHandler(operations TerminalOperations) *TerminalHandler {
	return &TerminalHandler{operations: operations}
}

func (h *TerminalHandler) HandleTerminalWS(w http.ResponseWriter, r *http.Request) {
	nodeID := strings.TrimSpace(r.URL.Query().Get("node_id"))
	if nodeID == "" {
		JSONError(w, http.StatusBadRequest, "node_id is required")
		return
	}
	if h.operations == nil {
		JSONError(w, http.StatusServiceUnavailable, "terminal service is unavailable")
		return
	}

	server := websocket.Server{
		Handshake: terminalHandshake,
		Handler: websocket.Handler(func(conn *websocket.Conn) {
			stop := context.AfterFunc(r.Context(), func() { _ = conn.Close() })
			defer stop()
			err := h.operations.OpenTerminal(r.Context(), nodeID, conn)
			if err != nil && r.Context().Err() == nil {
				_, writeErr := io.WriteString(conn, "\r\n[terminal error] "+err.Error()+"\r\n")
				if writeErr != nil {
					return
				}
			}
		}),
	}
	server.ServeHTTP(w, r)
}

func (h *TerminalHandler) HandleExecuteBatch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		NodeIDs []string `json:"node_ids"`
		Command string   `json:"command"`
	}
	if err := ParseJSONStrict(r, &body); err != nil {
		WriteBodyError(w, err)
		return
	}
	if len(body.NodeIDs) == 0 || len(body.NodeIDs) > maxBatchNodes {
		JSONError(w, http.StatusBadRequest, "node_ids must contain between 1 and 50 nodes")
		return
	}
	if strings.TrimSpace(body.Command) == "" || len(body.Command) > maxCommandBytes {
		JSONError(w, http.StatusBadRequest, "command must contain between 1 and 4096 bytes")
		return
	}
	if h.operations == nil {
		JSONError(w, http.StatusServiceUnavailable, "terminal service is unavailable")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), batchTimeout)
	defer cancel()
	results := h.operations.ExecuteBatch(ctx, body.NodeIDs, body.Command)
	JSONResponse(w, http.StatusOK, results)
}

func terminalHandshake(config *websocket.Config, request *http.Request) error {
	if protocol := middleware.SelectedWebSocketProtocol(request); protocol != "" {
		config.Protocol = []string{protocol}
	}

	origin := request.Header.Get("Origin")
	if origin == "" {
		return nil
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host != request.Host {
		return errors.New("websocket origin is not allowed")
	}
	config.Origin = parsed
	return nil
}
