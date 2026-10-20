package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/remoteoperations"
)

type transferStub struct {
	RemoteTransfers
	err     error
	content []byte
	delay   time.Duration
}

func (stub transferStub) CreateTransfer(context.Context, remoteoperations.Actor, remoteoperations.TransferInput) (remoteoperations.TransferSummary, error) {
	return remoteoperations.TransferSummary{ID: "transfer"}, stub.err
}

func (stub transferStub) ListTransfers(context.Context, remoteoperations.Actor) ([]remoteoperations.TransferSummary, error) {
	return []remoteoperations.TransferSummary{}, stub.err
}

func (stub transferStub) GetTransfer(context.Context, remoteoperations.Actor, string) (remoteoperations.TransferSummary, error) {
	return remoteoperations.TransferSummary{ID: "transfer"}, stub.err
}

func (stub transferStub) UploadChunk(context.Context, remoteoperations.Actor, remoteoperations.ChunkInput) error {
	return stub.err
}

func (stub transferStub) FinalizeTransfer(context.Context, remoteoperations.Actor, string) (string, error) {
	return "task", stub.err
}

func (stub transferStub) CancelTransfer(context.Context, remoteoperations.Actor, string) error {
	return stub.err
}

func (stub transferStub) DownloadTransfer(_ context.Context, _ remoteoperations.Actor, input remoteoperations.DownloadInput) error {
	if stub.err != nil {
		return stub.err
	}
	if stub.delay > 0 {
		time.Sleep(stub.delay)
	}
	return input.Consume(remoteoperations.DownloadInfo{Filename: "file.txt", Size: int64(len(stub.content))}, bytes.NewReader(stub.content))
}

func (stub transferStub) CollectSource(context.Context, remoteoperations.AttemptRequest, remoteoperations.SourceInfo) error {
	return stub.err
}

func (stub transferStub) UploadAgentChunk(context.Context, remoteoperations.AgentChunk) error {
	return stub.err
}

func (stub transferStub) DownloadAgentChunk(context.Context, remoteoperations.AgentChunk) ([]byte, error) {
	return bytes.Clone(stub.content), stub.err
}

func (stub transferStub) FinalizeCollect(context.Context, remoteoperations.AttemptRequest) error {
	return stub.err
}

func (stub transferStub) AgentFileReceipt(context.Context, remoteoperations.AttemptRequest) (remoteoperations.FileTransferReceipt, error) {
	return remoteoperations.FileTransferReceipt{Acknowledged: true, MissingChunks: []int{}}, stub.err
}

func (stub transferStub) AgentFileInfo(context.Context, remoteoperations.AttemptRequest) (remoteoperations.SourceInfo, error) {
	return remoteoperations.SourceInfo{Size: int64(len(stub.content))}, stub.err
}

func chunkRequest() *http.Request {
	r := operationRequest("bytes")
	r.Header.Set("Content-Type", "application/octet-stream")
	r.Header.Set("Content-Range", "bytes 0-4/5")
	r.Header.Set("X-Beat-Chunk-Index", "0")
	digest := sha256.Sum256([]byte("bytes"))
	r.Header.Set("X-Content-SHA256", hex.EncodeToString(digest[:]))
	r.Header.Set("X-Beat-Operation-Nonce", strings.Repeat("1", 64))
	return r
}

