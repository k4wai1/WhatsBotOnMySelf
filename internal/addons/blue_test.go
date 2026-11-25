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

func TestBlueCommands(t *testing.T) {
	if got := (Blue{}).Commands(); len(got) != 1 || got[0] != "blue" {
		t.Fatalf("comandos=%#v", got)
	}
}

func TestWagoLimite(t *testing.T) {
	l := wagoNuevoLimite(4, 10*time.Minute)
	ahora := time.Unix(1700000000, 0)
	l.ahora = func() time.Time { return ahora }
	for i := 0; i < 4; i++ {
		if !l.permitir("user") {
			t.Fatalf("uso %d debía permitirse", i+1)
		}
	}
	if l.permitir("user") {
		t.Fatal("el quinto uso debía bloquearse")
	}
	ahora = ahora.Add(11 * time.Minute)
	if !l.permitir("user") {
		t.Fatal("tras la ventana debía permitirse")
	}
}

func TestBlueSinImagenes(t *testing.T) {
	dir := t.TempDir()
	core.SetRoot(dir)
	blueLimite = wagoNuevoLimite(4, 10*time.Minute)

	c := &capture{}
	if err := (Blue{}).Handle(c.newCtx("blue")); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "No hay ejercicios") {
		t.Fatalf("textos=%#v", c.texts)
	}
}

func TestBlueEnviaImagen(t *testing.T) {
	dir := t.TempDir()
	core.SetRoot(dir)
	if err := os.MkdirAll(filepath.Join(dir, "assets", "blue"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "blue", "ejercicio.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	blueLimite = wagoNuevoLimite(4, 10*time.Minute)

	var caption string
	ctx := wagoCtx("1205@g.us")
	ctx.SendHook = func(m *waE2E.Message) error {
		if m.GetImageMessage() != nil {
			caption = m.GetImageMessage().GetCaption()
		}
		return nil
	}
	if err := (Blue{}).Handle(ctx); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if caption != "Unas integrales?" {
		t.Fatalf("caption=%q", caption)
	}
}
