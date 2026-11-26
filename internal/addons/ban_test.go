package addons

import (
	"path/filepath"
	"testing"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"whatsbot-go/internal/core"
)

func TestBanCommands(t *testing.T) {
	if got := (Ban{}).Commands(); len(got) != 1 || got[0] != "ban" {
		t.Fatalf("comandos=%#v", got)
	}
}

func TestBanParseRequest(t *testing.T) {
	targets, minutos := banParseRequest([]string{"10"}, []string{"584141234567@s.whatsapp.net"}, "")
	if len(targets) != 1 || targets[0] != "584141234567@s.whatsapp.net" || minutos != 10 {
		t.Fatalf("menciones: %#v, %d", targets, minutos)
	}

	targets, minutos = banParseRequest([]string{"5"}, nil, "584222333444@s.whatsapp.net")
	if len(targets) != 1 || targets[0] != "584222333444@s.whatsapp.net" || minutos != 5 {
		t.Fatalf("citado: %#v, %d", targets, minutos)
	}

	targets, minutos = banParseRequest([]string{"0414-1234567", "30"}, nil, "")
	if len(targets) != 1 || targets[0] != "584141234567@s.whatsapp.net" || minutos != 30 {
		t.Fatalf("número: %#v, %d", targets, minutos)
	}

	if targets, _ := banParseRequest([]string{"hola"}, nil, ""); len(targets) != 0 {
		t.Fatalf("texto inválido no debe dar objetivo: %#v", targets)
	}
}

func TestBanJIDNormalize(t *testing.T) {
	casos := map[string]string{
		"584141234567@s.whatsapp.net":    "584141234567@s.whatsapp.net",
		"584141234567:12@s.whatsapp.net": "584141234567@s.whatsapp.net",
		"584141234567@lid":               "584141234567@s.whatsapp.net",
	}
	for entrada, want := range casos {
		if got := wagoJIDNormalize(entrada); got != want {
			t.Fatalf("wagoJIDNormalize(%q)=%q, esperaba %q", entrada, got, want)
		}
	}
}

func TestBanStoreRoundtrip(t *testing.T) {
	dir := t.TempDir()
	prev := banPath
	banPath = func() string { return filepath.Join(dir, "banlist.json") }
	t.Cleanup(func() { banPath = prev })

	lista := []banEntry{{ChatID: "1205@g.us", Number: "584141234567", FullJID: "584141234567@s.whatsapp.net", Until: 123}}
	if err := banWrite(lista); err != nil {
		t.Fatalf("banWrite: %v", err)
	}
	got := banRead()
	if len(got) != 1 || got[0].Number != "584141234567" || got[0].Until != 123 {
		t.Fatalf("roundtrip inesperado: %#v", got)
	}
}

func TestBanFueraDeGrupo(t *testing.T) {
	if err := (Ban{}).Handle(wagoCtx("58412345678")); err != nil {
		t.Fatalf("fuera de grupo debe ser silencioso: %v", err)
	}
}

func TestBanParseConservaLID(t *testing.T) {
	targets, _ := banParseRequest([]string{"5"}, []string{"182790133969133@lid"}, "")
	if len(targets) != 1 || targets[0] != "182790133969133@lid" {
		t.Fatalf("mención @lid=%#v", targets)
	}
	targets, _ = banParseRequest([]string{"5"}, nil, "182790133969133:12@lid")
	if len(targets) != 1 || targets[0] != "182790133969133@lid" {
		t.Fatalf("citado @lid con dispositivo=%#v", targets)
	}
}

func TestBanResuelveLIDContraGrupo(t *testing.T) {
	numero := types.NewJID("584141234567", types.DefaultUserServer)
	lid := types.NewJID("182790133969133", "lid")
	info := &types.GroupInfo{Participants: []types.GroupParticipant{{
		JID: numero, PhoneNumber: numero, LID: lid,
	}}}
	targets, minutos := banParseRequest([]string{"5"}, []string{lid.String()}, "")
	if minutos != 5 {
		t.Fatalf("minutos=%d", minutos)
	}
	resueltos := banResolveTargets(info, targets)
	if len(resueltos) != 1 || resueltos[0] != numero {
		t.Fatalf("objetivo resuelto=%#v, esperaba %v", resueltos, numero)
	}
}

func TestBanHandleExpulsaLIDResuelto(t *testing.T) {
	dir := t.TempDir()
	prevPath, prevUpdate, prevGroup := banPath, banUpdateFn, wagoGroupInfoFn
	banPath = func() string { return filepath.Join(dir, "banlist.json") }
	t.Setenv("OWNER_NUMBERS", "584111111111")
	t.Setenv("BOT_NUMBERS", "584999999999")

	numero := types.NewJID("584141234567", types.DefaultUserServer)
	lid := types.NewJID("182790133969133", "lid")
	bot := types.NewJID("584999999999", types.DefaultUserServer)
	info := &types.GroupInfo{Participants: []types.GroupParticipant{
		{JID: numero, PhoneNumber: numero, LID: lid},
		{JID: bot, PhoneNumber: bot, IsAdmin: true},
	}}
	wagoGroupInfoFn = func(*core.Context) (*types.GroupInfo, error) { return info, nil }

	var expulsados []types.JID
	banUpdateFn = func(_ *whatsmeow.Client, _ types.JID, jid types.JID, _ whatsmeow.ParticipantChange) error {
		expulsados = append(expulsados, jid)
		return nil
	}
	t.Cleanup(func() {
		banPath, banUpdateFn, wagoGroupInfoFn = prevPath, prevUpdate, prevGroup
	})

	ctx := wagoCtx("1205@g.us")
	ctx.Command = "ban"
	ctx.Args = []string{"5"}
	ctx.Client = &whatsmeow.Client{}
	ctx.Message.Info.Sender = types.NewJID("584111111111", types.DefaultUserServer)
	ctx.Message.Message = &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text:        proto.String("ban @lid"),
		ContextInfo: &waE2E.ContextInfo{MentionedJID: []string{lid.String()}},
	}}
	var reacciones []string
	ctx.ReactHook = func(e string) error { reacciones = append(reacciones, e); return nil }

	if err := (Ban{}).Handle(ctx); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(expulsados) != 1 || expulsados[0] != numero {
		t.Fatalf("expulsados=%#v, esperaba %v", expulsados, numero)
	}
	lista := banRead()
	if len(lista) != 1 || lista[0].FullJID != numero.String() {
		t.Fatalf("banlist=%#v", lista)
	}
	if len(reacciones) == 0 || reacciones[len(reacciones)-1] != "✅" {
		t.Fatalf("reacciones=%#v", reacciones)
	}
}
