package publictheme

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const SchemaVersion = 1

var colorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
var blockIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

type Appearance struct {
	Mode         string `json:"mode"`
	Primary      string `json:"primary"`
	Accent       string `json:"accent"`
	ContentWidth string `json:"contentWidth"`
	Radius       string `json:"radius"`
	Surface      string `json:"surface"`
	Density      string `json:"density"`
}

type Layout struct {
	MinHeight string `json:"minHeight"`
	Padding   string `json:"padding"`
	Columns   int    `json:"columns"`
	Gap       string `json:"gap"`
}

type Block struct {
	ID      string         `json:"id"`
	Type    string         `json:"type"`
	Enabled bool           `json:"enabled"`
	Layout  Layout         `json:"layout"`
	Props   map[string]any `json:"props"`
}

type Page struct {
	Blocks []Block `json:"blocks"`
}

type Document struct {
	SchemaVersion int        `json:"schemaVersion"`
	Revision      int        `json:"revision"`
	PresetID      string     `json:"presetId,omitempty"`
	Appearance    Appearance `json:"appearance"`
	Home          Page       `json:"home"`
	NodeDetail    Page       `json:"nodeDetail"`
}

var homeBlockTypes = map[string]struct{}{
	"hero": {}, "summary-stats": {}, "group-filter": {}, "node-collection": {}, "announcement": {}, "spacer": {},
}

var detailBlockTypes = map[string]struct{}{
	"node-header": {}, "system-info": {}, "metric-summary": {}, "metric-trends": {}, "network-probes": {}, "announcement": {}, "spacer": {},
}

func DefaultDocument() Document {
	return Document{
		SchemaVersion: SchemaVersion,
		Revision:      1,
		PresetID:      "minimal",
		Appearance: Appearance{
			Mode: "system", Primary: "#18181b", Accent: "#71717a",
			ContentWidth: "wide", Radius: "sm", Surface: "solid", Density: "compact",
		},
		Home: Page{Blocks: []Block{
			{ID: "summary", Type: "summary-stats", Enabled: true, Layout: Layout{MinHeight: "auto", Padding: "none", Columns: 4, Gap: "sm"}, Props: map[string]any{
				"variant": "minimal", "items": []any{"all", "online", "warning", "offline"},
			}},
			{ID: "groups", Type: "group-filter", Enabled: true, Layout: Layout{MinHeight: "auto", Padding: "none", Gap: "sm"}, Props: map[string]any{
				"variant": "minimal", "title": "全部", "description": "选择分组查看节点",
			}},
			{ID: "nodes", Type: "node-collection", Enabled: true, Layout: Layout{MinHeight: "auto", Padding: "none", Columns: 4, Gap: "sm"}, Props: map[string]any{
				"variant": "minimal", "view": "card", "title": "节点", "description": "点击节点查看指标详情",
				"fields": []any{"status", "cpu", "memory", "disk", "network"},
			}},
		}},
		NodeDetail: Page{Blocks: []Block{
			{ID: "header", Type: "node-header", Enabled: true, Layout: Layout{MinHeight: "auto", Padding: "none", Gap: "md"}},
			{ID: "overview", Type: "system-info", Enabled: true, Layout: Layout{MinHeight: "auto", Padding: "none", Gap: "md"}},
			{ID: "summary", Type: "metric-summary", Enabled: true, Layout: Layout{MinHeight: "auto", Padding: "none", Gap: "sm"}},
			{ID: "trends", Type: "metric-trends", Enabled: true, Layout: Layout{MinHeight: "auto", Padding: "none", Gap: "md"}, Props: map[string]any{
				"defaultRange": "24h", "charts": []any{"cpu", "memory", "disk", "network", "load"},
			}},
			{ID: "probes", Type: "network-probes", Enabled: true, Layout: Layout{MinHeight: "auto", Padding: "none", Gap: "md"}},
		}},
	}
}

