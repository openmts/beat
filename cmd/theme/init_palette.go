package main

// 脚手架只提供经过校验器验证的调色板：作者改色后应重新跑 `beat-theme pack` 确认对比度。
// 每个强调色调只替换主色一族（primary、ring、chart-1 与浅色 accent 面），其余表面沿用中性灰蓝。
type accentFamily struct {
	lightPrimary          string
	lightAccent           string
	lightAccentForeground string
	darkPrimary           string
}

var themeAccents = map[string]accentFamily{
	"blue":   {lightPrimary: "#2758B8", lightAccent: "#DFEAFE", lightAccentForeground: "#214785", darkPrimary: "#9CBDFC"},
	"teal":   {lightPrimary: "#10666A", lightAccent: "#D6EFF0", lightAccentForeground: "#0B4A4C", darkPrimary: "#72D5C5"},
	"amber":  {lightPrimary: "#8A5A0B", lightAccent: "#F7EAD2", lightAccentForeground: "#5E3C06", darkPrimary: "#E7C476"},
	"violet": {lightPrimary: "#5B3BB5", lightAccent: "#EAE3FA", lightAccentForeground: "#3F2A80", darkPrimary: "#C7ADF0"},
}

// accentNames 是 --accent 的允许取值，顺序即文档顺序。
var accentNames = []string{"blue", "teal", "amber", "violet"}

// initPalette 生成完整的两套调色板：先铺中性表面，再用强调色调覆盖主色一族。
func initPalette(accent string) (map[string]string, map[string]string, bool) {
	family, ok := themeAccents[accent]
	if !ok {
		return nil, nil, false
	}
	light := map[string]string{
		"background": "#F4F7FC", "foreground": "#182238", "card": "#FFFFFF", "card-foreground": "#182238",
		"popover": "#FFFFFF", "popover-foreground": "#182238", "primary": family.lightPrimary, "primary-foreground": "#FFFFFF",
		"secondary": "#E8EEF8", "secondary-foreground": "#24324B", "muted": "#EBEFF5", "muted-foreground": "#526077",
		"accent": family.lightAccent, "accent-foreground": family.lightAccentForeground, "destructive": "#B32636",
		"destructive-foreground": "#FFFFFF", "border": "#C9D4E5", "input": "#C9D4E5", "ring": family.lightPrimary,
		"chart-1": family.lightPrimary, "chart-2": "#087E7D", "chart-3": "#865D16", "chart-4": "#7950A3", "chart-5": "#B32636",
	}
	dark := map[string]string{
		"background": "#111927", "foreground": "#E9EFF9", "card": "#182337", "card-foreground": "#E9EFF9",
		"popover": "#182337", "popover-foreground": "#E9EFF9", "primary": family.darkPrimary, "primary-foreground": "#142340",
		"secondary": "#24334D", "secondary-foreground": "#D6E3F7", "muted": "#1F2D43", "muted-foreground": "#B4C3DA",
		"accent": "#293E61", "accent-foreground": "#DFEAFE", "destructive": "#FFA7B2", "destructive-foreground": "#43121C",
		"border": "#405571", "input": "#405571", "ring": family.darkPrimary, "chart-1": family.darkPrimary,
		"chart-2": "#72D5C5", "chart-3": "#E7C476", "chart-4": "#C7ADF0", "chart-5": "#FFA7B2",
	}
	return light, dark, true
}
