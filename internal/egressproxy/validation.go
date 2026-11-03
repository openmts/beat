package egressproxy

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

const maximumProxyURLLength = 2048

func normalizeInput(input Update, current secretPayload, hasCurrent bool) (secretPayload, error) {
	proxyURL, err := validateProxyURL(input.ProxyURL, input.Enabled)
	if err != nil {
		return secretPayload{}, err
	}
	username := strings.TrimSpace(input.Username)
	if utf8.RuneCountInString(username) > 512 || strings.ContainsAny(username, "\x00\r\n") {
		return secretPayload{}, ErrInvalid
	}
	password := ""
	if hasCurrent {
		password = current.Password
	}
	if input.ClearPassword {
		password = ""
	} else if input.Password != nil {
		password = *input.Password
	}
	if len(password) > 2048 || strings.ContainsAny(password, "\x00\r\n") {
		return secretPayload{}, ErrInvalid
	}
	if username == "" {
		password = ""
	}
	return secretPayload{ProxyURL: proxyURL, Username: username, Password: password}, nil
}

func validateProxyURL(raw string, enabled bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if enabled {
			return "", ErrInvalid
		}
		return "", nil
	}
	if len(raw) > maximumProxyURLLength || strings.ContainsAny(raw, "\\\x00\r\n") {
		return "", ErrInvalid
	}
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		parsed.Path != "" && parsed.Path != "/" {
		return "", ErrInvalid
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" && parsed.Scheme != "socks5" && parsed.Scheme != "socks5h" {
		return "", ErrInvalid
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port < 1 || port > 65535 {
		return "", ErrInvalid
	}
	parsed.Path = ""
	return parsed.String(), nil
}

func ValidateURL(raw string, enabled bool) (string, error) {
	return validateProxyURL(raw, enabled)
}

func IsInvalid(err error) bool { return errors.Is(err, ErrInvalid) }
