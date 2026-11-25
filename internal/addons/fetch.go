package addons

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"whatsbot-go/internal/core"
)

// Fetch porta addons/fetch.js: ejecuta fastfetch y lo renderiza como PNG.
type Fetch struct{}

// Commands cumple core.Addon.
func (Fetch) Commands() []string { return []string{"fetch"} }

const (
	fetchFontSize   = 15
	fetchPadding    = 25
	fetchBg         = "#15141b"
	fetchFg         = "#edecee"
	fetchMaxOutput  = 1 << 20
	fetchExecTimout = 10 * time.Second
)

var fetchPalette = []string{
	"#110f18", "#ff6767", "#61ffca", "#ffca85", "#a277ff", "#f694ff", "#61ffca", "#edecee",
	"#4d4d4d", "#ff6767", "#61ffca", "#ffca85", "#a277ff", "#f694ff", "#61ffca", "#edecee",
}

// Inyectables para pruebas.
var (
	fetchRun      = fetchRunFastfetch
	fetchFontPath = func() string { return core.DataPath("assets", "fonts", "HackNerdFont-Regular.ttf") }
)

// Handle genera la imagen y la envía con el caption minimalista original.
func (Fetch) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}

	if err := ctx.React("⏳"); err != nil {
		slog.Error("Error en addon .fetch:", "error", err)
		_ = ctx.React("❌")
		return nil
	}

	img, err := fetchGenerateImage()
	if err != nil {
		slog.Error("Error en addon .fetch:", "error", err)
		_ = ctx.React("❌")
		return nil
	}

	if err := ctx.SendImage(img, "🖥️"); err != nil {
		slog.Error("Error en addon .fetch:", "error", err)
		_ = ctx.React("❌")
		return nil
	}

	if err := ctx.React("✅"); err != nil {
		slog.Error("Error en addon .fetch:", "error", err)
	}
	return nil
}

// fetchRunFastfetch ejecuta fastfetch con TERM=xterm-256color y conserva la
// salida aunque el proceso termine con error (igual que e.stdout en Node).
func fetchRunFastfetch() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), fetchExecTimout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "fastfetch", "--pipe", "false")
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	out, err := cmd.Output()
	if len(out) > fetchMaxOutput {
		out = out[:fetchMaxOutput]
	}
	return string(out), err
}

func fetchGenerateImage() ([]byte, error) {
	stdout, _ := fetchRun()
	if strings.TrimSpace(stdout) == "" {
		stdout = "Error ejecutando fastfetch"
	}
	return fetchRender(stdout)
}

type fetchCell struct {
	char  rune
	color string
}

type fetchToken struct {
	kind string // ansi | newline | cr | text
	code string
	text string
}

var (
	fetchOSCRe   = regexp.MustCompile(`\x1b\][^\x07\x1b]*(\x07|\x1b\\)`)
	fetchTokenRe = regexp.MustCompile(`(\x1b\[[0-9;?]*[a-zA-Z])|(\n)|(\r)|([^\x1b\r\n]+)`)
	fetchAnsiRe  = regexp.MustCompile(`\x1b\[([0-9;?]*)([a-zA-Z])`)
)

func fetchTokenize(s string) []fetchToken {
	s = fetchOSCRe.ReplaceAllString(s, "")
	matches := fetchTokenRe.FindAllStringSubmatch(s, -1)
	out := make([]fetchToken, 0, len(matches))
	for _, m := range matches {
		switch {
		case m[1] != "":
			out = append(out, fetchToken{kind: "ansi", code: m[1]})
		case m[2] != "":
			out = append(out, fetchToken{kind: "newline"})
		case m[3] != "":
			out = append(out, fetchToken{kind: "cr"})
		case m[4] != "":
			out = append(out, fetchToken{kind: "text", text: m[4]})
		}
	}
	return out
}