// BeatDefaultDocument is the original blue-gray Beat theme. It remains a
// first-class preset so users can switch back without losing the layout.
func BeatDefaultDocument() Document {
	return Document{
		SchemaVersion: SchemaVersion,
		Revision:      1,
		PresetID:      "beat-default",
		Appearance: Appearance{
			Mode: "system", Primary: "#2563eb", Accent: "#0ea5e9",
			ContentWidth: "wide", Radius: "md", Surface: "gradient", Density: "comfortable",
		},
		Home: Page{Blocks: []Block{
			{ID: "hero", Type: "hero", Enabled: true, Layout: Layout{MinHeight: "auto", Padding: "lg", Gap: "md"}, Props: map[string]any{
				"eyebrow": "Node observatory", "title": "网络节点运行概览",
				"description": "一眼掌握节点在线状态与资源水位。选择任意节点，进入多值指标监控视图，查看 CPU、内存、磁盘、网络与系统负载趋势。",
			}},
			{ID: "summary", Type: "summary-stats", Enabled: true, Layout: Layout{MinHeight: "auto", Padding: "none", Columns: 4, Gap: "sm"}, Props: map[string]any{
				"items": []any{"all", "online", "warning", "offline"},
			}},
			{ID: "groups", Type: "group-filter", Enabled: true, Layout: Layout{MinHeight: "auto", Padding: "md", Gap: "sm"}, Props: map[string]any{
				"title": "按分组查看", "description": "选择分组，只看旗下节点",
			}},
			{ID: "nodes", Type: "node-collection", Enabled: true, Layout: Layout{MinHeight: "tall", Padding: "none", Columns: 3, Gap: "md"}, Props: map[string]any{
				"view": "card", "title": "节点列表", "description": "点击卡片查看完整指标监控",
				"fields": []any{"status", "cpu", "memory", "disk", "network"},
			}},
		}},
		NodeDetail: Page{Blocks: []Block{
			{ID: "header", Type: "node-header", Enabled: true, Layout: Layout{MinHeight: "auto", Padding: "none", Gap: "md"}},
			{ID: "overview", Type: "system-info", Enabled: true, Layout: Layout{MinHeight: "auto", Padding: "none", Gap: "md"}},
			{ID: "summary", Type: "metric-summary", Enabled: true, Layout: Layout{MinHeight: "auto", Padding: "none", Gap: "sm"}},
			{ID: "trends", Type: "metric-trends", Enabled: true, Layout: Layout{MinHeight: "auto", Padding: "none", Gap: "md"}, Props: map[string]any{
				"defaultRange": "24h", "charts": []any{"cpu", "memory", "disk", "network", "load"},
			}},
			{ID: "probes", Type: "network-probes", Enabled: true, Layout: Layout{MinHeight: "auto", Padding: "none", Gap: "md"}},
		}},
	}
}

func Parse(raw json.RawMessage) (Document, error) {
	if len(raw) == 0 {
		return Document{}, errors.New("主题配置不能为空")
	}
	var doc Document
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Document{}, errors.New("主题配置格式无效")
	}
	return Normalize(doc)
}

func Normalize(doc Document) (Document, error) {
	if doc.SchemaVersion == 0 {
		doc.SchemaVersion = SchemaVersion
	}
	if doc.SchemaVersion != SchemaVersion {
		return Document{}, fmt.Errorf("不支持的主题版本 %d", doc.SchemaVersion)
	}
	doc.PresetID = strings.TrimSpace(doc.PresetID)
	if doc.PresetID != "" && !blockIDPattern.MatchString(doc.PresetID) {
		return Document{}, errors.New("主题预设 ID 无效")
	}
	doc.Appearance = normalizeAppearance(doc.Appearance)
	if err := validateAppearance(doc.Appearance); err != nil {
		return Document{}, err
	}
	home, err := normalizePage(doc.Home, homeBlockTypes, "首页")
	if err != nil {
		return Document{}, err
	}
	detail, err := normalizePage(doc.NodeDetail, detailBlockTypes, "节点详情")
	if err != nil {
		return Document{}, err
	}
	doc.Home = home
	doc.NodeDetail = detail
	data, err := json.Marshal(doc)
	if err != nil {
		return Document{}, fmt.Errorf("主题配置编码失败: %w", err)
	}
	if len(data) > 128<<10 {
		return Document{}, errors.New("主题配置不能超过 128 KiB")
	}
	return doc, nil
}

