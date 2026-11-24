package addons

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"whatsbot-go/internal/core"
)

type ftCapture struct {
	msgs      []*waE2E.Message
	reactions []string
}

func (c *ftCapture) ctx(withSticker bool) *core.Context {
	msg := &waE2E.Message{Conversation: proto.String("ft")}
	if withSticker {
		msg = &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String("ft"),
			ContextInfo: &waE2E.ContextInfo{
				QuotedMessage: &waE2E.Message{StickerMessage: &waE2E.StickerMessage{Mimetype: proto.String("image/webp")}},
			},
		}}
	}
	return &core.Context{
		Command: "ft",
		Message: &events.Message{
			Info:    types.MessageInfo{MessageSource: types.MessageSource{Chat: types.NewJID("1", types.DefaultUserServer)}},
			Message: msg,
		},
		SendHook: func(m *waE2E.Message) error {
			c.msgs = append(c.msgs, m)
			return nil
		},
		ReactHook: func(e string) error {
			c.reactions = append(c.reactions, e)
			return nil
		},
	}
}

func TestFtCommands(t *testing.T) {
	got := Ft{}.Commands()
	if len(got) != 1 || got[0] != "ft" {
		t.Fatalf("comandos inesperados: %#v", got)
	}
}

func TestFtSinSticker(t *testing.T) {
	c := &ftCapture{}
	if err := (Ft{}).Handle(c.ctx(false)); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.reactions) != 1 || c.reactions[0] != "❓" {
		t.Fatalf("reacciones=%#v", c.reactions)
	}
}

func TestFtConvierteYEnvia(t *testing.T) {
	dir := t.TempDir()
	prevDir, prevDownload, prevConvert, prevNow := ftCacheDir, ftDownload, ftConvert, ftNow
	ftCacheDir = func() string { return dir }
	ftDownload = func(*core.Context) ([]byte, error) { return []byte("RIFFxxxxWEBP"), nil }
	ftNow = func() time.Time { return time.UnixMilli(1700000000000) }
	ftConvert = func(_ context.Context, args ...string) error {
		out := args[len(args)-1]
		return os.WriteFile(out, []byte("\x89PNG final"), 0o600)
	}
	t.Cleanup(func() {
		ftCacheDir, ftDownload, ftConvert, ftNow = prevDir, prevDownload, prevConvert, prevNow
	})

	c := &ftCapture{}
	if err := (Ft{}).Handle(c.ctx(true)); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.msgs) != 1 || c.msgs[0].GetDocumentMessage() == nil {
		t.Fatalf("esperaba documento: %#v", c.msgs)
	}
	doc := c.msgs[0].GetDocumentMessage()
	if doc.GetFileName() != "file_1700000000000.png" {
		t.Fatalf("nombre inesperado: %q", doc.GetFileName())
	}
	if len(c.reactions) != 2 || c.reactions[1] != "✅" {
		t.Fatalf("reacciones=%#v", c.reactions)
	}

	// Sin fugas: los temporales del cache se limpian al terminar.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "" {
			t.Fatalf("quedó un temporal: %s", e.Name())
		}
	}
}

func TestFtFallaConvert(t *testing.T) {
	dir := t.TempDir()
	prevDir, prevDownload, prevConvert := ftCacheDir, ftDownload, ftConvert
	ftCacheDir = func() string { return dir }
	ftDownload = func(*core.Context) ([]byte, error) { return []byte("RIFFxxxxWEBP"), nil }
	ftConvert = func(context.Context, ...string) error { return errors.New("convert roto") }
	t.Cleanup(func() { ftCacheDir, ftDownload, ftConvert = prevDir, prevDownload, prevConvert })

	c := &ftCapture{}
	if err := (Ft{}).Handle(c.ctx(true)); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.reactions) == 0 || c.reactions[len(c.reactions)-1] != "❌" {
		t.Fatalf("reacciones=%#v", c.reactions)
	}
	if _, err := os.Stat(filepath.Join(dir, "input_0.webp")); !os.IsNotExist(err) {
		t.Fatal("el input debía limpiarse")
	}
}

var _ = bytes.Contains
