package publictheme

// Preset is a complete, Beat-maintained public theme. Presets intentionally
// contain the full document so the console never needs to assemble layout or
// appearance settings itself.
type Preset struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	IsDefault   bool     `json:"isDefault"`
	Document    Document `json:"document"`
}

func Presets() []Preset {
	minimal, _ := Normalize(DefaultDocument())
	beatDefault, _ := Normalize(BeatDefaultDocument())
	return []Preset{
		{
			ID:          "minimal",
			Name:        "极简",
			Description: "纯白底、黑灰主色与紧凑四列布局。弱化装饰，把注意力留给节点状态、资源水位和实时网络数据。",
			IsDefault:   true,
			Document:    minimal,
		},
		{
			ID:          "beat-default",
			Name:        "Beat 默认主题",
			Description: "清爽的蓝灰观测风格，突出节点状态、资源水位与多值指标趋势，适合日常巡检与值班大屏。",
			IsDefault:   false,
			Document:    beatDefault,
		},
	}
}

func PresetByID(id string) (Preset, bool) {
	for _, preset := range Presets() {
		if preset.ID == id {
			return preset, true
		}
	}
	return Preset{}, false
}
