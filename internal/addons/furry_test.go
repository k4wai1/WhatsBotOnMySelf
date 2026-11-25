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

func TestFurryCommands(t *testing.T) {
	if got := (Furry{}).Commands(); len(got) != 1 || got[0] != "furry" {
		t.Fatalf("comandos=%#v", got)
	}
}

func TestFurrySinImagenes(t *testing.T) {
	dir := t.TempDir()
	core.SetRoot(dir)
	furryLimite = wagoNuevoLimite(4, 10*time.Minute)

	c := &capture{}
	if err := (Furry{}).Handle(c.newCtx("furry")); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "No hay imágenes") {
		t.Fatalf("textos=%#v", c.texts)
	}
}

func TestFurryEnviaImagen(t *testing.T) {
	dir := t.TempDir()
	core.SetRoot(dir)
	if err := os.MkdirAll(filepath.Join(dir, "assets", "furry"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "furry", "f.jpg"), []byte("jpg"), 0o644); err != nil {
		t.Fatal(err)
	}
	furryLimite = wagoNuevoLimite(4, 10*time.Minute)

	var caption string
	ctx := wagoCtx("1205@g.us")
	ctx.SendHook = func(m *waE2E.Message) error {
		if m.GetImageMessage() != nil {
			caption = m.GetImageMessage().GetCaption()
		}
		return nil
	}
	if err := (Furry{}).Handle(ctx); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if caption != "Que tal unos furros?" {
		t.Fatalf("caption=%q", caption)
	}
}
