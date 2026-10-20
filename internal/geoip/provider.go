package geoip

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const providerResponseLimit = 32 * 1024

const providerTimeout = 10 * time.Second

func providerClient() *http.Client {
	return &http.Client{Timeout: providerTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: &http.Transport{
		Proxy: nil, DialContext: (&net.Dialer{Timeout: providerTimeout}).DialContext, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout: providerTimeout, ResponseHeaderTimeout: providerTimeout, MaxIdleConns: 8, MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second,
	}}
}

func providerURL(provider, source, credential string) (string, error) {
	ip, scope := Classify(source)
	if scope != "public" {
		return "", ErrInvalid
	}
	escaped := url.PathEscape(ip.String())
	switch provider {
	case "geojs_https":
		return "https://get.geojs.io/v1/ip/country/" + escaped + ".json", nil
	case "ipinfo_https":
		value := "https://ipinfo.io/" + escaped + "/json"
		if credential != "" {
			value += "?token=" + url.QueryEscape(credential)
		}
		return value, nil
	case "ip_api_pro_https":
		if credential == "" {
			return "", ErrInvalid
		}
		return "https://pro.ip-api.com/json/" + escaped + "?fields=status,countryCode&key=" + url.QueryEscape(credential), nil
	}
	return "", ErrInvalid
}

type providerLookup struct{ provider, source, credential string }

func (s *Service) remoteLookup(ctx context.Context, input providerLookup) (string, error) {
	provider, source, credential := input.provider, input.source, input.credential
	endpoint, err := providerURL(provider, source, credential)
	if err != nil {
		return "", err
	}
	if err = s.waitRemote(ctx); err != nil {
		return "", ErrUnavailable
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", ErrUnavailable
	}
	request.Header.Set("Accept", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		return "", ErrUnavailable
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return "", ErrUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, providerResponseLimit+1))
	if err != nil || len(body) > providerResponseLimit {
		return "", ErrUnavailable
	}
	return decodeProvider(provider, body)
}

func decodeProvider(provider string, body []byte) (string, error) {
	var value struct {
		Country          string `json:"country"`
		CountryCode      string `json:"countryCode"`
		CountryCodeGeoJS string `json:"country_code"`
		Status           string `json:"status"`
		Bogon            bool   `json:"bogon"`
	}
	if err := json.Unmarshal(body, &value); err != nil {
		return "", ErrUnavailable
	}
	country := value.Country
	switch provider {
	case "geojs_https":
		country = value.CountryCodeGeoJS
		if country == "" {
			country = value.Country
		}
	case "ip_api_pro_https":
		if value.Status != "success" {
			return "", ErrUnavailable
		}
		country = value.CountryCode
	case "ipinfo_https":
		if value.Bogon {
			return "", ErrUnavailable
		}
	default:
		return "", ErrInvalid
	}
	country = strings.ToUpper(country)
	if !ValidCountry(country) {
		return "", ErrUnavailable
	}
	return country, nil
}

func (s *Service) waitRemote(ctx context.Context) error {
	s.rateMu.Lock()
	now := time.Now()
	when := s.nextRemote
	if when.Before(now) {
		when = now
	}
	// 当前仅一个全局provider；1次/秒同时满足每host1次及全局5次上限。
	s.nextRemote = when.Add(time.Second)
	s.rateMu.Unlock()
	timer := time.NewTimer(time.Until(when))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
