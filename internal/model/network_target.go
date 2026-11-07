package model

import (
	"errors"
	"math"
	"net"
	"strconv"
	"strings"
)

func validateServerNetworkTarget(probeType, target string) error {
	switch probeType {
	case NetworkProbeICMP:
		if target != "{{host}}" {
			return errors.New("server ICMP target must be {{host}}")
		}
	case NetworkProbeTCP:
		if target != "{{host_port}}" {
			return errors.New("server TCP target must be {{host_port}}")
		}
	case NetworkProbeHTTP:
		if !strings.Contains(target, "{{host}}") && !strings.Contains(target, "{{host_port}}") {
			return errors.New("server HTTP target must contain {{host}} or {{host_port}}")
		}
	default:
		return errors.New("network task type is invalid")
	}
	for _, host := range []string{"example.com", "2001:db8::1"} {
		if _, err := RenderNetworkTarget(probeType, target, host, 22); err != nil {
			return err
		}
	}
	return nil
}

// RenderNetworkTarget 将服务端探测任务中的节点占位符替换为具体目标。
// 对 HTTP 目标中的 IPv6 地址自动加方括号；TCP 使用 net.JoinHostPort。
func RenderNetworkTarget(probeType, target, host string, port int) (string, error) {
	if strings.TrimSpace(host) == "" || port < 1 || port > 65535 {
		return "", errors.New("invalid network probe node target")
	}
	hostValue := host
	if probeType == NetworkProbeHTTP && strings.Contains(host, ":") &&
		!strings.HasPrefix(host, "[") && !strings.HasSuffix(host, "]") {
		hostValue = "[" + host + "]"
	}
	rendered := strings.ReplaceAll(target, "{{host_port}}", net.JoinHostPort(host, strconv.Itoa(port)))
	rendered = strings.ReplaceAll(rendered, "{{ssh_port}}", strconv.Itoa(port))
	rendered = strings.ReplaceAll(rendered, "{{host}}", hostValue)
	if strings.Contains(rendered, "{{") || strings.Contains(rendered, "}}") {
		return "", errors.New("network target template contains an unknown placeholder")
	}
	if err := validateNetworkTarget(probeType, rendered); err != nil {
		return "", err
	}
	return rendered, nil
}

func validateOptionalNonNegative(value *float64, field string) error {
	if value == nil {
		return nil
	}
	if math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 {
		return errors.New("network probe " + field + " is invalid")
	}
	return nil
}
