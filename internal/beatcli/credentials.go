package beatcli

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/beat/backend/internal/model"
)

const (
	// maximumTokenBytes 限制读取的令牌文件体积。
	maximumTokenBytes = 4 << 10
	// ServiceTokenEnv 是服务令牌的环境变量名。
	ServiceTokenEnv = "BEAT_SERVICE_TOKEN"
	// ServerEnv 是服务端地址的环境变量名。
	ServerEnv = "BEAT_SERVER"
	// stdinTokenPath 表示从标准输入读取令牌。
	stdinTokenPath = "-"
)

// ResolveServer 解析服务端地址，拒绝相对地址、非 http(s) 协议、凭据与查询串。
// IPv4、IPv6（含方括号形式）与反向代理路径前缀都受支持。
func ResolveServer(raw string) (*url.URL, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("server address is required: pass --server or set %s", ServerEnv)
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("server address is invalid: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("server address must use http or https: %q", trimmed)
	}
	if parsed.Hostname() == "" {
		return nil, fmt.Errorf("server address must include a host: %q", trimmed)
	}
	if parsed.User != nil {
		return nil, errors.New("server address must not contain credentials")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("server address must not contain a query or fragment")
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	return parsed, nil
}

// ResolveToken 依次从令牌文件、环境变量或标准输入解析服务令牌。
// path 为空时读取 BEAT_SERVICE_TOKEN，path 为 "-" 时读取标准输入。
func ResolveToken(path string, getenv func(string) string, stdin io.Reader) (string, error) {
	if path == stdinTokenPath {
		reader := stdin
		if reader == nil {
			return "", errors.New("standard input is unavailable")
		}
		return normalizeToken(io.LimitReader(reader, maximumTokenBytes))
	}
	if path != "" {
		return readTokenFile(path)
	}
	if getenv != nil {
		if value := getenv(ServiceTokenEnv); strings.TrimSpace(value) != "" {
			return normalizeToken(strings.NewReader(value))
		}
	}
	return "", fmt.Errorf("service token is required: pass --token-file or set %s", ServiceTokenEnv)
}

// readTokenFile 读取 0600 或更严格的令牌文件；组或其他用户可读的文件会被拒绝。
func readTokenFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("read token file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("token file must be a regular file: %s", path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("token file must not be readable by group or others: chmod 600 %s", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("read token file: %w", err)
	}
	defer func() { _ = file.Close() }()
	return normalizeToken(io.LimitReader(file, maximumTokenBytes))
}

// normalizeToken 校验令牌形态，避免把空值、带空白的值或错误类型的凭据发到服务端。
func normalizeToken(reader io.Reader) (string, error) {
	payload, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("read token: %w", err)
	}
	token := strings.TrimSpace(string(payload))
	if token == "" {
		return "", errors.New("service token is empty")
	}
	if strings.ContainsAny(token, " \t\r\n") {
		return "", errors.New("service token must not contain whitespace")
	}
	if !strings.HasPrefix(token, model.ServiceTokenPrefix) {
		return "", fmt.Errorf("service token must start with %q", model.ServiceTokenPrefix)
	}
	return token, nil
}
