package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/beat/backend/internal/model"
)

const (
	DeliverySuccess = "success"
	DeliveryFailed  = "failed"
)

type emailSenderFunc func(context.Context, EmailConfig, string, string) error

type Message struct {
	Kind    string `json:"kind"`
	Subject string `json:"subject"`
	Text    string `json:"text"`
	Data    any    `json:"data,omitempty"`
}

type Service struct {
	httpClient      *http.Client
	telegramBaseURL string
	emailSender     emailSenderFunc
	now             func() time.Time

	mu       sync.RWMutex
	statuses map[string]model.AlertDeliveryStatus
	success  atomic.Uint64
	failed   atomic.Uint64
}

type DeliveryStats struct {
	Success uint64
	Failed  uint64
}

func NewService() *Service {
	return &Service{
		httpClient:      newDeliveryHTTPClient(),
		telegramBaseURL: "https://api.telegram.org",
		emailSender:     deliverEmail,
		now:             model.NowUTC,
		statuses:        make(map[string]model.AlertDeliveryStatus),
	}
}

func (s *Service) Send(
	ctx context.Context,
	channel *model.AlertChannel,
	event *model.AlertEvent,
) (model.AlertDeliveryStatus, error) {
	return s.recordDelivery(channel.ID, s.dispatch(ctx, channel, event))
}

func (s *Service) SendMessage(
	ctx context.Context,
	channel *model.AlertChannel,
	message Message,
) (model.AlertDeliveryStatus, error) {
	return s.recordDelivery(channel.ID, s.dispatchMessage(ctx, channel, message))
}

func (s *Service) recordDelivery(
	channelID string,
	err error,
) (model.AlertDeliveryStatus, error) {
	status := model.AlertDeliveryStatus{
		State: DeliverySuccess, Message: "delivered", DeliveredAt: s.now(),
	}
	if err != nil {
		status.State = DeliveryFailed
		status.Message = err.Error()
		s.failed.Add(1)
	} else {
		s.success.Add(1)
	}
	s.mu.Lock()
	s.statuses[channelID] = status
	s.mu.Unlock()
	return status, err
}

func (s *Service) Stats() DeliveryStats {
	return DeliveryStats{Success: s.success.Load(), Failed: s.failed.Load()}
}

func (s *Service) Status(channelID string) *model.AlertDeliveryStatus {
	s.mu.RLock()
	status, found := s.statuses[channelID]
	s.mu.RUnlock()
	if !found {
		return nil
	}
	return &status
}

func (s *Service) Forget(channelID string) {
	s.mu.Lock()
	delete(s.statuses, channelID)
	s.mu.Unlock()
}

func (s *Service) dispatch(ctx context.Context, channel *model.AlertChannel, event *model.AlertEvent) error {
	normalized, err := NormalizeConfig(channel.ChannelType, channel.Config, "")
	if err != nil {
		return err
	}
	switch channel.ChannelType {
	case TypeWebhook:
		return s.sendWebhook(ctx, normalized, event)
	case TypeTelegram:
		return s.sendTelegram(ctx, normalized, event)
	case TypeEmail:
		return s.sendEmail(ctx, normalized, event)
	default:
		return fmt.Errorf("unsupported channel type %q", channel.ChannelType)
	}
}

func (s *Service) dispatchMessage(
	ctx context.Context,
	channel *model.AlertChannel,
	message Message,
) error {
	normalized, err := NormalizeConfig(channel.ChannelType, channel.Config, "")
	if err != nil {
		return err
	}
	switch channel.ChannelType {
	case TypeWebhook:
		return s.sendWebhookPayload(ctx, normalized, message)
	case TypeTelegram:
		return s.sendTelegramText(ctx, normalized, message.Text, "")
	case TypeEmail:
		return s.sendEmailText(ctx, normalized, message.Subject, message.Text)
	default:
		return fmt.Errorf("unsupported channel type %q", channel.ChannelType)
	}
}

func (s *Service) sendWebhook(ctx context.Context, raw string, event *model.AlertEvent) error {
	config, err := parseWebhookConfig(raw)
	if err != nil {
		return err
	}
	if strings.TrimSpace(config.Template) != "" {
		rendered := RenderTemplate(config.Template, event)
		contentType := "text/plain"
		if json.Valid([]byte(rendered)) {
			contentType = "application/json"
		}
		return s.sendWebhookBody(ctx, raw, rendered, contentType)
	}
	return s.sendWebhookPayload(ctx, raw, event)
}

func parseWebhookConfig(raw string) (WebhookConfig, error) {
	var config WebhookConfig
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return config, fmt.Errorf("decode webhook config: %w", err)
	}
	return config, nil
}

func (s *Service) sendWebhookBody(ctx context.Context, raw string, body string, contentType string) error {
	config, err := parseWebhookConfig(raw)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, config.URL, strings.NewReader(body))
	if err != nil {
		return errors.New("create webhook request failed")
	}
	request.Header.Set("Content-Type", contentType)
	applyWebhookHeaders(request, config.Headers)
	return s.doHTTP(request, "webhook")
}

func (s *Service) sendWebhookPayload(ctx context.Context, raw string, payloadValue any) error {
	config, err := parseWebhookConfig(raw)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(payloadValue)
	if err != nil {
		return fmt.Errorf("encode webhook payload: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, config.URL, bytes.NewReader(payload))
	if err != nil {
		return errors.New("create webhook request failed")
	}
	request.Header.Set("Content-Type", "application/json")
	applyWebhookHeaders(request, config.Headers)
	return s.doHTTP(request, "webhook")
}

// applyWebhookHeaders adds custom headers to a webhook request. Content-Type is
// preserved unless a custom header explicitly overrides it.
func applyWebhookHeaders(request *http.Request, headers map[string]string) {
	for key, value := range headers {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		request.Header.Set(key, value)
	}
}

func (s *Service) sendEmail(ctx context.Context, raw string, event *model.AlertEvent) error {
	config, err := parseEmailConfig(raw)
	if err != nil {
		return err
	}
	subject := fmt.Sprintf("[Beat] Alert %s", event.Status)
	return s.sendEmailText(ctx, raw, subject, RenderTemplate(config.Template, event))
}

func parseEmailConfig(raw string) (EmailConfig, error) {
	var config EmailConfig
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return config, fmt.Errorf("decode email config: %w", err)
	}
	return config, nil
}

func (s *Service) sendEmailText(
	ctx context.Context,
	raw string,
	subject string,
	body string,
) error {
	var config EmailConfig
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return fmt.Errorf("decode email config: %w", err)
	}
	if err := s.emailSender(ctx, config, subject, body); err != nil {
		return safeTransportFailure("email", err)
	}
	return nil
}

// deliveryText builds a default human-friendly message when no custom template
// is configured on the channel. It prefers the formatted event message (which
// already carries severity, rule, metric, node and value) and falls back to a
// structured summary.
func deliveryText(event *model.AlertEvent) string {
	body := event.Message
	if body == "" {
		rule := event.RuleName
		if rule == "" {
			rule = event.RuleID
		}
		node := event.NodeName
		if node == "" {
			node = event.NodeID
		}
		line := fmt.Sprintf("%s: value=%.2f (node %s)", rule, event.Value, node)
		if severity := event.Severity; severity != "" {
			line = fmt.Sprintf("[%s] %s", severity, line)
		}
		body = line
	}
	return fmt.Sprintf("%s\nTime: %s", body, event.TriggeredAt.UTC().Format(time.RFC3339))
}
