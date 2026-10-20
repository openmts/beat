package model

import (
	"fmt"
	"math"
	"strings"
	"unicode"
)

// Geo map 面板对齐 Grafana 的 Geomap：把序列落到地图坐标后渲染标记或热力。
// Beat 的查询结果只带 node_id/device_id，不带可选标签，读不到序列自身的经纬度，
// 因此坐标来源是面板自带的地点表（对应 Grafana 的 Lookup 模式），而不是标签取值。
const (
	// MaxDashboardGeoLocations 限制单面板地点表条数：地点表随看板定义保存，不能成为无界输入。
	MaxDashboardGeoLocations = 64
	// MaxDashboardGeoMatchBytes / MaxDashboardGeoLabelBytes 限制单条地点的匹配串与显示名长度。
	MaxDashboardGeoMatchBytes = 128
	MaxDashboardGeoLabelBytes = 64
	// DashboardGeoMaxLatitude 是 Web Mercator 能表示的纬度上限，超出即无法投影；
	// MaxDashboardGeoLongitude 是经度上限，东西半球对称。
	DashboardGeoMaxLatitude  = 85.05112878
	MaxDashboardGeoLongitude = 180
	// MinDashboardGeoZoom / MaxDashboardGeoZoom 是初始缩放级别：0 是整个世界的概览。
	MinDashboardGeoZoom = 0
	MaxDashboardGeoZoom = 8
	// MinDashboardGeoMarkerSize / MaxDashboardGeoMarkerSize 是标记直径（世界单位）。
	MinDashboardGeoMarkerSize = 2
	MaxDashboardGeoMarkerSize = 64
	// MaxDashboardGeoTileURLBytes 限制可选 XYZ 底图模板长度。
	MaxDashboardGeoTileURLBytes = 512
)

// 图层沿用 Grafana 的命名：标记与热力两种。
var dashboardGeoLayers = []string{"markers", "heatmap"}

const (
	defaultDashboardGeoLayer      = "markers"
	defaultDashboardGeoMarkerSize = 8
)

// DashboardGeoLocation 是地点表的一条：把序列匹配到的节点或设备映射到经纬度。
type DashboardGeoLocation struct {
	// Match 匹配序列所在的节点 ID、节点名或设备 ID，匹配时忽略大小写。
	Match string `json:"match"`
	// Label 是标记的可读名称；空值表示沿用匹配串。
	Label string `json:"label,omitempty"`
	// Latitude / Longitude 必须同时给出，缺一不可。
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
}

// validateDashboardGeomap 校验地点表、图层、视口与底图模板；这些字段只对 geomap 生效，
// 其它面板类型带上任何一项都会被拒绝，避免保存出永远不会渲染的配置。
func validateDashboardGeomap(panel DashboardPanel) error {
	if panel.Type != "geomap" {
		if geoCarriesOptions(panel) {
			return fmt.Errorf("%w: geo map options are only available on geo map panels", ErrInvalidDashboard)
		}
		return nil
	}
	if len(panel.GeoLocations) == 0 {
		return fmt.Errorf("%w: geo map panels require at least one location", ErrInvalidDashboard)
	}
	if len(panel.GeoLocations) > MaxDashboardGeoLocations {
		return fmt.Errorf("%w: at most %d geo locations are allowed", ErrInvalidDashboard, MaxDashboardGeoLocations)
	}
	matches := make(map[string]bool, len(panel.GeoLocations))
	for _, location := range panel.GeoLocations {
		if err := validateDashboardGeoLocation(location, matches); err != nil {
			return err
		}
	}
	layer := panel.GeoLayer
	if layer == "" {
		layer = defaultDashboardGeoLayer
	}
	if !containsString(dashboardGeoLayers, layer) {
		return fmt.Errorf("%w: geo map layer must be markers or heatmap", ErrInvalidDashboard)
	}
	if err := validateDashboardGeoViewport(panel); err != nil {
		return err
	}
	if panel.GeoMarkerSize != nil && (*panel.GeoMarkerSize < MinDashboardGeoMarkerSize || *panel.GeoMarkerSize > MaxDashboardGeoMarkerSize) {
		return fmt.Errorf("%w: geo map marker size must be between %d and %d", ErrInvalidDashboard, MinDashboardGeoMarkerSize, MaxDashboardGeoMarkerSize)
	}
	return validateDashboardGeoTileURL(panel.GeoTileURL)
}

