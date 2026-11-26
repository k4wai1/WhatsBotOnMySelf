package addons

import (
	"os"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"whatsbot-go/internal/core"
)

func wagoCtx(chat string, opts ...func(*core.Context)) *core.Context {
	server := types.DefaultUserServer
	switch {
	case strings.HasSuffix(chat, "@g.us"):
		server = types.GroupServer
	case strings.HasSuffix(chat, "@newsletter"):
		server = types.NewsletterServer
	}
	base := strings.SplitN(chat, "@", 2)[0]
	ctx := &core.Context{
		Command: "test",
		Message: &events.Message{
			Info: types.MessageInfo{MessageSource: types.MessageSource{
				Chat:   types.NewJID(base, server),
				Sender: types.NewJID("58412345678", types.DefaultUserServer),
			}},
			Message: &waE2E.Message{Conversation: proto.String("test")},
		},
		ReactHook: func(string) error { return nil },
		SendHook:  func(*waE2E.Message) error { return nil },
	}
	for _, opt := range opts {
		opt(ctx)
	}
	return ctx
}

func TestWagoNormalizeNumber(t *testing.T) {
	casos := map[string]string{
		"04141234567":     "584141234567",
		"4141234567":      "584141234567",
		"2121234567":      "582121234567",
		"+58 414-1234567": "584141234567",
		"123":             "",
		"abc":             "",
		"":                "",
	}
	for entrada, want := range casos {
		if got := wagoNormalizeNumber(entrada); got != want {
			t.Fatalf("wagoNormalizeNumber(%q)=%q, esperaba %q", entrada, got, want)
		}
	}
}

func TestWagoVCardNumbers(t *testing.T) {
	vcard := "BEGIN:VCARD\nTEL;waid=584141234567:584141234567\nEND:VCARD"
	got := wagoVCardNumbers(vcard)
	if len(got) != 1 || got[0] != "584141234567" {
		t.Fatalf("números=%#v", got)
	}
	if len(wagoVCardNumbers("sin numeros")) != 0 {
		t.Fatal("no debía extraer nada")
	}
}

func TestWagoOwnerNumbers(t *testing.T) {
	t.Setenv("OWNER_NUMBERS", "584141234567, +58 212-1234567")
	got := wagoOwnerNumbers()
	if len(got) != 2 || got[0] != "584141234567" || got[1] != "582121234567" {
		t.Fatalf("owners=%#v", got)
	}
	t.Setenv("OWNER_NUMBERS", "")
	if len(wagoOwnerNumbers()) != 0 {
		t.Fatal("sin env no debe haber dueños")
	}
}

func TestWagoIsAdminIn(t *testing.T) {
	admin := types.NewJID("584141234567", types.DefaultUserServer)
	info := &types.GroupInfo{Participants: []types.GroupParticipant{{JID: admin, IsAdmin: true}}}
	if !wagoIsAdminIn(info, admin) {
		t.Fatal("debía reconocer al admin")
	}
	otro := types.NewJID("584999999999", types.DefaultUserServer)
	if wagoIsAdminIn(info, otro) {
		t.Fatal("no debía reconocer a otro")
	}
	if wagoIsAdminIn(nil, admin) {
		t.Fatal("info nil no debe dar admin")
	}
}

func TestWagoBotIsAdmin(t *testing.T) {
	t.Setenv("BOT_NUMBERS", "584141234567")
	bot := types.NewJID("584141234567", types.DefaultUserServer)
	info := &types.GroupInfo{Participants: []types.GroupParticipant{{JID: bot, IsSuperAdmin: true}}}
	if !wagoBotIsAdmin(wagoCtx("1205@g.us"), info) {
		t.Fatal("debía detectar al bot admin")
	}
}

func TestWagoAppendLog(t *testing.T) {
	root := t.TempDir()
	core.SetRoot(root)
	wagoAppendLog("add.log", "línea de prueba")

	raw, err := os.ReadFile(root + "/assets/add.log")
	if err != nil {
		t.Fatalf("no se creó el log: %v", err)
	}
	if !strings.Contains(string(raw), "línea de prueba") {
		t.Fatalf("log inesperado: %q", string(raw))
	}
}

