package addons

import (
	"strings"
	"testing"
)

func TestReactionCommands(t *testing.T) {
	if got := (Reaction{}).Commands(); len(got) != 1 || got[0] != "reaction" {
		t.Fatalf("comandos=%#v", got)
	}
}

func TestReactionFormat(t *testing.T) {
	if got := reactionFormat(map[string]string{}, false); got != "🪄 Reacciona a este mensaje 👇" {
		t.Fatalf("inicial=%q", got)
	}
	if got := reactionFormat(map[string]string{}, true); got != "😐 Nadie reaccionó..." {
		t.Fatalf("final vacío=%q", got)
	}

	got := reactionFormat(map[string]string{
		"584111@s.whatsapp.net": "👍",
		"584222@s.whatsapp.net": "❤️",
	}, false)
	if !strings.Contains(got, "Reacciones actuales") || !strings.Contains(got, "👍 — 584111") || !strings.Contains(got, "❤️ — 584222") {
		t.Fatalf("en vivo=%q", got)
	}

	final := reactionFormat(map[string]string{"584111@s.whatsapp.net": "👍"}, true)
	if !strings.Contains(final, "Reacción finalizada") || !strings.Contains(final, "👍 — 584111") {
		t.Fatalf("final=%q", final)
	}
}

func TestReactionSinCliente(t *testing.T) {
	c := &capture{}
	if err := (Reaction{}).Handle(c.newCtx("reaction")); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.reactions) != 1 || c.reactions[0] != "❌" {
		t.Fatalf("reacciones=%#v", c.reactions)
	}
}
