package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/backup"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type fakeUploadOperations struct {
	*fakeBackupOperations
	started     backup.UploadSessionRequest
	chunk       backup.UploadChunkRequest
	chunkID     string
	cancelled   string
	finalized   string
	startErr    error
	chunkErr    error
	statusErr   error
	finalizeErr error
}

func (fake *fakeUploadOperations) StartUpload(
	_ context.Context,
	_ store.BackupUploadActor,
	request backup.UploadSessionRequest,
) (backup.UploadSessionStatus, error) {
	fake.started = request
	if fake.startErr != nil {
		return backup.UploadSessionStatus{}, fake.startErr
	}
	return backup.UploadSessionStatus{Session: model.BackupUploadSession{ID: "session-1",
		Filename: request.Filename, DeclaredBytes: request.SizeBytes, ChunkCount: 2}}, nil
}

func (fake *fakeUploadOperations) ListUploads(
	context.Context,
	store.BackupUploadActor,
) ([]model.BackupUploadSession, error) {
	if fake.statusErr != nil {
		return nil, fake.statusErr
	}
	return []model.BackupUploadSession{{ID: "session-1", State: model.BackupUploadStateOpen}}, nil
}

func (fake *fakeUploadOperations) UploadStatus(
	_ context.Context,
	_ store.BackupUploadActor,
	id string,
) (backup.UploadSessionStatus, error) {
	fake.chunkID = id
	if fake.statusErr != nil {
		return backup.UploadSessionStatus{}, fake.statusErr
	}
	return backup.UploadSessionStatus{Session: model.BackupUploadSession{ID: id},
		ReceivedIndexes: []int{1}}, nil
}

func (fake *fakeUploadOperations) UploadChunk(
	_ context.Context,
	_ store.BackupUploadActor,
	id string,
	request backup.UploadChunkRequest,
) (model.BackupUploadChunk, error) {
	fake.chunkID = id
	fake.chunk = request
	if fake.chunkErr != nil {
		return model.BackupUploadChunk{}, fake.chunkErr
	}
	return model.BackupUploadChunk{SessionID: id, Index: request.Index}, nil
}

func (fake *fakeUploadOperations) FinalizeUpload(
	_ context.Context,
	_ store.BackupUploadActor,
	id string,
) (model.BackupRecord, error) {
	fake.finalized = id
	if fake.finalizeErr != nil {
		return model.BackupRecord{}, fake.finalizeErr
	}
	return model.BackupRecord{ID: "backup-1", State: model.BackupStateValidated}, nil
}

func (fake *fakeUploadOperations) CancelUpload(
	_ context.Context,
	_ store.BackupUploadActor,
	id string,
) error {
	fake.cancelled = id
	return fake.finalizeErr
}

func newUploadHandlerFixture(t *testing.T) (*BackupUploadHandler, *fakeUploadOperations) {
	t.Helper()
	base, operations := newTestBackupHandler(t)
	upload := &fakeUploadOperations{fakeBackupOperations: operations}
	return NewBackupUploadHandler(upload, base.security), upload
}

func uploadRequest(method, target string, body string, recent bool) *http.Request {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if recent {
		request = request.WithContext(middleware.WithAdminPrincipal(request.Context(),
			*backupPrincipal(model.AdminRoleOwner, true)))
	} else {
		request = request.WithContext(middleware.WithAdminPrincipal(request.Context(),
			*backupPrincipal(model.AdminRoleOwner, false)))
	}
	return request
}

