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
	"whatsbot-go/internal/store"
)

type perfilCapture struct {
	texts     []string
	reactions []string
	msgs      []*waE2E.Message
}

func perfilNewCtx(c *perfilCapture, jid string, fromMe bool, info *waE2E.ContextInfo, st *store.Store) *core.Context {
	chat, _ := types.ParseJID(jid)
	msg := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("perfil")}}
	if info != nil {
		msg.ExtendedTextMessage.ContextInfo = info
	}
	return &core.Context{
		Command: "pf",
		Message: &events.Message{
			Info: types.MessageInfo{
				MessageSource: types.MessageSource{Chat: chat, Sender: chat, IsFromMe: fromMe, IsGroup: strings.HasSuffix(jid, "@g.us")},
				ID:            "PP1",
			},
			Message: msg,
		},
		Store: st,
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

func perfilStub(t *testing.T, fn func(*whatsmeow.Client, types.JID, bool) (string, error), fetch func(string) ([]byte, error)) {
	t.Helper()
	prevURL := perfilProfileURL
	prevFetch := perfilFetch
	perfilProfileURL = fn
	if fetch != nil {
		perfilFetch = fetch
	}
	t.Cleanup(func() {
		perfilProfileURL = prevURL
		perfilFetch = prevFetch
	})
}

func TestPerfilSinFoto(t *testing.T) {
	perfilStub(t, func(*whatsmeow.Client, types.JID, bool) (string, error) {
		return "", errors.New("sin foto")
	}, nil)
	c := &perfilCapture{}
	if err := (Perfil{}).Handle(perfilNewCtx(c, "584111@s.whatsapp.net", false, nil, nil)); err != nil {
		t.Fatal(err)
	}
	if len(c.reactions) != 2 || c.reactions[0] != "⏳" || c.reactions[1] != "🤷‍♂️" {
		t.Fatalf("reacciones inesperadas: %#v", c.reactions)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "❌ El bot no tiene acceso a la foto de perfil") {
		t.Fatalf("texto inesperado: %#v", c.texts)
	}
}

func TestPerfilEnviaFoto(t *testing.T) {
	perfilStub(t, func(_ *whatsmeow.Client, jid types.JID, preview bool) (string, error) {
		if jid.String() == "584222@s.whatsapp.net" && !preview {
			return "https://example.test/p.jpg", nil
		}
		return "", errors.New("no")
	}, func(string) ([]byte, error) { return []byte("imagen"), nil })

	c := &perfilCapture{}
	info := &waE2E.ContextInfo{MentionedJID: []string{"584222@s.whatsapp.net"}}
	if err := (Perfil{}).Handle(perfilNewCtx(c, "1206@g.us", false, info, nil)); err != nil {
		t.Fatal(err)
	}
	if len(c.msgs) != 1 || c.msgs[0].GetImageMessage() == nil {
		t.Fatalf("esperaba imagen: %#v", c.msgs)
	}
	if c.msgs[0].GetImageMessage().GetCaption() != "📸" {
		t.Fatalf("caption inesperada: %q", c.msgs[0].GetImageMessage().GetCaption())
	}
	if len(c.reactions) != 2 || c.reactions[0] != "⏳" || c.reactions[1] != "✅" {
		t.Fatalf("reacciones: %#v", c.reactions)
	}
}

func TestPerfilResuelveLIDConStore(t *testing.T) {
	dir := t.TempDir()
	st := store.New(dir + "/cache.json")
	st.Upsert("999111@lid", map[string]any{"id": "999111@lid", "pn": "584333"})

	var consultados []string
	perfilStub(t, func(_ *whatsmeow.Client, jid types.JID, _ bool) (string, error) {
		consultados = append(consultados, jid.String())
		return "", errors.New("no")
	}, nil)

	c := &perfilCapture{}
	info := &waE2E.ContextInfo{MentionedJID: []string{"999111@lid"}}
	_ = (Perfil{}).Handle(perfilNewCtx(c, "1207@g.us", false, info, st))

	found := false
	for _, jid := range consultados {
		if jid == "584333@s.whatsapp.net" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no se resolvió LID→PN, consultas: %#v", consultados)
	}
}

func TestPerfilCommands(t *testing.T) {
	got := Perfil{}.Commands()
	if len(got) != 2 || got[0] != "pf" || got[1] != "perfil" {
		t.Fatalf("comandos inesperados: %#v", got)
	}
}
