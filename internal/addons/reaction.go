package addons

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"whatsbot-go/internal/core"
	"whatsbot-go/internal/store"
)

type reactionSession struct {
	Reactions map[string]string
	JID       types.JID
	MsgID     string
	Timer     *time.Timer
}

var (
	reactionMu       sync.Mutex
	reactionSessions = map[string]*reactionSession{}
	reactionTimeout  = 60 * time.Second
	reactionClient   *whatsmeow.Client
)

// Reaction porta addons/reaction.js: sesión de 60s con resultado en vivo.
type Reaction struct{}

func init() {
	core.Register(Reaction{})
	core.RegisterBackground("reaction", reactionInit)
}

func (Reaction) Commands() []string { return []string{"reaction"} }

func (Reaction) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}
	if ctx.Client == nil {
		return ctx.React("❌")
	}
	resp, err := ctx.Client.SendMessage(context.Background(), ctx.ChatJID(),
		core.TextMessage("🪄 Reacciona a este mensaje 👇"))
	if err != nil {
		slog.Error("❌ [reaction] Error:", "error", err)
		return ctx.React("❌")
	}
	msgID := string(resp.ID)

	session := &reactionSession{
		Reactions: map[string]string{},
		JID:       ctx.ChatJID(),
		MsgID:     msgID,
	}
	session.Timer = time.AfterFunc(reactionTimeout, func() { reactionFinish(session) })

	reactionMu.Lock()
	reactionSessions[msgID] = session
	reactionMu.Unlock()
	return nil
}

func reactionInit(client *whatsmeow.Client, _ *store.Store) error {
	reactionClient = client
	if client == nil {
		return nil
	}
	client.AddEventHandler(func(evt any) {
		if msg, ok := evt.(*events.Message); ok && msg.Message != nil && msg.Message.GetReactionMessage() != nil {
			reactionHandle(msg)
		}
	})
	return nil
}

// reactionFormat arma el texto en vivo/final igual que reaction.js.
func reactionFormat(reactions map[string]string, final bool) string {
	if len(reactions) == 0 {
		if final {
			return "😐 Nadie reaccionó..."
		}
		return "🪄 Reacciona a este mensaje 👇"
	}
	users := make([]string, 0, len(reactions))
	for u := range reactions {
		users = append(users, u)
	}
	sort.Strings(users)
	lines := make([]string, 0, len(users))
	for _, u := range users {
		number := strings.Split(u, "@")[0]
		lines = append(lines, reactions[u]+" — "+number)
	}
	if final {
		return "✅ Reacción finalizada:\n" + strings.Join(lines, "\n")
	}
	return "💫 Reacciones actuales:\n" + strings.Join(lines, "\n")
}

func reactionHandle(msg *events.Message) {
	reaction := msg.Message.GetReactionMessage()
	key := reaction.GetKey()
	targetID := key.GetID()

	reactionMu.Lock()
	session, ok := reactionSessions[targetID]
	if !ok {
		reactionMu.Unlock()
		return
	}
	user := key.GetParticipant()
	if user == "" {
		user = key.GetRemoteJID()
	}
	if reaction.GetText() != "" {
		session.Reactions[user] = reaction.GetText()
	} else {
		delete(session.Reactions, user)
	}
	reactions := make(map[string]string, len(session.Reactions))
	for k, v := range session.Reactions {
		reactions[k] = v
	}
	jid := session.JID
	msgID := session.MsgID
	reactionMu.Unlock()

	reactionEdit(jid, msgID, reactionFormat(reactions, false))
}

func reactionFinish(session *reactionSession) {
	reactionMu.Lock()
	reactions := make(map[string]string, len(session.Reactions))
	for k, v := range session.Reactions {
		reactions[k] = v
	}
	delete(reactionSessions, session.MsgID)
	reactionMu.Unlock()

	reactionEdit(session.JID, session.MsgID, reactionFormat(reactions, true))
}

func reactionEdit(jid types.JID, msgID, text string) {
	if reactionClient == nil || msgID == "" {
		return
	}
	edit := reactionClient.BuildEdit(jid, types.MessageID(msgID), core.TextMessage(text))
	if _, err := reactionClient.SendMessage(context.Background(), jid, edit); err != nil {
		fmt.Printf("❌ [reaction] Error al editar: %v\n", err)
	}
}
