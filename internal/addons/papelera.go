package addons

import (
	"context"
	"log/slog"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"whatsbot-go/internal/core"
	"whatsbot-go/internal/store"
)

// Papelera porta addons/papelera.js: si un mensaje del bot recibe la reacción
// 🗑 o 🚮, se elimina al instante. Sin comandos.
type Papelera struct{}

func init() { core.RegisterBackground("papelera", papeleraInit) }

func (Papelera) Commands() []string { return nil }

func (Papelera) Handle(*core.Context) error { return nil }

// papeleraEsBasura replica esPapelera(): ignora el selector de variación U+FE0F.
func papeleraEsBasura(texto string) bool {
	t := strings.ReplaceAll(texto, "\uFE0F", "")
	return t == "🗑" || t == "🚮"
}

// papeleraEsDelBot replica esMensajeDelBot().
func papeleraEsDelBot(key *waCommon.MessageKey, botNums []string) bool {
	if key == nil || key.GetID() == "" {
		return false
	}
	if key.GetFromMe() {
		if key.GetParticipant() == "" {
			return true
		}
		p := wagoDigits(strings.SplitN(key.GetParticipant(), "@", 2)[0])
		return p == "" || wagoContains(botNums, p)
	}
	author := ""
	if key.GetParticipant() != "" {
		author = wagoDigits(strings.SplitN(key.GetParticipant(), "@", 2)[0])
	} else {
		author = wagoDigits(strings.SplitN(key.GetRemoteJID(), "@", 2)[0])
	}
	return author != "" && wagoContains(botNums, author)
}

// papeleraProcesar maneja una reacción concreta (exportada para pruebas).
func papeleraProcesar(client *whatsmeow.Client, key *waCommon.MessageKey, texto string) {
	if client == nil || key == nil || !papeleraEsBasura(texto) {
		return
	}
	jid := key.GetRemoteJID()
	if jid == "" || strings.HasPrefix(jid, "status@") {
		return
	}
	if !papeleraEsDelBot(key, wagoBotNumbersForClient(client)) {
		return
	}
	chat, err := types.ParseJID(jid)
	if err != nil {
		return
	}
	delKey := &waCommon.MessageKey{
		RemoteJID: proto.String(jid),
		ID:        proto.String(key.GetID()),
		FromMe:    proto.Bool(true),
	}
	if chat.Server == types.GroupServer {
		participant := key.GetParticipant()
		if participant == "" && client.Store != nil && client.Store.ID != nil {
			participant = client.Store.ID.String()
		}
		if participant != "" {
			delKey.Participant = proto.String(participant)
		}
	}
	msg := &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
		Type: waE2E.ProtocolMessage_REVOKE.Enum(),
		Key:  delKey,
	}}
	if _, err := client.SendMessage(context.Background(), chat, msg); err != nil {
		slog.Error("🗑️ papelera: no se pudo borrar", "id", key.GetID(), "error", err)
	}
}

func papeleraInit(client *whatsmeow.Client, _ *store.Store) error {
	if client == nil {
		return nil
	}
	client.AddEventHandler(func(evt any) {
		msg, ok := evt.(*events.Message)
		if !ok || msg.Message == nil {
			return
		}
		reaction := msg.Message.GetReactionMessage()
		if reaction == nil {
			return
		}
		papeleraProcesar(client, reaction.GetKey(), reaction.GetText())
	})
	return nil
}