func normalizeAppearance(value Appearance) Appearance {
	value.Mode = strings.TrimSpace(value.Mode)
	value.Primary = strings.TrimSpace(value.Primary)
	value.Accent = strings.TrimSpace(value.Accent)
	value.ContentWidth = strings.TrimSpace(value.ContentWidth)
	value.Radius = strings.TrimSpace(value.Radius)
	value.Surface = strings.TrimSpace(value.Surface)
	value.Density = strings.TrimSpace(value.Density)
	if value.Mode == "" {
		value.Mode = "system"
	}
	if value.Primary == "" {
		value.Primary = "#2563eb"
	}
	if value.Accent == "" {
		value.Accent = "#0ea5e9"
	}
	if value.ContentWidth == "" {
		value.ContentWidth = "wide"
	}
	if value.Radius == "" {
		value.Radius = "md"
	}
	if value.Surface == "" {
		value.Surface = "gradient"
	}
	if value.Density == "" {
		value.Density = "comfortable"
	}
	return value
}

func validateAppearance(value Appearance) error {
	switch value.Mode {
	case "system", "light", "dark":
	default:
		return errors.New("主题明暗模式无效")
	}
	if !colorPattern.MatchString(value.Primary) || !colorPattern.MatchString(value.Accent) {
		return errors.New("主题颜色必须是 #RRGGBB 格式")
	}
	switch value.ContentWidth {
	case "standard", "wide", "full":
	default:
		return errors.New("内容宽度配置无效")
	}
	switch value.Radius {
	case "sm", "md", "lg":
	default:
		return errors.New("圆角配置无效")
	}
	switch value.Surface {
	case "solid", "gradient", "mesh":
	default:
		return errors.New("背景样式配置无效")
	}
	switch value.Density {
	case "compact", "comfortable", "spacious":
	default:
		return errors.New("内容密度配置无效")
	}
	return nil
}

func normalizePage(page Page, allowed map[string]struct{}, label string) (Page, error) {
	if len(page.Blocks) > 24 {
		return Page{}, fmt.Errorf("%s最多配置 24 个模块", label)
	}
	seen := make(map[string]struct{}, len(page.Blocks))
	blocks := make([]Block, 0, len(page.Blocks))
	for index, block := range page.Blocks {
		block.ID = strings.TrimSpace(block.ID)
		block.Type = strings.TrimSpace(block.Type)
		if !blockIDPattern.MatchString(block.ID) {
			return Page{}, fmt.Errorf("%s第 %d 个模块 ID 无效", label, index+1)
		}
		if _, ok := seen[block.ID]; ok {
			return Page{}, fmt.Errorf("%s模块 ID %q 重复", label, block.ID)
		}
		seen[block.ID] = struct{}{}
		if _, ok := allowed[block.Type]; !ok {
			return Page{}, fmt.Errorf("%s模块类型 %q 不受支持", label, block.Type)
		}
		block.Layout = normalizeLayout(block.Layout)
		if err := validateLayout(block.Layout); err != nil {
			return Page{}, fmt.Errorf("%s模块 %q: %w", label, block.ID, err)
		}
		if block.Props == nil {
			block.Props = map[string]any{}
		}
		propData, err := json.Marshal(block.Props)
		if err != nil || len(propData) > 16<<10 {
			return Page{}, fmt.Errorf("%s模块 %q 的配置无效或超过 16 KiB", label, block.ID)
		}
		blocks = append(blocks, block)
	}
	page.Blocks = blocks
	return page, nil
}

func normalizeLayout(value Layout) Layout {
	value.MinHeight = strings.TrimSpace(value.MinHeight)
	value.Padding = strings.TrimSpace(value.Padding)
	value.Gap = strings.TrimSpace(value.Gap)
	if value.MinHeight == "" {
		value.MinHeight = "auto"
	}
	if value.Padding == "" {
		value.Padding = "none"
	}
	if value.Gap == "" {
		value.Gap = "md"
	}
	return value
}

func validateLayout(value Layout) error {
	switch value.MinHeight {
	case "auto", "sm", "md", "lg", "tall", "viewport":
	default:
		return errors.New("模块高度配置无效")
	}
	switch value.Padding {
	case "none", "sm", "md", "lg":
	default:
		return errors.New("模块内边距配置无效")
	}
	switch value.Gap {
	case "sm", "md", "lg":
	default:
		return errors.New("模块间距配置无效")
	}
	if value.Columns < 0 || value.Columns > 4 {
		return errors.New("模块列数必须为 0 到 4")
	}
	return nil
}
