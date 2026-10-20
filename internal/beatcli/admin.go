package beatcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	cli "github.com/urfave/cli/v3"
)

// A12：`owner(...)` 与 `router.admin` 路由只接受后台会话，服务令牌作用域覆盖不到这些动作，
// 所以 beat-cli 增加一条管理面凭据（登录 + 按需最近认证），而不是放宽任何一条服务端路由的鉴权。
const (
	loginPath          = "/api/v1/auth/login"
	reauthenticatePath = "/api/v1/auth/reauthenticate"
	sessionPath        = "/api/v1/auth/session"

	adminUserFlag     = "admin-user"
	adminPasswordFlag = "admin-password-file"
	adminTOTPFlag     = "admin-totp-code"
	sessionFileFlag   = "session-file"

	// maximumAdminSecretBytes 限制口令、TOTP 与会话文件的读取体积。
	maximumAdminSecretBytes = 4 << 10

	// AdminUserEnv、AdminPasswordEnv、AdminTOTPEnv 是管理面凭据的环境变量名。
	AdminUserEnv     = "BEAT_ADMIN_USER"
	AdminPasswordEnv = "BEAT_ADMIN_PASSWORD"
	AdminTOTPEnv     = "BEAT_ADMIN_TOTP"
)

// adminCredentials 是一次管理面登录所需的最小凭据集。
type adminCredentials struct {
	User     string
	Password string
	TOTP     string
}

// adminFlags 是管理面凭据的全局 flag，缺一不可地省略时只依赖环境变量。
func adminFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: adminUserFlag, Usage: "administrator username, or set " + AdminUserEnv},
		&cli.StringFlag{Name: adminPasswordFlag, Usage: "path to a 0600 administrator password file, or - for standard input"},
		&cli.StringFlag{Name: adminTOTPFlag, Usage: "one-time TOTP code when the administrator enabled two-factor authentication"},
		&cli.StringFlag{Name: sessionFileFlag, Usage: "reuse or cache the administrator session cookie in a 0600 file"},
	}
}

// Login 建立后台会话，并把会话 cookie 记在客户端上供后续请求复用。
func (c *Client) Login(ctx context.Context, credentials adminCredentials) error {
	payload, err := json.Marshal(map[string]string{
		"username": credentials.User, "password": credentials.Password, "totp_code": credentials.TOTP})
	if err != nil {
		return fmt.Errorf("encode credentials: %w", err)
	}
	response, err := c.doJSON(ctx, http.MethodPost, loginPath, payload)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if _, err := readBounded(response.Body); err != nil {
		return err
	}
	cookie, err := sessionCookie(response.Header)
	if err != nil {
		return err
	}
	c.cookie = cookie
	return nil
}

// Reauthenticate 打开最近认证窗口：owner 专属路由要求一次新的口令确认。
func (c *Client) Reauthenticate(ctx context.Context, credentials adminCredentials) error {
	if c.cookie == "" {
		return errors.New("administrator session is required before reauthentication")
	}
	payload, err := json.Marshal(map[string]string{"password": credentials.Password, "totp_code": credentials.TOTP})
	if err != nil {
		return fmt.Errorf("encode credentials: %w", err)
	}
	response, err := c.doJSON(ctx, http.MethodPost, reauthenticatePath, payload)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	_, err = readBounded(response.Body)
	return err
}

// VerifySession 用当前 cookie 读一次会话信息，用于判断缓存下来的会话是否还有效。
func (c *Client) VerifySession(ctx context.Context) error {
	response, err := c.do(ctx, http.MethodGet, sessionPath, nil)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	_, err = readBounded(response.Body)
	return err
}

// sessionCookie 取登录响应下发的会话 cookie；服务端登录只下发一个 cookie。
func sessionCookie(header http.Header) (string, error) {
	for _, value := range header.Values("Set-Cookie") {
		pair := strings.TrimSpace(strings.SplitN(value, ";", 2)[0])
		if name, _, found := strings.Cut(pair, "="); found && strings.TrimSpace(name) != "" && pair != "" {
			return pair, nil
		}
	}
	return "", errors.New("login succeeded without a session cookie")
}

// resolveAdminCredentials 依次从 flag、环境变量与私有文件解析管理面凭据。
func (a *application) resolveAdminCredentials(command *cli.Command) (adminCredentials, error) {
	user := a.adminValue(command, adminUserFlag, AdminUserEnv)
	if user == "" {
		return adminCredentials{}, fmt.Errorf("administrator user is required: pass --%s or set %s", adminUserFlag, AdminUserEnv)
	}
	password, err := a.adminPassword(command)
	if err != nil {
		return adminCredentials{}, err
	}
	return adminCredentials{User: user, Password: password, TOTP: a.adminValue(command, adminTOTPFlag, AdminTOTPEnv)}, nil
}

func (a *application) adminValue(command *cli.Command, flag, environment string) string {
	if value := strings.TrimSpace(command.String(flag)); value != "" {
		return value
	}
	return a.adminEnv(environment)
}

