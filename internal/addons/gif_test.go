package addons

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"

	"go.mau.fi/whatsmeow/proto/waE2E"

	"whatsbot-go/internal/core"
)

func TestGifCommands(t *testing.T) {
	got := (Gif{}).Commands()
	want := []string{"g", "ongif", "offgif", "topg"}
	if len(got) != len(want) {
		t.Fatalf("comandos=%#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("comandos=%#v", got)
		}
	}
}

func TestGifAcciones(t *testing.T) {
	if len(gifAcciones) < 350 {
		t.Fatalf("acciones=%d", len(gifAcciones))
	}
	for _, clave := range []string{"kiss", "hug", "slap", "furry", "uwu", "dance"} {
		accion, ok := gifAcciones[clave]
		if !ok || accion.Search == "" || accion.Verb == "" || accion.Participle == "" {
			t.Fatalf("acción %q inválida: %#v", clave, accion)
		}
	}
}

func TestGifCheckSpam(t *testing.T) {
	gifCooldownMu.Lock()
	gifCooldowns = map[string]map[string]map[string][]int64{}
	gifCooldownMu.Unlock()

	ahora := time.Unix(1700000000, 0)
	for i := 0; i < gifSpamMax; i++ {
		if !gifCheckSpam("1205@g.us", "584111@s.whatsapp.net", "kiss", ahora) {
			t.Fatalf("uso %d debía permitirse", i+1)
		}
	}
	if gifCheckSpam("1205@g.us", "584111@s.whatsapp.net", "kiss", ahora) {
		t.Fatal("el cuarto uso debía bloquearse")
	}
	if !gifCheckSpam("1205@g.us", "584111@s.whatsapp.net", "kiss", ahora.Add(3*time.Minute)) {
		t.Fatal("tras la ventana debía permitirse")
	}
	if !gifCheckSpam("1205@g.us", "584111@s.whatsapp.net", "hug", ahora) {
		t.Fatal("otra acción debía permitirse")
	}
}

func TestGifRegisterInteraction(t *testing.T) {
	dir := t.TempDir()
	prev := gifStatsDir
	gifStatsDir = func() string { return dir }
	t.Cleanup(func() { gifStatsDir = prev })

	chat := "1205@g.us"
	sender := "584111@s.whatsapp.net"
	target := "584222@s.whatsapp.net"
	if got := gifRegisterInteraction(chat, sender, "Ana", target, "Luis", "kiss"); got != 1 {
		t.Fatalf("contador=%d", got)
	}
	if got := gifRegisterInteraction(chat, sender, "Ana", target, "Luis", "kiss"); got != 2 {
		t.Fatalf("contador=%d", got)
	}
	stats := gifLoadStats()
	if stats[chat][sender].Targets[target].Actions["kiss"] != 2 {
		t.Fatalf("stats=%#v", stats)
	}
	if stats[chat][sender].Targets[target].Total != 2 {
		t.Fatalf("total=%#v", stats[chat][sender].Targets[target])
	}
}

func TestGifSendTop(t *testing.T) {
	dir := t.TempDir()
	prev := gifStatsDir
	gifStatsDir = func() string { return dir }
	t.Cleanup(func() { gifStatsDir = prev })

	chat := "1205@g.us"
	gifRegisterInteraction(chat, "a@s.whatsapp.net", "Ana", "b@s.whatsapp.net", "Luis", "kiss")

	c := &capture{}
	ctx := wagoCtx(chat)
	ctx.Command = "topg"
	ctx.SendHook = func(m *waE2E.Message) error { c.texts = append(c.texts, messageText(m)); return nil }
	if err := (Gif{}).Handle(ctx); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	// Sin config activa topg no responde.
	if len(c.texts) != 0 {
		t.Fatalf("textos=%#v", c.texts)
	}
	if err := gifSendTop(ctx); err != nil {
		t.Fatalf("gifSendTop: %v", err)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "Ana") || !strings.Contains(c.texts[0], "Luis") {
		t.Fatalf("textos=%#v", c.texts)
	}
}

func TestGifHelp(t *testing.T) {
	dir := t.TempDir()
	prev := gifStatsDir
	gifStatsDir = func() string { return dir }
	t.Cleanup(func() { gifStatsDir = prev })
	if err := gifSaveConfig(gifConfig{ActiveGroups: []string{"1205@g.us"}}); err != nil {
		t.Fatal(err)
	}

	c := &capture{}
	ctx := wagoCtx("1205@g.us")
	ctx.Command = "g"
	ctx.Args = []string{"help"}
	ctx.SendHook = func(m *waE2E.Message) error { c.texts = append(c.texts, messageText(m)); return nil }
	if err := (Gif{}).Handle(ctx); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "Guía de interacciones") || !strings.Contains(c.texts[0], "kiss") {
		t.Fatalf("textos=%#v", c.texts)
	}
}

func TestGifPresetEnvia(t *testing.T) {
	dir := t.TempDir()
	prevDir := gifStatsDir
	gifStatsDir = func() string { return dir }
	t.Cleanup(func() { gifStatsDir = prevDir })
	if err := gifSaveConfig(gifConfig{ActiveGroups: []string{"1205@g.us"}}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":[{"media_formats":{"tinymp4":{"url":"https://cdn.example/gif.mp4"}}}]}`))
	}))
	defer srv.Close()

	prevURL, prevHTTP, prevSend := gifTenorURL, gifHTTP, gifSendFn
	gifTenorURL = srv.URL
	gifHTTP = srv.Client()
	var caption string
	var mentions []types.JID
	gifSendFn = func(_ *core.Context, _ types.JID, gifURL, cap string, mens []types.JID) error {
		if gifURL != "https://cdn.example/gif.mp4" {
			t.Errorf("gifURL=%q", gifURL)
		}
		caption = cap
		mentions = mens
		return nil
	}
	t.Cleanup(func() { gifTenorURL, gifHTTP, gifSendFn = prevURL, prevHTTP, prevSend })
	t.Setenv("TENOR_API_KEY", "clave")
	t.Setenv("OWNER_NUMBERS", "")

	c := &capture{}
	ctx := wagoCtx("1205@g.us")
	ctx.Command = "g"
	ctx.Args = []string{"kiss", "@584222@s.whatsapp.net"}
	ctx.SendHook = func(m *waE2E.Message) error { c.texts = append(c.texts, messageText(m)); return nil }
	ctx.ReactHook = func(e string) error { c.reactions = append(c.reactions, e); return nil }
	ctx.Message.Message = &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text:        protoString("g kiss @584222"),
		ContextInfo: &waE2E.ContextInfo{MentionedJID: []string{"584222@s.whatsapp.net"}},
	}}

	if err := (Gif{}).Handle(ctx); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if !strings.Contains(caption, "besó") || !strings.Contains(caption, "1 veces") {
		t.Fatalf("caption=%q", caption)
	}
	if len(mentions) != 1 {
		t.Fatalf("menciones=%#v", mentions)
	}
}
