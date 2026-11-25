package addons

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"whatsbot-go/internal/core"
	"whatsbot-go/internal/store"
)

const (
	rmMaxBuffer = 250
	rmMaxCount  = 100
)

type rmStored struct {
	Info    types.MessageInfo
	Message *waE2E.Message
}

var (
	rmMu      sync.Mutex
	rmBuffers = map[string][]rmStored{}
	rmDelay   = 100 * time.Millisecond
	rmSleepFn = time.Sleep
)

// Rm porta addons/rm.js: borrado masivo con filtros de tipo y usuario.
type Rm struct{}

func init() {
	core.Register(Rm{})
	core.RegisterBackground("rm", rmInit)
}

func (Rm) Commands() []string { return []string{"rm"} }

func rmBufferPush(chat string, item rmStored) {
	rmMu.Lock()
	defer rmMu.Unlock()
	arr := append(rmBuffers[chat], item)
	if len(arr) > rmMaxBuffer {
		arr = arr[len(arr)-rmMaxBuffer:]
	}
	rmBuffers[chat] = arr
}

func rmBufferGet(chat string) []rmStored {
	rmMu.Lock()
	defer rmMu.Unlock()
	return append([]rmStored(nil), rmBuffers[chat]...)
}

func rmInit(client *whatsmeow.Client, _ *store.Store) error {
	if client == nil {
		return nil
	}
	client.AddEventHandler(func(evt any) {
		msg, ok := evt.(*events.Message)
		if !ok || msg.Message == nil {
			return
		}
		chat := msg.Info.Chat.String()
		if chat == "" {
			return
		}
		rmBufferPush(chat, rmStored{Info: msg.Info, Message: msg.Message})
	})
	return nil
}

// rmNormalize replica jidNormalizedUser: quita el device y conserva el server.
func rmNormalize(jid string) string {
	if jid == "" {
		return ""
	}
	server := "s.whatsapp.net"
	if idx := strings.Index(jid, "@"); idx >= 0 {
		server = jid[idx+1:]
	}
	user := strings.SplitN(jid, "@", 2)[0]
	user = strings.SplitN(user, ":", 2)[0]
	return user + "@" + server
}

func rmUser(jid string) string {
	base := strings.SplitN(jid, "@", 2)[0]
	return strings.SplitN(base, ":", 2)[0]
}

// rmParseArgs replica el bucle de argumentos de rm.js.
func rmParseArgs(args []string, mentados []string) (count int, formato, objetivo string) {
	count = 1
	formato = "all"
	if len(mentados) > 0 {
		objetivo = mentados[0]
	}
	for _, arg := range args {
		if n, err := strconv.Atoi(strings.TrimSpace(arg)); err == nil {
			count = n
			continue
		}
		if strings.HasPrefix(arg, "@") {
			if objetivo == "" {
				if num := wagoDigits(arg); num != "" {
					objetivo = num + "@s.whatsapp.net"
				}
			}
			continue
		}
		if strings.TrimSpace(arg) != "" {
			formato = strings.ToLower(strings.TrimSpace(arg))
		}
	}
	if count < 1 {
		count = 1
	}
	if count > rmMaxCount {
		count = rmMaxCount
	}
	return count, formato, objetivo
}

// rmDetailedType replica getDetailedMsgType().
func rmDetailedType(msg *waE2E.Message) string {
	if msg == nil {
		return ""
	}
	content := msg
	if content.GetEphemeralMessage() != nil {
		content = content.GetEphemeralMessage().GetMessage()
	}
	if content.GetViewOnceMessage() != nil {
		content = content.GetViewOnceMessage().GetMessage()
	}
	if content.GetViewOnceMessageV2() != nil {
		content = content.GetViewOnceMessageV2().GetMessage()
	}
	if content.GetDocumentWithCaptionMessage() != nil {
		content = content.GetDocumentWithCaptionMessage().GetMessage()
	}
	switch {
	case content.GetImageMessage() != nil:
		return "img"
	case content.GetVideoMessage() != nil:
		if content.GetVideoMessage().GetGifPlayback() {
			return "gif"
		}
		return "video"
	case content.GetAudioMessage() != nil:
		if content.GetAudioMessage().GetPTT() {
			return "ptt"
		}
		return "audio"
	case content.GetStickerMessage() != nil:
		return "sticker"
	case content.GetDocumentMessage() != nil:
		return "doc"
	case content.GetContactMessage() != nil, content.GetContactsArrayMessage() != nil:
		return "contact"
	case content.GetLocationMessage() != nil, content.GetLiveLocationMessage() != nil:
		return "location"
	}
	return "text"
}

func rmTipoCoincide(formato, tipo string) bool {
	switch formato {
	case "all":
		return true
	case "audio":
		return tipo == "audio" || tipo == "ptt"
	case "media":
		return tipo == "img" || tipo == "video" || tipo == "gif" || tipo == "audio" || tipo == "ptt" || tipo == "sticker" || tipo == "doc"
	default:
		return formato == tipo
	}
}

// rmDeleteMessage construye el protocolo REVOKE con la clave exacta.
func rmDeleteMessage(chat types.JID, id types.MessageID, participant string, fromMe bool) *waE2E.Message {
	key := &waCommon.MessageKey{
		RemoteJID: proto.String(chat.String()),
		ID:        proto.String(string(id)),
		FromMe:    proto.Bool(fromMe),
	}
	if participant != "" {
		key.Participant = proto.String(participant)
	}
	return &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
		Type: waE2E.ProtocolMessage_REVOKE.Enum(),
		Key:  key,
	}}
}