func (a *application) adminEnv(environment string) string {
	if a.options.Getenv == nil {
		return ""
	}
	return strings.TrimSpace(a.options.Getenv(environment))
}

// adminPassword 支持私有文件、标准输入与环境变量三种口令来源，避免口令出现在命令行历史里。
func (a *application) adminPassword(command *cli.Command) (string, error) {
	path := strings.TrimSpace(command.String(adminPasswordFlag))
	if path == stdinTokenPath {
		if a.options.Stdin == nil {
			return "", errors.New("standard input is unavailable")
		}
		return normalizeAdminSecret(a.options.Stdin)
	}
	if path != "" {
		return readPrivateSecret(path, "administrator password file")
	}
	if password := a.adminEnv(AdminPasswordEnv); password != "" {
		return password, nil
	}
	return "", fmt.Errorf("administrator password is required: pass --%s or set %s", adminPasswordFlag, AdminPasswordEnv)
}

// normalizeAdminSecret 读取有界口令并拒绝空值与内嵌空白，避免发出必然失败的请求。
func normalizeAdminSecret(reader io.Reader) (string, error) {
	payload, err := io.ReadAll(io.LimitReader(reader, maximumAdminSecretBytes+1))
	if err != nil {
		return "", fmt.Errorf("read administrator password: %w", err)
	}
	if int64(len(payload)) > maximumAdminSecretBytes {
		return "", fmt.Errorf("administrator password exceeds %d bytes", int64(maximumAdminSecretBytes))
	}
	password := strings.TrimSpace(string(payload))
	if password == "" {
		return "", errors.New("administrator password is empty")
	}
	return password, nil
}

// readPrivateSecret 读取 0600 或更严格的私有文件；组或其他用户可读的文件会被拒绝。
func readPrivateSecret(path, label string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", label, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s must be a regular file: %s", label, path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("%s must not be readable by group or others: chmod 600 %s", label, path)
	}
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", label, err)
	}
	defer func() { _ = file.Close() }()
	secret, err := normalizeAdminSecret(file)
	if err != nil {
		return "", fmt.Errorf("%s: %w", label, err)
	}
	return secret, nil
}

// adminSession 建立管理面会话，recent 为真时再补一次最近认证。
func (a *application) adminSession(ctx context.Context, command *cli.Command, recent bool) (*session, error) {
	server, err := ResolveServer(a.serverAddress(command))
	if err != nil {
		return nil, err
	}
	credentials, err := a.resolveAdminCredentials(command)
	if err != nil {
		return nil, err
	}
	httpClient, err := a.httpClient(command, server)
	if err != nil {
		return nil, err
	}
	client := NewClient(server, "", httpClient)
	if err := a.establishAdminSession(ctx, client, command, credentials); err != nil {
		return nil, err
	}
	if recent {
		if err := client.Reauthenticate(ctx, credentials); err != nil {
			return nil, err
		}
	}
	return &session{client: client, output: a.options.Output, summary: a.options.Errors, raw: command.Bool(rawFlag)}, nil
}

// establishAdminSession 优先复用仍然有效的会话文件，否则重新登录并按需写回。
func (a *application) establishAdminSession(
	ctx context.Context, client *Client, command *cli.Command, credentials adminCredentials,
) error {
	path := strings.TrimSpace(command.String(sessionFileFlag))
	if path == "" {
		return client.Login(ctx, credentials)
	}
	cookie, err := readSessionFile(path)
	switch {
	case err == nil:
		client.cookie = cookie
		if client.VerifySession(ctx) == nil {
			return nil
		}
	case errors.Is(err, os.ErrNotExist):
	default:
		return err
	}
	if err := client.Login(ctx, credentials); err != nil {
		return err
	}
	return writeSessionFile(path, client.cookie)
}

// readSessionFile 读取缓存的会话 cookie；组或其他用户可读的文件会被拒绝，避免会话泄露给同机账号。
func readSessionFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("read session file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("session file must be a regular file: %s", path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("session file must not be readable by group or others: chmod 600 %s", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("read session file: %w", err)
	}
	defer func() { _ = file.Close() }()
	payload, err := io.ReadAll(io.LimitReader(file, maximumAdminSecretBytes+1))
	if err != nil {
		return "", fmt.Errorf("read session file: %w", err)
	}
	if int64(len(payload)) > maximumAdminSecretBytes {
		return "", fmt.Errorf("session file exceeds %d bytes", int64(maximumAdminSecretBytes))
	}
	cookie := strings.TrimSpace(string(payload))
	if cookie == "" {
		return "", errors.New("session file is empty")
	}
	return cookie, nil
}

// writeSessionFile 以 0600 写入会话 cookie；权限由创建模式与 umask 共同保证。
func writeSessionFile(path, cookie string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("write session file: %w", err)
	}
	if _, err := io.WriteString(file, cookie+"\n"); err != nil {
		_ = file.Close()
		return fmt.Errorf("write session file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("write session file: %w", err)
	}
	return nil
}
