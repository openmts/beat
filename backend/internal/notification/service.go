package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
		httpClient:      &http.Client{Timeout: 10 * time.Second},
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
		return s.sendTelegramText(ctx, normalized, message.Text)
	case TypeEmail:
		return s.sendEmailText(ctx, normalized, message.Subject, message.Text)
	default:
		return fmt.Errorf("unsupported channel type %q", channel.ChannelType)
	}
}

func (s *Service) sendWebhook(ctx context.Context, raw string, event *model.AlertEvent) error {
	return s.sendWebhookPayload(ctx, raw, event)
}

func (s *Service) sendWebhookPayload(ctx context.Context, raw string, payloadValue any) error {
	var config WebhookConfig
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return fmt.Errorf("decode webhook config: %w", err)
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
	return s.doHTTP(request, "webhook")
}

func (s *Service) sendTelegram(ctx context.Context, raw string, event *model.AlertEvent) error {
	return s.sendTelegramText(ctx, raw, deliveryText(event))
}

func (s *Service) sendTelegramText(ctx context.Context, raw string, text string) error {
	var config TelegramConfig
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return fmt.Errorf("decode Telegram config: %w", err)
	}
	payload, err := json.Marshal(map[string]string{
		"chat_id": config.ChatID,
		"text":    text,
	})
	if err != nil {
		return fmt.Errorf("encode Telegram payload: %w", err)
	}
	endpoint := strings.TrimRight(s.telegramBaseURL, "/") + "/bot" + config.BotToken + "/sendMessage"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return errors.New("create Telegram request failed")
	}
	request.Header.Set("Content-Type", "application/json")
	return s.doHTTP(request, "Telegram")
}

func (s *Service) sendEmail(ctx context.Context, raw string, event *model.AlertEvent) error {
	subject := fmt.Sprintf("[Beat] Alert %s", event.Status)
	return s.sendEmailText(ctx, raw, subject, deliveryText(event))
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
	return s.emailSender(ctx, config, subject, body)
}

func (s *Service) doHTTP(request *http.Request, channelName string) error {
	response, err := s.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("%s request failed", channelName)
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64*1024))
	if response.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("%s returned status %d", channelName, response.StatusCode)
	}
	return nil
}

func deliveryText(event *model.AlertEvent) string {
	return fmt.Sprintf(
		"Beat alert %s\nMessage: %s\nRule: %s\nNode: %s\nValue: %.2f\nTime: %s",
		event.Status,
		event.Message,
		event.RuleID,
		event.NodeID,
		event.Value,
		event.TriggeredAt.UTC().Format(time.RFC3339),
	)
}
