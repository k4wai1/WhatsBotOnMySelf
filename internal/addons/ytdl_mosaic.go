package addons

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/disintegration/imaging"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"whatsbot-go/internal/core"
)

const (
	ytdlMosaicWidth   = 1200
	ytdlMosaicHeight  = 800
	ytdlMosaicCols    = 3
	ytdlMosaicRows    = 2
	ytdlMosaicMargin  = 24
	ytdlMosaicGap     = 18
	ytdlMosaicPad     = 10
	ytdlMosaicQuality = 82
	ytdlMosaicTimeout = 5 * time.Second
)

var (
	ytdlMosaicBG      = color.RGBA{0x0f, 0x0f, 0x0f, 0xff}
	ytdlMosaicCard    = color.RGBA{0x18, 0x18, 0x18, 0xff}
	ytdlMosaicRed     = color.RGBA{0xff, 0x00, 0x00, 0xff}
	ytdlMosaicGray    = color.RGBA{0xaa, 0xaa, 0xaa, 0xff}
	ytdlMosaicWhite   = color.RGBA{0xff, 0xff, 0xff, 0xff}
	ytdlMosaicNoThumb = color.RGBA{0x21, 0x21, 0x21, 0xff}
)

type ytdlMosaicFontSource struct {
	parsed *opentype.Font
}

func ytdlMosaicFont() *ytdlMosaicFontSource {
	if raw, err := os.ReadFile(core.DataPath("firasanscondensed-book.otf")); err == nil {
		if parsed, err := opentype.Parse(raw); err == nil && parsed != nil {
			return &ytdlMosaicFontSource{parsed: parsed}
		}
	}
	if parsed, err := opentype.Parse(goregular.TTF); err == nil && parsed != nil {
		return &ytdlMosaicFontSource{parsed: parsed}
	}
	return nil
}

func (s *ytdlMosaicFontSource) face(size float64) (font.Face, func()) {
	if s != nil && s.parsed != nil {
		face, err := opentype.NewFace(s.parsed, &opentype.FaceOptions{
			Size: size, DPI: 72, Hinting: font.HintingFull,
		})
		if err == nil && face != nil {
			return face, func() { _ = face.Close() }
		}
	}
	return basicfont.Face7x13, func() {}
}

func ytdlBuildMosaic(results []ytdlSearchResult, httpClient *http.Client) ([]byte, error) {
	if len(results) == 0 {
		return nil, fmt.Errorf("ytdl: no hay resultados para generar el mosaico")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	count := len(results)
	if count > ytdlMosaicCols*ytdlMosaicRows {
		count = ytdlMosaicCols * ytdlMosaicRows
	}

	thumbs := make([]image.Image, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			thumbs[i] = ytdlMosaicFetch(results[i].Thumbnail, httpClient)
		}(i)
	}
	wg.Wait()

	cardW := (ytdlMosaicWidth - 2*ytdlMosaicMargin - (ytdlMosaicCols-1)*ytdlMosaicGap) / ytdlMosaicCols
	cardH := (ytdlMosaicHeight - 2*ytdlMosaicMargin - (ytdlMosaicRows-1)*ytdlMosaicGap) / ytdlMosaicRows
	thumbW := cardW - 2*ytdlMosaicPad
	thumbH := thumbW * 9 / 16

	canvas := image.NewRGBA(image.Rect(0, 0, ytdlMosaicWidth, ytdlMosaicHeight))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(ytdlMosaicBG), image.Point{}, draw.Src)

	source := ytdlMosaicFont()
	titleFace, closeTitle := source.face(22)
	defer closeTitle()
	channelFace, closeChannel := source.face(16)
	defer closeChannel()
	numFace, closeNum := source.face(18)
	defer closeNum()
	durationFace, closeDuration := source.face(15)
	defer closeDuration()
	placeholderFace, closePlaceholder := source.face(54)
	defer closePlaceholder()

	textWidth := fixed.Int26_6(thumbW << 6)
	for i := 0; i < count; i++ {
		cardX := ytdlMosaicMargin + (i%ytdlMosaicCols)*(cardW+ytdlMosaicGap)
		cardY := ytdlMosaicMargin + (i/ytdlMosaicCols)*(cardH+ytdlMosaicGap)
		ytdlMosaicRoundRect(canvas, image.Rect(cardX, cardY, cardX+cardW, cardY+cardH), 12, ytdlMosaicCard, draw.Src)

		thumbX := cardX + ytdlMosaicPad
		thumbY := cardY + ytdlMosaicPad
		thumbRect := image.Rect(thumbX, thumbY, thumbX+thumbW, thumbY+thumbH)
		if thumbs[i] == nil {
			draw.Draw(canvas, thumbRect, image.NewUniform(ytdlMosaicNoThumb), image.Point{}, draw.Src)
			ytdlMosaicDrawCentered(canvas, strconv.Itoa(i+1), thumbRect, placeholderFace, ytdlMosaicGray)
		} else {
			filled := imaging.Fill(thumbs[i], thumbW, thumbH, imaging.Center, imaging.Lanczos)
			draw.Draw(canvas, thumbRect, filled, image.Point{}, draw.Src)
		}

		ytdlMosaicDrawRank(canvas, thumbX+8, thumbY+8, i+1, numFace)
		ytdlMosaicDrawDuration(canvas, thumbRect, ytdlFmtDuration(results[i].Duration), durationFace)

		title := strings.TrimSpace(results[i].Title)
		if title == "" {
			title = "Sin título"
		}
		channel := strings.TrimSpace(results[i].Channel)
		if channel == "" {
			channel = "Desconocido"
		}

		textX := thumbX + 2
		titleY := thumbY + thumbH + 29
		titleLines := ytdlMosaicWrap(&font.Drawer{Face: titleFace}, title, textWidth, 2)
		for line, text := range titleLines {
			ytdlMosaicDrawText(canvas, text, textX, titleY+line*26, titleFace, ytdlMosaicWhite)
		}
		channelLines := ytdlMosaicWrap(&font.Drawer{Face: channelFace}, channel, textWidth, 1)
		if len(channelLines) > 0 {
			ytdlMosaicDrawText(canvas, channelLines[0], textX, titleY+2*26+2, channelFace, ytdlMosaicGray)
		}
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, canvas, &jpeg.Options{Quality: ytdlMosaicQuality}); err != nil {
		return nil, fmt.Errorf("ytdl: error al codificar el mosaico JPEG: %w", err)
	}
	return buf.Bytes(), nil
}

