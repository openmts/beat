package agent

import (
	"net/http"
	"time"

	"github.com/beat/backend/internal/agenttransport"
)

func newAgentHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:       timeout,
		Transport:     agenttransport.NoProxyTransport(),
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
