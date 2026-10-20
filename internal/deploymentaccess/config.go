package deploymentaccess

import (
	"os"
	"strings"
)

// Environment variables that own the deployment access policy. None of these
// values is stored in SQLite; they stay deployment configuration.
const (
	EnvPublicBaseURL   = "BEAT_PUBLIC_BASE_URL"
	EnvPublicAPIOrigin = "BEAT_PUBLIC_API_ORIGINS"
	EnvPublicWSOrigin  = "BEAT_PUBLIC_WS_ORIGINS"
)

// Config 是启动时解析一次的部署访问策略。
type Config struct {
	publicBaseURL string
	apiOrigins    OriginPolicy
	wsOrigins     OriginPolicy
}

// Status 只描述策略是否有效以及生效的模式，不携带任何来源列表。
type Status struct {
	Valid          bool   `json:"valid"`
	PublicBaseURL  string `json:"public_base_url"`
	APIOriginMode  string `json:"api_origin_mode"`
	APIOriginCount int    `json:"api_origin_count"`
	WSOriginMode   string `json:"ws_origin_mode"`
	WSOriginCount  int    `json:"ws_origin_count"`
}

// Load 解析部署访问策略。未配置的项使用当前行为作为默认值：
// 公共 REST 默认允许任意来源，公共 WebSocket 默认只接受同源或已配置的外部地址。
func Load(getenv func(string) string) (Config, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	baseURL, err := ParsePublicBaseURL(getenv(EnvPublicBaseURL))
	if err != nil {
		return Config{}, err
	}
	apiValue := strings.TrimSpace(getenv(EnvPublicAPIOrigin))
	if apiValue == "" {
		apiValue = "*"
	}
	apiOrigins, err := ParseOrigins(apiValue)
	if err != nil {
		return Config{}, err
	}
	wsOrigins, err := loadWSOrigins(getenv(EnvPublicWSOrigin), baseURL)
	if err != nil {
		return Config{}, err
	}
	return Config{publicBaseURL: baseURL, apiOrigins: apiOrigins, wsOrigins: wsOrigins}, nil
}

// loadWSOrigins 在未显式配置时回落到规范外部地址，绝不从请求头推导。
func loadWSOrigins(value string, baseURL string) (OriginPolicy, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed != "" {
		return ParseOrigins(trimmed)
	}
	if baseURL == "" {
		return OriginPolicy{exact: map[string]struct{}{}}, nil
	}
	return ParseOrigins(baseURL)
}

// PublicBaseURL 返回规范外部地址，未配置时为空。
func (config Config) PublicBaseURL() string { return config.publicBaseURL }

// APIOrigins 返回公共 REST 来源策略。
func (config Config) APIOrigins() OriginPolicy { return config.apiOrigins }

// WSOrigins 返回公共 WebSocket 来源策略。
func (config Config) WSOrigins() OriginPolicy { return config.wsOrigins }

// Status 汇总可展示的策略状态；来源列表始终以数量代替。
func (config Config) Status() Status {
	return Status{
		Valid:          true,
		PublicBaseURL:  config.publicBaseURL,
		APIOriginMode:  config.apiOrigins.Mode(),
		APIOriginCount: config.apiOrigins.Count(),
		WSOriginMode:   config.wsOrigins.Mode(),
		WSOriginCount:  config.wsOrigins.Count(),
	}
}
