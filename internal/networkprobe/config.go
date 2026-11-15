package networkprobe

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const Kind = "three_network_probe"

const (
	CarrierTelecom = "telecom"
	CarrierUnicom  = "unicom"
	CarrierMobile  = "mobile"

	ProtocolTCP = "tcp"

	FamilyAuto = "auto"
	FamilyIPv4 = "ipv4"
	FamilyIPv6 = "ipv6"
)

type Config struct {
	Targets []Target      `json:"targets"`
	Probe   ProbeSettings `json:"probe"`
}

type Target struct {
	Carrier       string `json:"carrier"`
	Label         string `json:"label"`
	Host          string `json:"host"`
	Port          int    `json:"port"`
	Protocol      string `json:"protocol"`
	AddressFamily string `json:"addressFamily"`
}

type ProbeSettings struct {
	Count      int `json:"count"`
	IntervalMs int `json:"intervalMs"`
	TimeoutMs  int `json:"timeoutMs"`
}

func ParseConfig(raw []byte) (Config, error) {
	var config Config
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("解析三网探测配置失败: %w", err)
	}
	return NormalizeConfig(config)
}

func NormalizeConfig(config Config) (Config, error) {
	if config.Probe.Count == 0 {
		config.Probe.Count = 10
	}
	if config.Probe.IntervalMs == 0 {
		config.Probe.IntervalMs = 200
	}
	if config.Probe.TimeoutMs == 0 {
		config.Probe.TimeoutMs = 1000
	}
	if config.Probe.Count < 1 || config.Probe.Count > 30 {
		return Config{}, errors.New("探测次数必须在 1 到 30 次之间")
	}
	if config.Probe.IntervalMs < 50 || config.Probe.IntervalMs > 5000 {
		return Config{}, errors.New("探测间隔必须在 50 到 5000 毫秒之间")
	}
	if config.Probe.TimeoutMs < 100 || config.Probe.TimeoutMs > 10000 {
		return Config{}, errors.New("单次探测超时必须在 100 到 10000 毫秒之间")
	}
	if len(config.Targets) != 3 {
		return Config{}, errors.New("三网探测必须配置电信、联通、移动三个目标")
	}
	seen := map[string]struct{}{}
	for index := range config.Targets {
		target := &config.Targets[index]
		target.Carrier = strings.ToLower(strings.TrimSpace(target.Carrier))
		target.Label = strings.TrimSpace(target.Label)
		target.Host = strings.TrimSpace(target.Host)
		target.Protocol = strings.ToLower(strings.TrimSpace(target.Protocol))
		target.AddressFamily = strings.ToLower(strings.TrimSpace(target.AddressFamily))
		if !validCarrier(target.Carrier) {
			return Config{}, errors.New("目标运营商必须是 telecom、unicom 或 mobile")
		}
		if _, exists := seen[target.Carrier]; exists {
			return Config{}, errors.New("电信、联通、移动目标不能重复")
		}
		seen[target.Carrier] = struct{}{}
		if target.Label == "" {
			target.Label = carrierName(target.Carrier)
		}
		if len([]rune(target.Label)) > 64 {
			return Config{}, errors.New("目标名称不能超过 64 个字符")
		}
		if target.Host == "" || len(target.Host) > 253 || strings.ContainsAny(target.Host, " \t\r\n/") || strings.Contains(target.Host, "://") {
			return Config{}, fmt.Errorf("%s目标地址无效", carrierName(target.Carrier))
		}
		if target.Protocol == "" {
			target.Protocol = ProtocolTCP
		}
		if target.Protocol != ProtocolTCP {
			return Config{}, errors.New("当前原型仅支持 TCP 探测")
		}
		if target.Port == 0 {
			target.Port = 443
		}
		if target.Port < 1 || target.Port > 65535 {
			return Config{}, fmt.Errorf("%s TCP 端口必须在 1 到 65535 之间", carrierName(target.Carrier))
		}
		if target.AddressFamily == "" {
			target.AddressFamily = FamilyAuto
		}
		if target.AddressFamily != FamilyAuto && target.AddressFamily != FamilyIPv4 && target.AddressFamily != FamilyIPv6 {
			return Config{}, errors.New("地址族必须是 auto、ipv4 或 ipv6")
		}
	}
	return config, nil
}

func MarshalConfig(config Config) ([]byte, error) {
	normalized, err := NormalizeConfig(config)
	if err != nil {
		return nil, err
	}
	return json.Marshal(normalized)
}

func validCarrier(value string) bool {
	return value == CarrierTelecom || value == CarrierUnicom || value == CarrierMobile
}

func carrierName(value string) string {
	switch value {
	case CarrierTelecom:
		return "电信"
	case CarrierUnicom:
		return "联通"
	case CarrierMobile:
		return "移动"
	default:
		return value
	}
}