func TestRemoteTransferAdminRoutes(t *testing.T) {
	h := NewRemoteTransferHandler(transferStub{content: []byte("bytes")})
	for _, handle := range []http.HandlerFunc{h.HandleList, h.HandleGet, h.HandleCreate, h.HandleFinalize, h.HandleCancel} {
		w := httptest.NewRecorder()
		handle(w, operationRequest("{}"))
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	h.HandleUpload(w, chunkRequest())
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	h.HandleDownload(w, operationRequest(""))
	if w.Code != 200 || w.Body.String() != "bytes" || w.Header().Get("Content-Type") != "application/octet-stream" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	h.HandleDownload(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	h.HandleCreate(w, operationRequest("bad"))
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
}

func TestRemoteTransferAgentControlsAndBinaryHeaders(t *testing.T) {
	h := NewRemoteTransferHandler(transferStub{content: []byte("bytes")})
	for _, body := range []string{`{"nonce":"n","source":{"size":5,"digest":"d"}}`, `{"nonce":"n","finalize":true}`} {
		r := operationRequest(body)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.HandleAgentUpload(w, r)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	h.HandleAgentUpload(w, chunkRequest())
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	h.HandleAgentDownload(w, chunkRequest())
	if w.Code != 200 || w.Body.String() != "bytes" || w.Header().Get("Content-Range") != "bytes 0-4/5" || w.Header().Get("X-Content-SHA256") == "" {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, body := range []string{"bad", `{}`, `{"source":{},"finalize":true}`} {
		r := operationRequest(body)
		r.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		h.HandleAgentUpload(w, r)
		if w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
	h = NewRemoteTransferHandler(transferStub{err: remoteoperations.ErrBusy})
	r := operationRequest(`{"finalize":true}`)
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.HandleAgentUpload(w, r)
	if w.Code != 503 || w.Header().Get("Retry-After") != "1" {
		t.Fatal(w.Code)
	}
}

func TestRemoteTransferChunkBoundsAndCapacity(t *testing.T) {
	for _, change := range []func(*http.Request){func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, func(r *http.Request) { r.Header.Set("Content-Encoding", "gzip") },
		func(r *http.Request) { r.Header.Set("X-Beat-Chunk-Index", "bad") }, func(r *http.Request) { r.ContentLength = -1 }, func(r *http.Request) { r.Header.Set("Content-Range", "bad") },
		func(r *http.Request) { r.Header.Set("Content-Range", "bytes 1-5/6") }, func(r *http.Request) { r.Header.Set("Content-Range", "bytes 0-4/5 garbage") }, func(r *http.Request) { r.Body = io.NopCloser(strings.NewReader("shorter")) }} {
		r := chunkRequest()
		change(r)
		if _, err := readTransferChunk(r); err != remoteoperations.ErrInvalid {
			t.Fatal("invalid chunk accepted")
		}
	}
	h := NewRemoteTransferHandler(transferStub{})
	for range cap(h.slots) {
		h.slots <- struct{}{}
	}
	for _, handle := range []http.HandlerFunc{h.HandleUpload, h.HandleAgentUpload, h.HandleAgentDownload} {
		w := httptest.NewRecorder()
		handle(w, chunkRequest())
		if w.Code != 429 {
			t.Fatal(w.Code)
		}
	}
	for range cap(h.streams) {
		h.streams <- struct{}{}
	}
	w := httptest.NewRecorder()
	h.HandleDownload(w, operationRequest(""))
	if w.Code != 429 {
		t.Fatal(w.Code)
	}
}

type deadlineWriter struct {
	*httptest.ResponseRecorder
	deadline time.Time
	err      error
}

func (writer *deadlineWriter) SetWriteDeadline(value time.Time) error {
	writer.deadline = value
	return writer.err
}

func TestArtifactDownloadDeadlineIsBoundedAndErrorsAreHandled(t *testing.T) {
	h := NewRemoteTransferHandler(transferStub{content: []byte("bytes")})
	w := &deadlineWriter{ResponseRecorder: httptest.NewRecorder()}
	h.HandleDownload(w, operationRequest(""))
	if w.Code != 200 || time.Until(w.deadline) < 29*time.Minute || time.Until(w.deadline) > 31*time.Minute {
		t.Fatal(w.Code, w.deadline)
	}
	w = &deadlineWriter{ResponseRecorder: httptest.NewRecorder(), err: errors.New("fixture deadline failure")}
	h.HandleDownload(w, operationRequest(""))
	if w.Code != 503 {
		t.Fatal("deadline failure ignored")
	}
}

func TestArtifactDownloadOverridesShortServerWriteTimeout(t *testing.T) {
	h := NewRemoteTransferHandler(transferStub{content: []byte("bytes"), delay: 50 * time.Millisecond})
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandleDownload(w, r.WithContext(operationRequest("").Context()))
	}))
	server.Config.WriteTimeout = 10 * time.Millisecond
	server.Start()
	t.Cleanup(server.Close)
	response, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 200 || string(body) != "bytes" {
		t.Fatal(response.StatusCode, err)
	}
}
