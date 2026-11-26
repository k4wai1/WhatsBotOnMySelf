package addons

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"whatsbot-go/internal/core"
)

func TestCitaCommands(t *testing.T) {
	got := Cita{}.Commands()
	if len(got) != 1 || got[0] != "cita" {
		t.Fatalf("comandos inesperados: %#v", got)
	}
}

func TestCitaParseArgs(t *testing.T) {
	v, h, color, clean := citaParseArgs([]string{"v", "hola", "c", "mundo"})
	if !v || h {
		t.Fatalf("orientación inesperada: v=%v h=%v", v, h)
	}
	if !color {
		t.Fatal("debía detectar la opción de color")
	}
	if len(clean) != 2 || clean[0] != "hola" || clean[1] != "mundo" {
		t.Fatalf("texto limpio inesperado: %#v", clean)
	}

	v, h, _, _ = citaParseArgs([]string{"horizontal", "texto"})
	if v || !h {
		t.Fatalf("orientación inesperada: v=%v h=%v", v, h)
	}

	v, h, _, _ = citaParseArgs([]string{"texto"})
	if v || h {
		t.Fatalf("sin flags no debe fijar orientación: v=%v h=%v", v, h)
	}
}

func TestCitaSinTexto(t *testing.T) {
	c := &capture{}
	ctx := &core.Context{
		Command: "cita",
		Args:    nil,
		Message: &events.Message{
			Info:    types.MessageInfo{MessageSource: types.MessageSource{Chat: types.NewJID("1", types.DefaultUserServer)}},
			Message: &waE2E.Message{Conversation: proto.String("cita")},
		},
		SendHook:  func(m *waE2E.Message) error { return nil },
		ReactHook: func(e string) error { c.reactions = append(c.reactions, e); return nil },
	}
	if err := (Cita{}).Handle(ctx); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.reactions) != 1 || c.reactions[0] != "⏳" {
		t.Fatalf("reacciones=%#v", c.reactions)
	}
}

func TestCitaGeneraImagen(t *testing.T) {
	avatar := func() []byte {
		img := image.NewRGBA(image.Rect(0, 0, 64, 64))
		img.Set(0, 0, color.RGBA{G: 255, A: 255})
		var buf bytes.Buffer
		_ = png.Encode(&buf, img)
		return buf.Bytes()
	}()

	prevAvatar, prevRand := citaAvatar, citaRand
	citaAvatar = func(*core.Context, string, string) ([]byte, error) { return avatar, nil }
	citaRand = func() float64 { return 0.9 }
	t.Cleanup(func() { citaAvatar, citaRand = prevAvatar, prevRand })

	var enviados []*waE2E.Message
	var reacciones []string
	ctx := &core.Context{
		Command: "cita",
		Args:    []string{"hola", "mundo"},
		Message: &events.Message{
			Info:    types.MessageInfo{MessageSource: types.MessageSource{Chat: types.NewJID("58412345678", types.DefaultUserServer)}},
			Message: &waE2E.Message{Conversation: proto.String("cita hola mundo")},
		},
		SendHook:  func(m *waE2E.Message) error { enviados = append(enviados, m); return nil },
		ReactHook: func(e string) error { reacciones = append(reacciones, e); return nil },
	}
	if err := (Cita{}).Handle(ctx); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(enviados) != 1 || enviados[0].GetImageMessage() == nil {
		t.Fatalf("esperaba una imagen: %#v", enviados)
	}
	if len(reacciones) < 2 || reacciones[len(reacciones)-1] != "✅" {
		t.Fatalf("reacciones=%#v", reacciones)
	}
}

func TestCitaCodigosTwemoji(t *testing.T) {
	casos := []struct {
		texto string
		code  string
	}{
		{"❌", "274c"},
		{"⚠️", "26a0"},
		{"👨‍👩‍👧", "1f468-200d-1f469-200d-1f467"},
		{"👍🏽", "1f44d-1f3fd"},
		{"🇺🇸", "1f1fa-1f1f8"},
		{"1️⃣", "0031-20e3"},
	}
	for _, caso := range casos {
		segs := citaSegments(caso.texto)
		if len(segs) != 1 || !segs[0].isEmoji || segs[0].code != caso.code {
			t.Errorf("%q → %#v; esperaba un emoji con código %q", caso.texto, segs, caso.code)
		}
	}
}

func TestCitaSegmentaTextoMixto(t *testing.T) {
	segs := citaSegments("❌ YouTube ⚠️")
	if len(segs) != 3 {
		t.Fatalf("segmentos inesperados: %#v", segs)
	}
	if !segs[0].isEmoji || segs[0].code != "274c" {
		t.Fatalf("primer segmento=%#v", segs[0])
	}
	if segs[1].isEmoji || segs[1].text != " YouTube " {
		t.Fatalf("segmento de texto=%#v", segs[1])
	}
	if strings.ContainsRune(segs[1].text, '\uFE0F') {
		t.Fatal("quedó un U+FE0F suelto en el texto")
	}
	if !segs[2].isEmoji || segs[2].code != "26a0" {
		t.Fatalf("último segmento=%#v", segs[2])
	}
}

func TestCitaBanderasConsecutivas(t *testing.T) {
	segs := citaSegments("🇺🇸🇯🇵")
	if len(segs) != 2 || segs[0].code != "1f1fa-1f1f8" || segs[1].code != "1f1ef-1f1f5" {
		t.Fatalf("banderas= %#v", segs)
	}
}

func TestCitaGeneraImagenConEmojis(t *testing.T) {
	avatar := func() []byte {
		img := image.NewRGBA(image.Rect(0, 0, 64, 64))
		img.Set(0, 0, color.RGBA{B: 255, A: 255})
		var buf bytes.Buffer
		_ = png.Encode(&buf, img)
		return buf.Bytes()
	}()

	emoji := image.NewNRGBA(image.Rect(0, 0, 72, 72))
	for y := 0; y < 72; y++ {
		for x := 0; x < 72; x++ {
			emoji.SetNRGBA(x, y, color.NRGBA{R: 255, A: 255})
		}
	}
	llamadas := map[string]int{}
	prevFetch := citaFetchEmoji
	citaFetchEmoji = func(code string) (image.Image, error) {
		llamadas[code]++
		return emoji, nil
	}
	t.Cleanup(func() { citaFetchEmoji = prevFetch })

	final, err := citaCompose("❌ YouTube ⚠️", "Luis 👨‍👩‍👧", "58412345678", avatar, false, true)
	if err != nil {
		t.Fatalf("citaCompose: %v", err)
	}
	for _, code := range []string{"274c", "26a0", "1f468-200d-1f469-200d-1f467"} {
		if llamadas[code] == 0 {
			t.Fatalf("no se solicitó Twemoji %q: %#v", code, llamadas)
		}
	}

	img, err := png.Decode(bytes.NewReader(final))
	if err != nil {
		t.Fatal(err)
	}
	rojos := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			if a > 0 && r > 0xC000 && g < 0x4000 && bl < 0x4000 {
				rojos++
			}
		}
	}
	if rojos == 0 {
		t.Fatal("el canvas no contiene píxeles del emoji (rojo)")
	}
}
