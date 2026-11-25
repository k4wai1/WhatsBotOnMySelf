package addons

import (
	"bytes"
	"fmt"
	"image"
	"strings"

	"github.com/liyue201/goqr"

	"whatsbot-go/internal/core"
)

const (
	scanMaxBytes = 16 * 1024 * 1024
	scanMaxChars = 1500
)

// Scan porta addons/scan.js: lee el QR de una imagen.
type Scan struct{}

func init() { core.Register(Scan{}) }

func (Scan) Commands() []string { return []string{"scan"} }

var scanDecode = func(img image.Image) (string, error) {
	codes, err := goqr.Recognize(img)
	if err != nil {
		return "", err
	}
	for _, code := range codes {
		if code != nil && len(code.Payload) > 0 {
			return string(code.Payload), nil
		}
	}
	return "", nil
}

var scanScales = []int{1200, 800, 1600, 2000, 600, 2400, 0}

// scanDecodeBuffer replica decodeQrFromBuffer: prueba varias escalas y una
// variante en grises.
func scanDecodeBuffer(buffer []byte) string {
	img, _, err := image.Decode(bytes.NewReader(buffer))
	if err != nil {
		return ""
	}
	for _, scale := range scanScales {
		candidate := img
		if scale > 0 {
			candidate = qrFitInside(img, scale)
		}
		if data, err := scanDecode(candidate); err == nil && data != "" {
			return data
		}
	}
	gray := scanGrayscale(img)
	if data, err := scanDecode(gray); err == nil && data != "" {
		return data
	}
	return ""
}

func scanGrayscale(img image.Image) image.Image {
	b := img.Bounds()
	out := image.NewGray(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			out.Set(x, y, img.At(x, y))
		}
	}
	return out
}

func scanClean(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.ReplaceAll(text, "\x00", "")
	return strings.TrimSpace(text)
}

func (Scan) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}

	var buffer []byte
	var err error
	hasImage := false
	if ctx.Message != nil && ctx.Message.Message != nil && ctx.Message.Message.GetImageMessage() != nil {
		hasImage = true
		buffer, err = ctx.DownloadMedia()
	} else if quoted := ctx.QuotedMessage(); quoted != nil && quoted.GetImageMessage() != nil {
		hasImage = true
		buffer, err = ctx.DownloadQuoted()
	}
	if err != nil {
		return ctx.React("❌")
	}
	if !hasImage || len(buffer) == 0 {
		if reactErr := ctx.React("⚠️"); reactErr != nil {
			return reactErr
		}
		return ctx.Reply("Envía una imagen con el comando `.scan` o responde a una imagen con `.scan`.")
	}
	if len(buffer) > scanMaxBytes {
		if reactErr := ctx.React("⚠️"); reactErr != nil {
			return reactErr
		}
		return ctx.Reply("❌ Imagen demasiado grande (máx 16 MB).")
	}

	if reactErr := ctx.React("🔍"); reactErr != nil {
		return reactErr
	}
	decoded := scanDecodeBuffer(buffer)
	if decoded == "" {
		if reactErr := ctx.React("❌"); reactErr != nil {
			return reactErr
		}
		return ctx.Reply("No se encontró ningún QR legible en la imagen.")
	}

	if reactErr := ctx.React("✅"); reactErr != nil {
		return reactErr
	}
	clean := scanClean(decoded)
	if qrJSLength(clean) > scanMaxChars {
		clean = qrTruncate(clean, scanMaxChars) + "…"
	}
	if clean == "" {
		clean = "(vacío)"
	}
	return ctx.Reply(clean)
}

var _ = fmt.Sprintf
