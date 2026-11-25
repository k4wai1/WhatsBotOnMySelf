package addons

import (
	"bytes"
	"image/png"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"

	"whatsbot-go/internal/core"
)

func TestCortisolCommands(t *testing.T) {
	if got := (Cortisol{}).Commands(); len(got) != 1 || got[0] != "cor" {
		t.Fatalf("comandos=%#v", got)
	}
}

func TestCortisolPorcentaje(t *testing.T) {
	ahora := time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)
	a := cortisolPorcentaje("58412345678@s.whatsapp.net", ahora)
	b := cortisolPorcentaje("58412345678@s.whatsapp.net", ahora)
	if a != b || a < 0 || a > 100 {
		t.Fatalf("porcentaje=%d/%d", a, b)
	}
	otro := cortisolPorcentaje("58412345678@s.whatsapp.net", ahora.AddDate(0, 0, 1))
	_ = otro
}

func TestCortisolCaption(t *testing.T) {
	casos := map[int]string{
		10: "Modo Zen...",
		30: "Todo chill 😌",
		50: "Normal 🤔",
		70: "Alerta alta 😬",
		90: "Al máximo 🔥",
	}
	for pct, want := range casos {
		if got := cortisolCaption(pct); got != want {
			t.Fatalf("cortisolCaption(%d)=%q, esperaba %q", pct, got, want)
		}
	}
}

func TestCortisolImagen(t *testing.T) {
	data := cortisolGenerarImagen(50, "Ana")
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("png inválido: %v", err)
	}
	if img.Bounds().Dx() != 800 || img.Bounds().Dy() != 500 {
		t.Fatalf("tamaño=%v", img.Bounds())
	}
}

func TestCortisolHandle(t *testing.T) {
	prev := corAhora
	corAhora = func() time.Time { return time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { corAhora = prev })

	var caption string
	var reacciones []string
	ctx := wagoCtx("1205@g.us")
	ctx.SendHook = func(m *waE2E.Message) error {
		if m.GetImageMessage() != nil {
			caption = m.GetImageMessage().GetCaption()
		}
		return nil
	}
	ctx.ReactHook = func(e string) error { reacciones = append(reacciones, e); return nil }
	if err := (Cortisol{}).Handle(ctx); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(reacciones) != 1 || reacciones[0] != "🧠" {
		t.Fatalf("reacciones=%#v", reacciones)
	}
	if !strings.Contains(caption, "Usuario") || !strings.Contains(caption, "%") {
		t.Fatalf("caption=%q", caption)
	}
}

var _ = core.DataPath
