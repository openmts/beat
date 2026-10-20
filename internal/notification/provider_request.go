package notification

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/beat/backend/internal/model"
)

type requestInput struct {
	dispatch model.NotificationDispatch
	config   ProviderConfig
	payload  model.NotificationPayload
	now      time.Time
}

func buildProviderRequest(ctx context.Context, input requestInput) (*http.Request, error) {
	config, payload := input.config, input.payload
	method, endpoint, contentType := http.MethodPost, config.Endpoint, "application/json"
	var body []byte
	var err error
	switch input.dispatch.ChannelType {
	case TypeWebhook:
		body, err = json.Marshal(map[string]any{"delivery_id": input.dispatch.DeliveryID, "event_kind": payload.Data["event.kind"], "subject": payload.Subject, "text": payload.Text, "data": payload.Data})
	case TypeTelegram:
		if len(payload.Text) > 4096 {
			return nil, ErrTemplate
		}
		endpoint = strings.TrimRight(endpoint, "/") + "/bot" + config.BotToken + "/sendMessage"
		body, err = json.Marshal(map[string]any{"chat_id": config.ChatID, "text": payload.Text, "parse_mode": config.ParseMode})
	case TypeBark:
		endpoint = strings.TrimRight(endpoint, "/") + "/push"
		body, err = json.Marshal(map[string]any{"device_key": config.DeviceKey, "title": payload.Subject, "body": payload.Text, "level": config.Level, "group": config.Group, "icon": config.Icon, "sound": config.Sound, "url": config.URL})
	case TypeServerChanTurbo:
		endpoint = "https://sctapi.ftqq.com/" + url.PathEscape(config.SendKey) + ".send"
		noip := 0
		if config.NoIP {
			noip = 1
		}
		body, err = json.Marshal(map[string]any{"title": payload.Subject, "desp": payload.Text, "channel": config.Channel, "openid": config.OpenID, "noip": noip})
	case TypeServerChan3:
		body, err = json.Marshal(map[string]any{"title": payload.Subject, "desp": payload.Text, "tags": strings.Join(config.Tags, "|")})
	case TypeCustomHTTP:
		method = config.Method
		endpoint, body, contentType, err = customRequest(config, payload)
	default:
		return nil, ErrProviderConfig
	}
	if err != nil || len(body) > 64*1024 {
		return nil, ErrTemplate
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, ErrProviderConfig
	}
	request.Header.Set("Content-Type", contentType)
	if err := requestHeaders(request, input); err != nil {
		return nil, err
	}
	applyProviderCredentials(request, input, body)
	return request, nil
}

func applyProviderCredentials(request *http.Request, input requestInput, body []byte) {
	request.Header.Set("X-Beat-Delivery-ID", input.dispatch.DeliveryID)
	request.Header.Set("Idempotency-Key", input.dispatch.DeliveryID)
	if input.config.Auth != nil {
		applyRequestAuth(request, *input.config.Auth)
	}
	if input.config.HMAC != nil {
		signRequest(request, signatureInput{config: *input.config.HMAC, body: body, now: input.now})
	}
}

func requestHeaders(request *http.Request, input requestInput) error {
	for name, value := range input.config.Headers {
		if input.dispatch.ChannelType == TypeCustomHTTP {
			parts, err := CompileTemplate(value, true)
			if err != nil {
				return err
			}
			value, err = RenderDeclarative(parts, TemplateValues{Data: input.payload.Data, Secrets: input.config.NamedSecrets, Maximum: 8192})
			if err != nil {
				return err
			}
		}
		if !validHeaderName(name) || !validProviderScalar(value, maximumHeaderBytes) {
			return ErrProviderConfig
		}
		request.Header.Set(name, value)
	}
	return nil
}

func applyRequestAuth(request *http.Request, auth HTTPAuth) {
	if auth.Mode == "basic" {
		request.SetBasicAuth(auth.Username, auth.Password)
	}
	if auth.Mode == "bearer" {
		request.Header.Set("Authorization", "Bearer "+auth.Token)
	}
}

type signatureInput struct {
	config HTTPSignature
	body   []byte
	now    time.Time
}

func signRequest(request *http.Request, input signatureInput) {
	timestamp := strconv.FormatInt(input.now.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(input.config.Secret))
	_, _ = mac.Write([]byte(timestamp + "\n" + request.Method + "\n" + request.URL.RequestURI() + "\n"))
	_, _ = mac.Write(input.body)
	request.Header.Set(input.config.Header, "sha256="+hex.EncodeToString(mac.Sum(nil)))
	request.Header.Set(input.config.TimestampHeader, timestamp)
}

func redirectPolicy(maximum int, origin *url.URL) func(*http.Request, []*http.Request) error {
	return func(request *http.Request, previous []*http.Request) error {
		if len(previous) > maximum || request.URL.Scheme != origin.Scheme || !strings.EqualFold(request.URL.Host, origin.Host) || request.URL.User != nil {
			return http.ErrUseLastResponse
		}
		return nil
	}
}
