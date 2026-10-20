//go:build linux

package agentoperations

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beat/backend/internal/remoteoperations/protocol"
)

type responseTransport func(*http.Request) (*http.Response, error)

func (transport responseTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

type brokenResponseReader struct{}

func (brokenResponseReader) Read([]byte) (int, error) {
	return 0, errors.New("untrusted secret response")
}

func TestHTTPRedirectsCannotForwardNodeCredentials(t *testing.T) {
	reached := false
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	client, err := NewClient(ClientConfig{ServerURL: source.URL, Token: "private-node-token"})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Start(t.Context(), testLease("true")); !errors.Is(err, ErrTransport) {
		t.Fatal("redirect accepted")
	}
	if reached {
		t.Fatal("node credentials followed redirect")
	}
}

func TestMalformedResultReceiptsAndUnreadableResponsesAreNotAcknowledged(t *testing.T) {
	for _, body := range []string{`{"code":200,"data":{"acknowledged":false}}`, `{"code":200,"extra":true,"data":{"acknowledged":true}}`, `{"code":200,"data":{"acknowledged":true}} {}`, strings.Repeat("x", maximumJournalBytes+1)} {
		if err := decodeFileReceipt(strings.NewReader(body)); err == nil {
			t.Fatal("invalid upload receipt accepted")
		}
	}
	if err := decodeFileReceipt(brokenResponseReader{}); err == nil || strings.Contains(err.Error(), "untrusted") {
		t.Fatal("response error reflected private content")
	}
	client, err := NewClient(ClientConfig{ServerURL: "http://fixture.invalid", Token: "secret", Transport: responseTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(brokenResponseReader{})}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Start(t.Context(), testLease("true")); !errors.Is(err, ErrTransport) {
		t.Fatal("response read error ignored")
	}
	client.http.Transport = responseTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewBufferString(`{"code":200,"data":{"acknowledged":false}}`))}, nil
	})
	if err := client.Complete(t.Context(), protocol.Completion{Attempt: protocol.AttemptRequest{ID: "attempt", Nonce: strings.Repeat("0", 64)}}); !errors.Is(err, ErrTransport) {
		t.Fatal("unacknowledged completion deleted")
	}
}

func TestInvalidFileRequestsNeverReachTransport(t *testing.T) {
	client, err := NewClient(ClientConfig{ServerURL: "http://fixture.invalid", Token: "secret", Transport: responseTransport(func(*http.Request) (*http.Response, error) { t.Error("invalid request sent"); return nil, ErrTransport })})
	if err != nil {
		t.Fatal(err)
	}
	lease := testLease("true")
	if _, err := client.DownloadChunk(t.Context(), lease, 0); err == nil {
		t.Fatal("missing file accepted")
	}
	lease = fileLease("deploy", nil)
	if _, err := client.DownloadChunk(t.Context(), lease, -1); err == nil {
		t.Fatal("negative chunk accepted")
	}
	if err := client.UploadChunk(t.Context(), lease, FileChunk{Index: 0, Total: 3, Data: []byte("mismatch")}); err == nil {
		t.Fatal("inconsistent chunk length accepted")
	}
	lease.AttemptID = "../escape"
	if _, err := client.fileRequest(t.Context(), lease, http.MethodGet); err == nil {
		t.Fatal("unsafe attempt path accepted")
	}
	if _, err := client.DeclareSource(context.Background(), lease, protocol.SourceInfo{}); err == nil {
		t.Fatal("unsafe source attempt accepted")
	}
	for _, status := range []int{401, 403, 404, 409, 503} {
		if fileStatusError(status, http.Header{}) == nil {
			t.Fatal("failure status accepted")
		}
	}
	if !errors.Is(fileStatusError(409, http.Header{"Retry-After": []string{"1"}}), ErrTransport) {
		t.Fatal("temporary scan conflict became terminal")
	}
}