func TestBackupUploadHandlerStartAuthorizesAndValidates(t *testing.T) {
	handler, operations := newUploadHandlerFixture(t)
	request := uploadRequest(http.MethodPost, "/api/v1/admin/backups/uploads",
		`{"filename":"restore.zip","size_bytes":8,"digest":"`+strings.Repeat("a", 64)+`"}`, true)
	response := httptest.NewRecorder()
	handler.HandleStart(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("start status = %d body %s", response.Code, response.Body.String())
	}
	if operations.started.Filename != "restore.zip" || operations.started.SizeBytes != 8 {
		t.Fatalf("start request = %+v", operations.started)
	}
	if !strings.Contains(response.Body.String(), `"maximum_parallel_chunks"`) ||
		!strings.Contains(response.Body.String(), `"received_indexes"`) {
		t.Fatalf("start body = %s", response.Body.String())
	}
	stale := uploadRequest(http.MethodPost, "/api/v1/admin/backups/uploads", `{}`, false)
	response = httptest.NewRecorder()
	handler.HandleStart(response, stale)
	if response.Code != http.StatusPreconditionRequired {
		t.Fatalf("stale start status = %d", response.Code)
	}
	invalid := uploadRequest(http.MethodPost, "/api/v1/admin/backups/uploads", `{`, true)
	response = httptest.NewRecorder()
	handler.HandleStart(response, invalid)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid start status = %d", response.Code)
	}
	operations.startErr = backup.ErrUploadLimit
	response = httptest.NewRecorder()
	handler.HandleStart(response, uploadRequest(http.MethodPost, "/api/v1/admin/backups/uploads",
		`{"filename":"restore.zip","size_bytes":8,"digest":"x"}`, true))
	if response.Code != http.StatusConflict {
		t.Fatalf("limit start status = %d", response.Code)
	}
	operations.startErr = errors.New("boom")
	response = httptest.NewRecorder()
	handler.HandleStart(response, uploadRequest(http.MethodPost, "/api/v1/admin/backups/uploads",
		`{"filename":"restore.zip","size_bytes":8,"digest":"x"}`, true))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("failing start status = %d", response.Code)
	}
}

func TestBackupUploadHandlerListStatusAndCancel(t *testing.T) {
	handler, operations := newUploadHandlerFixture(t)
	response := httptest.NewRecorder()
	handler.HandleList(response, uploadRequest(http.MethodGet, "/api/v1/admin/backups/uploads", "", false))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "session-1") {
		t.Fatalf("list status = %d body %s", response.Code, response.Body.String())
	}
	status := uploadRequest(http.MethodGet, "/api/v1/admin/backups/uploads/session-9/status", "", false)
	status.SetPathValue("session", "session-9")
	response = httptest.NewRecorder()
	handler.HandleStatus(response, status)
	if response.Code != http.StatusOK || operations.chunkID != "session-9" ||
		!strings.Contains(response.Body.String(), `"received_indexes":[1]`) {
		t.Fatalf("status = %d body %s", response.Code, response.Body.String())
	}
	operations.statusErr = backup.ErrUploadNotFound
	response = httptest.NewRecorder()
	handler.HandleStatus(response, status)
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d", response.Code)
	}
	operations.statusErr = nil
	cancel := uploadRequest(http.MethodDelete, "/api/v1/admin/backups/uploads/session-9", "", false)
	cancel.SetPathValue("session", "session-9")
	response = httptest.NewRecorder()
	handler.HandleCancel(response, cancel)
	if response.Code != http.StatusOK || operations.cancelled != "session-9" {
		t.Fatalf("cancel = %d cancelled %q", response.Code, operations.cancelled)
	}
	operations.finalizeErr = backup.ErrUploadClosed
	response = httptest.NewRecorder()
	handler.HandleCancel(response, cancel)
	if response.Code != http.StatusConflict {
		t.Fatalf("closed cancel = %d", response.Code)
	}
	operations.finalizeErr = nil
	unavailable := uploadRequest(http.MethodGet, "/api/v1/admin/backups/uploads", "", false)
	anonymous := httptest.NewRequest(http.MethodGet, "/api/v1/admin/backups/uploads", nil)
	response = httptest.NewRecorder()
	handler.HandleStatus(response, anonymous)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d", response.Code)
	}
	response = httptest.NewRecorder()
	handler.HandleList(response, unavailable)
	if response.Code != http.StatusOK {
		t.Fatalf("owner list = %d", response.Code)
	}
}

