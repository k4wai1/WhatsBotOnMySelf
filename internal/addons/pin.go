package addons

import (
	"log/slog"
	"strconv"
	"time"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"whatsbot-go/internal/core"
)

// Pin porta addons/pin.js: fija/desfija el mensaje citado.
type Pin struct{}

func init() { core.Register(Pin{}) }

func (Pin) Commands() []string { return []string{"pin", "unpin"} }

func (Pin) Handle(ctx *core.Context) error {
	if ctx == nil || ctx.Message == nil {
		return nil
	}
	if !wagoIsGroup(ctx) {
		return nil
	}

	info, err := wagoGroupInfo(ctx)
	if err != nil {
		slog.Error("❌ [PIN] Error:", "error", err)
		return ctx.React("❌")
	}

	if !wagoSenderIsAdminOrOwner(ctx, info) {
		return ctx.React("⛔")
	}

	contextInfo := ctx.ContextInfo()
	if contextInfo == nil || contextInfo.GetStanzaID() == "" {
		return ctx.Reply("⚠️ Debes responder al mensaje que deseas fijar o desfijar.")
	}

	key := &waCommon.MessageKey{
		RemoteJID:   proto.String(ctx.ChatJID().String()),
		ID:          proto.String(contextInfo.GetStanzaID()),
		Participant: proto.String(contextInfo.GetParticipant()),
		FromMe:      proto.Bool(false),
	}

	if ctx.Command == "unpin" {
		if err := pinSend(ctx, key, false, 0); err != nil {
			slog.Error("❌ [PIN] Error:", "error", err)
			return ctx.React("❌")
		}
		return ctx.React("📍")
	}

	if ctx.Command == "pin" {
		if err := pinSend(ctx, key, true, pinDuration(ctx.Args)); err != nil {
			slog.Error("❌ [PIN] Error:", "error", err)
			return ctx.React("❌")
		}
		return ctx.React("📌")
	}
	return nil
}

// pinDuration replica los tiempos de pin.js: 24h por defecto, 7d con "1",
// 30d con "2".
func pinDuration(args []string) int64 {
	if len(args) > 0 {
		if args[0] == "1" {
			return 604800
		}
		if args[0] == "2" {
			return 2592000
		}
	}
	return 86400
}

func pinSend(ctx *core.Context, key *waCommon.MessageKey, fijar bool, segundos int64) error {
	tipo := waE2E.PinInChatMessage_PIN_FOR_ALL
	if !fijar {
		tipo = waE2E.PinInChatMessage_UNPIN_FOR_ALL
	}
	msg := &waE2E.Message{
		PinInChatMessage: &waE2E.PinInChatMessage{
			Key:               key,
			Type:              tipo.Enum(),
			SenderTimestampMS: proto.Int64(time.Now().UnixMilli()),
		},
		MessageContextInfo: &waE2E.MessageContextInfo{
			MessageAddOnDurationInSecs: proto.Uint32(uint32(segundos)),
		},
	}
	return ctx.SendMessage(msg)
}

var _ = strconv.Itoa
