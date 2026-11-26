package addons

import (
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

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

func TestAddConsultaWhatsAppConMas(t *testing.T) {
	got := addWhatsAppQuery("58412345678")
	if len(got) != 1 || got[0] != "+58412345678" {
		t.Fatalf("consulta=%#v", got)
	}
}

func TestAddNumbersWrappersYArray(t *testing.T) {
	vcard1 := "BEGIN:VCARD\nTEL;waid=584141234567:+58 414-1234567\nEND:VCARD"
	vcard2 := "BEGIN:VCARD\nTEL;TYPE=CELL:+58 212-1234567\nEND:VCARD"
	array := &waE2E.Message{ContactsArrayMessage: &waE2E.ContactsArrayMessage{
		Contacts: []*waE2E.ContactMessage{
			{Vcard: protoString(vcard1)},
			{Vcard: protoString(vcard2)},
		},
	}}
	envuelto := &waE2E.Message{EphemeralMessage: &waE2E.FutureProofMessage{
		Message: &waE2E.Message{ViewOnceMessageV2: &waE2E.FutureProofMessage{Message: array}},
	}}
	got := addNumbersFromMessage(envuelto)
	if len(got) != 2 || got[0] != "584141234567" || got[1] != "582121234567" {
		t.Fatalf("contacts array=%#v", got)
	}

	disp := &waE2E.Message{DeviceSentMessage: &waE2E.DeviceSentMessage{
		Message: &waE2E.Message{ContactMessage: &waE2E.ContactMessage{Vcard: protoString(vcard1)}},
	}}
	got = addNumbersFromMessage(disp)
	if len(got) != 1 || got[0] != "584141234567" {
		t.Fatalf("device sent=%#v", got)
	}
}

func TestAddCollectDeviceYDifferenteLID(t *testing.T) {
	chat := types.NewJID("1205", types.GroupServer)
	lid := types.NewJID("182790133969133", "lid")
	numero := types.JID{User: "58412345678", Server: types.DefaultUserServer, Device: 12}
	collector := &addCollector{Chat: chat, Sender: lid, PromptID: "p1"}
	key := chat.String() + "|" + lid.String()
	addMu.Lock()
	addCollectors[key] = collector
	addMu.Unlock()
	t.Cleanup(func() {
		addMu.Lock()
		delete(addCollectors, key)
		addMu.Unlock()
	})

	vcard := "BEGIN:VCARD\nTEL;waid=584141234567:+58 414-1234567\nEND:VCARD"
	msg := &events.Message{
		Info: types.MessageInfo{MessageSource: types.MessageSource{
			Chat: chat, Sender: numero, SenderAlt: lid,
		}},
		Message: &waE2E.Message{ContactMessage: &waE2E.ContactMessage{Vcard: protoString(vcard)}},
	}
	addCollect(collector, nil, msg)
	if !collector.Collected {
		t.Fatal("el mensaje del mismo usuario con otro dispositivo debía recolectarse")
	}
	addMu.Lock()
	_, quedo := addCollectors[key]
	addMu.Unlock()
	if quedo {
		t.Fatal("el colector debía borrarse tras recolectar números válidos")
	}

	otro := &events.Message{
		Info: types.MessageInfo{MessageSource: types.MessageSource{
			Chat: chat, Sender: types.NewJID("584999999999", types.DefaultUserServer),
		}},
		Message: &waE2E.Message{ContactMessage: &waE2E.ContactMessage{Vcard: protoString(vcard)}},
	}
	segundo := &addCollector{Chat: chat, Sender: lid, PromptID: "p2"}
	addCollect(segundo, nil, otro)
	if segundo.Collected {
		t.Fatal("un remitente distinto no debe activar el colector")
	}
}
