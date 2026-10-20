package loginsecurity

import (
	"strings"

	"github.com/beat/backend/internal/model"
)

const maximumUserAgentBytes = 512

// Device 只保留归一化后的浏览器族、操作系统族与设备类别。
type Device struct {
	Browser string
	OS      string
	Class   string
}

var browserTokens = []struct{ token, family string }{
	{"Edg/", "edge"}, {"OPR/", "opera"}, {"Opera", "opera"}, {"Firefox/", "firefox"},
	{"Chrome/", "chrome"}, {"Chromium/", "chromium"}, {"CriOS/", "chrome"}, {"FxiOS/", "firefox"},
	{"Safari/", "safari"}, {"curl/", "curl"}, {"Wget/", "wget"},
	{"python-requests/", "python_requests"}, {"Go-http-client/", "go_http_client"}, {"PostmanRuntime/", "postman"},
}

var automationBrowsers = map[string]bool{
	"curl": true, "wget": true, "python_requests": true, "go_http_client": true, "postman": true,
}

var osTokens = []struct{ token, family string }{
	{"Windows NT", "windows"}, {"Android", "android"}, {"iPhone", "ios"}, {"iPad", "ipados"},
	{"Mac OS X", "macos"}, {"CrOS", "chromeos"}, {"FreeBSD", "freebsd"}, {"Linux", "linux"},
}

// SummarizeDevice 在本地解析受限长度的 User-Agent，解析不了就返回 unknown。
func SummarizeDevice(raw string) Device {
	if len(raw) > maximumUserAgentBytes {
		raw = raw[:maximumUserAgentBytes]
	}
	browser := matchToken(raw, browserTokens)
	return Device{Browser: browser, OS: matchToken(raw, osTokens), Class: deviceClass(raw, browser)}
}

func matchToken(raw string, table []struct{ token, family string }) string {
	for _, candidate := range table {
		if strings.Contains(raw, candidate.token) {
			return candidate.family
		}
	}
	return model.SecurityDeviceClassUnknown
}

func deviceClass(raw, browser string) string {
	switch {
	case automationBrowsers[browser]:
		return model.SecurityDeviceClassAutomation
	case strings.Contains(raw, "iPad") || strings.Contains(raw, "Tablet"):
		return model.SecurityDeviceClassTablet
	case strings.Contains(raw, "Mobile") || strings.Contains(raw, "Android") || strings.Contains(raw, "iPhone"):
		return model.SecurityDeviceClassMobile
	case browser == model.SecurityDeviceClassUnknown:
		return model.SecurityDeviceClassUnknown
	default:
		return model.SecurityDeviceClassDesktop
	}
}
