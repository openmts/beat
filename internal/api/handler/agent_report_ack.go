package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/beat/backend/internal/model"
)

func (h *NodeHandler) finishAgentACK(w http.ResponseWriter, status int, ack model.AgentReportACK) {
	if h.mtsStore != nil {
		h.mtsStore.ObserveAgentReport(ack)
	}
	message := "report accepted"
	if status >= http.StatusBadRequest {
		message = "report rejected"
	}
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(Response{Code: status, Message: message, Data: ack}); err != nil {
		slog.Warn("agent report acknowledgement failed")
	}
}
