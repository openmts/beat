package notification

import (
	"encoding/json"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	defaultDispatchTimeoutSeconds = 15
	maximumDispatchTimeoutSeconds = 30
	maximumRequestBytes           = 64 * 1024
	maximumEndpointBytes          = 2048
	maximumHeaderBytes            = 8192
	maximumCollectionFields       = 32
	maximumNamedSecrets           = 8
	maximumRecipients             = 8
	maximumJSONNodes              = 512
	maximumJSONDepth              = 8
)

var providerFields = map[string]string{
	TypeWebhook:         "endpoint headers",
	TypeTelegram:        "endpoint bot_token chat_id parse_mode",
	TypeEmail:           "host port username password from to security",
	TypeBark:            "endpoint device_key level group icon sound url compatibility",
	TypeServerChanTurbo: "endpoint send_key open_id channel noip",
	TypeServerChan3:     "endpoint tags",
	TypeCustomHTTP:      "endpoint method headers query body_mode body named_secrets auth hmac",
}

func validateProviderFields(input ConfigWrite) error {
	fields, exists := providerFields[input.ChannelType]
	if !exists {
		return ErrProviderConfig
	}
	allowed := map[string]bool{"timeout_seconds": true, "redirects": true}
	for _, field := range strings.Fields(fields) {
		allowed[field] = true
	}
	for _, raw := range []json.RawMessage{input.Config, input.Previous} {
		if len(raw) == 0 {
			continue
		}
		var supplied map[string]json.RawMessage
		if len(raw) > MaximumConfigBytes {
			return ErrProviderConfig
		}
		if err := json.Unmarshal(raw, &supplied); err != nil || supplied == nil {
			return ErrProviderConfig
		}
		for field := range supplied {
			if !allowed[field] {
				return ErrProviderConfig
			}
		}
	}
	if len(input.ClearSecrets) > maximumCollectionFields {
		return ErrProviderConfig
	}
	for _, path := range input.ClearSecrets {
		if !secretConfigPath(path) || !allowed[strings.SplitN(path, ".", 2)[0]] {
			return ErrProviderConfig
		}
	}
	return nil
}

func validProviderScalar(value string, maximum int) bool {
	return len(value) <= maximum && utf8.ValidString(value) && strings.IndexFunc(value, unicode.IsControl) < 0
}

func validateHTTPAuth(auth *HTTPAuth, enabled bool) error {
	if auth == nil {
		return nil
	}
	for _, value := range []string{auth.Username, auth.Password, auth.Token} {
		if !validProviderScalar(value, maximumHeaderBytes) {
			return ErrProviderConfig
		}
	}
	switch auth.Mode {
	case "none":
		if auth.Username != "" || auth.Password != "" || auth.Token != "" {
			return ErrProviderConfig
		}
	case "basic":
		if strings.Contains(auth.Username, ":") || auth.Token != "" || enabled && auth.Username == "" {
			return ErrProviderConfig
		}
	case "bearer":
		if auth.Username != "" || auth.Password != "" || strings.ContainsAny(auth.Token, " \t") || enabled && auth.Token == "" {
			return ErrProviderConfig
		}
	default:
		return ErrProviderConfig
	}
	return nil
}

func validateSignature(config *ProviderConfig, enabled bool) error {
	signature := config.HMAC
	if signature == nil {
		return nil
	}
	if signature.Header == "" {
		signature.Header = "X-Beat-Signature"
	}
	if signature.TimestampHeader == "" {
		signature.TimestampHeader = "X-Beat-Timestamp"
	}
	if !validProviderScalar(signature.Secret, maximumHeaderBytes) || enabled && signature.Secret == "" || strings.EqualFold(signature.Header, signature.TimestampHeader) {
		return ErrProviderConfig
	}
	for _, name := range []string{signature.Header, signature.TimestampHeader} {
		if !validHeaderName(name) || strings.EqualFold(name, "Content-Type") || strings.EqualFold(name, "Authorization") {
			return ErrProviderConfig
		}
		for configured := range config.Headers {
			if strings.EqualFold(name, configured) {
				return ErrProviderConfig
			}
		}
	}
	return nil
}

func validateHeaderTemplates(headers map[string]string) error {
	seen := make(map[string]bool, len(headers))
	for name, value := range headers {
		canonical := http.CanonicalHeaderKey(name)
		if seen[canonical] || !validHeaderName(name) || !validProviderScalar(value, maximumHeaderBytes) {
			return ErrProviderConfig
		}
		seen[canonical] = true
		if _, err := CompileTemplate(value, true); err != nil {
			return err
		}
	}
	return nil
}

func validHeaderName(name string) bool {
	if name == "" || len(name) > 128 {
		return false
	}
	for _, r := range name {
		valid := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", r)
		if !valid {
			return false
		}
	}
	switch strings.ToLower(name) {
	case "host", "connection", "content-length", "transfer-encoding", "proxy-authorization", "proxy-authenticate", "te", "trailer", "upgrade", "keep-alive", "idempotency-key", "x-beat-delivery-id":
		return false
	}
	return true
}
