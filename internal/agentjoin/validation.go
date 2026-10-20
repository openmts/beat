package agentjoin

import (
	"errors"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"github.com/beat/backend/internal/agentcredential"
)

const maximumPort = 65535

func NormalizeServerURL(value string, allowHTTP bool) (string, error) {
	invalid := errors.New("server_url must be an HTTP(S) origin; HTTP requires explicit opt-in")
	if len(value) > MaximumServerURLBytes || strings.IndexFunc(value, unicode.IsSpace) >= 0 {
		return "", invalid
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Opaque != "" {
		return "", invalid
	}
	if parsed.Scheme != "https" && (parsed.Scheme != "http" || !allowHTTP) {
		return "", invalid
	}
	if parsed.RawQuery != "" || parsed.ForceQuery || strings.Contains(value, "#") ||
		(parsed.EscapedPath() != "" && parsed.EscapedPath() != "/") {
		return "", invalid
	}
	host := strings.ToLower(parsed.Hostname())
	if !ValidHost(host) || strings.HasSuffix(parsed.Host, ":") {
		return "", invalid
	}
	port := parsed.Port()
	if port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > maximumPort {
			return "", invalid
		}
		port = strconv.Itoa(value)
	}
	if (parsed.Scheme == "https" && port == "443") || (parsed.Scheme == "http" && port == "80") {
		port = ""
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return parsed.Scheme + "://" + host, nil
}

func (request Request) Validate() error {
	if request.RequestID == "" || len(request.RequestID) > MaximumRequestIDBytes ||
		strings.IndexFunc(request.RequestID, invalidRequestIDRune) >= 0 {
		return errors.New("request_id must contain 1 to 128 letters, numbers, underscores or hyphens")
	}
	if _, ok := agentcredential.DisplayPrefix(request.AgentToken); !ok {
		return errors.New("invalid agent credential")
	}
	if strings.TrimSpace(request.NodeName) == "" || len(request.NodeName) > MaximumNodeNameBytes ||
		strings.IndexFunc(request.NodeName, unicode.IsControl) >= 0 {
		return errors.New("node_name must contain 1 to 100 bytes without control characters")
	}
	if request.AdvertisedHost != "" && !ValidHost(request.AdvertisedHost) {
		return errors.New("advertised_host must be an IP address or hostname")
	}
	if request.SSHPort < 1 || request.SSHPort > maximumPort {
		return errors.New("ssh_port must be between 1 and 65535")
	}
	return nil
}

func invalidRequestIDRune(value rune) bool {
	return !asciiAlphaNumeric(value) && value != '_' && value != '-'
}

func asciiAlphaNumeric(value rune) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9'
}

func ValidHost(value string) bool {
	if value == "" || len(value) > MaximumHostBytes {
		return false
	}
	if address, err := netip.ParseAddr(value); err == nil {
		return address.Zone() == ""
	}
	for _, label := range strings.Split(strings.TrimSuffix(value, "."), ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if !asciiAlphaNumeric(character) && character != '-' {
				return false
			}
		}
	}
	return true
}
