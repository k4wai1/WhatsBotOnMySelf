package addons

import (
	"os"
	"path/filepath"
	"testing"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"whatsbot-go/internal/core"
)

func emojiTestCtx(t *testing.T, command string) (*core.Context, string, *capture) {
	t.Helper()
	dir := t.TempDir()
	prevCfg, prevStats := emojiConfigPath, emojiStatsPath
	emojiConfigPath = func() string { return filepath.Join(dir, "reacciones_config.json") }
	emojiStatsPath = func() string { return filepath.Join(dir, "reacciones_stats.json") }
	t.Cleanup(func() {
		emojiConfigPath = prevCfg
		emojiStatsPath = prevStats
	})

	jid := types.JID{User: "12345", Server: "g.us"}
	msg := &events.Message{Info: types.MessageInfo{MessageSource: types.MessageSource{Chat: jid, Sender: jid}}}
	c := &capture{}
	ctx := &core.Context{
		Command: command,
		Message: msg,
	}
	_ = ctx
	return &core.Context{
		Command: command,
		Message: msg,
		ReactHook: func(e string) error {
			c.reactions = append(c.reactions, e)
			return nil
		},
	}, jid.String(), c
}

func TestEmojiToGifCommands(t *testing.T) {
	got := EmojiToGif{}.Commands()
	if len(got) != 2 || got[0] != "onreaccion" || got[1] != "offreaccion" {
		t.Fatalf("comandos inesperados: %#v", got)
	}
}

func TestEmojiOnOff(t *testing.T) {
	ctx, jid, c := emojiTestCtx(t, "onreaccion")
	if err := (EmojiToGif{}).Handle(ctx); err != nil {
		t.Fatalf("Handle(onreaccion): %v", err)
	}
	cfg, err := emojiLoadConfig()
	if err != nil {
		t.Fatalf("emojiLoadConfig: %v", err)
	}
	if !emojiContains(cfg.ActiveGroups, jid) {
		t.Fatalf("el grupo no quedó activo: %#v", cfg.ActiveGroups)
	}

	ctx2 := &core.Context{
		Command: "offreaccion",
		Message: ctx.Message,
		ReactHook: func(e string) error {
			c.reactions = append(c.reactions, e)
			return nil
		},
	}
	if err := (EmojiToGif{}).Handle(ctx2); err != nil {
		t.Fatalf("Handle(offreaccion): %v", err)
	}
	cfg, err = emojiLoadConfig()
	if err != nil {
		t.Fatalf("emojiLoadConfig: %v", err)
	}
	if emojiContains(cfg.ActiveGroups, jid) {
		t.Fatalf("el grupo sigue activo: %#v", cfg.ActiveGroups)
	}
	if len(c.reactions) != 2 || c.reactions[0] != "✅" || c.reactions[1] != "❌" {
		t.Fatalf("reacciones inesperadas: %#v", c.reactions)
	}
}

func TestEmojiOnRepetido(t *testing.T) {
	ctx, _, c := emojiTestCtx(t, "onreaccion")
	if err := (EmojiToGif{}).Handle(ctx); err != nil {
		t.Fatalf("primer Handle: %v", err)
	}
	if err := (EmojiToGif{}).Handle(ctx); err != nil {
		t.Fatalf("segundo Handle: %v", err)
	}
	if len(c.reactions) != 2 || c.reactions[0] != "✅" || c.reactions[1] != "👍" {
		t.Fatalf("reacciones inesperadas: %#v", c.reactions)
	}
}

func TestEmojiOffSinSesion(t *testing.T) {
	ctx, _, c := emojiTestCtx(t, "offreaccion")
	if err := (EmojiToGif{}).Handle(ctx); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(c.reactions) != 1 || c.reactions[0] != "👍" {
		t.Fatalf("esperaba 👍, obtuve %#v", c.reactions)
	}
}

func TestEmojiEnsureFiles(t *testing.T) {
	dir := t.TempDir()
	prevCfg, prevStats := emojiConfigPath, emojiStatsPath
	emojiConfigPath = func() string { return filepath.Join(dir, "statistics", "reacciones_config.json") }
	emojiStatsPath = func() string { return filepath.Join(dir, "statistics", "reacciones_stats.json") }
	t.Cleanup(func() {
		emojiConfigPath = prevCfg
		emojiStatsPath = prevStats
	})

	if err := emojiEnsureFiles(); err != nil {
		t.Fatalf("emojiEnsureFiles: %v", err)
	}
	raw, err := os.ReadFile(emojiConfigPath())
	if err != nil {
		t.Fatalf("config no creada: %v", err)
	}
	cfg, err := emojiLoadConfig()
	if err != nil {
		t.Fatalf("config inválida: %v", err)
	}
	if len(cfg.ActiveGroups) != 0 {
		t.Fatalf("config inesperada: %s", raw)
	}
	if _, err := os.Stat(emojiStatsPath()); err != nil {
		t.Fatalf("stats no creadas: %v", err)
	}
}

func TestEmojiIsHeart(t *testing.T) {
	for _, e := range emojiHeartEmojis {
		if !emojiIsHeart(e) {
			t.Fatalf("%q debería ser corazón", e)
		}
	}
	if emojiIsHeart("👍") || emojiIsHeart("") {
		t.Fatal("emoji no corazón aceptado")
	}
}