func rmBotOwnUser(client *whatsmeow.Client) string {
	if client != nil && client.Store != nil && client.Store.ID != nil {
		return client.Store.ID.User
	}
	return ""
}

func rmEsDelBot(client *whatsmeow.Client, info types.MessageInfo, botNums []string) bool {
	if info.IsFromMe {
		return true
	}
	if info.Sender.User != "" && wagoContains(botNums, info.Sender.User) {
		return true
	}
	return false
}

func (Rm) Handle(ctx *core.Context) error {
	if ctx == nil || !wagoIsGroup(ctx) {
		return nil
	}
	client := ctx.Client
	info, err := wagoGroupInfo(ctx)
	if err != nil {
		slog.Error("❌ [rm] Error:", "error", err)
		return ctx.React("❌")
	}
	if !wagoSenderIsAdminOrOwner(ctx, info) {
		return ctx.Reply("⛔ Comando de moderación: Requiere Admin.")
	}
	botNums := wagoBotNumbers(ctx)
	if !wagoBotIsAdmin(ctx, info) {
		return ctx.Reply("❌ No puedo borrar mensajes de otros si no soy Administrador.")
	}

	quotedInfo := ctx.ContextInfo()
	var mentados []string
	if quotedInfo != nil {
		mentados = quotedInfo.GetMentionedJID()
	}
	count, formato, objetivo := rmParseArgs(ctx.Args, mentados)

	if count == 1 && formato == "all" && objetivo == "" && quotedInfo != nil && quotedInfo.GetStanzaID() != "" {
		fromMe := rmEsDelBot(client, types.MessageInfo{MessageSource: types.MessageSource{
			IsFromMe: quotedInfo.GetParticipant() == "" || wagoContains(botNums, rmUser(quotedInfo.GetParticipant())),
		}}, botNums)
		msg := rmDeleteMessage(ctx.ChatJID(), types.MessageID(quotedInfo.GetStanzaID()), quotedInfo.GetParticipant(), fromMe)
		if client != nil {
			if _, err := client.SendMessage(context.Background(), ctx.ChatJID(), msg); err != nil {
				slog.Error("❌ [rm] Error:", "error", err)
				return ctx.React("❌")
			}
		}
		return ctx.React("✅")
	}

	msgs := rmBufferGet(ctx.ChatJID().String())
	if len(msgs) == 0 {
		return ctx.Reply("❌ No tengo un registro de mensajes en memoria para este chat.")
	}

	citando := quotedInfo != nil && quotedInfo.GetStanzaID() != ""
	anchorID := ""
	if quotedInfo != nil {
		anchorID = quotedInfo.GetStanzaID()
	}
	if anchorID == "" && ctx.Message != nil {
		anchorID = string(ctx.Message.Info.ID)
	}
	anchorIndex := -1
	for i, m := range msgs {
		if string(m.Info.ID) == anchorID {
			anchorIndex = i
			break
		}
	}
	if citando && anchorIndex == -1 {
		return ctx.Reply("❌ El mensaje citado es muy antiguo y ya no está en mi memoria reciente (límite: " + strconv.Itoa(rmMaxBuffer) + " mensajes).")
	}
	if anchorIndex == -1 {
		anchorIndex = len(msgs) - 1
	}

	targetNorm := ""
	if objetivo != "" {
		targetNorm = rmNormalize(objetivo)
	}
	var aBorrar []rmStored
	propio := ""
	if ctx.Message != nil {
		propio = string(ctx.Message.Info.ID)
	}

	for i := anchorIndex; i >= 0 && len(aBorrar) < count; i-- {
		m := msgs[i]
		if string(m.Info.ID) == propio {
			continue
		}
		tipo := rmDetailedType(m.Message)
		if !rmTipoCoincide(formato, tipo) {
			continue
		}
		if targetNorm == "" {
			aBorrar = append(aBorrar, m)
			continue
		}
		participante := m.Info.Sender.String()
		if participante == "" && !m.Info.IsFromMe {
			participante = ctx.ChatJID().String()
		}
		esBot := m.Info.IsFromMe || participante == ""
		if esBot {
			if rmNormalize(targetNorm) == rmNormalize(participante) || wagoContains(botNums, rmUser(targetNorm)) {
				aBorrar = append(aBorrar, m)
			}
			continue
		}
		if rmNormalize(participante) == targetNorm {
			aBorrar = append(aBorrar, m)
		}
	}

	if len(aBorrar) == 0 {
		detalle := ""
		if objetivo != "" {
			detalle = " de ese usuario"
		}
		return ctx.Reply("🤷‍♂️ No encontré mensajes de tipo '" + formato + "'" + detalle + " por encima de ese punto en mi memoria actual.")
	}

	if err := ctx.React("⏳"); err != nil {
		return err
	}

	borrados := 0
	for _, m := range aBorrar {
		esBot := rmEsDelBot(client, m.Info, botNums)
		participante := m.Info.Sender.String()
		if esBot && participante == "" {
			if client != nil && client.Store != nil && client.Store.ID != nil {
				participante = client.Store.ID.String()
			}
		}
		msg := rmDeleteMessage(ctx.ChatJID(), m.Info.ID, participante, esBot)
		if client != nil {
			if _, err := client.SendMessage(context.Background(), ctx.ChatJID(), msg); err != nil {
				slog.Error("❌ [rm] Error borrando mensaje en bucle:", "error", err)
				continue
			}
		}
		borrados++
		rmSleepFn(rmDelay)
	}

	if borrados > 1 {
		return ctx.SendText("🧹 Se eliminaron " + strconv.Itoa(borrados) + " mensajes.")
	}
	return ctx.React("✅")
}
