package addons

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"sync/atomic"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"whatsbot-go/internal/core"
)

func TestLoveCommands(t *testing.T) {
	if got := (Love{}).Commands(); len(got) != 1 || got[0] != "love" {
		t.Fatalf("comandos=%#v", got)
	}
}

func TestLovePorcentajeSimetrico(t *testing.T) {
	a := lovePorcentaje("a@s.whatsapp.net", "b@s.whatsapp.net")
	b := lovePorcentaje("b@s.whatsapp.net", "a@s.whatsapp.net")
	if a != b || a < 0 || a > 100 {
		t.Fatalf("porcentaje=%d/%d", a, b)
	}
}

func TestLoveCaption(t *testing.T) {
	casos := map[int]string{
		10:  "Esto está más frío",
		30:  "falta chispa",
		60:  "bonita pareja",
		90:  "Amor del bueno",
		100: "ALMAS GEMELAS",
	}
	for pct, want := range casos {
		if got := loveCaption(pct); !strings.Contains(got, want) {
			t.Fatalf("loveCaption(%d)=%q, esperaba %q", pct, got, want)
		}
	}
}

func TestLoveImagen(t *testing.T) {
	data := loveGenerarImagen(75, "Ana", "Luis")
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("png inválido: %v", err)
	}
	if img.Bounds().Dx() != 900 || img.Bounds().Dy() != 300 {
		t.Fatalf("tamaño=%v", img.Bounds())
	}
}

func TestLoveSinPareja(t *testing.T) {
	c := &capture{}
	ctx := wagoCtx("1205@g.us")
	ctx.Command = "love"
	ctx.SendHook = func(m *waE2E.Message) error { c.texts = append(c.texts, messageText(m)); return nil }
	if err := (Love{}).Handle(ctx); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "Menciona a alguien") {
		t.Fatalf("textos=%#v", c.texts)
	}
}

func TestLoveHandleMencion(t *testing.T) {
	var caption string
	var menciones []string
	ctx := wagoCtx("1205@g.us")
	ctx.Message.Message = &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text:        proto.String("love @584222"),
		ContextInfo: &waE2E.ContextInfo{MentionedJID: []string{"584222@s.whatsapp.net"}},
	}}
	ctx.Command = "love"
	ctx.SendHook = func(m *waE2E.Message) error {
		if m.GetImageMessage() != nil {
			caption = m.GetImageMessage().GetCaption()
			if m.GetImageMessage().GetContextInfo() != nil {
				menciones = m.GetImageMessage().GetContextInfo().GetMentionedJID()
			}
		}
		return nil
	}
	ctx.ReactHook = func(string) error { return nil }
	if err := (Love{}).Handle(ctx); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if !strings.Contains(caption, "%") {
		t.Fatalf("caption=%q", caption)
	}
	if len(menciones) != 2 {
		t.Fatalf("menciones=%#v", menciones)
	}
}

func loveImagenSolida(c color.NRGBA, size int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func lovePix(img image.Image, x, y int) color.NRGBA {
	return color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
}

func loveContarBlancos(img image.Image, x0, y0, x1, y1 int) int {
	n := 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			c := lovePix(img, x, y)
			if c.R > 240 && c.G > 240 && c.B > 240 && c.A > 200 {
				n++
			}
		}
	}
	return n
}

func TestLoveImagenConFotosMock(t *testing.T) {
	verde := loveImagenSolida(color.NRGBA{R: 0, G: 190, B: 70, A: 255}, 200)
	azul := loveImagenSolida(color.NRGBA{R: 30, G: 60, B: 220, A: 255}, 200)

	data := loveGenerarImagenConAvatares(75, "Ana", "Luis", verde, azul)
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("png inválido: %v", err)
	}
	if img.Bounds().Dx() != loveWidth || img.Bounds().Dy() != loveHeight {
		t.Fatalf("tamaño=%v", img.Bounds())
	}

	a1 := lovePix(img, 200, 150)
	if a1.G < 150 || a1.R > 80 {
		t.Fatalf("avatar izquierdo=%#v, esperaba verde", a1)
	}
	a2 := lovePix(img, 700, 150)
	if a2.B < 150 || a2.G > 120 {
		t.Fatalf("avatar derecho=%#v, esperaba azul", a2)
	}

	corazon := lovePix(img, 450, 225)
	if corazon.R < 200 || corazon.G > 90 || corazon.B < 100 {
		t.Fatalf("corazón=%#v, esperaba fucsia", corazon)
	}
	if n := loveContarBlancos(img, 400, 140, 500, 180); n < 20 {
		t.Fatalf("porcentaje no dibujado: solo %d píxeles blancos", n)
	}
}

func TestLoveFallbackInicial(t *testing.T) {
	data := loveGenerarImagen(75, "Ana", "~")
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("png inválido: %v", err)
	}

	enPaleta := false
	c := lovePix(img, 180, 180)
	for _, p := range lovePalette {
		if c.R == p.R && c.G == p.G && c.B == p.B {
			enPaleta = true
			break
		}
	}
	if !enPaleta {
		t.Fatalf("fallback=%#v, esperaba color vibrante de la paleta", c)
	}
	if n := loveContarBlancos(img, 170, 100, 230, 150); n < 5 {
		t.Fatalf("inicial 'A' no dibujada: %d píxeles blancos", n)
	}
	if n := loveContarBlancos(img, 670, 100, 730, 150); n < 5 {
		t.Fatalf("inicial '?' no dibujada: %d píxeles blancos", n)
	}
}

func TestLoveHandleConFotosMock(t *testing.T) {
	var llamadas atomic.Int32
	prev := loveFetchAvatar
	loveFetchAvatar = func(_ *core.Context, _ string) (image.Image, error) {
		llamadas.Add(1)
		return loveImagenSolida(color.NRGBA{R: 10, G: 190, B: 40, A: 255}, 200), nil
	}
	t.Cleanup(func() { loveFetchAvatar = prev })

	var caption string
	ctx := wagoCtx("1205@g.us")
	ctx.Command = "love"
	ctx.Message.Message = &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text:        proto.String("love @584222"),
		ContextInfo: &waE2E.ContextInfo{MentionedJID: []string{"584222@s.whatsapp.net"}},
	}}
	ctx.SendHook = func(m *waE2E.Message) error {
		if m.GetImageMessage() != nil {
			caption = m.GetImageMessage().GetCaption()
		}
		return nil
	}
	if err := (Love{}).Handle(ctx); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if llamadas.Load() != 2 {
		t.Fatalf("llamadas a loveFetchAvatar=%d, esperaba 2", llamadas.Load())
	}
	if !strings.Contains(caption, "%") {
		t.Fatalf("caption=%q", caption)
	}
}
