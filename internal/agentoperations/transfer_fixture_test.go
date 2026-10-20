//go:build linux

package agentoperations

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/beat/backend/internal/remoteoperations/protocol"
)

type fileServer struct {
	truncateDownload                              bool
	uploadFailure                                 int
	missingOverride                               []int
	mu                                            sync.Mutex
	lease                                         protocol.Lease
	data                                          []byte
	chunks                                        map[int][]byte
	source                                        *protocol.SourceInfo
	results                                       []protocol.Result
	started                                       bool
	starts, downloads, finalizes                  int
	startStatus, downloadFailure, finalizeFailure int
	badRange, badDigest, cancel                   bool
	onUpload                                      func(int)
	delayDownload                                 time.Duration
}

type fileFixture struct {
	runner *Runner
	config Config
	server *fileServer
	client *Client
	http   *httptest.Server
	root   string
}

func fileLease(kind string, data []byte) protocol.Lease {
	digest := sha256.Sum256(data)
	return protocol.Lease{AttemptID: "file-attempt", Nonce: strings.Repeat("ab", 32), ExpiresAt: time.Now().Add(time.Minute).UnixMilli(), Payload: protocol.TaskInput{Limits: protocol.Limits{Kind: kind, Targets: 1, TimeoutSeconds: 5, OutputBytes: 4096}, File: &protocol.FileSpec{RootID: "files", RelativePath: "payload.bin", Size: int64(len(data)), Digest: hex.EncodeToString(digest[:]), Mode: 0o600, ArtifactID: "artifact"}}}
}

func newFileFixture(t *testing.T, kind string, data []byte) *fileFixture {
	t.Helper()
	root := privateTestDir(t)
	server := &fileServer{lease: fileLease(kind, data), data: append([]byte(nil), data...), chunks: map[int][]byte{}}
	transport := httptest.NewServer(http.HandlerFunc(server.serve))
	t.Cleanup(transport.Close)
	client, err := NewClient(ClientConfig{ServerURL: transport.URL, Token: "fixture-node-token"})
	if err != nil {
		t.Fatal(err)
	}
	config := Config{Enabled: true, StateDir: filepath.Join(privateTestDir(t), "state"), Commands: CommandConfig{MaximumTimeout: "5s", MaximumOutputBytes: 4096}, FileRoots: []FileRoot{{ID: "files", Name: "Fixture files", Path: root, Read: true, Write: true, MaximumBytes: 32 << 20, Overwrite: true, Executable: true}}}
	runner, err := NewRunner(RunnerConfig{Local: config, Gateway: client})
	if err != nil {
		t.Fatal(err)
	}
	runner.heartbeat = 10 * time.Millisecond
	t.Cleanup(func() { _ = runner.Close() })
	return &fileFixture{runner: runner, config: config, server: server, client: client, http: transport, root: root}
}

func (server *fileServer) serve(response http.ResponseWriter, request *http.Request) {
	server.mu.Lock()
	defer server.mu.Unlock()
	if request.Header.Get("Authorization") != "Bearer fixture-node-token" || request.URL.RawQuery != "" {
		response.WriteHeader(401)
		return
	}
	switch request.URL.Path {
	case "/api/v1/agent-operations/lease":
		server.reply(response, server.lease)
	case "/api/v1/agent-operations/attempts/file-attempt/start":
		if !server.checkNonce(request) {
			response.WriteHeader(403)
			return
		}
		server.starts++
		if server.startStatus != 0 {
			response.WriteHeader(server.startStatus)
			return
		}
		server.started = true
		server.reply(response, protocol.StartAuthorization{Authorized: true})
	case "/api/v1/agent-operations/attempts/file-attempt/heartbeat":
		if !server.checkNonce(request) {
			response.WriteHeader(403)
			return
		}
		server.reply(response, protocol.HeartbeatResponse{CancelRequested: server.cancel})
	case "/api/v1/agent-operations/attempts/file-attempt/complete":
		server.complete(response, request)
	case "/api/v1/agent-operations/attempts/file-attempt/chunks":
		server.fileRequest(response, request)
	default:
		response.WriteHeader(404)
	}
}

func (server *fileServer) reply(response http.ResponseWriter, data any) {
	response.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(response).Encode(protocol.AgentResponse[any]{Code: 200, Message: "success", Data: data})
}

func (server *fileServer) checkNonce(request *http.Request) bool {
	var input protocol.AgentAttemptInput
	return json.NewDecoder(request.Body).Decode(&input) == nil && input.Nonce == server.lease.Nonce
}

func (server *fileServer) complete(response http.ResponseWriter, request *http.Request) {
	var input protocol.AgentCompletionInput
	if json.NewDecoder(request.Body).Decode(&input) != nil || input.Nonce != server.lease.Nonce || !server.started {
		response.WriteHeader(409)
		return
	}
	server.results = append(server.results, input.Result)
	server.reply(response, protocol.CompletionReceipt{Acknowledged: true})
}

