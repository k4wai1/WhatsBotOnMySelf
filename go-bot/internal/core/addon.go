// Package core define el contrato de addons y el router de mensajes,
// replicando el cargador dinámico de index.js.
package core

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"whatsbot-go/internal/store"
)

// Context es el equivalente del handler(sock, msg, args, store) de Node.js.
type Context struct {
	Client  *whatsmeow.Client
	Message *events.Message
	Store   *store.Store

	// Argumentos ya parseados por el router.
	Args    []string
	Command string
	Prefix  string
	RawText string

	// SendHook y ReactHook permiten capturar las salidas en pruebas sin
	// necesitar una conexión real con WhatsApp.
	SendHook  func(msg *waE2E.Message) error
	ReactHook func(emoji string) error
}

// Addon es un comando registrable.
type Addon interface {
	Commands() []string
	Handle(ctx *Context) error
}

// BackgroundAddon replica el `addon.init(sock, store)` de index.js: procesos
// silenciosos que arrancan junto al bot.
type BackgroundAddon interface {
	Init(client *whatsmeow.Client, st *store.Store) error
}

// BackgroundFunc permite registrar un Init sin definir un tipo nuevo.
type BackgroundFunc func(client *whatsmeow.Client, st *store.Store) error

// ChatJID es el JID del chat donde llegó el mensaje.
func (c *Context) ChatJID() types.JID {
	if c.Message == nil {
		return types.EmptyJID
	}
	return c.Message.Info.Chat
}

// SenderJID es quien envió el mensaje (participante en grupos).
func (c *Context) SenderJID() types.JID {
	if c.Message == nil {
		return types.EmptyJID
	}
	return c.Message.Info.Sender
}

// SendText envía texto plano al chat.
func (c *Context) SendText(text string) error {
	return c.SendMessage(&waE2E.Message{Conversation: proto.String(text)})
}

// Reply envía texto citando el mensaje original (equivalente a { quoted: msg }).
func (c *Context) Reply(text string) error {
	return c.SendQuoted(&waE2E.Message{Conversation: proto.String(text)}, c.Message)
}

// SendMessage envía un mensaje arbitrario al chat actual.
func (c *Context) SendMessage(msg *waE2E.Message) error {
	if msg == nil {
		return nil
	}
	if c.SendHook != nil {
		return c.SendHook(msg)
	}
	if c.Client == nil || c.Message == nil {
		return nil
	}
	_, err := c.Client.SendMessage(context.Background(), c.Message.Info.Chat, msg)
	return err
}

// SendQuoted envía msg citando el mensaje indicado.
func (c *Context) SendQuoted(msg *waE2E.Message, quoted *events.Message) error {
	if msg == nil {
		return nil
	}
	if quoted != nil {
		info := quotedContextInfo(quoted)
		if info != nil {
			setContextInfo(msg, info)
		}
	}
	return c.SendMessage(msg)
}

// React pone una reacción sobre el mensaje que disparó el addon.
func (c *Context) React(emoji string) error {
	if emoji == "" {
		return nil
	}
	if c.ReactHook != nil {
		return c.ReactHook(emoji)
	}
	if c.Client == nil || c.Message == nil {
		return nil
	}
	reaction := c.Client.BuildReaction(c.Message.Info.Chat, c.Message.Info.Sender, c.Message.Info.ID, emoji)
	_, err := c.Client.SendMessage(context.Background(), c.Message.Info.Chat, reaction)
	return err
}

// QuotedMessage devuelve el mensaje citado, si lo hay.
func (c *Context) QuotedMessage() *waE2E.Message {
	if c.Message == nil {
		return nil
	}
	info := contextInfoOf(c.Message.Message)
	if info == nil {
		return nil
	}
	return info.GetQuotedMessage()
}

// DownloadMedia descarga el contenido multimedia del mensaje actual como
// bytes (reemplaza downloadMediaMessage de Baileys).
func (c *Context) DownloadMedia() ([]byte, error) {
	if c.Client == nil || c.Message == nil {
		return nil, nil
	}
	target := downloadableMedia(c.Message.Message)
	if target == nil {
		return nil, nil
	}
	return c.Client.Download(context.Background(), target)
}

