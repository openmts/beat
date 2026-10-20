package handler

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/beat/backend/internal/remoteoperations"
)

const maximumArtifactDownloadDuration = 30 * time.Minute

type RemoteTransfers interface {
	CreateTransfer(context.Context, remoteoperations.Actor, remoteoperations.TransferInput) (remoteoperations.TransferSummary, error)
	ListTransfers(context.Context, remoteoperations.Actor) ([]remoteoperations.TransferSummary, error)
	GetTransfer(context.Context, remoteoperations.Actor, string) (remoteoperations.TransferSummary, error)
	UploadChunk(context.Context, remoteoperations.Actor, remoteoperations.ChunkInput) error
	FinalizeTransfer(context.Context, remoteoperations.Actor, string) (string, error)
	CancelTransfer(context.Context, remoteoperations.Actor, string) error
	DiscardTransferDraft(context.Context, remoteoperations.Actor, string) error
	DownloadTransfer(context.Context, remoteoperations.Actor, remoteoperations.DownloadInput) error
	CollectSource(context.Context, remoteoperations.AttemptRequest, remoteoperations.SourceInfo) error
	UploadAgentChunk(context.Context, remoteoperations.AgentChunk) error
	DownloadAgentChunk(context.Context, remoteoperations.AgentChunk) ([]byte, error)
	FinalizeCollect(context.Context, remoteoperations.AttemptRequest) error
	AgentFileReceipt(context.Context, remoteoperations.AttemptRequest) (remoteoperations.FileTransferReceipt, error)
	AgentFileInfo(context.Context, remoteoperations.AttemptRequest) (remoteoperations.SourceInfo, error)
}

type RemoteTransferHandler struct {
	RemoteOperationsHandler
	transfers RemoteTransfers
	slots     chan struct{}
	streams   chan struct{}
}

func NewRemoteTransferHandler(transfers RemoteTransfers) *RemoteTransferHandler {
	return &RemoteTransferHandler{transfers: transfers, slots: make(chan struct{}, 8), streams: make(chan struct{}, 2)}
}

func (handler *RemoteTransferHandler) HandleList(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor remoteoperations.Actor) (any, error) {
		return handler.transfers.ListTransfers(r.Context(), actor)
	})
}

func (handler *RemoteTransferHandler) HandleGet(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor remoteoperations.Actor) (any, error) {
		return handler.transfers.GetTransfer(r.Context(), actor, r.PathValue("id"))
	})
}

func (handler *RemoteTransferHandler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor remoteoperations.Actor) (any, error) {
		var input remoteoperations.TransferInput
		if err := decodeOperation(r, &input); err != nil {
			return nil, err
		}
		return handler.transfers.CreateTransfer(r.Context(), actor, input)
	})
}

func (handler *RemoteTransferHandler) HandleFinalize(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor remoteoperations.Actor) (any, error) {
		id, err := handler.transfers.FinalizeTransfer(r.Context(), actor, r.PathValue("id"))
		return map[string]string{"task_id": id}, err
	})
}

func (handler *RemoteTransferHandler) HandleCancel(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor remoteoperations.Actor) (any, error) {
		return remoteoperations.CompletionReceipt{Acknowledged: true}, handler.transfers.CancelTransfer(r.Context(), actor, r.PathValue("id"))
	})
}

// HandleDiscardDraft 删除一个从未 finalize 的上传草稿；被在途任务或扫描引用时回 409。
func (handler *RemoteTransferHandler) HandleDiscardDraft(w http.ResponseWriter, r *http.Request) {
	handler.admin(w, r, func(actor remoteoperations.Actor) (any, error) {
		return remoteoperations.CompletionReceipt{Acknowledged: true}, handler.transfers.DiscardTransferDraft(r.Context(), actor, r.PathValue("id"))
	})
}

func (handler *RemoteTransferHandler) HandleUpload(w http.ResponseWriter, r *http.Request) {
	if !handler.admitChunk(w) {
		return
	}
	defer handler.releaseChunk()
	handler.admin(w, r, func(actor remoteoperations.Actor) (any, error) {
		chunk, err := readTransferChunk(r)
		if err != nil {
			return nil, err
		}
		defer clear(chunk.Content)
		chunk.TransferID = r.PathValue("id")
		return remoteoperations.CompletionReceipt{Acknowledged: true}, handler.transfers.UploadChunk(r.Context(), actor, chunk)
	})
}

func (handler *RemoteTransferHandler) HandleDownload(w http.ResponseWriter, r *http.Request) {
	select {
	case handler.streams <- struct{}{}:
		defer func() { <-handler.streams }()
	default:
		operationHeaders(w)
		JSONError(w, http.StatusTooManyRequests, "download capacity is exhausted")
		return
	}
	started := false
	handler.adminDownload(w, r, downloadAction{started: &started, run: func(actor remoteoperations.Actor) error {
		ctx, cancel := context.WithTimeout(r.Context(), maximumArtifactDownloadDuration)
		defer cancel()
		if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(maximumArtifactDownloadDuration)); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return remoteoperations.ErrUnavailable
		}
		return handler.transfers.DownloadTransfer(ctx, actor, remoteoperations.DownloadInput{ID: r.PathValue("id"), Consume: func(info remoteoperations.DownloadInfo, reader io.Reader) error {
			operationHeaders(w)
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": info.Filename}))
			w.Header().Set("Content-Length", strconv.FormatInt(info.Size, 10))
			started = true
			_, err := io.Copy(w, reader)
			return err
		}})
	}})
}

func (handler *RemoteTransferHandler) admitChunk(w http.ResponseWriter) bool {
	select {
	case handler.slots <- struct{}{}:
		return true
	default:
		operationHeaders(w)
		JSONError(w, http.StatusTooManyRequests, "file transfer capacity is exhausted")
		return false
	}
}

func (handler *RemoteTransferHandler) releaseChunk() { <-handler.slots }
