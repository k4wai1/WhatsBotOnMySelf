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

func TestFelixCommands(t *testing.T) {
	if got := (Felix{}).Commands(); len(got) != 1 || got[0] != "felix" {
		t.Fatalf("comandos=%#v", got)
	}
}

func TestFelixSinAudio(t *testing.T) {
	dir := t.TempDir()
	core.SetRoot(dir)
	felixLimite = wagoNuevoLimite(4, 10*time.Minute)

	c := &capture{}
	if err := (Felix{}).Handle(c.newCtx("felix")); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "no está disponible") {
		t.Fatalf("textos=%#v", c.texts)
	}
}

func TestFelixEnviaAudio(t *testing.T) {
	dir := t.TempDir()
	core.SetRoot(dir)
	if err := os.MkdirAll(filepath.Join(dir, "assets", "audios"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "audios", "felix.mp3"), []byte("mp3"), 0o644); err != nil {
		t.Fatal(err)
	}
	felixLimite = wagoNuevoLimite(4, 10*time.Minute)

	var mimetype string
	ctx := wagoCtx("1205@g.us")
	ctx.SendHook = func(m *waE2E.Message) error {
		if m.GetAudioMessage() != nil {
			mimetype = m.GetAudioMessage().GetMimetype()
		}
		return nil
	}
	if err := (Felix{}).Handle(ctx); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if mimetype != "audio/mp4" {
		t.Fatalf("mimetype=%q", mimetype)
	}
}