func TestWagoSameUser(t *testing.T) {
	base := types.NewJID("584141234567", types.DefaultUserServer)
	dispositivo := types.JID{User: "584141234567", Server: types.DefaultUserServer, Device: 12}
	if !wagoSameUser(base, dispositivo) {
		t.Fatal("el dispositivo :12 debe considerarse el mismo usuario")
	}
	if wagoSameUser(base, types.NewJID("182790133969133", "lid")) {
		t.Fatal("un @lid no debe compararse como el mismo usuario")
	}
	if wagoSameUser(types.EmptyJID, base) {
		t.Fatal("EmptyJID no debe coincidir")
	}
}

func TestWagoResolveParticipantLID(t *testing.T) {
	numero := types.NewJID("584141234567", types.DefaultUserServer)
	lid := types.NewJID("182790133969133", "lid")
	info := &types.GroupInfo{Participants: []types.GroupParticipant{{
		JID: numero, PhoneNumber: numero, LID: lid, IsAdmin: true,
	}}}

	jid, nro := wagoResolveParticipant(info, lid.String())
	if jid != numero || nro != "584141234567" {
		t.Fatalf("@lid resuelto=%v/%q, esperaba %v", jid, nro, numero)
	}

	jid, nro = wagoResolveParticipant(info, "584141234567:12@s.whatsapp.net")
	if jid != numero || nro != "584141234567" {
		t.Fatalf("dispositivo resuelto=%v/%q", jid, nro)
	}

	desconocido := types.NewJID("999999999999", "lid")
	jid, _ = wagoResolveParticipant(info, desconocido.String())
	if jid != desconocido {
		t.Fatalf("un @lid ajeno al grupo no debe convertirse: %v", jid)
	}
}

func TestWagoAdminConParticipantesLID(t *testing.T) {
	t.Setenv("OWNER_NUMBERS", "")
	t.Setenv("BOT_NUMBERS", "584999999999")
	numero := types.NewJID("584141234567", types.DefaultUserServer)
	lid := types.NewJID("182790133969133", "lid")
	bot := types.NewJID("584999999999", types.DefaultUserServer)
	botLID := types.NewJID("182790133969140", "lid")
	info := &types.GroupInfo{Participants: []types.GroupParticipant{
		{JID: numero, PhoneNumber: numero, LID: lid, IsAdmin: true},
		{JID: bot, PhoneNumber: bot, LID: botLID, IsAdmin: true},
	}}

	if !wagoIsAdminIn(info, lid) || !wagoIsAdminIn(info, numero) {
		t.Fatal("el admin debe reconocerse por @lid y por número")
	}
	ctx := wagoCtx("1205@g.us")
	ctx.Message.Info.Sender = lid
	ctx.Message.Info.SenderAlt = numero
	if !wagoSenderIsAdminOrOwner(ctx, info) {
		t.Fatal("el remitente @lid debe pasar la comprobación de admin")
	}
	if !wagoBotIsAdmin(ctx, info) {
		t.Fatal("el bot debe reconocerse como admin cuando el grupo informa su @lid")
	}
}

func TestWagoVCardNumbersTEL(t *testing.T) {
	vcard := "BEGIN:VCARD\nTEL;TYPE=CELL;waid=584141234567:+58 414-1234567\nEND:VCARD"
	got := wagoVCardNumbers(vcard)
	if len(got) != 1 || got[0] != "584141234567" {
		t.Fatalf("TEL/waid=%#v", got)
	}
	vcard = "BEGIN:VCARD\nTEL;TYPE=CELL:+58 212-1234567\nEND:VCARD"
	got = wagoVCardNumbers(vcard)
	if len(got) != 1 || got[0] != "582121234567" {
		t.Fatalf("solo TEL=%#v", got)
	}
}
