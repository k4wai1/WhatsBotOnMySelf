package addons

import (
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
)

func TestIdCommands(t *testing.T) {
	if got := (Id{}).Commands(); len(got) != 1 || got[0] != "id" {
		t.Fatalf("comandos=%#v", got)
	}
}

func TestIdHandle(t *testing.T) {
	var textos []string
	ctx := wagoCtx("1205@g.us")
	ctx.SendHook = func(m *waE2E.Message) error {
		if m.GetConversation() != "" {
			textos = append(textos, m.GetConversation())
		}
		if m.GetExtendedTextMessage() != nil {
			textos = append(textos, m.GetExtendedTextMessage().GetText())
		}
		return nil
	}
	if err := (Id{}).Handle(ctx); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(textos) != 1 || !strings.Contains(textos[0], "1205@g.us") || !strings.Contains(textos[0], "Info del Chat") {
		t.Fatalf("textos=%#v", textos)
	}
}
