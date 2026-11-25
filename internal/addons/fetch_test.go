package addons

import (
	"errors"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"

	"whatsbot-go/internal/core"
)

type fetchCapture struct {
	msgs      []*waE2E.Message
	reactions []string
}

func (c *fetchCapture) ctx() *core.Context {
	return &core.Context{
		Command: "fetch",
		SendHook: func(m *waE2E.Message) error {
			c.msgs = append(c.msgs, m)
			return nil
		},
		ReactHook: func(e string) error {
			c.reactions = append(c.reactions, e)
			return nil
		},
	}
}

func TestFetchCommands(t *testing.T) {
	got := Fetch{}.Commands()
	if len(got) != 1 || got[0] != "fetch" {
		t.Fatalf("comandos inesperados: %#v", got)
	}
}

func TestFetchConFastfetchRotoRenderizaIgual(t *testing.T) {
	// fetch.js cae a "Error ejecutando fastfetch" y aun así envía la tarjeta.
	font := filepath.Join("..", "..", "..", "assets", "fonts", "HackNerdFont-Regular.ttf")
	if _, err := os.Stat(font); err != nil {
		t.Skip("no está la fuente HackNerdFont en el repo")
	}
	prevRun, prevFont := fetchRun, fetchFontPath
	fetchRun = func() (string, error) { return "", errors.New("fastfetch: not found") }
	fetchFontPath = func() string { return font }
	t.Cleanup(func() { fetchRun, fetchFontPath = prevRun, prevFont })

	c := &fetchCapture{}
	if err := (Fetch{}).Handle(c.ctx()); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.msgs) != 1 || c.msgs[0].GetImageMessage() == nil {
		t.Fatalf("esperaba la tarjeta de error: %#v", c.msgs)
	}
}

func TestFetchGeneraImagen(t *testing.T) {
	font := filepath.Join("..", "..", "..", "assets", "fonts", "HackNerdFont-Regular.ttf")
	if _, err := os.Stat(font); err != nil {
		t.Skip("no está la fuente HackNerdFont en el repo")
	}

	prevRun, prevFont := fetchRun, fetchFontPath
	fetchRun = func() (string, error) {
		return "\x1b[32muser\x1b[0m@host\n\x1b[34mOS\x1b[0m: Linux\nUptime: 2 hours\n", nil
	}
	fetchFontPath = func() string { return font }
	t.Cleanup(func() { fetchRun, fetchFontPath = prevRun, prevFont })

	c := &fetchCapture{}
	if err := (Fetch{}).Handle(c.ctx()); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.msgs) != 1 || c.msgs[0].GetImageMessage() == nil {
		t.Fatalf("esperaba una imagen: %#v", c.msgs)
	}
	if !strings.Contains(c.msgs[0].GetImageMessage().GetCaption(), "🖥️") {
		t.Fatalf("caption inesperada: %q", c.msgs[0].GetImageMessage().GetCaption())
	}
	if len(c.reactions) != 2 || c.reactions[1] != "✅" {
		t.Fatalf("reacciones=%#v", c.reactions)
	}
}

func TestFetchTokenizeYSinAnsi(t *testing.T) {
	tokens := fetchTokenize("\x1b[31mhola\x1b[0m\nmundo")
	if len(tokens) == 0 {
		t.Fatal("el tokenizador no devolvió nada")
	}
	var texto strings.Builder
	for _, tok := range tokens {
		texto.WriteString(tok.text)
	}
	if !strings.Contains(texto.String(), "hola") || !strings.Contains(texto.String(), "mundo") {
		t.Fatalf("tokenizado inesperado: %q", texto.String())
	}
}

func TestFetchParseColor(t *testing.T) {
	got := fetchParseColor("#ff0000", color.RGBA{})
	if got.R != 255 || got.G != 0 || got.B != 0 {
		t.Fatalf("no parseó el color: %#v", got)
	}
}
