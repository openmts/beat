package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"

	cli "github.com/urfave/cli/v3"

	"github.com/beat/backend/internal/theme"
)

const (
	initThemeFile    = "theme.json"
	initManifestFile = "beat-theme.json"
	initLicenseFile  = "license.json"
	initPreviewFile  = "preview.png"
	initPreviewWide  = 1280
	initPreviewHigh  = 720
)

// initLicenseText 是脚手架自带的 MIT 文本，作者可替换成自己选择的许可证。
const initLicenseText = `{
  "license": "MIT",
  "copyright": "Copyright (c) 2026 Your name",
  "text": "MIT License\n\nPermission is hereby granted, free of charge, to any person obtaining a copy of this theme and associated documentation files (the \"Software\"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:\n\nThe above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.\n\nTHE SOFTWARE IS PROVIDED \"AS IS\", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.\n"
}
`

type initOptions struct {
	name, id, output, overview, detail, density, font, accent string
	radius                                                    int
	force                                                     bool
}

// initScaffold 是脚手架内容：定义 JSON 与生成预览图需要的两个调色板取值。
type initScaffold struct {
	definition []byte
	background string
	headline   string
}

func initCommand(output io.Writer) *cli.Command {
	return &cli.Command{Name: "init", Usage: "scaffold a declarative theme that packs cleanly", Flags: []cli.Flag{
		&cli.StringFlag{Name: "name", Required: true, Usage: "display name, also the default id source"},
		&cli.StringFlag{Name: "output", Required: true, Usage: "destination directory, created when missing"},
		&cli.StringFlag{Name: "id", Usage: "theme id; defaults to beat-<slug of the name>"},
		&cli.StringFlag{Name: "overview", Value: "cards", Usage: "cards, table, tiles or list"},
		&cli.StringFlag{Name: "detail", Value: "balanced", Usage: "balanced, compact or spotlight"},
		&cli.StringFlag{Name: "density", Value: "comfortable", Usage: "comfortable or compact"},
		&cli.StringFlag{Name: "font", Usage: "system, serif or mono"},
		&cli.StringFlag{Name: "accent", Value: "blue", Usage: strings.Join(accentNames, ", ")},
		&cli.IntFlag{Name: "radius", Value: 6, Usage: "0-8, base corner radius in px"},
		&cli.BoolFlag{Name: "force", Usage: "write into a non-empty directory"},
	}, Action: func(_ context.Context, command *cli.Command) error {
		if command.Args().Len() != 0 {
			return fmt.Errorf("init does not accept positional arguments")
		}
		return runInit(initOptions{name: command.String("name"), id: command.String("id"), output: command.String("output"),
			overview: command.String("overview"), detail: command.String("detail"), density: command.String("density"),
			font: command.String("font"), accent: command.String("accent"), radius: command.Int("radius"), force: command.Bool("force")}, output)
	}}
}

func runInit(options initOptions, output io.Writer) error {
	if strings.TrimSpace(options.name) == "" || strings.TrimSpace(options.output) == "" {
		return fmt.Errorf("init needs a non-empty name and output")
	}
	scaffold, err := initDefinition(options)
	if err != nil {
		return err
	}
	preview, err := initPreview(scaffold.background, scaffold.headline)
	if err != nil {
		return err
	}
	files := map[string][]byte{initThemeFile: scaffold.definition, initManifestFile: initManifest(options),
		initLicenseFile: []byte(initLicenseText), initPreviewFile: preview}
	if err := initWriteDirectory(options, files); err != nil {
		return err
	}
	raw, err := theme.BuildArchive(theme.PackOptions{Source: options.output, Output: filepath.Join(options.output, "validation-only.zip")})
	if err != nil {
		return fmt.Errorf("scaffold failed the production validator: %w", err)
	}
	if err := describeArchive(output, raw); err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "created %d files in %s; next: beat-theme pack --source %s --output %s.theme.zip\n",
		len(files), options.output, options.output, options.output)
	return err
}

