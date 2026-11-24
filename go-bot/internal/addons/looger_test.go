package addons

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func loogerSetup(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	prevOut := loogerOut
	prevNow := loogerNow
	loogerOut = buf
	loogerNow = func() time.Time { return time.Date(2026, 6, 15, 14, 3, 5, 0, time.Local) }
	t.Cleanup(func() {
		loogerOut = prevOut
		loogerNow = prevNow
	})
	return buf
}

func loogerMessage(chat, sender string, fromMe, group bool, msg *waE2E.Message) *events.Message {
	chatJID, _ := types.ParseJID(chat)
	senderJID, _ := types.ParseJID(sender)
	return &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: chatJID, Sender: senderJID, IsFromMe: fromMe, IsGroup: group},
			ID:            "LOG1",
			PushName:      "Luis",
			Timestamp:     time.Date(2026, 6, 15, 14, 3, 5, 0, time.Local),
		},
		Message: msg,
	}
}

func TestLoogerDM(t *testing.T) {
	buf := loogerSetup(t)
	msg := loogerMessage("584111@s.whatsapp.net", "584111@s.whatsapp.net", false, false, &waE2E.Message{Conversation: proto.String("hola mundo")})
	loogerProcess(msg)

	out := buf.String()
	for _, want := range []string{"👤 DM", "⏱️ 14:03:05", "ID Chat:\x1b[0m 584111@s.whatsapp.net", "De:\x1b[0m Luis (+584111)", "Mensaje:\x1b[0m hola mundo"} {
		if !strings.Contains(out, want) {
			t.Fatalf("log sin %q:\n%s", want, out)
		}
	}
}

func TestLoogerGrupoDesdeBot(t *testing.T) {
	buf := loogerSetup(t)
	msg := loogerMessage("1203@g.us", "584222@s.whatsapp.net", true, true, &waE2E.Message{Conversation: proto.String("respuesta")})
	loogerProcess(msg)

	out := buf.String()
	for _, want := range []string{"👥 GRUPO", "\x1b[32m", "🤖 (Bot) (+584222)", "respuesta"} {
		if !strings.Contains(out, want) {
			t.Fatalf("log sin %q:\n%s", want, out)
		}
	}
}

func TestLoogerSticker(t *testing.T) {
	buf := loogerSetup(t)
	sticker := &waE2E.StickerMessage{
		FileLength: proto.Uint64(2048),
		Mimetype:   proto.String("image/webp"),
		IsAnimated: proto.Bool(true),
	}
	msg := loogerMessage("1204@g.us", "584333@s.whatsapp.net", false, true, &waE2E.Message{StickerMessage: sticker})
	loogerProcess(msg)

	out := buf.String()
	for _, want := range []string{"Archivo:\x1b[0m 2 KB | image/webp (Animado)", "[Adjunto: stickerMessage]"} {
		if !strings.Contains(out, want) {
			t.Fatalf("log sin %q:\n%s", want, out)
		}
	}
}

func TestLoogerReaccion(t *testing.T) {
	buf := loogerSetup(t)
	reaction := &waE2E.ReactionMessage{
		Text: proto.String("👍"),
		Key:  &waCommon.MessageKey{ID: proto.String("ABC123")},
	}
	msg := loogerMessage("1205@g.us", "584444@s.whatsapp.net", false, true, &waE2E.Message{ReactionMessage: reaction})
	loogerProcess(msg)

	out := buf.String()
	for _, want := range []string{"[Reaccionó con Emoji]", "Emoji:\x1b[0m 👍", "Al Mensaje ID:\x1b[0m ABC123"} {
		if !strings.Contains(out, want) {
			t.Fatalf("log sin %q:\n%s", want, out)
		}
	}
}

func TestLoogerFormatBytes(t *testing.T) {
	casos := map[uint64]string{
		0:       "0 Bytes",
		1023:    "1023 Bytes",
		1024:    "1 KB",
		1536:    "1.5 KB",
		1048576: "1 MB",
		1572864: "1.5 MB",
	}
	for n, want := range casos {
		if got := loogerFormatBytes(n); got != want {
			t.Fatalf("loogerFormatBytes(%d)=%q esperaba %q", n, got, want)
		}
	}
}

func TestLoogerMsgType(t *testing.T) {
	if got := loogerMsgType(&waE2E.Message{Conversation: proto.String("x")}); got != "conversation" {
		t.Fatalf("tipo=%q", got)
	}
	if got := loogerMsgType(&waE2E.Message{ImageMessage: &waE2E.ImageMessage{}}); got != "imageMessage" {
		t.Fatalf("tipo=%q", got)
	}
	if got := loogerMsgType(nil); got != "desconocido" {
		t.Fatalf("tipo=%q", got)
	}
}

func TestLoogerInitNil(t *testing.T) {
	if err := loogerInit(nil, nil); err != nil {
		t.Fatalf("loogerInit(nil) error: %v", err)
	}
}
