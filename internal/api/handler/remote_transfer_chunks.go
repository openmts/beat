package handler

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/remoteoperations"
)

func readTransferChunk(r *http.Request) (remoteoperations.ChunkInput, error) {
	var input remoteoperations.ChunkInput
	kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || kind != "application/octet-stream" || r.Header.Get("Content-Encoding") != "" {
		return input, remoteoperations.ErrInvalid
	}
	input.Index, err = strconv.Atoi(r.Header.Get("X-Beat-Chunk-Index"))
	if err != nil || input.Index < 0 || input.Index >= 4096 || r.ContentLength < 1 || r.ContentLength > remoteoperations.SpoolChunkBytes {
		return input, remoteoperations.ErrInvalid
	}
	var start, end int64
	if _, err := fmt.Sscanf(r.Header.Get("Content-Range"), "bytes %d-%d/%d", &start, &end, &input.Total); err != nil {
		return input, remoteoperations.ErrInvalid
	}
	if start != int64(input.Index)*remoteoperations.SpoolChunkBytes || end-start+1 != r.ContentLength || end >= input.Total || input.Total > remoteoperations.MaximumFileBytes {
		return input, remoteoperations.ErrInvalid
	}
	if r.Header.Get("Content-Range") != fmt.Sprintf("bytes %d-%d/%d", start, end, input.Total) {
		return input, remoteoperations.ErrInvalid
	}
	input.Content, err = io.ReadAll(io.LimitReader(r.Body, remoteoperations.SpoolChunkBytes+1))
	if err != nil || int64(len(input.Content)) != r.ContentLength {
		clear(input.Content)
		return remoteoperations.ChunkInput{}, remoteoperations.ErrInvalid
	}
	input.Digest = r.Header.Get("X-Content-SHA256")
	return input, nil
}

type downloadAction struct {
	run     func(remoteoperations.Actor) error
	started *bool
}

func (handler *RemoteTransferHandler) adminDownload(w http.ResponseWriter, r *http.Request, action downloadAction) {
	operationHeaders(w)
	principal, ok := middleware.AdminPrincipal(r.Context())
	if !ok {
		JSONError(w, http.StatusUnauthorized, "administrator session is required")
		return
	}
	err := action.run(remoteoperations.Actor{UserID: principal.User.ID, SessionID: principal.Session.ID, RequestID: middleware.RequestID(r.Context()), IPAddress: middleware.ClientIP(r), UserAgent: r.UserAgent()})
	if err != nil && !*action.started {
		operationResponse(w, nil, err)
	}
}
