// qr.go porta addons/qr.js: generación y lectura de códigos QR.
package addons

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"strings"
	"unicode/utf16"

	"github.com/disintegration/imaging"
	"github.com/liyue201/goqr"
	qrcode "github.com/skip2/go-qrcode"
	_ "golang.org/x/image/webp"

	"whatsbot-go/internal/core"
)

const (
	qrMaxText       = 1000
	qrMaxImageBytes = 8 * 1024 * 1024
)

// qrEncode y qrDecode son inyectables para las pruebas.
var qrEncode = func(text string) ([]byte, error) {
	return qrcode.Encode(text, qrcode.Medium, 640)
}

var qrDecode = func(img image.Image) (string, error) {
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

// Qr porta el addon .qr.
type Qr struct{}

// Commands cumple core.Addon.
func (Qr) Commands() []string { return []string{"qr"} }

// Handle replica los dos modos del addon original.
func (q Qr) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}

	text := strings.TrimSpace(strings.Join(ctx.Args, " "))

	if text == "" {
		buffer, hasImage, err := qrSourceImage(ctx)
		if err != nil {
			return qrFail(ctx, err)
		}
		if !hasImage || len(buffer) == 0 {
			if reactErr := ctx.React("⚠️"); reactErr != nil {
				return reactErr
			}
			return ctx.Reply("📱 *Códigos QR*\n\n• `.qr <texto o url>` → genera un QR\n• Responde a una imagen con `.qr` → lee su contenido")
		}

		if len(buffer) > qrMaxImageBytes {
			if reactErr := ctx.React("⚠️"); reactErr != nil {
				return reactErr
			}
			return ctx.Reply("❌ Imagen demasiado grande (máx 8MB).")
		}

		if reactErr := ctx.React("🔍"); reactErr != nil {
			return reactErr
		}
		decoded, err := qrDecodeBuffer(buffer)
		if err != nil {
			return qrFail(ctx, err)
		}
		if decoded == "" {
			if reactErr := ctx.React("❌"); reactErr != nil {
				return reactErr
			}
			return ctx.Reply("No encontré ningún QR legible en esa imagen. Acércala, mejora la luz o mándala en mejor calidad.")
		}

		if reactErr := ctx.React("✅"); reactErr != nil {
			return reactErr
		}
		safe := decoded
		if qrJSLength(safe) > 1500 {
			safe = qrTruncate(safe, 1500) + "…"
		}
		return ctx.Reply("🔓 *Contenido del QR:*\n\n" + safe)
	}

	if qrJSLength(text) > qrMaxText {
		if reactErr := ctx.React("⚠️"); reactErr != nil {
			return reactErr
		}
		return ctx.Reply(fmt.Sprintf("❌ Máximo %d caracteres.", qrMaxText))
	}

	if reactErr := ctx.React("⏳"); reactErr != nil {
		return reactErr
	}
	png, err := qrEncode(text)
	if err != nil {
		return qrFail(ctx, err)
	}
	if err := ctx.SendImage(png, fmt.Sprintf("📱 QR de %d caracteres", qrJSLength(text))); err != nil {
		return qrFail(ctx, err)
	}
	return ctx.React("✅")
}

// qrSourceImage devuelve la imagen adjunta al mensaje o la citada.
func qrSourceImage(ctx *core.Context) ([]byte, bool, error) {
	if ctx.Message != nil && ctx.Message.Message != nil && ctx.Message.Message.GetImageMessage() != nil {
		data, err := ctx.DownloadMedia()
		return data, true, err
	}
	if quoted := ctx.QuotedMessage(); quoted != nil && quoted.GetImageMessage() != nil {
		data, err := ctx.DownloadQuoted()
		return data, true, err
	}
	return nil, false, nil
}

// qrDecodeBuffer replica decodeQrFromBuffer: prueba varias escalas.
func qrDecodeBuffer(buffer []byte) (string, error) {
	img, _, err := image.Decode(bytes.NewReader(buffer))
	if err != nil {
		return "", err
	}
	for _, width := range []int{800, 1200, 500, 1600} {
		scaled := qrFitInside(img, width)
		data, err := qrDecode(scaled)
		if err != nil {
			continue
		}
		if data != "" {
			return data, nil
		}
	}
	return "", nil
}

// qrFitInside replica resize({fit:'inside', withoutEnlargement:true}) sobre un
// cuadro cuadrado, preservando alfa (ensureAlpha).
func qrFitInside(img image.Image, side int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	maxDim := w
	if h > maxDim {
		maxDim = h
	}
	var out image.Image = img
	if maxDim > side && maxDim > 0 {
		scale := float64(side) / float64(maxDim)
		out = imaging.Resize(img, int(float64(w)*scale), int(float64(h)*scale), imaging.Lanczos)
	}
	rgba := image.NewNRGBA(image.Rect(0, 0, out.Bounds().Dx(), out.Bounds().Dy()))
	draw.Draw(rgba, rgba.Bounds(), out, out.Bounds().Min, draw.Src)
	return rgba
}

func qrFail(ctx *core.Context, err error) error {
	if err != nil {
		fmt.Printf("❌ [qr]: %v\n", err)
	}
	if reactErr := ctx.React("❌"); reactErr != nil {
		return reactErr
	}
	return nil
}

// qrJSLength imita String.length de JavaScript (unidades UTF-16).
func qrJSLength(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

func qrTruncate(s string, units int) string {
	var b strings.Builder
	used := 0
	for _, r := range s {
		rl := utf16.RuneLen(r)
		if used+rl > units {
			break
		}
		b.WriteRune(r)
		used += rl
	}
	return b.String()
}

func init() { core.Register(Qr{}) }
