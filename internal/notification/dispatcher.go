package notification

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/beat/backend/internal/model"
)

type DispatcherConfig struct {
	Egress    *Egress
	TLSConfig *tls.Config
	Now       func() time.Time
}

type Dispatcher struct {
	egress *Egress
	tls    *tls.Config
	now    func() time.Time
}

func NewDispatcher(options ...DispatcherConfig) *Dispatcher {
	config := DispatcherConfig{}
	if len(options) > 0 {
		config = options[0]
	}
	if config.Egress == nil {
		config.Egress = NewEgress()
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.TLSConfig == nil {
		config.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	secureTLS := config.TLSConfig.Clone()
	if secureTLS.MinVersion < tls.VersionTLS12 {
		secureTLS.MinVersion = tls.VersionTLS12
	}
	return &Dispatcher{egress: config.Egress, tls: secureTLS, now: config.Now}
}

func (dispatcher *Dispatcher) Send(ctx context.Context, dispatch model.NotificationDispatch) model.NotificationSendResult {
	if dispatcher.tls.InsecureSkipVerify {
		return sendRejected("tls_policy_invalid", "validation")
	}
	if err := validateProviderFields(ConfigWrite{ChannelType: dispatch.ChannelType, Config: dispatch.Config}); err != nil {
		return sendRejected("config_invalid", "validation")
	}
	config, err := DecodeProvider(dispatch.Config)
	if err != nil {
		return sendRejected("config_invalid", "validation")
	}
	if err := validateProvider(dispatch.ChannelType, &config, true); err != nil {
		return sendRejected("config_invalid", "validation")
	}
	payload, err := renderPresentation(dispatch, config)
	if err != nil {
		return sendRejected("template_invalid", "render")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(config.TimeoutSeconds)*time.Second)
	defer cancel()
	if dispatch.ChannelType == TypeEmail {
		return dispatcher.sendSMTP(ctx, smtpDispatch{config: config, payload: payload, policy: dispatch.Policy})
	}
	request, err := buildProviderRequest(ctx, requestInput{dispatch: dispatch, config: config, payload: payload, now: dispatcher.now()})
	if err != nil {
		return sendRejected("template_invalid", "render")
	}
	if err := dispatcher.egress.Validate(ctx, request.URL.String(), dispatch.Policy); err != nil {
		return transportResult(err)
	}
	transport := dispatcher.transport(dispatch.Policy, request.URL.Scheme == "http")
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Duration(config.TimeoutSeconds) * time.Second, CheckRedirect: redirectPolicy(config.Redirects, request.URL)}
	response, err := client.Do(request)
	if err != nil {
		return transportResult(err)
	}
	defer func() { _ = response.Body.Close() }()
	body, readErr := io.ReadAll(io.LimitReader(response.Body, maximumDeliveryResponseBytes+1))
	if len(body) > maximumDeliveryResponseBytes {
		return sendRejected("response_too_large", "response")
	}
	return providerResponse(responseInput{response: response, body: body, readErr: readErr, kind: dispatch.ChannelType, compatibility: config.Compatibility, now: dispatcher.now()})
}

func (dispatcher *Dispatcher) transport(policy model.NotificationEgressPolicy, loopbackOnly bool) *http.Transport {
	return &http.Transport{Proxy: nil, DisableKeepAlives: true, DisableCompression: true, TLSClientConfig: dispatcher.tls.Clone(),
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return dispatcher.egress.DialChecked(ctx, DialTarget{Network: network, Address: address, Policy: policy, LoopbackOnly: loopbackOnly})
		},
		TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second, MaxResponseHeaderBytes: 64 * 1024}
}

type responseInput struct {
	response            *http.Response
	body                []byte
	readErr             error
	kind, compatibility string
	now                 time.Time
}

func providerResponse(input responseInput) model.NotificationSendResult {
	status := input.response.StatusCode
	class := strconv.Itoa(status/100) + "xx"
	if status < 200 || status >= 300 {
		retry := status == 408 || status == 425 || status == 429 || status >= 500 && status < 600
		return model.NotificationSendResult{Retryable: retry, ErrorCode: "http_rejected", Stage: "response", StatusClass: class, RetryAfter: retryAfter(input.response.Header.Get("Retry-After"), input.now)}
	}
	if input.kind == TypeBark && input.compatibility != "http_2xx" {
		var result struct {
			Code int `json:"code"`
		}
		if err := json.Unmarshal(input.body, &result); err != nil || result.Code != 200 || input.readErr != nil {
			return sendRejected("provider_rejected", "response")
		}
	}
	if input.kind == TypeTelegram {
		var result struct {
			OK bool `json:"ok"`
		}
		if err := json.Unmarshal(input.body, &result); err != nil || !result.OK || input.readErr != nil {
			return sendRejected("provider_rejected", "response")
		}
	}
	if input.kind == TypeServerChanTurbo || input.kind == TypeServerChan3 {
		var result struct {
			Code *int `json:"code"`
			Data struct {
				Errno *int `json:"errno"`
			} `json:"data"`
		}
		if err := json.Unmarshal(input.body, &result); err != nil || result.Code == nil || *result.Code != 0 || input.readErr != nil {
			return sendRejected("provider_rejected", "response")
		}
		if result.Data.Errno != nil && *result.Data.Errno != 0 {
			return sendRejected("provider_rejected", "response")
		}
	}
	return model.NotificationSendResult{Success: true, StatusClass: class}
}

func retryAfter(value string, now time.Time) time.Duration {
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds <= 0 {
			return 0
		}
		return min(time.Duration(min(seconds, 3600))*time.Second, time.Hour)
	}
	when, err := http.ParseTime(value)
	if err != nil || !now.Before(when) {
		return 0
	}
	return min(when.Sub(now), time.Hour)
}

func sendRejected(code, stage string) model.NotificationSendResult {
	return model.NotificationSendResult{ErrorCode: code, Stage: stage}
}

func transportResult(err error) model.NotificationSendResult {
	var failure transportFailure
	if errors.As(err, &failure) {
		return model.NotificationSendResult{Retryable: failure.retryable, ErrorCode: failure.code, Stage: failure.stage}
	}
	if errors.Is(err, context.Canceled) {
		return model.NotificationSendResult{Retryable: true, ErrorCode: "request_cancelled", Stage: "request"}
	}
	var timeout net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout() {
		return model.NotificationSendResult{Retryable: true, ErrorCode: "request_timeout", Stage: "request"}
	}
	return model.NotificationSendResult{Retryable: true, ErrorCode: "transport_failed", Stage: "request"}
}
