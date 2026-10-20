package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"mime"
	"net/http"
	"strconv"

	"github.com/beat/backend/internal/remoteoperations"
)

func (handler *RemoteTransferHandler) HandleAgentUpload(w http.ResponseWriter, r *http.Request) {
	if !handler.admitChunk(w) {
		return
	}
	defer handler.releaseChunk()
	kind, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if kind == "application/json" {
		handler.handleFileControl(w, r)
		return
	}
	chunk, err := readTransferChunk(r)
	if err != nil {
		operationResponse(w, nil, err)
		return
	}
	defer clear(chunk.Content)
	attempt := operationAttempt(r, r.Header.Get("X-Beat-Operation-Nonce"))
	err = handler.transfers.UploadAgentChunk(r.Context(), remoteoperations.AgentChunk{Attempt: attempt, Index: chunk.Index, Digest: chunk.Digest, Total: chunk.Total, Content: chunk.Content})
	if err != nil {
		operationResponse(w, nil, err)
		return
	}
	receipt, err := handler.transfers.AgentFileReceipt(r.Context(), attempt)
	operationResponse(w, receipt, err)
}

func (handler *RemoteTransferHandler) handleFileControl(w http.ResponseWriter, r *http.Request) {
	var input remoteoperations.FileControlInput
	if err := decodeOperation(r, &input); err != nil {
		operationResponse(w, nil, err)
		return
	}
	if (input.Source == nil) == !input.Finalize {
		operationResponse(w, nil, remoteoperations.ErrInvalid)
		return
	}
	attempt := operationAttempt(r, input.Nonce)
	var err error
	if input.Source != nil {
		err = handler.transfers.CollectSource(r.Context(), attempt, *input.Source)
	} else {
		err = handler.transfers.FinalizeCollect(r.Context(), attempt)
	}
	if err != nil {
		operationResponse(w, nil, err)
		return
	}
	receipt, err := handler.transfers.AgentFileReceipt(r.Context(), attempt)
	operationResponse(w, receipt, err)
}

func (handler *RemoteTransferHandler) HandleAgentDownload(w http.ResponseWriter, r *http.Request) {
	if !handler.admitChunk(w) {
		return
	}
	defer handler.releaseChunk()
	index, err := strconv.Atoi(r.Header.Get("X-Beat-Chunk-Index"))
	if err != nil || index < 0 || index >= 4096 {
		operationResponse(w, nil, remoteoperations.ErrInvalid)
		return
	}
	attempt := operationAttempt(r, r.Header.Get("X-Beat-Operation-Nonce"))
	info, err := handler.transfers.AgentFileInfo(r.Context(), attempt)
	if err != nil {
		operationResponse(w, nil, err)
		return
	}
	content, err := handler.transfers.DownloadAgentChunk(r.Context(), remoteoperations.AgentChunk{Attempt: attempt, Index: index})
	if err != nil {
		operationResponse(w, nil, err)
		return
	}
	defer clear(content)
	operationHeaders(w)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	start := int64(index) * remoteoperations.SpoolChunkBytes
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, start+int64(len(content))-1, info.Size))
	digest := sha256.Sum256(content)
	w.Header().Set("X-Content-SHA256", hex.EncodeToString(digest[:]))
	_, _ = w.Write(content)
}
