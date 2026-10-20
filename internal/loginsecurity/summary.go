package loginsecurity

import (
	"strings"

	"github.com/beat/backend/internal/model"
)

// SummaryFromSource 生成事件展示用的来源摘要；只有截断前缀或分类标签。
func SummaryFromSource(class, prefix string) string {
	if prefix != "" {
		return prefix
	}
	if class != "" {
		return class
	}
	return model.SecuritySourceClassUnknown
}

// SummaryFromDevice 生成事件展示用的设备摘要，未知部分不参与拼接。
func SummaryFromDevice(browser, operatingSystem, class string) string {
	parts := make([]string, 0, 3)
	for _, value := range []string{browser, operatingSystem, class} {
		if value != "" && value != model.SecurityDeviceClassUnknown {
			parts = append(parts, value)
		}
	}
	if len(parts) == 0 {
		return model.SecurityDeviceClassUnknown
	}
	return strings.Join(parts, " · ")
}
