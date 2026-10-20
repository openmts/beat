package agentrolloutremote

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/beat/backend/internal/agentrollout"
)

const (
	DefaultRepository = "openmts/beat"
	DefaultAPIURL     = "https://api.github.com"
)

var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

type Config struct {
	Rollout    *agentrollout.Service
	Repository string
	APIURL     string
	Token      string
	HTTPClient *http.Client
	Now        func() time.Time
}

type Service struct {
	rollout    *agentrollout.Service
	repository string
	api        *url.URL
	token      string
	client     *http.Client
	now        func() time.Time
	slots      chan struct{}
}

func New(config Config) (*Service, error) {
	if config.Rollout == nil {
		return nil, ErrInvalid
	}
	if config.Repository == "" {
		config.Repository = DefaultRepository
	}
	if !validRepository(config.Repository) {
		return nil, ErrInvalid
	}
	if config.APIURL == "" {
		config.APIURL = DefaultAPIURL
	}
	api, err := parseAPIURL(config.APIURL)
	if err != nil || invalidToken(config.Token) {
		return nil, ErrInvalid
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	client := &http.Client{Transport: http.DefaultTransport}
	if config.HTTPClient != nil {
		copy := *config.HTTPClient
		client = &copy
	}
	service := &Service{rollout: config.Rollout, repository: config.Repository, api: api, token: config.Token, client: client, now: config.Now, slots: make(chan struct{}, 2)}
	client.CheckRedirect = service.allowRedirect
	return service, nil
}

func parseAPIURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" && parsed.Path != "/" {
		return nil, ErrInvalid
	}
	if parsed.Scheme != "https" && (parsed.Scheme != "http" || !loopbackHost(parsed.Hostname())) {
		return nil, ErrInvalid
	}
	parsed.Path = ""
	return parsed, nil
}

func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

func validRepository(value string) bool {
	if !repositoryPattern.MatchString(value) {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "." || part == ".." {
			return false
		}
	}
	return true
}

func invalidToken(value string) bool {
	return len(value) > 4096 || strings.ContainsAny(value, "\x00\r\n")
}

func (s *Service) allowRedirect(request *http.Request, via []*http.Request) error {
	if s == nil || request == nil || len(via) > 4 || !s.safeDownloadURL(request.URL) {
		return ErrUnavailable
	}
	return nil
}

func (s *Service) safeDownloadURL(target *url.URL) bool {
	if target == nil || target.User != nil || target.Fragment != "" {
		return false
	}
	secure := target.Scheme == "https" || target.Scheme == "http" && loopbackHost(target.Hostname())
	if !secure {
		return false
	}
	host := strings.ToLower(target.Hostname())
	return host == strings.ToLower(s.api.Hostname()) || host == "github.com" || strings.HasSuffix(host, ".githubusercontent.com")
}

func (s *Service) apiURL(parts ...string) (*url.URL, error) {
	values := append([]string{s.api.Path}, parts...)
	joined, err := url.JoinPath(s.api.String(), values...)
	if err != nil {
		return nil, ErrUnavailable
	}
	parsed, err := url.Parse(joined)
	if err != nil {
		return nil, ErrUnavailable
	}
	return parsed, nil
}

func (s *Service) repositoryURL(parts ...string) (*url.URL, error) {
	repositoryParts := append([]string{"repos", path.Clean(s.repository)}, parts...)
	return s.apiURL(repositoryParts...)
}

func (s *Service) acquire(ctx context.Context) error {
	select {
	case s.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return errors.Join(ErrUnavailable, ctx.Err())
	}
}
