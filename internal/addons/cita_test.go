package addons

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
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
