package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/beat/backend/internal/model"
)

func (s *Service) sendTelegram(ctx context.Context, raw string, event *model.AlertEvent) error {
	config, err := parseTelegramConfig(raw)
	if err != nil {
		return err
	}
	text := RenderTemplate(config.Template, event)
	if config.ParseMode == "MarkdownV2" {
		text = RenderTemplateWith(config.Template, event, EscapeMarkdownV2)
	}
	return s.sendTelegramText(ctx, raw, text, config.ParseMode)
}

func (s *Service) sendTelegramText(ctx context.Context, raw string, text string, parseMode string) error {
	var config TelegramConfig
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return fmt.Errorf("decode Telegram config: %w", err)
	}
	payload := map[string]any{
		"chat_id": config.ChatID,
		"text":    text,
	}
	if parseMode != "" {
		payload["parse_mode"] = parseMode
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode Telegram payload: %w", err)
	}
	endpoint := strings.TrimRight(s.telegramBaseURL, "/") + "/bot" + config.BotToken + "/sendMessage"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return errors.New("create Telegram request failed")
	}
	request.Header.Set("Content-Type", "application/json")
	return s.doHTTP(request, "Telegram")
}

func parseTelegramConfig(raw string) (TelegramConfig, error) {
	var config TelegramConfig
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return config, fmt.Errorf("decode Telegram config: %w", err)
	}
	return config, nil
}
