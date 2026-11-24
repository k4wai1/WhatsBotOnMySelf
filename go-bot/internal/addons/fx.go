package addons

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"strings"

	"github.com/disintegration/imaging"
	_ "golang.org/x/image/webp"

	"whatsbot-go/internal/core"
)

func init() {
	core.Register(Fx{})
}

const fxMaxBytes = 15 * 1024 * 1024

var fxAyuda = []string{
	"🎨 *Efectos de imagen*",
	"",
	"Responde a una imagen o sticker con:",
	"• `.fx blur` — desenfoque",
	"• `.fx gris` — blanco y negro",
	"• `.fx sepia` — tono vintage",
	"• `.fx espejo` — reflejo horizontal",
}

// Fx porta addons/fx.js: .fx <blur|gris|sepia|espejo> y .efecto.
type Fx struct{}

// Commands cumple core.Addon.
func (Fx) Commands() []string { return []string{"fx", "efecto"} }

// Handle replica las validaciones y respuestas del addon original.
func (Fx) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}

	efecto := ""
	if len(ctx.Args) > 0 {
		efecto = strings.ToLower(ctx.Args[0])
	}
	if !fxEfectoValido(efecto) {
		if err := ctx.React("⚠️"); err != nil {
			return err
		}
		return ctx.SendQuoted(core.TextMessage(strings.Join(fxAyuda, "\n")), ctx.Message)
	}

	if err := ctx.React("🔄"); err != nil {
		return err
	}

	input, err := fxObtenerImagen(ctx)
	if err != nil || len(input) == 0 {
		if reactErr := ctx.React("❌"); reactErr != nil {
			return reactErr
		}
		return ctx.SendQuoted(core.TextMessage("No encontré imagen. Responde a una foto, imagen o sticker estático."), ctx.Message)
	}

	src, _, err := image.Decode(bytes.NewReader(input))
	if err != nil {
		if reactErr := ctx.React("❌"); reactErr != nil {
			return reactErr
		}
		return fmt.Errorf("fx: imagen inválida: %w", err)
	}

	out, err := fxAplicar(src, efecto)
	if err != nil {
		if reactErr := ctx.React("❌"); reactErr != nil {
			return reactErr
		}
		return err
	}

	var buf bytes.Buffer
	if err := imaging.Encode(&buf, out, imaging.PNG); err != nil {
		if reactErr := ctx.React("❌"); reactErr != nil {
			return reactErr
		}
		return err
	}

	if err := ctx.SendImage(buf.Bytes(), "✨ "+efecto); err != nil {
		return err
	}
	return ctx.React("✨")
}

func fxEfectoValido(efecto string) bool {
	switch efecto {
	case "blur", "gris", "sepia", "espejo":
		return true
	}
	return false
}

// fxObtenerImagen replica obtenerImagen(): imagen propia o imagen/sticker citado.
func fxObtenerImagen(ctx *core.Context) ([]byte, error) {
	var data []byte
	var err error
	if ctx.Message != nil && ctx.Message.Message != nil && ctx.Message.Message.GetImageMessage() != nil {
		data, err = ctx.DownloadMedia()
	} else {
		msg := ctx.QuotedMessage()
		if msg == nil || (msg.GetImageMessage() == nil && msg.GetStickerMessage() == nil) {
			return nil, nil
		}
		data, err = ctx.DownloadQuoted()
	}
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > fxMaxBytes {
		return nil, nil
	}
	return data, nil
}

// fxAplicar aplica el efecto con la misma semántica que sharp.
func fxAplicar(src image.Image, efecto string) (image.Image, error) {
	switch efecto {
	case "blur":
		return imaging.Blur(src, 8), nil
	case "gris":
		return imaging.Grayscale(src), nil
	case "sepia":
		gray := imaging.Grayscale(src)
		return imaging.AdjustFunc(gray, func(c color.NRGBA) color.NRGBA {
			g := uint16(c.R)
			c.R = uint8(g * 255 / 255)
			c.G = uint8(g * 220 / 255)
			c.B = uint8(g * 177 / 255)
			return c
		}), nil
	case "espejo":
		return imaging.FlipH(src), nil
	}
	return nil, fmt.Errorf("efecto desconocido: %s", efecto)
}
