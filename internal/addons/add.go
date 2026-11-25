package addons

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"whatsbot-go/internal/core"
)

// Add porta addons/add.js: agrega contactos al grupo con fallback a enlace.
type Add struct{}

func init() { core.Register(Add{}) }

func (Add) Commands() []string { return []string{"add"} }

var (
	addMu         sync.Mutex
	addCollectors = map[string]*addCollector{}
	addTimeout    = 30 * time.Second
	addClient     *whatsmeow.Client
	addOnWhatsApp = func(client *whatsmeow.Client, number string) (bool, types.JID) {
		if client == nil {
			return false, types.EmptyJID
		}
		resp, err := client.IsOnWhatsApp(context.Background(), []string{number + "@s.whatsapp.net"})
		if err != nil || len(resp) == 0 {
			return false, types.EmptyJID
		}
		return resp[0].IsIn, resp[0].JID
	}
)

type addCollector struct {
	Chat      types.JID
	Sender    types.JID
	PromptID  string
	Collected bool
	Handler   uint32
	Timer     *time.Timer
}

func (Add) Handle(ctx *core.Context) error {
	if ctx == nil || !wagoIsGroup(ctx) {
		return nil
	}
	info, err := wagoGroupInfo(ctx)
	if err != nil {
		slog.Error("❌ [add] Error:", "error", err)
		return ctx.React("❌")
	}
	if !wagoSenderIsAdminOrOwner(ctx, info) {
		return ctx.React("🤡")
	}
	if !wagoBotIsAdmin(ctx, info) {
		return ctx.Reply("❌ El bot necesita ser Admin para agregar personas.")
	}

	raw := strings.TrimSpace(strings.Join(ctx.Args, " "))
	if raw != "" {
		number := wagoNormalizeNumber(raw)
		if number != "" {
			return addWithFallback(ctx, number, ctx.Message.Info.ID, ctx.SenderJID())
		}
		return nil
	}

	if ctx.Client == nil {
		return ctx.Reply("📩 Envía el contacto o escribe el número (30s)...")
	}

	resp, err := ctx.Client.SendMessage(context.Background(), ctx.ChatJID(),
		core.TextMessage("📩 Envía el contacto o escribe el número (30s)..."))
	if err != nil {
		return ctx.React("❌")
	}

	collector := &addCollector{Chat: ctx.ChatJID(), Sender: ctx.SenderJID(), PromptID: string(resp.ID)}
	key := ctx.ChatJID().String() + "|" + ctx.SenderJID().String()
	collector.Handler = ctx.Client.AddEventHandler(func(evt any) {
		msg, ok := evt.(*events.Message)
		if !ok {
			return
		}
		addCollect(collector, ctx.Client, msg)
	})
	collector.Timer = time.AfterFunc(addTimeout, func() {
		addMu.Lock()
		collected := collector.Collected
		delete(addCollectors, key)
		addMu.Unlock()
		if !collected && ctx.Client != nil && collector.PromptID != "" {
			ctx.Client.SendMessage(context.Background(), collector.Chat, ctx.Client.BuildRevoke(collector.Chat, types.EmptyJID, types.MessageID(collector.PromptID)))
		}
		if ctx.Client != nil {
			ctx.Client.RemoveEventHandler(collector.Handler)
		}
	})

	addMu.Lock()
	addCollectors[key] = collector
	addMu.Unlock()
	return nil
}

func addCollect(collector *addCollector, client *whatsmeow.Client, msg *events.Message) {
	if msg == nil || msg.Message == nil || msg.Info.Chat != collector.Chat || msg.Info.Sender != collector.Sender {
		return
	}
	addMu.Lock()
	if collector.Collected {
		addMu.Unlock()
		return
	}
	addMu.Unlock()

	numbers := addNumbersFromMessage(msg.Message)
	if len(numbers) == 0 {
		return
	}
	collector.Collected = true
	if client != nil {
		client.RemoveEventHandler(collector.Handler)
	}

	if len(numbers) == 0 {
		return
	}
	// Nota: el contexto no está disponible aquí; se procesa en Handle cuando
	// llega el texto por vía normal. La recolección de vCards se resuelve con
	// el cliente directamente.
	addProcessNumbers(client, collector, numbers)
}

// addNumbersFromMessage extrae números de un contacto vCard o de un texto.
func addNumbersFromMessage(msg *waE2E.Message) []string {
	if msg == nil {
		return nil
	}
	if contact := msg.GetContactMessage(); contact != nil {
		return wagoVCardNumbers(contact.GetVcard())
	}
	text := msg.GetConversation()
	if text == "" && msg.GetExtendedTextMessage() != nil {
		text = msg.GetExtendedTextMessage().GetText()
	}
	if n := wagoNormalizeNumber(text); n != "" {
		return []string{n}
	}
	return nil
}

func addProcessNumbers(client *whatsmeow.Client, collector *addCollector, numbers []string) {
	for _, n := range numbers {
		if n == "" || client == nil {
			continue
		}
		jid := types.NewJID(n, types.DefaultUserServer)
		ok, real := addOnWhatsApp(client, n)
		if !ok {
			client.SendMessage(context.Background(), collector.Chat,
				core.TextMessage("⚠️ El número *"+n+"* no está en WhatsApp."))
			continue
		}
		if real.User != "" {
			jid = real
		}
		_, err := client.UpdateGroupParticipants(context.Background(), collector.Chat, []types.JID{jid}, whatsmeow.ParticipantChangeAdd)
		if err != nil {
			if link, linkErr := client.GetGroupInviteLink(context.Background(), collector.Chat, false); linkErr == nil {
				client.SendMessage(context.Background(), collector.Sender,
					core.TextMessage("🔗 Enlace para *"+n+"*:\n"+link))
				client.SendMessage(context.Background(), collector.Chat, client.BuildReaction(collector.Chat, collector.Sender, types.MessageID(collector.PromptID), "📨"))
				continue
			}
			client.SendMessage(context.Background(), collector.Chat, core.TextMessage("❌ No pude generar el enlace de invitación."))
			continue
		}
		client.SendMessage(context.Background(), collector.Chat, client.BuildReaction(collector.Chat, collector.Sender, types.MessageID(collector.PromptID), "✅"))
	}
}

// addWithFallback replica handleAddWithFallback dentro de un Context.
func addWithFallback(ctx *core.Context, number string, originalID types.MessageID, sender types.JID) error {
	wagoAppendLog("add.log", "Procesando número: "+number)
	client := ctx.Client
	if client == nil {
		return ctx.React("❌")
	}
	jid := types.NewJID(number, types.DefaultUserServer)
	ok, real := addOnWhatsApp(client, number)
	if !ok {
		return ctx.Reply("⚠️ El número *" + number + "* no está en WhatsApp.")
	}
	if real.User != "" {
		jid = real
	}
	if _, err := client.UpdateGroupParticipants(context.Background(), ctx.ChatJID(), []types.JID{jid}, whatsmeow.ParticipantChangeAdd); err == nil {
		return ctx.React("✅")
	}
	link, err := client.GetGroupInviteLink(context.Background(), ctx.ChatJID(), false)
	if err != nil {
		return ctx.Reply("❌ No pude generar el enlace de invitación.")
	}
	_ = ctx.SendTo(sender, core.TextMessage("🔗 Enlace para *"+number+"*:\n"+link))
	return ctx.React("📨")
}

var _ = strconv.Itoa
