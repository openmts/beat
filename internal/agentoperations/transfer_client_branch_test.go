//go:build linux

package agentoperations

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/beat/backend/internal/remoteoperations/protocol"
)

func transferClient(t *testing.T, transport http.RoundTripper) *Client {
	t.Helper()
	client, err := NewClient(ClientConfig{ServerURL: "http://fixture.invalid", Token: "secret", Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func unusedTransport(t *testing.T) http.RoundTripper {
	t.Helper()
	return responseTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid request reached the transport")
		return nil, ErrTransport
	})
}

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestFileControlRejectsUnacknowledgedReceipt(t *testing.T) {
	client := transferClient(t, responseTransport(func(*http.Request) (*http.Response, error) {
		return jsonResponse(`{"code":200,"data":{"acknowledged":false}}`), nil
	}))
	lease := fileLease("collect", []byte("payload-16-bytes!"))
	if _, err := client.DeclareSource(t.Context(), lease, protocol.SourceInfo{Size: 16}); !errors.Is(err, ErrTransport) {
		t.Fatalf("unacknowledged source receipt accepted: %v", err)
	}
	if err := client.FinalizeFile(t.Context(), lease); !errors.Is(err, ErrTransport) {
		t.Fatalf("unacknowledged finalize accepted: %v", err)
	}
}

func TestFileTransferRejectsInvalidAttemptIdentity(t *testing.T) {
	client := transferClient(t, unusedTransport(t))
	invalid := fileLease("collect", []byte("payload-16-bytes!"))
	invalid.AttemptID = ""
	if _, err := client.DownloadChunk(t.Context(), invalid, 0); !errors.Is(err, ErrPolicy) {
		t.Fatalf("download error = %v", err)
	}
	chunk := FileChunk{Index: 0, Total: invalid.Payload.File.Size, Data: []byte("payload-16-bytes!")}
	if err := client.UploadChunk(t.Context(), invalid, chunk); !errors.Is(err, ErrPolicy) {
		t.Fatalf("upload error = %v", err)
	}
	if _, err := client.fileRequest(t.Context(), invalid, http.MethodGet); !errors.Is(err, ErrPolicy) {
		t.Fatalf("file request error = %v", err)
	}
}

func TestFileRequestRejectsInvalidHTTPMethod(t *testing.T) {
	client := transferClient(t, unusedTransport(t))
	lease := fileLease("collect", []byte("payload-16-bytes!"))
	if _, err := client.fileRequest(t.Context(), lease, "BAD METHOD"); !errors.Is(err, ErrTransport) {
		t.Fatalf("file request error = %v", err)
	}
}

func TestDownloadChunkRejectsShortBodyAndRetryableConflict(t *testing.T) {
	data := []byte("payload-16-bytes!")
	lease := fileLease("collect", data)
	length := len(data)
	short := transferClient(t, responseTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			ContentLength: int64(length),
			Header: http.Header{
				"Content-Range":    []string{contentRange(int64(length), 0, length)},
				"X-Content-SHA256": []string{strings.Repeat("0", 64)},
			},
			Body: io.NopCloser(bytes.NewReader(data[:length-4])),
		}, nil
	}))
	if _, err := short.DownloadChunk(t.Context(), lease, 0); !errors.Is(err, ErrFileIntegrity) {
		t.Fatalf("short body error = %v", err)
	}
	busy := transferClient(t, responseTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusConflict,
			Header:     http.Header{"Retry-After": []string{"1"}},
			Body:       io.NopCloser(strings.NewReader("busy")),
		}, nil
	}))
	if _, err := busy.DownloadChunk(t.Context(), lease, 0); !errors.Is(err, ErrTransport) {
		t.Fatalf("retryable conflict error = %v", err)
	}
}

func TestControlPlaneRejectsInvalidPolicyAndAttemptIdentity(t *testing.T) {
	client := transferClient(t, unusedTransport(t))
	if _, err := client.Lease(t.Context(), protocol.Policy{}); !errors.Is(err, ErrPolicy) {
		t.Fatalf("lease error = %v", err)
	}
	completion := protocol.Completion{Attempt: protocol.AttemptRequest{Nonce: strings.Repeat("ab", 32)}}
	if err := client.Complete(t.Context(), completion); !errors.Is(err, ErrTransport) {
		t.Fatalf("complete error = %v", err)
	}
}

func TestClientRequestRejectsUnparsableBaseAddress(t *testing.T) {
	client := &Client{base: "http://\x01", token: "secret", http: &http.Client{}}
	if err := client.Start(t.Context(), testLease("true")); !errors.Is(err, ErrTransport) {
		t.Fatalf("start error = %v", err)
	}
}

func TestValidAttemptIDEnforcesLengthBounds(t *testing.T) {
	if validAttemptID("") || validAttemptID(strings.Repeat("a", 129)) {
		t.Fatal("out-of-range attempt identity accepted")
	}
}
