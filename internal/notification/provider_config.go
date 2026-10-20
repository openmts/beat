package notification

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strings"
)

const (
	TypeBark            = "bark"
	TypeServerChanTurbo = "serverchan_turbo"
	TypeServerChan3     = "serverchan_3"
	TypeCustomHTTP      = "custom_http"
	MaximumConfigBytes  = 16 * 1024
)

var ErrProviderConfig = errors.New("notification provider configuration is invalid")

type HTTPAuth struct {
	Mode     string `json:"mode"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Token    string `json:"token,omitempty"`
}

type HTTPSignature struct {
	Secret          string `json:"secret"`
	Header          string `json:"header,omitempty"`
	TimestampHeader string `json:"timestamp_header,omitempty"`
}

// ProviderConfig 只在配置写入/派发窗口内持有，不直接返回HTTP或日志。
type ProviderConfig struct {
	Endpoint       string            `json:"endpoint,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	BotToken       string            `json:"bot_token,omitempty"`
	ChatID         string            `json:"chat_id,omitempty"`
	ParseMode      string            `json:"parse_mode,omitempty"`
	Host           string            `json:"host,omitempty"`
	Port           int               `json:"port,omitempty"`
	Username       string            `json:"username,omitempty"`
	Password       string            `json:"password,omitempty"`
	From           string            `json:"from,omitempty"`
	To             []string          `json:"to,omitempty"`
	Security       string            `json:"security,omitempty"`
	DeviceKey      string            `json:"device_key,omitempty"`
	Level          string            `json:"level,omitempty"`
	Group          string            `json:"group,omitempty"`
	Icon           string            `json:"icon,omitempty"`
	Sound          string            `json:"sound,omitempty"`
	URL            string            `json:"url,omitempty"`
	Compatibility  string            `json:"compatibility,omitempty"`
	SendKey        string            `json:"send_key,omitempty"`
	OpenID         string            `json:"open_id,omitempty"`
	Channel        string            `json:"channel,omitempty"`
	NoIP           bool              `json:"noip,omitempty"`
	Tags           []string          `json:"tags,omitempty"`
	Method         string            `json:"method,omitempty"`
	Query          map[string]string `json:"query,omitempty"`
	BodyMode       string            `json:"body_mode,omitempty"`
	Body           json.RawMessage   `json:"body,omitempty"`
	NamedSecrets   map[string]string `json:"named_secrets,omitempty"`
	Auth           *HTTPAuth         `json:"auth,omitempty"`
	HMAC           *HTTPSignature    `json:"hmac,omitempty"`
	TimeoutSeconds int               `json:"timeout_seconds,omitempty"`
	Redirects      int               `json:"redirects,omitempty"`
}

func (ProviderConfig) String() string { return "notification provider configuration" }

func (ProviderConfig) GoString() string { return "notification provider configuration" }

type ConfigWrite struct {
	ChannelType      string
	Config, Previous json.RawMessage
	ClearSecrets     []string
	Enabled          bool
}

type CanonicalConfig struct {
	Secret, Public json.RawMessage
	Present        map[string]bool
}

func CanonicalizeProvider(input ConfigWrite) (CanonicalConfig, error) {
	if err := validateProviderFields(input); err != nil {
		return CanonicalConfig{}, err
	}
	raw, err := mergeProviderConfig(input)
	if err != nil {
		return CanonicalConfig{}, err
	}
	defer clear(raw)
	config, err := DecodeProvider(raw)
	if err != nil {
		return CanonicalConfig{}, err
	}
	if err := validateProvider(input.ChannelType, &config, input.Enabled); err != nil {
		return CanonicalConfig{}, err
	}
	secret, err := json.Marshal(config)
	if err != nil || len(secret) > MaximumConfigBytes {
		return CanonicalConfig{}, ErrProviderConfig
	}
	public, present := publicProvider(input.ChannelType, config)
	encoded, err := json.Marshal(public)
	if err != nil {
		return CanonicalConfig{}, err
	}
	return CanonicalConfig{Secret: secret, Public: encoded, Present: present}, nil
}

func DecodeProvider(raw json.RawMessage) (ProviderConfig, error) {
	var config ProviderConfig
	if len(raw) > MaximumConfigBytes || len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' {
		return config, ErrProviderConfig
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return config, ErrProviderConfig
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return config, ErrProviderConfig
	}
	return config, nil
}

