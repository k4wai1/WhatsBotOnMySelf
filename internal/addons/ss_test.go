package addons

import (
	"errors"
	"os"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"

	"whatsbot-go/internal/core"
)

type ssCaptureCtx struct {
	msgs      []*waE2E.Message
	texts     []string
	reactions []string
}

func (c *ssCaptureCtx) ctx(args ...string) *core.Context {
	return &core.Context{
		Command: "ss",
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

func TestSsCommands(t *testing.T) {
	got := Ss{}.Commands()
	if len(got) != 2 || got[0] != "ss" || got[1] != "screenshot" {
		t.Fatalf("comandos inesperados: %#v", got)
	}
}

func TestSsNormalizeURL(t *testing.T) {
	casos := map[string]string{
		"google.com":          "https://google.com",
		"https://x.com/u":     "https://x.com/u",
		"HTTP://EJEMPLO.TEST": "http://EJEMPLO.TEST",
		"":                    "",
		"ftp://x.com":         "",
		"javascript:alert(1)": "",
	}
	for entrada, want := range casos {
		if got := ssNormalizeURL(entrada); got != want {
			t.Fatalf("ssNormalizeURL(%q)=%q, esperaba %q", entrada, got, want)
		}
	}
}

func TestSsAyudaSinURL(t *testing.T) {
	c := &ssCaptureCtx{}
	if err := (Ss{}).Handle(c.ctx()); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.reactions) != 1 || c.reactions[0] != "⚠️" {
		t.Fatalf("reacciones=%#v", c.reactions)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "Captura páginas web") {
		t.Fatalf("ayuda inesperada: %#v", c.texts)
	}
}

func TestSsCapturaYEnvia(t *testing.T) {
	prev := ssCapture
	var capturada string
	ssCapture = func(url, outPath string) error {
		capturada = url
		return os.WriteFile(outPath, make([]byte, ssMinBytes+10), 0o600)
	}
	t.Cleanup(func() { ssCapture = prev })

	c := &ssCaptureCtx{}
	if err := (Ss{}).Handle(c.ctx("example.com")); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if capturada != "https://example.com" {
		t.Fatalf("url capturada=%q", capturada)
	}
	if len(c.msgs) != 1 || c.msgs[0].GetImageMessage() == nil {
		t.Fatalf("esperaba imagen: %#v", c.msgs)
	}
	if got := c.msgs[0].GetImageMessage().GetCaption(); !strings.Contains(got, "https://example.com") {
		t.Fatalf("caption=%q", got)
	}
	if len(c.reactions) != 2 || c.reactions[len(c.reactions)-1] != "✅" {
		t.Fatalf("reacciones=%#v", c.reactions)
	}
}

func TestSsFallaCaptura(t *testing.T) {
	prev := ssCapture
	ssCapture = func(string, string) error { return errors.New("sin chrome") }
	t.Cleanup(func() { ssCapture = prev })

	c := &ssCaptureCtx{}
	if err := (Ss{}).Handle(c.ctx("example.com")); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.reactions) == 0 || c.reactions[len(c.reactions)-1] != "❌" {
		t.Fatalf("reacciones=%#v", c.reactions)
	}
}

func TestSsCapturaVacia(t *testing.T) {
	prev := ssCapture
	ssCapture = func(url, outPath string) error { return os.WriteFile(outPath, []byte("x"), 0o600) }
	t.Cleanup(func() { ssCapture = prev })

	c := &ssCaptureCtx{}
	if err := (Ss{}).Handle(c.ctx("example.com")); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.reactions) == 0 || c.reactions[len(c.reactions)-1] != "❌" {
		t.Fatalf("reacciones=%#v", c.reactions)
	}
}