func ytdlMosaicFetch(url string, client *http.Client) image.Image {
	if strings.TrimSpace(url) == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), ytdlMosaicTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil
	}
	img, _, err := image.Decode(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil
	}
	return img
}

func ytdlMosaicRoundRect(img *image.RGBA, rect image.Rectangle, radius int, c color.Color, op draw.Op) {
	w, h := rect.Dx(), rect.Dy()
	if w <= 0 || h <= 0 {
		return
	}
	if radius > w/2 {
		radius = w / 2
	}
	if radius > h/2 {
		radius = h / 2
	}

	mask := image.NewAlpha(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			cx, cy := x, y
			if x < radius {
				cx = radius - 1
			} else if x >= w-radius {
				cx = w - radius
			}
			if y < radius {
				cy = radius - 1
			} else if y >= h-radius {
				cy = h - radius
			}
			inside := true
			if cx != x || cy != y {
				dx, dy := x-cx, y-cy
				inside = dx*dx+dy*dy <= radius*radius
			}
			if inside {
				mask.SetAlpha(x, y, color.Alpha{A: 255})
			}
		}
	}
	draw.DrawMask(img, rect, image.NewUniform(c), image.Point{}, mask, image.Point{}, op)
}

func ytdlMosaicDrawRank(img *image.RGBA, x, y, num int, face font.Face) {
	rect := image.Rect(x, y, x+36, y+28)
	ytdlMosaicRoundRect(img, rect, 14, ytdlMosaicRed, draw.Src)
	ytdlMosaicDrawCentered(img, strconv.Itoa(num), rect, face, ytdlMosaicWhite)
}

func ytdlMosaicDrawDuration(img *image.RGBA, thumb image.Rectangle, label string, face font.Face) {
	if label == "" {
		return
	}
	drawer := &font.Drawer{Face: face}
	textWidth := int(drawer.MeasureString(label) >> 6)
	bw := textWidth + 18
	bh := 24
	if bw > thumb.Dx()-16 {
		bw = thumb.Dx() - 16
	}
	if bw < bh {
		bw = bh
	}
	rect := image.Rect(thumb.Max.X-bw-8, thumb.Max.Y-bh-8, thumb.Max.X-8, thumb.Max.Y-8)
	ytdlMosaicRoundRect(img, rect, 6, color.RGBA{0x00, 0x00, 0x00, 0xb0}, draw.Over)
	ytdlMosaicDrawCentered(img, label, rect, face, ytdlMosaicWhite)
}

func ytdlMosaicDrawText(img *image.RGBA, text string, x, y int, face font.Face, c color.Color) {
	d := &font.Drawer{Dst: img, Src: image.NewUniform(c), Face: face, Dot: fixed.P(x, y)}
	d.DrawString(text)
}

func ytdlMosaicDrawCentered(img *image.RGBA, text string, rect image.Rectangle, face font.Face, c color.Color) {
	d := &font.Drawer{Dst: img, Src: image.NewUniform(c), Face: face}
	w := int(d.MeasureString(text) >> 6)
	metrics := face.Metrics()
	textH := int((metrics.Ascent + metrics.Descent) >> 6)
	d.Dot = fixed.P(rect.Min.X+(rect.Dx()-w)/2, rect.Min.Y+(rect.Dy()-textH)/2+int(metrics.Ascent>>6))
	d.DrawString(text)
}

func ytdlMosaicWrap(d *font.Drawer, text string, maxWidth fixed.Int26_6, maxLines int) []string {
	words := strings.Fields(text)
	if len(words) == 0 || maxLines <= 0 {
		return nil
	}
	runes := []rune(strings.Join(words, " "))
	lines := make([]string, 0, maxLines)
	for len(runes) > 0 && len(lines) < maxLines {
		last := len(lines) == maxLines-1
		n := 0
		for n < len(runes) && d.MeasureString(string(runes[:n+1])) <= maxWidth {
			n++
		}
		if n == 0 {
			n = 1
		}
		line := runes[:n]
		rest := runes[n:]
		if len(rest) > 0 && last {
			ellipsis := "…"
			limit := maxWidth - d.MeasureString(ellipsis)
			for len(line) > 0 && d.MeasureString(string(line)) > limit {
				line = line[:len(line)-1]
			}
			line = append(line, []rune(ellipsis)...)
			rest = nil
		}
		lines = append(lines, string(line))
		runes = rest
	}
	return lines
}