func fetchLayout(stdout string) (map[int]map[int]fetchCell, int, int) {
	tokens := fetchTokenize(stdout)

	grid := make(map[int]map[int]fetchCell)
	cx, cy := 0, 0
	maxCx, maxCy := 0, 0
	currentFg := fetchFg
	bold := false
	savedCx, savedCy := 0, 0

	setRow := func(y int) map[int]fetchCell {
		row := grid[y]
		if row == nil {
			row = make(map[int]fetchCell)
			grid[y] = row
		}
		return row
	}

	for _, token := range tokens {
		switch token.kind {
		case "newline":
			cy++
			cx = 0
			if cy > maxCy {
				maxCy = cy
			}
		case "cr":
			cx = 0
		case "text":
			row := setRow(cy)
			for _, r := range token.text {
				row[cx] = fetchCell{char: r, color: currentFg}
				cx++
				if cx > maxCx {
					maxCx = cx
				}
			}
		case "ansi":
			m := fetchAnsiRe.FindStringSubmatch(token.code)
			if m == nil {
				continue
			}
			paramsStr, cmd := m[1], m[2]
			params := []int{0}
			if paramsStr != "" {
				params = nil
				for _, p := range strings.Split(paramsStr, ";") {
					n, _ := strconv.Atoi(p)
					params = append(params, n)
				}
			}

			switch cmd {
			case "m":
				for i := 0; i < len(params); i++ {
					c := params[i]
					switch {
					case c == 0:
						currentFg = fetchFg
						bold = false
					case c == 1:
						bold = true
					case c == 22:
						bold = false
					case c >= 30 && c <= 37:
						idx := c - 30
						if bold {
							idx += 8
						}
						currentFg = fetchPaletteAt(idx)
					case c >= 90 && c <= 97:
						currentFg = fetchPaletteAt(c - 90 + 8)
					case c == 38 && i+1 < len(params) && params[i+1] == 5:
						if i+2 < len(params) {
							currentFg = fetchPaletteAt(params[i+2])
						} else {
							currentFg = "#61ffca"
						}
						i += 2
					case c == 38 && i+1 < len(params) && params[i+1] == 2:
						if i+4 < len(params) {
							currentFg = fmt.Sprintf("rgb(%d,%d,%d)", params[i+2], params[i+3], params[i+4])
						}
						i += 4
					}
				}
			case "A":
				cy = maxInt(0, cy-fetchParamOr(paramsStr, 1))
			case "B":
				cy += fetchParamOr(paramsStr, 1)
				if cy > maxCy {
					maxCy = cy
				}
			case "C":
				cx += fetchParamOr(paramsStr, 1)
				if cx > maxCx {
					maxCx = cx
				}
			case "D":
				cx = maxInt(0, cx-fetchParamOr(paramsStr, 1))
			case "H", "f":
				row := 1
				col := 1
				if len(params) > 0 && params[0] > 0 {
					row = params[0]
				}
				if len(params) > 1 && params[1] > 0 {
					col = params[1]
				}
				cy = maxInt(0, row-1)
				cx = maxInt(0, col-1)
				if cy > maxCy {
					maxCy = cy
				}
				if cx > maxCx {
					maxCx = cx
				}
			case "s":
				savedCx, savedCy = cx, cy
			case "u":
				cx, cy = savedCx, savedCy
			case "K":
				n := 0
				if paramsStr != "" {
					n, _ = strconv.Atoi(paramsStr)
				}
				row := setRow(cy)
				switch n {
				case 0:
					for x := cx; x <= maxCx; x++ {
						delete(row, x)
					}
				case 1:
					for x := 0; x <= cx; x++ {
						delete(row, x)
					}
				case 2:
					grid[cy] = make(map[int]fetchCell)
				}
			}
		}
	}

	for maxCy >= 0 {
		row, ok := grid[maxCy]
		if !ok || len(row) == 0 {
			maxCy--
			continue
		}
		empty := true
		for _, cell := range row {
			if cell.char != 0 {
				empty = false
				break
			}
		}
		if !empty {
			break
		}
		maxCy--
	}

	return grid, maxCx, maxCy
}

func fetchPaletteAt(i int) string {
	if i < 0 || i >= len(fetchPalette) {
		return "#61ffca"
	}
	return fetchPalette[i]
}

func fetchParamOr(paramsStr string, def int) int {
	if paramsStr == "" {
		return def
	}
	n, err := strconv.Atoi(paramsStr)
	if err != nil {
		return def
	}
	return n
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func fetchRender(stdout string) ([]byte, error) {
	grid, maxCx, maxCy := fetchLayout(stdout)

	face, err := fetchLoadFace()
	if err != nil {
		return nil, err
	}

	charWidth := float64(font.MeasureString(face, "M")) / 64
	if charWidth <= 0 {
		charWidth = float64(fetchFontSize) * 0.6
	}
	lineHeight := int(math.Floor(float64(fetchFontSize) * 1.2))

	width := int(math.Ceil(float64(maxCx+1)*charWidth + float64(fetchPadding*2)))
	height := int(math.Ceil(float64(maxCy+1)*float64(lineHeight) + float64(fetchPadding*2)))
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	bg := fetchParseColor(fetchBg, color.RGBA{21, 20, 27, 255})
	draw.Draw(img, img.Bounds(), &image.Uniform{C: bg}, image.Point{}, draw.Src)

	ascent := face.Metrics().Ascent
	for y := 0; y <= maxCy; y++ {
		row := grid[y]
		if len(row) == 0 {
			continue
		}
		for x := 0; x <= maxCx; {
			cell, ok := row[x]
			if !ok {
				x++
				continue
			}
			startX := x
			runColor := cell.color
			var run []rune
			for x <= maxCx {
				c, exists := row[x]
				if !exists || c.color != runColor || c.char == 0 {
					break
				}
				run = append(run, c.char)
				x++
			}
			if len(run) == 0 {
				continue
			}
			dotX := fetchPadding + int(math.Round(float64(startX)*charWidth))
			dotY := fetchPadding + y*lineHeight + int(math.Round(float64(ascent)/64))
			d := &font.Drawer{
				Dst:  img,
				Src:  &image.Uniform{C: fetchParseColor(runColor, color.RGBA{237, 236, 238, 255})},
				Face: face,
				Dot:  fixed.P(dotX, dotY),
			}
			d.DrawString(string(run))
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func fetchLoadFace() (font.Face, error) {
	var data []byte
	if p, err := os.ReadFile(fetchFontPath()); err == nil {
		data = p
	}
	if len(data) == 0 {
		data = goregular.TTF
	}

	f, err := opentype.Parse(data)
	if err != nil {
		return nil, err
	}
	return opentype.NewFace(f, &opentype.FaceOptions{
		Size:    fetchFontSize,
		DPI:     72,
		Hinting: font.HintingFull,
	})
}

func fetchParseColor(s string, fallback color.RGBA) color.RGBA {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "#") {
		hex := strings.TrimPrefix(s, "#")
		if len(hex) == 6 {
			if n, err := strconv.ParseUint(hex, 16, 32); err == nil {
				return color.RGBA{uint8(n >> 16), uint8(n >> 8), uint8(n), 255}
			}
		}
	}
	if strings.HasPrefix(s, "rgb(") && strings.HasSuffix(s, ")") {
		parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(s, "rgb("), ")"), ",")
		if len(parts) >= 3 {
			r, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
			g, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
			b, _ := strconv.Atoi(strings.TrimSpace(parts[2]))
			return color.RGBA{uint8(r), uint8(g), uint8(b), 255}
		}
	}
	return fallback
}

func init() { core.Register(Fetch{}) }