// initDefinition 生成完整的 theme.json：布局与调色板都由生产校验器接受的取值构成。
func initDefinition(options initOptions) (initScaffold, error) {
	light, dark, ok := initPalette(options.accent)
	if !ok {
		return initScaffold{}, fmt.Errorf("unknown accent %q, choose one of %s", options.accent, strings.Join(accentNames, ", "))
	}
	definition := map[string]any{"schema_version": 1, "overview": options.overview, "detail": options.detail,
		"density": options.density, "radius": options.radius, "light": light, "dark": dark}
	if options.font != "" {
		definition["font"] = options.font
	}
	raw, err := json.MarshalIndent(definition, "", "  ")
	if err != nil {
		return initScaffold{}, err
	}
	if _, err := theme.ParseDefinition(raw); err != nil {
		return initScaffold{}, fmt.Errorf("scaffold definition rejected: %w", err)
	}
	return initScaffold{definition: append(raw, '\n'), background: light["background"], headline: light["primary"]}, nil
}

func initManifest(options initOptions) []byte {
	id := options.id
	if id == "" {
		id = "beat-" + initSlug(options.name)
	}
	manifest := map[string]any{"schema_version": 1, "id": id, "name": options.name,
		"description": options.name + " theme scaffold.", "version": "1.0.0", "author": "Beat theme author",
		"license": "MIT", "runtime": "declarative", "minimum_beat_version": "0.1.0", "preview": initPreviewFile,
		"supported_locales": []string{"en"}}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil
	}
	return append(raw, '\n')
}

// initSlug 把显示名收敛成主题 ID 允许的字符集。
func initSlug(name string) string {
	var builder strings.Builder
	for _, character := range strings.ToLower(name) {
		switch {
		case character >= 'a' && character <= 'z' || character >= '0' && character <= '9':
			builder.WriteRune(character)
		case builder.Len() > 0 && !strings.HasSuffix(builder.String(), "-"):
			builder.WriteByte('-')
		}
	}
	slug := strings.Trim(builder.String(), "-")
	if slug == "" {
		slug = "theme"
	}
	if len(slug) > 40 {
		slug = strings.Trim(slug[:40], "-")
	}
	return slug
}

func initWriteDirectory(options initOptions, files map[string][]byte) error {
	entries, err := os.ReadDir(options.output)
	if err == nil && len(entries) > 0 && !options.force {
		return fmt.Errorf("%s already contains files; pass --force to overwrite", options.output)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(options.output, 0o700); err != nil {
		return err
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(options.output, name), content, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// initPreview 用调色板生成一张确定性的预览图；作者上线前应替换成真实截图。
func initPreview(background, headline string) ([]byte, error) {
	base := initColor(background, color.RGBA{R: 244, G: 247, B: 252, A: 255})
	accent := initColor(headline, color.RGBA{R: 39, G: 88, B: 184, A: 255})
	canvas := image.NewRGBA(image.Rect(0, 0, initPreviewWide, initPreviewHigh))
	for y := 0; y < initPreviewHigh; y++ {
		for x := 0; x < initPreviewWide; x++ {
			ratio := float64(x+y) / float64(initPreviewWide+initPreviewHigh)
			canvas.Set(x, y, initMix(base, accent, ratio*0.6))
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, canvas); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func initColor(value string, fallback color.RGBA) color.RGBA {
	if len(value) != 7 || value[0] != '#' {
		return fallback
	}
	decoded, err := hex.DecodeString(value[1:])
	if err != nil {
		return fallback
	}
	return color.RGBA{R: decoded[0], G: decoded[1], B: decoded[2], A: 255}
}

func initMix(from, to color.RGBA, ratio float64) color.RGBA {
	return color.RGBA{R: initBlend(from.R, to.R, ratio), G: initBlend(from.G, to.G, ratio), B: initBlend(from.B, to.B, ratio), A: 255}
}

func initBlend(left, right uint8, ratio float64) uint8 {
	return uint8(float64(left) + (float64(right)-float64(left))*ratio)
}
