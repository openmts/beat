package theme

import (
	"encoding/hex"
	"math"
)

var paletteTokens = []string{
	"background", "foreground", "card", "card-foreground", "popover", "popover-foreground",
	"primary", "primary-foreground", "secondary", "secondary-foreground", "muted", "muted-foreground",
	"accent", "accent-foreground", "destructive", "destructive-foreground", "border", "input", "ring",
	"chart-1", "chart-2", "chart-3", "chart-4", "chart-5",
}

func PaletteTokens() []string { return append([]string(nil), paletteTokens...) }

func validatePalette(palette map[string]string) error {
	if len(palette) != len(paletteTokens) {
		return ErrManifestInvalid
	}
	luminances := make(map[string]float64, len(palette))
	for _, key := range paletteTokens {
		value, err := colorLuminance(palette[key])
		if err != nil {
			return err
		}
		luminances[key] = value
	}
	pairs := [][2]string{{"background", "foreground"}, {"card", "card-foreground"}, {"popover", "popover-foreground"},
		{"primary", "primary-foreground"}, {"secondary", "secondary-foreground"}, {"muted", "muted-foreground"},
		{"accent", "accent-foreground"}, {"destructive", "destructive-foreground"}}
	for _, pair := range pairs {
		if contrast(luminances[pair[0]], luminances[pair[1]]) < 4.5 {
			return ErrManifestInvalid
		}
	}
	for _, key := range []string{"ring", "chart-1", "chart-2", "chart-3", "chart-4", "chart-5"} {
		if contrast(luminances[key], luminances["card"]) < 3 {
			return ErrManifestInvalid
		}
	}
	return nil
}

func colorLuminance(color string) (float64, error) {
	if len(color) != 7 || color[0] != '#' {
		return 0, ErrManifestInvalid
	}
	decoded, err := hex.DecodeString(color[1:])
	if err != nil {
		return 0, ErrManifestInvalid
	}
	return linearColor(decoded[0])*0.2126 + linearColor(decoded[1])*0.7152 + linearColor(decoded[2])*0.0722, nil
}

func linearColor(value byte) float64 {
	normalized := float64(value) / 255
	if normalized <= 0.04045 {
		return normalized / 12.92
	}
	return math.Pow((normalized+0.055)/1.055, 2.4)
}

func contrast(first, second float64) float64 {
	return (math.Max(first, second) + 0.05) / (math.Min(first, second) + 0.05)
}
