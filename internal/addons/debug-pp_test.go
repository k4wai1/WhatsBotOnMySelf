package addons

import (
	"errors"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"whatsbot-go/internal/core"
)

type debugPpCapture struct {
	texts     []string
	reactions []string
	msgs      []*waE2E.Message
}

func debugPpNewCtx(c *debugPpCapture, jid string, info *waE2E.ContextInfo) *core.Context {
	chat, _ := types.ParseJID(jid)
	msg := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("getpp")}}
	if info != nil {
		msg.ExtendedTextMessage.ContextInfo = info
	}
	return &core.Context{
		Command: "getpp",
		Message: &events.Message{
			Info:    types.MessageInfo{MessageSource: types.MessageSource{Chat: chat, Sender: chat}, ID: "PP1"},
			Message: msg,
		},
		SendHook: func(m *waE2E.Message) error {
			c.msgs = append(c.msgs, m)
			c.texts = append(c.texts, messageText(m))
			return nil
		},
		ReactHook: func(emoji string) error {
			c.reactions = append(c.reactions, emoji)
			return nil
		},
	}
}

func debugPpStub(t *testing.T, urlFn func(*whatsmeow.Client, types.JID, bool) (string, error)) {
	t.Helper()
	prevURL := debugPpProfileURL
	prevFetch := debugPpFetch
	debugPpProfileURL = urlFn
	debugPpFetch = func(string) ([]byte, error) { return []byte("imagen"), nil }
	t.Cleanup(func() {
		debugPpProfileURL = prevURL
		debugPpFetch = prevFetch
	})
}

func TestDebugPpSinFoto(t *testing.T) {
	debugPpStub(t, func(*whatsmeow.Client, types.JID, bool) (string, error) {
		return "", errors.New("401 no autorizado")
	})
	c := &debugPpCapture{}
	ctx := debugPpNewCtx(c, "584111@s.whatsapp.net", nil)
	if err := (DebugPp{}).Handle(ctx); err != nil {
		t.Fatal(err)
	}
	if len(c.reactions) != 2 || c.reactions[0] != "📸" || c.reactions[1] != "✅" {
		t.Fatalf("reacciones inesperadas: %#v", c.reactions)
	}
	out := c.texts[0]
	for _, want := range []string{
		"🖼️ *REPORTE DE FOTO DE PERFIL*",
		"🎯 *Objetivo:* 584111@s.whatsapp.net",
		"❌ *Método 1 (Crudo/Alta):* 401 no autorizado",
		"❌ *Método 2 (Puro/Alta):* 401 no autorizado",
		"❌ *Método 3 (Puro/Baja):* Privacidad estricta o sin foto.",
		"💀 *Conclusión:*",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("reporte sin %q:\n%s", want, out)
		}
	}
}

func TestDebugPpConFoto(t *testing.T) {
	debugPpStub(t, func(_ *whatsmeow.Client, jid types.JID, preview bool) (string, error) {
		if jid.String() == "584222@s.whatsapp.net" && !preview {
			return "https://example.test/pp.jpg", nil
		}
		return "", errors.New("privacidad")
	})
	c := &debugPpCapture{}
	info := &waE2E.ContextInfo{MentionedJID: []string{"584222@s.whatsapp.net"}}
	if err := (DebugPp{}).Handle(debugPpNewCtx(c, "1205@g.us", info)); err != nil {
		t.Fatal(err)
	}
	if len(c.msgs) != 1 || c.msgs[0].GetImageMessage() == nil {
		t.Fatalf("esperaba una imagen: %#v", c.msgs)
	}
	caption := c.msgs[0].GetImageMessage().GetCaption()
	if !strings.Contains(caption, "✅ *Método 1 (Crudo/Alta):* ¡Éxito!") || !strings.Contains(caption, "🔗 *URL Obtenida:* https://example.test/pp.jpg") {
		t.Fatalf("caption inesperada:\n%s", caption)
	}
	if len(c.reactions) != 2 || c.reactions[1] != "✅" {
		t.Fatalf("reacciones: %#v", c.reactions)
	}
}

func TestDebugPpCommands(t *testing.T) {
	got := DebugPp{}.Commands()
	if len(got) != 2 || got[0] != "getpp" || got[1] != "debugpp" {
		t.Fatalf("comandos inesperados: %#v", got)
	}
}
