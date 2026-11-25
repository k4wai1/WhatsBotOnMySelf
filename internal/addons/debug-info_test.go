package addons

import (
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"whatsbot-go/internal/core"
	"whatsbot-go/internal/store"
)

type debugInfoCapture struct {
	texts     []string
	reactions []string
}

func debugInfoNewCtx(c *debugInfoCapture, chatJID, senderJID, pushName string, isGroup bool, info *waE2E.ContextInfo, st *store.Store) *core.Context {
	chat, _ := types.ParseJID(chatJID)
	sender, _ := types.ParseJID(senderJID)
	msg := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("info")}}
	if info != nil {
		msg.ExtendedTextMessage.ContextInfo = info
	}
	ev := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: chat, Sender: sender, IsGroup: isGroup},
			ID:            "DBG1",
			PushName:      pushName,
		},
		Message: msg,
	}
	return &core.Context{
		Command: "info",
		Message: ev,
		Store:   st,
		SendHook: func(m *waE2E.Message) error {
			c.texts = append(c.texts, messageText(m))
			return nil
		},
		ReactHook: func(emoji string) error {
			c.reactions = append(c.reactions, emoji)
			return nil
		},
	}
}

func TestDebugInfoJIDEstandar(t *testing.T) {
	c := &debugInfoCapture{}
	ctx := debugInfoNewCtx(c, "584111@s.whatsapp.net", "584111@s.whatsapp.net", "Luis", false, nil, nil)
	if err := (DebugInfo{}).Handle(ctx); err != nil {
		t.Fatal(err)
	}
	if len(c.reactions) != 2 || c.reactions[0] != "📡" || c.reactions[1] != "✅" {
		t.Fatalf("reacciones inesperadas: %#v", c.reactions)
	}
	out := c.texts[0]
	for _, want := range []string{
		"🔍 *REPORTE DE EXTRACCIÓN DE DATOS*",
		"🎯 *ID Objetivo crudo:* 584111@s.whatsapp.net",
		"JID Estándar (Número Real)",
		"✅ No requirió conversión (Es público)",
		"✅ *Número Real:* 584111",
		"✅ *msg.pushName:* Luis",
		"❌ *Store Caché:* Indisponible.",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("reporte sin %q:\n%s", want, out)
		}
	}
}

func TestDebugInfoLIDResueltoPorStore(t *testing.T) {
	dir := t.TempDir()
	st := store.New(dir + "/cache.json")
	st.Upsert("584222@s.whatsapp.net", map[string]any{
		"id":     "584222@s.whatsapp.net",
		"lid":    "999000@lid",
		"notify": "Contacto LID",
	})

	info := &waE2E.ContextInfo{MentionedJID: []string{"999000@lid"}}
	c := &debugInfoCapture{}
	ctx := debugInfoNewCtx(c, "1203@g.us", "584222@s.whatsapp.net", "Luis", true, info, st)
	if err := (DebugInfo{}).Handle(ctx); err != nil {
		t.Fatal(err)
	}
	out := c.texts[0]
	for _, want := range []string{
		"LID (Identidad Oculta)",
		"✅ Resuelto vía Llave Maestra en Store",
		"✅ *Número Real:* 584222",
		"✅ *Store Caché:* Contacto LID",
		"Notificación",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("reporte sin %q:\n%s", want, out)
		}
	}
}

func TestDebugInfoCitadoParticipante(t *testing.T) {
	info := &waE2E.ContextInfo{
		Participant:   proto.String("584333@s.whatsapp.net"),
		QuotedMessage: &waE2E.Message{Conversation: proto.String("hola")},
	}
	c := &debugInfoCapture{}
	ctx := debugInfoNewCtx(c, "1204@g.us", "584444@s.whatsapp.net", "Luis", true, info, nil)
	if err := (DebugInfo{}).Handle(ctx); err != nil {
		t.Fatal(err)
	}
	out := c.texts[0]
	if !strings.Contains(out, "🎯 *ID Objetivo crudo:* 584333@s.whatsapp.net") {
		t.Fatalf("objetivo inesperado:\n%s", out)
	}
	if !strings.Contains(out, "❌ *msg.pushName:* Indisponible") {
		t.Fatalf("pushName no debía usarse:\n%s", out)
	}
}

func TestDebugInfoCommands(t *testing.T) {
	got := DebugInfo{}.Commands()
	if len(got) != 2 || got[0] != "info" || got[1] != "debug" {
		t.Fatalf("comandos inesperados: %#v", got)
	}
}
