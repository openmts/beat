package agentoperations

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/beat/backend/internal/remoteoperations/protocol"
)

func (client *Client) DeclareSource(ctx context.Context, lease protocol.Lease, source protocol.SourceInfo) (protocol.FileTransferReceipt, error) {
	return client.fileControl(ctx, lease, protocol.FileControlInput{Nonce: lease.Nonce, Source: &source})
}

func (client *Client) FinalizeFile(ctx context.Context, lease protocol.Lease) error {
	_, err := client.fileControl(ctx, lease, protocol.FileControlInput{Nonce: lease.Nonce, Finalize: true})
	return err
}

func (client *Client) fileControl(ctx context.Context, lease protocol.Lease, input protocol.FileControlInput) (protocol.FileTransferReceipt, error) {
	var response protocol.AgentResponse[protocol.FileTransferReceipt]
	err := client.attempt(ctx, lease, wireRequest{path: "chunks", input: input, output: &response})
	if err != nil {
		return response.Data, err
	}
	if !response.Data.Acknowledged {
		return response.Data, ErrTransport
	}
	return response.Data, nil
}

func (client *Client) DownloadChunk(ctx context.Context, lease protocol.Lease, index int) ([]byte, error) {
	if lease.Payload.File == nil {
		return nil, ErrPolicy
	}
	length, err := chunkLength(lease.Payload.File.Size, index)
	if err != nil {
		return nil, err
	}
	request, err := client.fileRequest(ctx, lease, http.MethodGet)
	if err != nil {
		return nil, err
	}
	request.Header.Set("X-Beat-Chunk-Index", strconv.Itoa(index))
	response, err := client.doFile(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.ContentLength != int64(length) || response.Header.Get("Content-Range") != contentRange(lease.Payload.File.Size, index, length) {
		return nil, ErrFileIntegrity
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, int64(length)+1))
	if err != nil {
		clear(content)
		return nil, ErrTransport
	}
	if len(content) != length {
		clear(content)
		return nil, ErrFileIntegrity
	}
	digest := sha256.Sum256(content)
	if hex.EncodeToString(digest[:]) != response.Header.Get("X-Content-SHA256") {
		clear(content)
		return nil, ErrFileIntegrity
	}
	return content, nil
}

func (client *Client) UploadChunk(ctx context.Context, lease protocol.Lease, chunk FileChunk) error {
	length, err := chunkLength(chunk.Total, chunk.Index)
	if err != nil || length != len(chunk.Data) {
		return ErrPolicy
	}
	request, err := client.fileRequest(ctx, lease, http.MethodPost)
	if err != nil {
		return err
	}
	request.Body = io.NopCloser(bytes.NewReader(chunk.Data))
	request.ContentLength = int64(length)
	request.Header.Set("Content-Type", "application/octet-stream")
	request.Header.Set("Content-Range", contentRange(chunk.Total, chunk.Index, length))
	request.Header.Set("X-Beat-Chunk-Index", strconv.Itoa(chunk.Index))
	digest := sha256.Sum256(chunk.Data)
	request.Header.Set("X-Content-SHA256", hex.EncodeToString(digest[:]))
	response, err := client.doFile(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	return decodeFileReceipt(response.Body)
}

func (client *Client) fileRequest(ctx context.Context, lease protocol.Lease, method string) (*http.Request, error) {
	if !validAttemptID(lease.AttemptID) || len(lease.Nonce) != 64 {
		return nil, ErrPolicy
	}
	request, err := http.NewRequestWithContext(ctx, method, client.base+"/api/v1/agent-operations/attempts/"+lease.AttemptID+"/chunks", nil)
	if err != nil {
		return nil, ErrTransport
	}
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("Authorization", "Bearer "+client.token)
	request.Header.Set("X-Beat-Operation-Nonce", lease.Nonce)
	return request, nil
}

func (client *Client) doFile(request *http.Request) (*http.Response, error) {
	bounded := *client.http
	bounded.Timeout = time.Minute
	response, err := bounded.Do(request)
	if err != nil {
		return nil, ErrTransport
	}
	if response.StatusCode == http.StatusOK {
		return response, nil
	}
	if response.StatusCode == http.StatusConflict && response.Header.Get("Retry-After") != "" {
		_ = response.Body.Close()
		return nil, ErrTransport
	}
	_ = response.Body.Close()
	if response.StatusCode == http.StatusConflict || response.StatusCode == http.StatusNotFound {
		return nil, ErrFileConflict
	}
	if response.StatusCode >= 400 && response.StatusCode < 500 {
		return nil, ErrPolicy
	}
	return nil, ErrTransport
}

func contentRange(total int64, index, length int) string {
	start := int64(index) * protocol.FileChunkBytes
	return fmt.Sprintf("bytes %d-%d/%d", start, start+int64(length)-1, total)
}
