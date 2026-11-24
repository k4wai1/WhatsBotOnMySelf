package addons

import (
	"errors"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"whatsbot-go/internal/core"
)

type imgCapture struct {
	msgs      []*waE2E.Message
	texts     []string
	reactions []string
}

func (c *imgCapture) ctx(withImage bool) *core.Context {
	msg := &waE2E.Message{Conversation: protoString("img")}
	if withImage {
		msg = &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Mimetype: protoString("image/jpeg")}}
	}
	return &core.Context{
		Command: "img",
		Message: &events.Message{
			Info:    types.MessageInfo{MessageSource: types.MessageSource{Chat: types.NewJID("1", types.DefaultUserServer)}},
			Message: msg,
		},
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

func protoString(s string) *string { return &s }

func TestImgCommands(t *testing.T) {
	got := Img{}.Commands()
	if len(got) != 2 || got[0] != "img" || got[1] != "vision" {
		t.Fatalf("comandos inesperados: %#v", got)
	}
}

func TestImgSinImagen(t *testing.T) {
	c := &imgCapture{}
	if err := (Img{}).Handle(c.ctx(false)); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.reactions) != 1 || c.reactions[0] != "❓" {
		t.Fatalf("reacciones=%#v", c.reactions)
	}
}

func TestImgDescribe(t *testing.T) {
	prevDownload, prevChat := imgDownload, imgChat
	imgDownload = func(*core.Context, bool) ([]byte, error) { return []byte("jpegdata"), nil }
	imgChat = func(dataURL, prompt string) (string, error) {
		if !strings.HasPrefix(dataURL, "data:") || prompt == "" {
			t.Fatalf("dataURL/prompt inesperados: %q / %q", dataURL, prompt)
		}
		return "Un gato naranja.", nil
	}
	t.Cleanup(func() { imgDownload, imgChat = prevDownload, prevChat })

	c := &imgCapture{}
	if err := (Img{}).Handle(c.ctx(true)); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.reactions) != 2 || c.reactions[0] != "👁️" || c.reactions[1] != "✅" {
		t.Fatalf("reacciones=%#v", c.reactions)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "gato naranja") {
		t.Fatalf("texto inesperado: %#v", c.texts)
	}
}

func TestImgFallaChat(t *testing.T) {
	prevDownload, prevChat := imgDownload, imgChat
	imgDownload = func(*core.Context, bool) ([]byte, error) { return []byte("jpegdata"), nil }
	imgChat = func(string, string) (string, error) { return "", errors.New("api caída") }
	t.Cleanup(func() { imgDownload, imgChat = prevDownload, prevChat })

	c := &imgCapture{}
	if err := (Img{}).Handle(c.ctx(true)); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.reactions) == 0 || c.reactions[len(c.reactions)-1] != "❌" {
		t.Fatalf("reacciones=%#v", c.reactions)
	}
}
