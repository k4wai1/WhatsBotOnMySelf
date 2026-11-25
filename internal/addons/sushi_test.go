package addons

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"

	"whatsbot-go/internal/core"
)

func TestSushiCommands(t *testing.T) {
	if got := (Sushi{}).Commands(); len(got) != 1 || got[0] != "sushi" {
		t.Fatalf("comandos=%#v", got)
	}
}

func TestSushiSinImagenes(t *testing.T) {
	dir := t.TempDir()
	core.SetRoot(dir)
	sushiLimite = wagoNuevoLimite(4, 10*time.Minute)

	c := &capture{}
	if err := (Sushi{}).Handle(c.newCtx("sushi")); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "No hay imágenes de Sushi") {
		t.Fatalf("textos=%#v", c.texts)
	}
}

func TestSushiEnviaImagen(t *testing.T) {
	dir := t.TempDir()
	core.SetRoot(dir)
	if err := os.MkdirAll(filepath.Join(dir, "assets", "img_sushi"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "img_sushi", "s.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	sushiLimite = wagoNuevoLimite(4, 10*time.Minute)

	var caption string
	ctx := wagoCtx("1205@g.us")
	ctx.SendHook = func(m *waE2E.Message) error {
		if m.GetImageMessage() != nil {
			caption = m.GetImageMessage().GetCaption()
		}
		return nil
	}
	if err := (Sushi{}).Handle(ctx); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if caption != "Miau!" {
		t.Fatalf("caption=%q", caption)
	}
}
