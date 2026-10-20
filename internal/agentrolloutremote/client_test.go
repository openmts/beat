package agentrolloutremote

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentrollout"
)

func TestListUsesBoundedPageAndMarksReleaseReadiness(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		if request.URL.Path != "/repos/openmts/beat/releases" || request.URL.Query().Get("per_page") != "20" || request.URL.Query().Get("page") != "1" {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		if request.Header.Get("Authorization") != "Bearer fixture-token" || request.Header.Get("X-GitHub-Api-Version") == "" {
			http.Error(w, "missing headers", http.StatusUnauthorized)
			return
		}
		items := make([]apiRelease, 0, pageSize)
		for index := range pageSize {
			arch := "amd64"
			if index%2 == 1 {
				arch = "arm64"
			}
			items = append(items, apiRelease{ID: int64(index + 1), TagName: "v1.0." + strconv.Itoa(index), Name: "Beat", PublishedAt: time.Unix(int64(index+1), 0), Assets: []apiAsset{
				{ID: 100, Name: ManifestAssetName, Size: 10}, {ID: 101, Name: SignatureAssetName, Size: 64},
				{ID: 102, Name: SigstoreAssetName, Size: 10}, {ID: 103, Name: "beat-agent_linux_" + arch, Size: 10},
			}})
		}
		_ = json.NewEncoder(w).Encode(items)
	}))
	defer server.Close()
	service, err := New(Config{Rollout: &agentrollout.Service{}, APIURL: server.URL, Token: "fixture-token"})
	if err != nil {
		t.Fatal(err)
	}
	page, err := service.List(context.Background(), 1)
	if err != nil || len(page.Items) != pageSize || page.NextPage == nil || *page.NextPage != 2 || calls.Load() != 1 {
		t.Fatalf("page = %+v, %v", page, err)
	}
	for _, item := range page.Items[:2] {
		if !item.Ready || item.Reason != "ready" || item.Version == "" {
			t.Fatalf("release summary = %+v", item)
		}
	}
	if _, err := service.List(context.Background(), 0); err != ErrInvalid || calls.Load() != 1 {
		t.Fatal("invalid page reached GitHub")
	}
}

func TestGitHubHTTPFailuresAreClassified(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Query().Get("case") {
		case "rate":
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusTooManyRequests)
		case "not-found":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusBadGateway)
		}
	}))
	defer server.Close()
	service, err := New(Config{Rollout: &agentrollout.Service{}, APIURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	base, _ := service.repositoryURL("releases")
	for testCase, want := range map[string]error{"rate": ErrRateLimited, "not-found": ErrNotFound, "down": ErrUnavailable} {
		target := *base
		target.RawQuery = "case=" + testCase
		var output any
		err := service.readAPI(context.Background(), &target, &output)
		if !errors.Is(err, want) {
			t.Fatalf("%s = %v", testCase, err)
		}
		if testCase == "rate" {
			limited, ok := err.(*RateLimitError)
			if !ok || limited.RetryAfter != 7*time.Second {
				t.Fatalf("rate limit = %#v", err)
			}
		}
	}
}

func TestReleaseSummariesAndTagsAreStrict(t *testing.T) {
	service := &Service{rollout: &agentrollout.Service{}}
	for tag, want := range map[string]bool{"v1.2.3": true, "1.2.3": false, "v1.2": false, "v1.2.3-rc.1": true} {
		_, valid := versionFromTag(tag)
		if valid != want {
			t.Fatalf("%s validity = %v", tag, valid)
		}
	}
	draft := service.summarizeRelease(apiRelease{TagName: "v1.2.3", Draft: true})
	missing := service.summarizeRelease(apiRelease{TagName: "v1.2.3"})
	if draft.Ready || draft.Reason != "invalid" || missing.Ready || missing.Reason != "missing_assets" {
		t.Fatalf("summaries: draft=%+v missing=%+v", draft, missing)
	}
}

func TestReadAPIRejectsTrailingJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("{} {}"))
	}))
	defer server.Close()
	service, err := New(Config{Rollout: &agentrollout.Service{}, APIURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	target, _ := service.repositoryURL("releases")
	var output map[string]any
	if err := service.readAPI(context.Background(), target, &output); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("trailing JSON = %v", err)
	}
}
