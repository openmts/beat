//go:build linux

package agentupdate

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPSBoundedRangeAndTrust(t *testing.T) {
	for _, mode := range []string{"partial", "whole", "redirect", "encoding", "length", "range", "status", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != artifactPathPrefix+"/assignment/linux" || r.URL.RawQuery != "" || r.Header.Get("X-Beat-Rollout-Nonce") != f.assignment.Nonce || r.Header.Get("Authorization") != "Bearer private-token" || r.Header.Get("Accept-Encoding") != "identity" {
					t.Error("incorrect bound request")
				}
				if r.Header.Get("Range") != "bytes=0-511" {
					t.Error("missing bounded range")
				}
				if mode == "redirect" {
					http.Redirect(w, r, "https://elsewhere.test", http.StatusFound)
					return
				}
				if mode == "status" {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				w.Header().Set("Content-Length", "512")
				w.Header().Set("Content-Range", "bytes 0-511/512")
				if mode == "length" {
					w.Header().Set("Content-Length", "511")
				}
				if mode == "range" {
					w.Header().Set("Content-Range", "bytes 0-511/1024")
				}
				if mode == "encoding" {
					w.Header().Set("Content-Encoding", "gzip")
				}
				if mode == "whole" {
					w.WriteHeader(http.StatusOK)
				} else {
					w.WriteHeader(http.StatusPartialContent)
				}
				_, err := w.Write(f.next)
				if err != nil && mode != "length" {
					t.Error(err)
				}
			}))
			defer server.Close()
			roots := x509.NewCertPool()
			roots.AddCert(server.Certificate())
			fetcher, err := NewHTTPFetcher(HTTPConfig{ServerURL: server.URL, AgentToken: "private-token", TLS: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}})
			must(t, err)
			defer fetcher.Close()
			if fetcher.TrustedServer() != server.URL {
				t.Fatal("origin changed")
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "canceled" {
				cancel()
			}
			response, err := fetcher.Fetch(ctx, FetchRequest{Assignment: f.assignment, Length: 512})
			if mode != "partial" && mode != "whole" {
				wantError(t, ErrDownload, err)
				return
			}
			must(t, err)
			body, err := io.ReadAll(response.Body)
			must(t, err)
			must(t, response.Body.Close())
			if string(body) != string(f.next) {
				t.Fatal("body changed")
			}
		})
	}
}

func TestHTTPConfigurationAndBounds(t *testing.T) {
	for _, config := range []HTTPConfig{
		{ServerURL: "http://beat.test", AgentToken: "x"},
		{ServerURL: "https://beat.test"},
		{ServerURL: "https://beat.test", AgentToken: "x\r\n"},
		{ServerURL: "https://beat.test", AgentToken: "x", PathPrefix: "/arbitrary"},
		{ServerURL: "https://beat.test", AgentToken: "x", TLS: &tls.Config{InsecureSkipVerify: true}},
		{ServerURL: "https://beat.test", AgentToken: "x", TLS: &tls.Config{ServerName: "elsewhere"}},
	} {
		_, e := NewHTTPFetcher(config)
		wantError(t, ErrConfiguration, e)
	}
	fetcher, e := NewHTTPFetcher(HTTPConfig{ServerURL: "https://beat.test", AgentToken: "x"})
	must(t, e)
	defer fetcher.Close()
	_, e = fetcher.Fetch(context.Background(), FetchRequest{})
	wantError(t, ErrAssignment, e)
	f := newFixture(t)
	for _, offset := range []int64{-1, 512, 513} {
		if validFetchRequest(FetchRequest{Assignment: f.assignment, Offset: offset, Length: 1}) {
			t.Fatal(offset)
		}
	}
	request := FetchRequest{Assignment: f.assignment, Offset: 1, Length: 10}
	response := &http.Response{StatusCode: 200, ContentLength: 10, Header: make(http.Header)}
	if validRangeResponse(response, request) {
		t.Fatal("accepted partial 200")
	}
	response.StatusCode = 206
	response.Header.Set("Content-Range", fmt.Sprintf("bytes 1-10/%d", f.assignment.Artifact.Size))
	if !validRangeResponse(response, request) {
		t.Fatal("valid range rejected")
	}
	fetcher.server.Host = "bad\n"
	_, e = fetcher.Fetch(context.Background(), FetchRequest{Assignment: f.assignment, Length: 512})
	wantError(t, ErrDownload, e)
}

type responseFetcher struct {
	response FetchResponse
	err      error
}

func (f responseFetcher) TrustedServer() string { return "https://beat.test" }

func (f responseFetcher) Fetch(context.Context, FetchRequest) (FetchResponse, error) {
	return f.response, f.err
}

func TestDownloadRejectsBodyFailures(t *testing.T) {
	f := newFixture(t)
	input := FetchRequest{Assignment: f.assignment, Length: 512}
	for _, mode := range []string{"error", "nil", "offset", "length", "total", "short", "long", "close", "read"} {
		t.Run(mode, func(t *testing.T) {
			response := FetchResponse{Body: io.NopCloser(strings.NewReader(string(f.next))), Length: 512, Total: 512}
			var fetchErr error
			switch mode {
			case "error":
				fetchErr = ErrDownload
			case "nil":
				response.Body = nil
			case "offset":
				response.Offset = 1
			case "length":
				response.Length = 1
			case "total":
				response.Total = 1
			case "short":
				response.Body = io.NopCloser(strings.NewReader("short"))
			case "long":
				response.Body = io.NopCloser(strings.NewReader(string(f.next) + "extra"))
			case "close":
				response.Body = &failedBody{Reader: strings.NewReader(string(f.next))}
			case "read":
				response.Body = io.NopCloser(failedReader{})
			}
			f.u.config.Fetcher = responseFetcher{response: response, err: fetchErr}
			if e := f.u.downloadChunk(context.Background(), io.Discard, input); e == nil {
				t.Fatal("invalid response accepted")
			}
		})
	}
}

type failedBody struct{ io.Reader }

func (*failedBody) Close() error { return errInjected }

type failedReader struct{}

func (failedReader) Read([]byte) (int, error) { return 0, errInjected }
