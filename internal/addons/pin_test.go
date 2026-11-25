package addons

import (
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"

	"whatsbot-go/internal/core"
)

func TestPinCommands(t *testing.T) {
	got := (Pin{}).Commands()
	if len(got) != 2 || got[0] != "pin" || got[1] != "unpin" {
		t.Fatalf("comandos=%#v", got)
	}
}

func TestPinDuration(t *testing.T) {
	casos := []struct {
		args []string
		want int64
	}{
		{nil, 86400},
		{[]string{"1"}, 604800},
		{[]string{"2"}, 2592000},
		{[]string{"9"}, 86400},
	}
	for _, caso := range casos {
		if got := pinDuration(caso.args); got != caso.want {
			t.Fatalf("pinDuration(%v)=%d, esperaba %d", caso.args, got, caso.want)
		}
	}
}

func TestPinSinGrupo(t *testing.T) {
	ctx := wagoCtx("58412345678")
	ctx.Command = "pin"
	if err := (Pin{}).Handle(ctx); err != nil {
		t.Fatalf("fuera de grupos debe ser silencioso: %v", err)
	}
}

func TestPinNoAutorizado(t *testing.T) {
	t.Setenv("OWNER_NUMBERS", "")
	prev := wagoGroupInfoFn
	wagoGroupInfoFn = func(*core.Context) (*types.GroupInfo, error) { return &types.GroupInfo{}, nil }
	t.Cleanup(func() { wagoGroupInfoFn = prev })

	ctx := wagoCtx("1205@g.us")
	ctx.Command = "pin"
	var reacciones []string
	ctx.ReactHook = func(e string) error { reacciones = append(reacciones, e); return nil }

	if err := (Pin{}).Handle(ctx); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(reacciones) != 1 || reacciones[0] != "⛔" {
		t.Fatalf("reacciones=%#v", reacciones)
	}
}

func TestPinSinMensajeCitado(t *testing.T) {
	t.Setenv("OWNER_NUMBERS", "58412345678")
	prev := wagoGroupInfoFn
	wagoGroupInfoFn = func(*core.Context) (*types.GroupInfo, error) { return &types.GroupInfo{}, nil }
	t.Cleanup(func() { wagoGroupInfoFn = prev })

	ctx := wagoCtx("1205@g.us")
	ctx.Command = "pin"
	var textos []string
	ctx.SendHook = func(m *waE2E.Message) error {
		if m.GetConversation() != "" {
			textos = append(textos, m.GetConversation())
		}
		if m.GetExtendedTextMessage() != nil {
			textos = append(textos, m.GetExtendedTextMessage().GetText())
		}
		return nil
	}
	if err := (Pin{}).Handle(ctx); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(textos) != 1 || !strings.Contains(textos[0], "Debes responder al mensaje") {
		t.Fatalf("textos=%#v", textos)
	}
}
