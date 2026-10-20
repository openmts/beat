package beatcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// maximumResponseBytes 限制写入内存的 JSON 响应体积。
	maximumResponseBytes = 8 << 20
	// maximumErrorBytes 限制错误响应读取，避免上游大页面刷屏终端。
	maximumErrorBytes = 4 << 10
	// maximumErrorMessage 限制展示给用户的错误文本长度。
	maximumErrorMessage = 200
	// authorizationScheme 是数据面要求的认证方案。
	authorizationScheme = "Bearer"
	// DefaultTimeout 是未显式指定超时时的请求上限。
	DefaultTimeout = 30 * time.Second
	// maximumRetryAfter 限制 Retry-After 提示，异常头部不会造成离谱等待。
	maximumRetryAfter = 24 * time.Hour
	// maximumRedirects 限制同主机重定向次数。
	maximumRedirects = 5
)

// APIError 是数据面返回的非成功响应。消息只含服务端有界文本，绝不含令牌。
type APIError struct {
	Status     int
	Message    string
	RetryAfter time.Duration
}

func (err *APIError) Error() string {
	message := err.Message
	if message == "" {
		message = http.StatusText(err.Status)
	}
	if err.RetryAfter > 0 {
		return fmt.Sprintf("%s (HTTP %d, retry after %s)", message, err.Status, err.RetryAfter)
	}
	return fmt.Sprintf("%s (HTTP %d)", message, err.Status)
}

// Unauthorized 判断错误是否为认证或授权拒绝，供调用方区分退出语义。
func (err *APIError) Unauthorized() bool {
	return err.Status == http.StatusUnauthorized || err.Status == http.StatusForbidden
}

// Client 使用 Bearer 服务令牌访问 Beat 数据面；管理员会话命令改用会话 cookie。
type Client struct {
	base   *url.URL
	token  string
	cookie string
	client *http.Client
}

// NewClient 构造数据面客户端；httpClient 为空时使用带超时与同主机重定向策略的默认客户端。
func NewClient(base *url.URL, token string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: DefaultTimeout, CheckRedirect: sameHostRedirects(base.Host)}
	}
	return &Client{base: base, token: token, client: httpClient}
}

// Get 读取有界 JSON 响应，用于只读数据面端点。
func (c *Client) Get(ctx context.Context, path string, query url.Values) ([]byte, error) {
	return c.call(ctx, http.MethodGet, path, query)
}

// Post 发送空体 POST，用于触发备份等已授权动作。
func (c *Client) Post(ctx context.Context, path string) ([]byte, error) {
	return c.call(ctx, http.MethodPost, path, nil)
}

// PostJSON 发送 JSON 体 POST；只用于数据面已有的 JSON 写入口。
func (c *Client) PostJSON(ctx context.Context, path string, payload []byte) ([]byte, error) {
	response, err := c.doJSON(ctx, http.MethodPost, path, payload)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	return readBounded(response.Body)
}

// Download 把响应流写入 output，返回写入字节数。
func (c *Client) Download(ctx context.Context, path string, output io.Writer) (int64, error) {
	response, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = response.Body.Close() }()
	written, err := io.Copy(output, response.Body)
	if err != nil {
		return written, fmt.Errorf("write download: %w", err)
	}
	return written, nil
}

func (c *Client) call(ctx context.Context, method, path string, query url.Values) ([]byte, error) {
	response, err := c.do(ctx, method, path, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	return readBounded(response.Body)
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values) (*http.Response, error) {
	request, err := c.newRequest(ctx, method, path, query)
	if err != nil {
		return nil, err
	}
	response, err := c.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	if response.StatusCode >= http.StatusMultipleChoices {
		defer func() { _ = response.Body.Close() }()
		return nil, decodeAPIError(response)
	}
	return response, nil
}

func (c *Client) newRequest(ctx context.Context, method, path string, query url.Values) (*http.Request, error) {
	target := *c.base
	target.Path = c.base.Path + path
	if len(query) > 0 {
		target.RawQuery = query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, method, target.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	switch {
	case c.cookie != "":
		request.Header.Set("Cookie", c.cookie)
	case c.token != "":
		request.Header.Set("Authorization", authorizationScheme+" "+c.token)
	}
	request.Header.Set("Accept", "application/json")
	return request, nil
}

// doJSON 发送带 JSON 体的请求，并带上服务端自身的 Origin：后台登录接口按同源校验拒绝匿名来源。
func (c *Client) doJSON(ctx context.Context, method, path string, payload []byte) (*http.Response, error) {
	target := *c.base
	target.Path = c.base.Path + path
	request, err := http.NewRequestWithContext(ctx, method, target.String(), bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Origin", c.base.Scheme+"://"+c.base.Host)
	if c.cookie != "" {
		request.Header.Set("Cookie", c.cookie)
	}
	response, err := c.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	if response.StatusCode >= http.StatusMultipleChoices {
		defer func() { _ = response.Body.Close() }()
		return nil, decodeAPIError(response)
	}
	return response, nil
}

// readBounded 读取响应体并拒绝超过上限的载荷，避免一次性读入超大响应。
func readBounded(body io.Reader) ([]byte, error) {
	payload, err := io.ReadAll(io.LimitReader(body, maximumResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if int64(len(payload)) > maximumResponseBytes {
		return nil, fmt.Errorf("response exceeds %d bytes", int64(maximumResponseBytes))
	}
	return payload, nil
}

func decodeAPIError(response *http.Response) error {
	payload, err := io.ReadAll(io.LimitReader(response.Body, maximumErrorBytes))
	if err != nil {
		// 错误响应读取失败时退回状态码文本，不掩盖原始状态。
		payload = nil
	}
	var envelope struct {
		Message string `json:"message"`
	}
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &envelope)
	}
	return &APIError{
		Status:     response.StatusCode,
		Message:    truncate(envelope.Message, maximumErrorMessage),
		RetryAfter: retryAfter(response.Header.Get("Retry-After"), time.Now()),
	}
}

// truncate 按字符截断错误文本，避免超长上游消息进入终端。
func truncate(value string, limit int) string {
	trimmed := strings.TrimSpace(value)
	runes := []rune(trimmed)
	if len(runes) <= limit {
		return trimmed
	}
	return string(runes[:limit])
}

// retryAfter 解析秒数或 HTTP 日期形式的 Retry-After，并限制在最大等待窗口内。
func retryAfter(value string, now time.Time) time.Duration {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(trimmed); err == nil {
		if seconds <= 0 {
			return 0
		}
		return boundRetryAfter(time.Duration(seconds) * time.Second)
	}
	if moment, err := http.ParseTime(trimmed); err == nil {
		if !moment.After(now) {
			return 0
		}
		return boundRetryAfter(moment.Sub(now))
	}
	return 0
}

func boundRetryAfter(value time.Duration) time.Duration {
	if value <= 0 {
		return 0
	}
	if value > maximumRetryAfter {
		return maximumRetryAfter
	}
	return value
}

func sameHostRedirects(host string) func(*http.Request, []*http.Request) error {
	return func(request *http.Request, via []*http.Request) error {
		if len(via) >= maximumRedirects {
			return errors.New("too many redirects")
		}
		if request.URL.Host != host {
			return fmt.Errorf("refusing redirect to another host: %s", request.URL.Host)
		}
		return nil
	}
}