func publicProvider(kind string, c ProviderConfig) (map[string]any, map[string]bool) {
	public := map[string]any{"endpoint_display": maskedEndpoint(c.Endpoint), "timeout_seconds": c.TimeoutSeconds, "redirects": c.Redirects}
	present := map[string]bool{"endpoint": c.Endpoint != "", "headers": len(c.Headers) > 0}
	switch kind {
	case TypeTelegram:
		public["parse_mode"] = c.ParseMode
		present["bot_token"] = c.BotToken != ""
		present["chat_id"] = c.ChatID != ""
	case TypeEmail:
		public["port"], public["security"], public["recipient_count"] = c.Port, c.Security, len(c.To)
		present["host"], present["username"], present["password"], present["from"], present["to"] = c.Host != "", c.Username != "", c.Password != "", c.From != "", len(c.To) > 0
	case TypeBark:
		public["level"], public["compatibility"], public["group"], public["sound"] = c.Level, c.Compatibility, c.Group, c.Sound
		present["device_key"], present["icon"], present["url"] = c.DeviceKey != "", c.Icon != "", c.URL != ""
	case TypeServerChanTurbo:
		public["channel"], public["noip"] = c.Channel, c.NoIP
		present["send_key"], present["open_id"] = c.SendKey != "", c.OpenID != ""
	case TypeServerChan3:
		public["tags"] = c.Tags
	case TypeCustomHTTP:
		public["method"], public["body_mode"] = c.Method, c.BodyMode
		public["header_count"], public["query_count"], public["has_body"] = len(c.Headers), len(c.Query), len(c.Body) > 0
		present["query"], present["body"] = len(c.Query) > 0, len(c.Body) > 0
		for name, value := range c.NamedSecrets {
			present["named_secrets."+name] = value != ""
		}
		present["auth"], present["hmac"] = c.Auth != nil, c.HMAC != nil
	}
	return public, present
}

func maskedEndpoint(raw string) string {
	endpoint, err := url.Parse(raw)
	if err != nil || endpoint.Scheme == "" {
		return ""
	}
	return endpoint.Scheme + "://[configured-host]"
}

func mergeProviderConfig(input ConfigWrite) (json.RawMessage, error) {
	current := map[string]any{}
	if len(input.Previous) > 0 {
		if err := json.Unmarshal(input.Previous, &current); err != nil || current == nil {
			return nil, ErrProviderConfig
		}
	}
	var update map[string]any
	if len(input.Config) == 0 {
		input.Config = json.RawMessage(`{}`)
	}
	if len(input.Config) > MaximumConfigBytes {
		return nil, ErrProviderConfig
	}
	if err := json.Unmarshal(input.Config, &update); err != nil || update == nil {
		return nil, ErrProviderConfig
	}
	mergeConfigObject(current, update, "")
	for _, path := range input.ClearSecrets {
		if !secretConfigPath(path) {
			return nil, ErrProviderConfig
		}
		clearConfigPath(current, strings.Split(path, "."))
	}
	encoded, err := json.Marshal(current)
	if err != nil || len(encoded) > MaximumConfigBytes {
		return nil, ErrProviderConfig
	}
	return encoded, nil
}

func mergeConfigObject(current, update map[string]any, prefix string) {
	for key, value := range update {
		path := prefix + key
		if prefix == "" && (key == "headers" || key == "query") {
			if object, ok := value.(map[string]any); ok {
				if len(object) > 0 {
					current[key] = object
				}
				continue
			}
		}
		if secretConfigPath(path) && blankSecretInput(path, value) {
			continue
		}
		if nested, ok := value.(map[string]any); ok && prefix == "" && (key == "auth" || key == "hmac" || key == "named_secrets") {
			previous, _ := current[key].(map[string]any)
			if previous == nil {
				previous = map[string]any{}
			}
			mergeConfigObject(previous, nested, path+".")
			current[key] = previous
		} else {
			current[key] = value
		}
	}
}

func secretConfigPath(path string) bool {
	switch path {
	case "endpoint", "bot_token", "chat_id", "host", "username", "password", "from", "to", "device_key", "send_key", "open_id", "headers", "query", "body", "auth", "hmac", "auth.password", "auth.token", "auth.username", "hmac.secret", "icon", "url":
		return true
	}
	return strings.HasPrefix(path, "named_secrets.") && secretNamePattern.MatchString(strings.TrimPrefix(path, "named_secrets."))
}

func blankSecretInput(path string, value any) bool {
	if value == nil {
		return true
	}
	if text, ok := value.(string); ok {
		return text == ""
	}
	if items, ok := value.([]any); ok && path == "to" {
		return len(items) == 0
	}
	return false
}

func clearConfigPath(object map[string]any, path []string) {
	if len(path) == 1 {
		delete(object, path[0])
		return
	}
	nested, ok := object[path[0]].(map[string]any)
	if ok {
		clearConfigPath(nested, path[1:])
	}
}
