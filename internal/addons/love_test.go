package addons

import (
	"bytes"
	"image/png"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
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