// geoCarriesOptions 列出所有对非 geo map 面板无意义的字段；新增面板字段时也要在这里登记。
func geoCarriesOptions(panel DashboardPanel) bool {
	return len(panel.GeoLocations) != 0 || panel.GeoLayer != "" || panel.GeoTileURL != "" ||
		panel.GeoZoom != nil || panel.GeoCenterLatitude != nil || panel.GeoCenterLongitude != nil ||
		panel.GeoMarkerSize != nil
}

// validateDashboardGeoLocation 校验单条地点：匹配串唯一且可读、坐标有限且在投影范围内。
func validateDashboardGeoLocation(location DashboardGeoLocation, matches map[string]bool) error {
	if !dashboardText(location.Match, MaxDashboardGeoMatchBytes) {
		return fmt.Errorf("%w: geo location match text is invalid", ErrInvalidDashboard)
	}
	key := strings.ToLower(strings.TrimSpace(location.Match))
	if matches[key] {
		return fmt.Errorf("%w: geo location matches must be unique", ErrInvalidDashboard)
	}
	matches[key] = true
	if location.Label != "" && !dashboardText(location.Label, MaxDashboardGeoLabelBytes) {
		return fmt.Errorf("%w: geo location label is invalid", ErrInvalidDashboard)
	}
	if location.Latitude == nil || location.Longitude == nil ||
		!isFinite(*location.Latitude) || !isFinite(*location.Longitude) {
		return fmt.Errorf("%w: geo location requires finite latitude and longitude", ErrInvalidDashboard)
	}
	if math.Abs(*location.Latitude) > DashboardGeoMaxLatitude || math.Abs(*location.Longitude) > MaxDashboardGeoLongitude {
		return fmt.Errorf("%w: geo location coordinates are out of range", ErrInvalidDashboard)
	}
	return nil
}

// validateDashboardGeoViewport 校验初始视口：缩放级别、中心点必须成对出现且在投影范围内。
func validateDashboardGeoViewport(panel DashboardPanel) error {
	if panel.GeoZoom != nil && (*panel.GeoZoom < MinDashboardGeoZoom || *panel.GeoZoom > MaxDashboardGeoZoom) {
		return fmt.Errorf("%w: geo map zoom must be between %d and %d", ErrInvalidDashboard, MinDashboardGeoZoom, MaxDashboardGeoZoom)
	}
	if (panel.GeoCenterLatitude == nil) != (panel.GeoCenterLongitude == nil) {
		return fmt.Errorf("%w: geo map center requires both latitude and longitude", ErrInvalidDashboard)
	}
	if panel.GeoCenterLatitude == nil {
		return nil
	}
	latitude, longitude := *panel.GeoCenterLatitude, *panel.GeoCenterLongitude
	if !isFinite(latitude) || !isFinite(longitude) ||
		math.Abs(latitude) > DashboardGeoMaxLatitude || math.Abs(longitude) > MaxDashboardGeoLongitude {
		return fmt.Errorf("%w: geo map center is out of range", ErrInvalidDashboard)
	}
	return nil
}

// validateDashboardGeoTileURL 只接受带 {z}/{x}/{y} 占位符的 http(s) XYZ 模板：
// 模板由浏览器直连底图服务，因此拒绝其它协议和未知占位符。
func validateDashboardGeoTileURL(raw string) error {
	if raw == "" {
		return nil
	}
	if len(raw) > MaxDashboardGeoTileURLBytes ||
		strings.IndexFunc(raw, unicode.IsSpace) != -1 || strings.IndexFunc(raw, unicode.IsControl) != -1 {
		return fmt.Errorf("%w: geo map tile template is too long or contains whitespace", ErrInvalidDashboard)
	}
	if !strings.HasPrefix(raw, "https://") && !strings.HasPrefix(raw, "http://") {
		return fmt.Errorf("%w: geo map tile template must use http or https", ErrInvalidDashboard)
	}
	rest := raw
	for _, token := range []string{"{z}", "{x}", "{y}"} {
		if !strings.Contains(rest, token) {
			return fmt.Errorf("%w: geo map tile template requires {z}, {x} and {y}", ErrInvalidDashboard)
		}
		rest = strings.ReplaceAll(rest, token, "")
	}
	if strings.ContainsAny(rest, "{}") {
		return fmt.Errorf("%w: geo map tile template has unknown placeholders", ErrInvalidDashboard)
	}
	return nil
}
