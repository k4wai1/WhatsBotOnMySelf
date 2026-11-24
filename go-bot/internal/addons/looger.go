// addons/looger.js → looger.go
// Hook de registro: imprime un bloque por cada mensaje (entrantes y salientes).
package addons

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"

	"whatsbot-go/internal/core"
	"whatsbot-go/internal/store"
)

var (
	loogerOut io.Writer = os.Stdout
	loogerNow           = time.Now
)

func loogerFormatBytes(bytes uint64) string {
	if bytes == 0 {
		return "0 Bytes"
	}
	const k = 1024
	sizes := []string{"Bytes", "KB", "MB", "GB"}
	i := 0
	v := float64(bytes)
	for v >= k && i < len(sizes)-1 {
		v /= k
		i++
	}
	rounded := float64(int64(v*100+0.5)) / 100
	return strconv.FormatFloat(rounded, 'f', -1, 64) + " " + sizes[i]
}

// loogerMsgType aproxima Object.keys(msg.message)[0].
func loogerMsgType(msg *waE2E.Message) string {
	if msg == nil {
		return "desconocido"
	}
	switch {
	case msg.GetConversation() != "":
		return "conversation"
	case msg.GetExtendedTextMessage() != nil:
		return "extendedTextMessage"
	case msg.GetImageMessage() != nil:
		return "imageMessage"
	case msg.GetVideoMessage() != nil:
		return "videoMessage"
	case msg.GetAudioMessage() != nil:
		return "audioMessage"
	case msg.GetDocumentMessage() != nil:
		return "documentMessage"
	case msg.GetStickerMessage() != nil:
		return "stickerMessage"
	case msg.GetReactionMessage() != nil:
		return "reactionMessage"
	case msg.GetContactMessage() != nil:
		return "contactMessage"
	case msg.GetLocationMessage() != nil:
		return "locationMessage"
	case msg.GetProtocolMessage() != nil:
		return "protocolMessage"
	}
	return "desconocido"
}

// loogerProcess replica el cuerpo del listener messages.upsert.
func loogerProcess(msg *events.Message) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(loogerOut, "\x1b[31m[LOG-ERROR] Fallo al procesar el logger:\x1b[0m %v\n", r)
		}
	}()

	if msg == nil || msg.Message == nil {
		return
	}

	jid := msg.Info.Chat.String()
	when := msg.Info.Timestamp
	if when.IsZero() {
		when = loogerNow()
	}
	timeStr := when.Format("15:04:05")
	msgType := loogerMsgType(msg.Message)
	isFromMe := msg.Info.IsFromMe

	tipoChat := "👤 DM"
	color := "\x1b[36m" // Cyan
	if strings.HasSuffix(jid, "@g.us") {
		tipoChat = "👥 GRUPO"
		color = "\x1b[33m"
	} else if strings.HasSuffix(jid, "@newsletter") {
		tipoChat = "📢 CANAL"
		color = "\x1b[35m"
	}
	if isFromMe {
		color = "\x1b[32m"
	}

	senderName := "Anónimo"
	if isFromMe {
		senderName = "🤖 (Bot)"
	} else if msg.Info.PushName != "" {
		senderName = msg.Info.PushName
	}

	senderJID := msg.Info.Sender.String()
	if senderJID == "" {
		senderJID = jid
	}
	senderNum := strings.SplitN(senderJID, "@", 2)[0]

	content := msg.Message.GetConversation()
	if content == "" {
		content = msg.Message.GetExtendedTextMessage().GetText()
	}

	mediaDetails := ""
	var fileLength uint64
	mimetype := ""
	caption := ""
	animated := false
	switch msgType {
	case "imageMessage":
		m := msg.Message.GetImageMessage()
		fileLength, mimetype, caption = m.GetFileLength(), m.GetMimetype(), m.GetCaption()
	case "videoMessage":
		m := msg.Message.GetVideoMessage()
		fileLength, mimetype, caption = m.GetFileLength(), m.GetMimetype(), m.GetCaption()
	case "audioMessage":
		m := msg.Message.GetAudioMessage()
		fileLength, mimetype = m.GetFileLength(), m.GetMimetype()
	case "documentMessage":
		m := msg.Message.GetDocumentMessage()
		fileLength, mimetype, caption = m.GetFileLength(), m.GetMimetype(), m.GetCaption()
	case "stickerMessage":
		m := msg.Message.GetStickerMessage()
		fileLength, mimetype = m.GetFileLength(), m.GetMimetype()
		animated = m.GetIsAnimated()
	}
	if fileLength > 0 {
		if mimetype == "" {
			mimetype = "Desconocido"
		}
		mediaDetails = "\n📦 \x1b[1mArchivo:\x1b[0m " + loogerFormatBytes(fileLength) + " | " + mimetype
		if msgType == "stickerMessage" {
			if animated {
				mediaDetails += " (Animado)"
			} else {
				mediaDetails += " (Estático)"
			}
		}
		if content == "" {
			if caption != "" {
				content = caption
			} else {
				content = "[Adjunto: " + msgType + "]"
			}
		}
	}

	reactionDetails := ""
	if msgType == "reactionMessage" {
		m := msg.Message.GetReactionMessage()
		emoji := m.GetText()
		targetID := "Desconocido"
		if m.GetKey() != nil && m.GetKey().GetID() != "" {
			targetID = m.GetKey().GetID()
		}
		if emoji != "" {
			content = "[Reaccionó con Emoji]"
			reactionDetails = "\n🎭 \x1b[1mEmoji:\x1b[0m " + emoji + "\n🎯 \x1b[1mAl Mensaje ID:\x1b[0m " + targetID
		} else {
			content = "[Eliminó su reacción]"
			reactionDetails = "\n🗑️ \x1b[1mAl Mensaje ID:\x1b[0m " + targetID
		}
	}

	if content == "" {
		content = "[Evento del sistema: " + msgType + "]"
	}

	fmt.Fprintf(loogerOut, "\n%s╭─── [%s] ⏱️ %s\x1b[0m\n", color, tipoChat, timeStr)
	fmt.Fprintf(loogerOut, "%s│\x1b[0m 🆔 \x1b[1mID Chat:\x1b[0m %s\n", color, jid)
	fmt.Fprintf(loogerOut, "%s│\x1b[0m 👤 \x1b[1mDe:\x1b[0m %s (+%s)\n", color, senderName, senderNum)
	fmt.Fprintf(loogerOut, "%s│\x1b[0m 💬 \x1b[1mMensaje:\x1b[0m %s%s%s\n", color, content, mediaDetails, reactionDetails)
	fmt.Fprintf(loogerOut, "%s╰───────────────────────────────────────\x1b[0m\n", color)
}

// loogerInit conecta el logger al evento de mensajes de whatsmeow.
func loogerInit(client *whatsmeow.Client, _ *store.Store) error {
	if client == nil {
		return nil
	}
	client.AddEventHandler(func(evt any) {
		if m, ok := evt.(*events.Message); ok {
			loogerProcess(m)
		}
	})
	return nil
}

func init() {
	core.RegisterBackground("looger", loogerInit)
}
