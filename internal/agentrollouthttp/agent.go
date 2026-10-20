package agentrollouthttp

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/beat/backend/internal/agentrollout"
	"github.com/beat/backend/internal/agentrollout/protocol"
	"github.com/beat/backend/internal/api/middleware"
)

var timeNow = time.Now

func (h *Handler) lease(w http.ResponseWriter, r *http.Request) {
	identity, ok := middleware.AgentIdentity(r.Context())
	if !ok {
		respond(w, nil, agentrollout.ErrForbidden)
		return
	}
	var caps protocol.Capabilities
	if err := decode(r, &caps); err != nil {
		respond(w, nil, err)
		return
	}
	value, err := h.service.Lease(r.Context(), identity, caps)
	respond(w, value, err)
}

func (h *Handler) observe(w http.ResponseWriter, r *http.Request) {
	identity, ok := middleware.AgentIdentity(r.Context())
	if !ok {
		respond(w, nil, agentrollout.ErrForbidden)
		return
	}
	var input protocol.Observation
	if err := decode(r, &input); err != nil {
		respond(w, nil, err)
		return
	}
	respond(w, nil, h.service.Observe(r.Context(), identity, input))
}

func (h *Handler) download(w http.ResponseWriter, r *http.Request) {
	headers(w)
	identity, ok := middleware.AgentIdentity(r.Context())
	if !ok {
		respond(w, nil, agentrollout.ErrForbidden)
		return
	}
	if r.Method != http.MethodGet || r.URL.RawQuery != "" || len(r.Header.Values("X-Beat-Rollout-Nonce")) != 1 || !singleRange(r.Header.Get("Range")) {
		respond(w, nil, agentrollout.ErrInvalid)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), transferTimeout)
	defer cancel()
	value, err := h.service.OpenDownload(ctx, agentrollout.DownloadRequest{Identity: identity, AssignmentID: r.PathValue("assignment_id"), ArtifactID: r.PathValue("artifact_id"), Nonce: r.Header.Get("X-Beat-Rollout-Nonce")})
	if err != nil {
		respond(w, nil, err)
		return
	}
	defer func() { _ = value.Close() }()
	deadline := timeNow().Add(transferTimeout)
	if value.ExpiresAt.Before(deadline) {
		deadline = value.ExpiresAt
	}
	if err := transferDeadline(w, deadline, false); err != nil {
		respond(w, nil, err)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Encoding", "identity")
	w.Header().Set("ETag", `"sha256-`+value.Artifact.SHA256+`"`)
	w.Header().Set("Content-Disposition", "attachment")
	http.ServeContent(noStoreWriter{w}, r, "artifact", time.Time{}, value)
}

func singleRange(value string) bool {
	if value == "" {
		return true
	}
	if len(value) > 128 || !strings.HasPrefix(value, "bytes=") || strings.Contains(value, ",") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(value, "bytes="), "-")
	if len(parts) != 2 {
		return false
	}
	for _, part := range parts {
		if !validRangeBound(part) {
			return false
		}
	}
	return parts[0] != "" || parts[1] != ""
}

type noStoreWriter struct{ http.ResponseWriter }

func (w noStoreWriter) WriteHeader(status int) {
	headers(w.ResponseWriter)
	w.ResponseWriter.WriteHeader(status)
}

func validRangeBound(part string) bool {
	if part == "" {
		return true
	}
	value, err := strconv.ParseInt(part, 10, 64)
	return err == nil && value >= 0
}
