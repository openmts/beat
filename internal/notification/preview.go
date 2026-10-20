package notification

import (
	"context"
	"io"
	"strings"
	"time"

	"github.com/beat/backend/internal/model"
)

type Preview struct {
	Method      string            `json:"method"`
	URL         string            `json:"url"`
	Headers     map[string]string `json:"headers"`
	Body        string            `json:"body"`
	ContentType string            `json:"content_type"`
	Subject     string            `json:"subject"`
	Text        string            `json:"text"`
}

func PreviewDispatch(dispatch model.NotificationDispatch) (Preview, error) {
	config, err := DecodeProvider(dispatch.Config)
	if err != nil {
		return Preview{}, err
	}
	config.Body, err = redactPreviewBody(config.Body)
	if err != nil {
		return Preview{}, err
	}
	redactProvider(&config)
	payload, err := renderPresentation(dispatch, config)
	if err != nil {
		return Preview{}, err
	}
	preview := Preview{Subject: payload.Subject, Text: payload.Text, Headers: map[string]string{}}
	if dispatch.ChannelType == TypeEmail {
		preview.Method = "SMTP"
		preview.URL = "smtp://[configured-host]"
		preview.Body = payload.Text
		preview.ContentType = "text/plain; charset=utf-8"
		return preview, nil
	}
	request, err := buildProviderRequest(context.Background(), requestInput{dispatch: dispatch, config: config, payload: payload, now: time.Unix(0, 0)})
	if err != nil {
		return preview, err
	}
	preview.Method, preview.URL, preview.ContentType = request.Method, maskedEndpoint(request.URL.String()), request.Header.Get("Content-Type")
	for name := range request.Header {
		value := "[REDACTED]"
		if strings.EqualFold(name, "Content-Type") {
			value = preview.ContentType
		}
		preview.Headers[name] = value
	}
	if request.GetBody != nil {
		body, err := request.GetBody()
		if err != nil {
			return preview, ErrTemplate
		}
		encoded, readErr := io.ReadAll(io.LimitReader(body, 64*1024+1))
		closeErr := body.Close()
		if readErr != nil || closeErr != nil || len(encoded) > 64*1024 {
			return preview, ErrTemplate
		}
		preview.Body = string(encoded)
	}
	return preview, nil
}

func redactProvider(config *ProviderConfig) {
	const redacted = "[REDACTED]"
	if config.BotToken != "" {
		config.BotToken = redacted
	}
	if config.ChatID != "" {
		config.ChatID = redacted
	}
	if config.DeviceKey != "" {
		config.DeviceKey = redacted
	}
	if config.SendKey != "" {
		config.SendKey = redacted
	}
	if config.OpenID != "" {
		config.OpenID = redacted
	}
	if config.Icon != "" {
		config.Icon = redacted
	}
	if config.URL != "" {
		config.URL = redacted
	}
	for key := range config.Headers {
		config.Headers[key] = redacted
	}
	for key := range config.NamedSecrets {
		config.NamedSecrets[key] = redacted
	}
	if config.Auth != nil {
		config.Auth.Username, config.Auth.Password, config.Auth.Token = redacted, redacted, redacted
	}
	if config.HMAC != nil {
		config.HMAC.Secret = redacted
	}
}