func (server *fileServer) fileRequest(response http.ResponseWriter, request *http.Request) {
	if !server.started {
		response.WriteHeader(403)
		return
	}
	if request.Method == http.MethodGet {
		server.download(response, request)
		return
	}
	if request.Header.Get("Content-Type") == "application/octet-stream" {
		server.upload(response, request)
		return
	}
	var input protocol.FileControlInput
	if json.NewDecoder(request.Body).Decode(&input) != nil || input.Nonce != server.lease.Nonce {
		response.WriteHeader(403)
		return
	}
	if input.Source != nil {
		server.declare(response, *input.Source)
		return
	}
	if input.Finalize {
		server.finalize(response)
		return
	}
	response.WriteHeader(400)
}

func (server *fileServer) declare(response http.ResponseWriter, source protocol.SourceInfo) {
	if server.source != nil && *server.source != source {
		response.WriteHeader(409)
		return
	}
	server.source = &source
	missing := []int{}
	for index := range chunkCount(source.Size) {
		if server.chunks[index] == nil {
			missing = append(missing, index)
		}
	}
	if server.missingOverride != nil {
		missing = server.missingOverride
	}
	server.reply(response, protocol.FileTransferReceipt{Acknowledged: true, MissingChunks: missing})
}

func (server *fileServer) download(response http.ResponseWriter, request *http.Request) {
	if request.Header.Get("X-Beat-Operation-Nonce") != server.lease.Nonce {
		response.WriteHeader(403)
		return
	}
	index, err := strconv.Atoi(request.Header.Get("X-Beat-Chunk-Index"))
	if err != nil {
		response.WriteHeader(400)
		return
	}
	length, err := chunkLength(int64(len(server.data)), index)
	if err != nil {
		response.WriteHeader(400)
		return
	}
	server.downloads++
	if server.downloadFailure != 0 {
		status := server.downloadFailure
		server.downloadFailure = 0
		response.WriteHeader(status)
		return
	}
	if server.delayDownload > 0 {
		delay := server.delayDownload
		server.mu.Unlock()
		time.Sleep(delay)
		server.mu.Lock()
	}
	start := index * protocol.FileChunkBytes
	data := server.data[start : start+length]
	digest := sha256.Sum256(data)
	response.Header().Set("Content-Length", strconv.Itoa(length))
	response.Header().Set("Content-Range", contentRange(int64(len(server.data)), index, length))
	response.Header().Set("X-Content-SHA256", hex.EncodeToString(digest[:]))
	if server.badRange {
		response.Header().Set("Content-Range", "bytes 1-2/3")
	}
	if server.badDigest {
		response.Header().Set("X-Content-SHA256", strings.Repeat("0", 64))
	}
	if server.truncateDownload {
		server.truncateDownload = false
		_, _ = response.Write(data[:len(data)/2])
		return
	}
	_, _ = response.Write(data)
}

func (server *fileServer) upload(response http.ResponseWriter, request *http.Request) {
	index, err := strconv.Atoi(request.Header.Get("X-Beat-Chunk-Index"))
	if err != nil || server.source == nil {
		response.WriteHeader(400)
		return
	}
	content, err := io.ReadAll(io.LimitReader(request.Body, protocol.FileChunkBytes+1))
	if err != nil {
		response.WriteHeader(400)
		return
	}
	digest := sha256.Sum256(content)
	if request.Header.Get("X-Beat-Operation-Nonce") != server.lease.Nonce || request.ContentLength != int64(len(content)) || request.Header.Get("Content-Range") != contentRange(server.source.Size, index, len(content)) || request.Header.Get("X-Content-SHA256") != hex.EncodeToString(digest[:]) {
		response.WriteHeader(400)
		return
	}
	server.chunks[index] = content
	if server.uploadFailure != 0 {
		status := server.uploadFailure
		server.uploadFailure = 0
		response.WriteHeader(status)
		return
	}
	if server.onUpload != nil {
		server.onUpload(index)
	}
	server.reply(response, protocol.FileTransferReceipt{Acknowledged: true})
}

func (server *fileServer) finalize(response http.ResponseWriter) {
	server.finalizes++
	if server.finalizeFailure != 0 {
		status := server.finalizeFailure
		server.finalizeFailure = 0
		response.Header().Set("Retry-After", "1")
		response.WriteHeader(status)
		return
	}
	var content []byte
	for index := range chunkCount(server.source.Size) {
		content = append(content, server.chunks[index]...)
	}
	digest := sha256.Sum256(content)
	if int64(len(content)) != server.source.Size || hex.EncodeToString(digest[:]) != server.source.Digest {
		response.WriteHeader(409)
		return
	}
	server.reply(response, protocol.FileTransferReceipt{Acknowledged: true})
}

func writeFixturePayload(t *testing.T, fixture *fileFixture, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(fixture.root, "payload.bin"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}