func TestBackupUploadHandlerChunkParsesRangeAndMapsErrors(t *testing.T) {
	handler, operations := newUploadHandlerFixture(t)
	request := uploadRequest(http.MethodPut, "/api/v1/admin/backups/uploads/session-1/chunks/1", "data", false)
	request.SetPathValue("session", "session-1")
	request.SetPathValue("index", "1")
	request.Header.Set(chunkIndexHeader, "1")
	request.Header.Set(chunkRangeHeader, "bytes 1048576-2097151/4194304")
	request.Header.Set(chunkDigestHeader, strings.Repeat("b", 64))
	response := httptest.NewRecorder()
	handler.HandleChunk(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("chunk status = %d body %s", response.Code, response.Body.String())
	}
	if operations.chunk.Index != 1 || operations.chunk.Offset != 1048576 ||
		operations.chunk.SizeBytes != 1048576 || operations.chunk.Digest != strings.Repeat("b", 64) {
		t.Fatalf("chunk request = %+v", operations.chunk)
	}
	for name, headers := range map[string]map[string]string{
		"missing":   {},
		"bad range": {chunkIndexHeader: "1", chunkRangeHeader: "items 1-2/3"},
		"bad index": {chunkIndexHeader: "x", chunkRangeHeader: "bytes 0-1/2"},
		"bad end":   {chunkIndexHeader: "1", chunkRangeHeader: "bytes 0-z/2"},
	} {
		t.Run(name, func(t *testing.T) {
			broken := uploadRequest(http.MethodPut, "/x", "data", false)
			for key, value := range headers {
				broken.Header.Set(key, value)
			}
			response := httptest.NewRecorder()
			handler.HandleChunk(response, broken)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("broken chunk status = %d", response.Code)
			}
		})
	}
	for name, failure := range map[string]struct {
		err    error
		status int
	}{
		"digest":     {err: backup.ErrUploadChunkDigest, status: http.StatusUnprocessableEntity},
		"conflict":   {err: backup.ErrUploadChunkConflict, status: http.StatusConflict},
		"incomplete": {err: backup.ErrUploadIncomplete, status: http.StatusConflict},
		"not found":  {err: backup.ErrUploadNotFound, status: http.StatusNotFound},
		"unknown":    {err: errors.New("boom"), status: http.StatusBadRequest},
	} {
		t.Run(name, func(t *testing.T) {
			operations.chunkErr = failure.err
			response := httptest.NewRecorder()
			handler.HandleChunk(response, request)
			if response.Code != failure.status {
				t.Fatalf("chunk failure status = %d", response.Code)
			}
		})
	}
}

func TestBackupUploadHandlerFinalize(t *testing.T) {
	handler, operations := newUploadHandlerFixture(t)
	request := uploadRequest(http.MethodPost, "/api/v1/admin/backups/uploads/session-1/finalize", "", true)
	request.SetPathValue("session", "session-1")
	response := httptest.NewRecorder()
	handler.HandleFinalize(response, request)
	if response.Code != http.StatusCreated || operations.finalized != "session-1" {
		t.Fatalf("finalize = %d finalized %q", response.Code, operations.finalized)
	}
	operations.finalizeErr = backup.ErrUploadIncomplete
	response = httptest.NewRecorder()
	handler.HandleFinalize(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("incomplete finalize = %d", response.Code)
	}
	operations.finalizeErr = backup.ErrUploadUnavailable
	response = httptest.NewRecorder()
	handler.HandleFinalize(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable finalize = %d", response.Code)
	}
	operations.finalizeErr = backup.ErrUploadFilename
	response = httptest.NewRecorder()
	handler.HandleFinalize(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid finalize = %d", response.Code)
	}
}
