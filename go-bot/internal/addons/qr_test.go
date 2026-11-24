package addons

import (
	"errors"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"

	"whatsbot-go/internal/core"
)

type qrCapture struct {
	msgs      []*waE2E.Message
	texts     []string
	reactions []string
}

func (c *qrCapture) ctx(args ...string) *core.Context {
	return &core.Context{
		Command: "qr",
		Args:    args,
		SendHook: func(m *waE2E.Message) error {
			c.msgs = append(c.msgs, m)
			if m.GetConversation() != "" {
				c.texts = append(c.texts, m.GetConversation())
			}
			if m.GetExtendedTextMessage() != nil {
				c.texts = append(c.texts, m.GetExtendedTextMessage().GetText())
			}
			return nil
		},
		ReactHook: func(e string) error {
			c.reactions = append(c.reactions, e)
			return nil
		},
	}
}

func TestQrCommands(t *testing.T) {
	got := Qr{}.Commands()
	if len(got) != 1 || got[0] != "qr" {
		t.Fatalf("comandos inesperados: %#v", got)
	}
}

func TestQrGeneraImagen(t *testing.T) {
	prev := qrEncode
	qrEncode = func(text string) ([]byte, error) {
		if text != "hola mundo" {
			t.Fatalf("texto inesperado: %q", text)
		}
		return []byte("\x89PNG\r\n\x1a\nfake"), nil
	}
	t.Cleanup(func() { qrEncode = prev })

	c := &qrCapture{}
	if err := (Qr{}).Handle(c.ctx("hola", "mundo")); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.msgs) != 1 || c.msgs[0].GetImageMessage() == nil {
		t.Fatalf("esperaba una imagen: %#v", c.msgs)
	}
	caption := c.msgs[0].GetImageMessage().GetCaption()
	if !strings.Contains(caption, "QR de 10 caracteres") {
		t.Fatalf("caption inesperada: %q", caption)
	}
	if len(c.reactions) != 2 || c.reactions[0] != "⏳" || c.reactions[1] != "✅" {
		t.Fatalf("reacciones=%#v", c.reactions)
	}
}

func TestQrAyudaSinArgumentos(t *testing.T) {
	c := &qrCapture{}
	if err := (Qr{}).Handle(c.ctx()); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.reactions) != 1 || c.reactions[0] != "⚠️" {
		t.Fatalf("reacciones=%#v", c.reactions)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "Códigos QR") || !strings.Contains(c.texts[0], ".qr <texto o url>") {
		t.Fatalf("ayuda inesperada: %#v", c.texts)
	}
}

func TestQrTextoDemasiadoLargo(t *testing.T) {
	c := &qrCapture{}
	if err := (Qr{}).Handle(c.ctx(strings.Repeat("a", qrMaxText+1))); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.reactions) != 1 || c.reactions[0] != "⚠️" {
		t.Fatalf("reacciones=%#v", c.reactions)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "Máximo 1000 caracteres") {
		t.Fatalf("mensaje inesperado: %#v", c.texts)
	}
}

func TestQrFallaEncode(t *testing.T) {
	prev := qrEncode
	qrEncode = func(string) ([]byte, error) { return nil, errors.New("boom") }
	t.Cleanup(func() { qrEncode = prev })

	c := &qrCapture{}
	if err := (Qr{}).Handle(c.ctx("hola")); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.reactions) == 0 || c.reactions[len(c.reactions)-1] != "❌" {
		t.Fatalf("reacciones=%#v", c.reactions)
	}
}

func TestQrDecodeBufferReal(t *testing.T) {
	png, err := qrEncode("https://ejemplo.test/qr")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := qrDecodeBuffer(png)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got != "https://ejemplo.test/qr" {
		t.Fatalf("decodificado=%q", got)
	}
}

func TestQrJSLengthSurrogates(t *testing.T) {
	if n := qrJSLength("hola"); n != 4 {
		t.Fatalf("qrJSLength(hola)=%d", n)
	}
	// Un emoji fuera del BMP ocupa 2 unidades UTF-16, como en JS.
	if n := qrJSLength("😺"); n != 2 {
		t.Fatalf("qrJSLength(emoji)=%d", n)
	}
}