// downloadableMedia devuelve el sub-mensaje con los metadatos de descarga.
func downloadableMedia(msg *waE2E.Message) whatsmeow.DownloadableMessage {
	if msg == nil {
		return nil
	}
	switch {
	case msg.GetImageMessage() != nil:
		return msg.GetImageMessage()
	case msg.GetVideoMessage() != nil:
		return msg.GetVideoMessage()
	case msg.GetAudioMessage() != nil:
		return msg.GetAudioMessage()
	case msg.GetDocumentMessage() != nil:
		return msg.GetDocumentMessage()
	case msg.GetStickerMessage() != nil:
		return msg.GetStickerMessage()
	}
	return nil
}

// ─── Registro global (auto-registro vía init()) ─────────────────────────────

var (
	defaultMu     sync.RWMutex
	defaultRouter *Router
)

// DefaultRouter devuelve el router global perezoso.
func DefaultRouter() *Router {
	defaultMu.RLock()
	r := defaultRouter
	defaultMu.RUnlock()
	if r != nil {
		return r
	}

	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultRouter == nil {
		defaultRouter = NewRouter(nil)
	}
	return defaultRouter
}

// SetDefaultRouter reemplaza el router global (usado por main y por tests).
func SetDefaultRouter(r *Router) {
	defaultMu.Lock()
	defaultRouter = r
	defaultMu.Unlock()
}

// Register registra un addon en el router global.
func Register(a Addon) { DefaultRouter().Register(a) }

// RegisterBackground registra un proceso en segundo plano en el router global.
func RegisterBackground(name string, fn BackgroundFunc) {
	DefaultRouter().RegisterBackground(name, fn)
}

// ─── Helpers de protobuf ────────────────────────────────────────────────────

func contextInfoOf(msg *waE2E.Message) *waE2E.ContextInfo {
	if msg == nil {
		return nil
	}
	switch {
	case msg.GetExtendedTextMessage() != nil:
		return msg.GetExtendedTextMessage().GetContextInfo()
	case msg.GetImageMessage() != nil:
		return msg.GetImageMessage().GetContextInfo()
	case msg.GetVideoMessage() != nil:
		return msg.GetVideoMessage().GetContextInfo()
	case msg.GetDocumentMessage() != nil:
		return msg.GetDocumentMessage().GetContextInfo()
	case msg.GetAudioMessage() != nil:
		return msg.GetAudioMessage().GetContextInfo()
	case msg.GetStickerMessage() != nil:
		return msg.GetStickerMessage().GetContextInfo()
	case msg.GetContactMessage() != nil:
		return msg.GetContactMessage().GetContextInfo()
	case msg.GetContactsArrayMessage() != nil:
		return msg.GetContactsArrayMessage().GetContextInfo()
	case msg.GetLocationMessage() != nil:
		return msg.GetLocationMessage().GetContextInfo()
	}
	return nil
}

func quotedContextInfo(quoted *events.Message) *waE2E.ContextInfo {
	if quoted == nil || quoted.Message == nil {
		return nil
	}
	return &waE2E.ContextInfo{
		StanzaID:      proto.String(quoted.Info.ID),
		Participant:   proto.String(quoted.Info.Sender.String()),
		QuotedMessage: quoted.Message,
	}
}

func setContextInfo(msg *waE2E.Message, info *waE2E.ContextInfo) {
	if msg == nil || info == nil {
		return
	}
	switch {
	case msg.GetConversation() != "":
		// Conversation no lleva ContextInfo: se promueve a texto extendido
		// para poder citar, igual que hace Baileys.
		msg.ExtendedTextMessage = &waE2E.ExtendedTextMessage{
			Text:        proto.String(msg.GetConversation()),
			ContextInfo: info,
		}
		msg.Conversation = nil
	case msg.GetExtendedTextMessage() != nil:
		msg.ExtendedTextMessage.ContextInfo = info
	case msg.GetImageMessage() != nil:
		msg.ImageMessage.ContextInfo = info
	case msg.GetVideoMessage() != nil:
		msg.VideoMessage.ContextInfo = info
	case msg.GetDocumentMessage() != nil:
		msg.DocumentMessage.ContextInfo = info
	case msg.GetAudioMessage() != nil:
		msg.AudioMessage.ContextInfo = info
	case msg.GetStickerMessage() != nil:
		msg.StickerMessage.ContextInfo = info
	}
}

// TextMessage construye un mensaje de texto plano.
func TextMessage(text string) *waE2E.Message {
	return &waE2E.Message{Conversation: proto.String(text)}
}

// NormalizeCommand replica .toLowerCase() de JavaScript para ASCII y también
// recorta prefijos pegados por descuido ("!dado" → "dado").
func NormalizeCommand(cmd string) string {
	return strings.ToLower(strings.TrimSpace(cmd))
}

var _ = slog.Default
