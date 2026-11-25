package addons

import (
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"

	"whatsbot-go/internal/core"
)

func TestAddCommands(t *testing.T) {
	if got := (Add{}).Commands(); len(got) != 1 || got[0] != "add" {
		t.Fatalf("comandos=%#v", got)
	}
}

func TestAddNumbersFromMessage(t *testing.T) {
	vcard := &waE2E.Message{ContactMessage: &waE2E.ContactMessage{
		Vcard: protoString("BEGIN:VCARD\nTEL;waid=584141234567:584141234567\nEND:VCARD"),
	}}
	got := addNumbersFromMessage(vcard)
	if len(got) != 1 || got[0] != "584141234567" {
		t.Fatalf("vcard=%#v", got)
	}

	texto := &waE2E.Message{Conversation: protoString("0414-1234567")}
	got = addNumbersFromMessage(texto)
	if len(got) != 1 || got[0] != "584141234567" {
		t.Fatalf("texto=%#v", got)
	}

	ext := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: protoString("hola")}}
	if len(addNumbersFromMessage(ext)) != 0 {
		t.Fatal("texto sin número no debe devolver nada")
	}
	if len(addNumbersFromMessage(nil)) != 0 {
		t.Fatal("nil no debe devolver nada")
	}
}

func TestAddFueraDeGrupo(t *testing.T) {
	ctx := wagoCtx("58412345678")
	if err := (Add{}).Handle(ctx); err != nil {
		t.Fatalf("fuera de grupo debe ser silencioso: %v", err)
	}
}

func TestAddPideNumeroSinCliente(t *testing.T) {
	t.Setenv("OWNER_NUMBERS", "58412345678")
	t.Setenv("BOT_NUMBERS", "584999999999")
	prev := wagoGroupInfoFn
	wagoGroupInfoFn = func(*core.Context) (*types.GroupInfo, error) {
		bot := types.NewJID("584999999999", types.DefaultUserServer)
		return &types.GroupInfo{Participants: []types.GroupParticipant{{JID: bot, IsAdmin: true}}}, nil
	}
	t.Cleanup(func() { wagoGroupInfoFn = prev })

	ctx := wagoCtx("1205@g.us")
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
	if err := (Add{}).Handle(ctx); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(textos) != 1 || !strings.Contains(textos[0], "Envía el contacto o escribe el número") {
		t.Fatalf("textos=%#v", textos)
	}
}
