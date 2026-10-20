package beatcli

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	cli "github.com/urfave/cli/v3"
)

const (
	// maximumCABundleBytes 限制本地 CA 包读取体积。
	maximumCABundleBytes = 1 << 20
	// flag names 在命令树与动作间共享，避免字符串漂移。
	serverFlag  = "server"
	tokenFlag   = "token-file"
	caFlag      = "ca-file"
	timeoutFlag = "timeout"
	rawFlag     = "raw"
	outputFlag  = "output"
	forceFlag   = "force"
)

// Options 注入命令依赖，便于测试替换标准流、环境与传输层。
type Options struct {
	Output    io.Writer
	Errors    io.Writer
	Stdin     io.Reader
	Getenv    func(string) string
	Transport http.RoundTripper
}

// application 承载命令树共享的依赖。
type application struct {
	options Options
}

// session 保存一次命令执行解析出的连接与输出目标。
type session struct {
	client  *Client
	output  io.Writer
	summary io.Writer
	raw     bool
}

func normalizeOptions(options Options) Options {
	if options.Output == nil {
		options.Output = os.Stdout
	}
	if options.Errors == nil {
		options.Errors = os.Stderr
	}
	if options.Stdin == nil {
		options.Stdin = os.Stdin
	}
	if options.Getenv == nil {
		options.Getenv = os.Getenv
	}
	return options
}

// connect 解析服务端、令牌与传输策略，返回可发起请求的会话。
func (a *application) connect(command *cli.Command) (*session, error) {
	server, err := ResolveServer(a.serverAddress(command))
	if err != nil {
		return nil, err
	}
	token, err := ResolveToken(command.String(tokenFlag), a.options.Getenv, a.options.Stdin)
	if err != nil {
		return nil, err
	}
	client, err := a.httpClient(command, server)
	if err != nil {
		return nil, err
	}
	return &session{
		client:  NewClient(server, token, client),
		output:  a.options.Output,
		summary: a.options.Errors,
		raw:     command.Bool(rawFlag),
	}, nil
}

func (a *application) serverAddress(command *cli.Command) string {
	if address := strings.TrimSpace(command.String(serverFlag)); address != "" {
		return address
	}
	return a.options.Getenv(ServerEnv)
}

func (a *application) httpClient(command *cli.Command, server *url.URL) (*http.Client, error) {
	timeout := command.Duration(timeoutFlag)
	if timeout <= 0 {
		return nil, errors.New("--timeout must be positive")
	}
	transport, err := a.transport(command)
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Transport:     transport,
		Timeout:       timeout,
		CheckRedirect: sameHostRedirects(server.Host),
	}, nil
}

func (a *application) transport(command *cli.Command) (http.RoundTripper, error) {
	caPath := command.String(caFlag)
	if a.options.Transport != nil {
		if caPath != "" {
			return nil, errors.New("--ca-file cannot be combined with a custom transport")
		}
		return a.options.Transport, nil
	}
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("default HTTP transport is unavailable")
	}
	cloned := base.Clone()
	if caPath == "" {
		return cloned, nil
	}
	pool, err := certificatePool(caPath)
	if err != nil {
		return nil, err
	}
	config := tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool}
	if cloned.TLSClientConfig != nil {
		config = *cloned.TLSClientConfig.Clone()
		config.RootCAs = pool
	}
	cloned.TLSClientConfig = &config
	return cloned, nil
}

// certificatePool 读取附加 CA 包；不含 PEM 证书的文件会被拒绝，避免静默失效。
func certificatePool(path string) (*x509.CertPool, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read CA bundle: %w", err)
	}
	defer func() { _ = file.Close() }()
	payload, err := io.ReadAll(io.LimitReader(file, maximumCABundleBytes))
	if err != nil {
		return nil, fmt.Errorf("read CA bundle: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(payload) {
		return nil, fmt.Errorf("CA bundle contains no PEM certificate: %s", path)
	}
	return pool, nil
}

func (s *session) get(ctx context.Context, path string, query url.Values) error {
	body, err := s.client.Get(ctx, path, query)
	if err != nil {
		return err
	}
	return writeResponse(s.output, body, s.raw)
}

func (s *session) post(ctx context.Context, path string) error {
	body, err := s.client.Post(ctx, path)
	if err != nil {
		return err
	}
	return writeResponse(s.output, body, s.raw)
}

func (s *session) postJSON(ctx context.Context, path string, payload []byte) error {
	body, err := s.client.PostJSON(ctx, path, payload)
	if err != nil {
		return err
	}
	return writeResponse(s.output, body, s.raw)
}

// addQuery 只写入显式指定的字符串 flag，未指定时不发送空参数。
func addQuery(query url.Values, command *cli.Command, names ...string) {
	for _, name := range names {
		if value := strings.TrimSpace(command.String(name)); value != "" {
			query.Set(name, value)
		}
	}
}

// pagingQuery 校验并写入分页参数，避免把负数交给服务端。
func pagingQuery(query url.Values, command *cli.Command, names ...string) error {
	for _, name := range names {
		if !command.IsSet(name) {
			continue
		}
		value := command.Int(name)
		if value < 0 {
			return fmt.Errorf("--%s must not be negative", name)
		}
		query.Set(name, strconv.Itoa(value))
	}
	return nil
}
