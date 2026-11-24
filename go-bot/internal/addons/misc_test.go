package addons

import (
	"errors"
	"strings"
	"testing"
	"time"

	"whatsbot-go/internal/core"
)

func TestMiscCommands(t *testing.T) {
	got := Misc{}.Commands()
	if len(got) != 2 || got[0] != "ping" || got[1] != "pong" {
		t.Fatalf("comandos inesperados: %#v", got)
	}
}

func TestMiscPing(t *testing.T) {
	prev := miscPingServer
	miscPingServer = func(*core.Context) (time.Duration, error) { return 42 * time.Millisecond, nil }
	t.Cleanup(func() { miscPingServer = prev })

	c := &capture{}
	if err := (Misc{}).Handle(c.newCtx("ping")); err != nil {
		t.Fatalf("Handle(ping) devolvió error: %v", err)
	}
	if len(c.texts) != 1 {
		t.Fatalf("esperaba 1 texto, obtuve %#v", c.texts)
	}
	for _, fragment := range []string{"🚀 *Ping a WhatsApp*", "⚡ Latencia: *42ms*"} {
		if !strings.Contains(c.texts[0], fragment) {
			t.Fatalf("falta %q en %q", fragment, c.texts[0])
		}
	}
	if len(c.reactions) != 2 || c.reactions[0] != "📡" || c.reactions[1] != "✅" {
		t.Fatalf("reacciones inesperadas: %#v", c.reactions)
	}
}

func TestMiscPong(t *testing.T) {
	c := &capture{}
	if err := (Misc{}).Handle(c.newCtx("pong")); err != nil {
		t.Fatalf("Handle(pong) devolvió error: %v", err)
	}
	if len(c.texts) != 2 || c.texts[0] != "🏓" {
		t.Fatalf("textos inesperados: %#v", c.texts)
	}
	if !strings.HasPrefix(c.texts[1], "🏓 pong: ") || !strings.HasSuffix(c.texts[1], "ms") {
		t.Fatalf("respuesta pong inesperada: %q", c.texts[1])
	}
	if len(c.reactions) != 0 {
		t.Fatalf("no esperaba reacciones, obtuve %#v", c.reactions)
	}
}

func TestMiscPingPropagaError(t *testing.T) {
	prev := miscPingServer
	boom := errors.New("sin red")
	miscPingServer = func(*core.Context) (time.Duration, error) { return 0, boom }
	t.Cleanup(func() { miscPingServer = prev })

	c := &capture{}
	if err := (Misc{}).Handle(c.newCtx("ping")); !errors.Is(err, boom) {
		t.Fatalf("esperaba el error de red, obtuve %v", err)
	}
	if len(c.reactions) != 1 || c.reactions[0] != "📡" {
		t.Fatalf("reacciones inesperadas: %#v", c.reactions)
	}
}

func TestMiscComandoDesconocido(t *testing.T) {
	c := &capture{}
	if err := (Misc{}).Handle(c.newCtx("otro")); err != nil {
		t.Fatalf("Handle(otro) devolvió error: %v", err)
	}
	if len(c.reactions) != 1 || c.reactions[0] != "❓" {
		t.Fatalf("esperaba ❓, obtuve %#v", c.reactions)
	}
}
