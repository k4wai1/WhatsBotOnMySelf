package addons

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"

	"whatsbot-go/internal/core"
)

func TestTopCommands(t *testing.T) {
	got := (Top{}).Commands()
	if len(got) != 2 || got[0] != "ongrup" || got[1] != "top" {
		t.Fatalf("comandos=%#v", got)
	}
}

func TestTopFormatoMiles(t *testing.T) {
	casos := map[int]string{0: "0", 999: "999", 1000: "1.000", 1234567: "1.234.567"}
	for entrada, want := range casos {
		if got := topFormatoMiles(entrada); got != want {
			t.Fatalf("topFormatoMiles(%d)=%q, esperaba %q", entrada, got, want)
		}
	}
}

func TestTopStatsRoundtrip(t *testing.T) {
	dir := t.TempDir()
	prevDir := topStatsDir
	topStatsDir = func() string { return dir }
	t.Cleanup(func() { topStatsDir = prevDir })

	chat := "1205@g.us"
	if err := topGuardarActivos([]string{chat}); err != nil {
		t.Fatalf("topGuardarActivos: %v", err)
	}
	if !wagoContains(topActiveGroups(), chat) {
		t.Fatalf("activos=%#v", topActiveGroups())
	}

	topUpdateStats(chat, "584141234567@s.whatsapp.net", "msg", "Ana", 3)
	topUpdateStats(chat, "584141234567@s.whatsapp.net", "char", "Ana", 10)
	stats := topCargarStats(chat, "")
	if stats == nil {
		t.Fatal("no se cargaron stats")
	}
	user := stats.Users["584141234567@s.whatsapp.net"]
	if user == nil || user.Msg != 3 || user.Char != 10 || user.Name != "Ana" {
		t.Fatalf("usuario=%#v", user)
	}
}

func TestTopActivar(t *testing.T) {
	dir := t.TempDir()
	prevDir := topStatsDir
	topStatsDir = func() string { return dir }
	t.Cleanup(func() { topStatsDir = prevDir })

	c := &capture{}
	ctx := wagoCtx("1205@g.us")
	ctx.Command = "ongrup"
	ctx.SendHook = func(m *waE2E.Message) error {
		c.texts = append(c.texts, messageText(m))
		return nil
	}
	if err := (Top{}).Handle(ctx); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "Estadísticas activadas") {
		t.Fatalf("textos=%#v", c.texts)
	}

	c2 := &capture{}
	ctx2 := wagoCtx("1205@g.us")
	ctx2.Command = "ongrup"
	ctx2.SendHook = func(m *waE2E.Message) error {
		c2.texts = append(c2.texts, messageText(m))
		return nil
	}
	if err := (Top{}).Handle(ctx2); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c2.texts) != 1 || !strings.Contains(c2.texts[0], "ya están activas") {
		t.Fatalf("textos=%#v", c2.texts)
	}
}

func TestTopRanking(t *testing.T) {
	dir := t.TempDir()
	prevDir, prevNow := topStatsDir, topNow
	topStatsDir = func() string { return dir }
	topNow = func() time.Time { return time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { topStatsDir, topNow = prevDir, prevNow })

	chat := "1205@g.us"
	stats := &topStats{UptimeMinutos: 60, Users: map[string]*topUsuario{
		"a@s.whatsapp.net": {Name: "Ana", Msg: 1234},
		"b@s.whatsapp.net": {Name: "Luis", Msg: 50},
	}}
	if err := core.WriteJSON(topStatsPath(chat, "2026-01"), stats); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	c := &capture{}
	ctx := wagoCtx(chat)
	ctx.Command = "top"
	ctx.Args = []string{"2026-01", "mensajes"}
	ctx.SendHook = func(m *waE2E.Message) error {
		c.texts = append(c.texts, messageText(m))
		return nil
	}
	if err := (Top{}).Handle(ctx); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.texts) != 1 {
		t.Fatalf("textos=%#v", c.texts)
	}
	for _, want := range []string{"Top Mensajes", "2026-01", "🥇 Ana: 1.234", "🥈 Luis: 50", "Uptime Bot"} {
		if !strings.Contains(c.texts[0], want) {
			t.Fatalf("texto sin %q:\n%s", want, c.texts[0])
		}
	}
}

func TestTopSinDatos(t *testing.T) {
	dir := t.TempDir()
	prevDir := topStatsDir
	topStatsDir = func() string { return dir }
	t.Cleanup(func() { topStatsDir = prevDir })

	c := &capture{}
	ctx := wagoCtx("1205@g.us")
	ctx.Command = "top"
	ctx.SendHook = func(m *waE2E.Message) error {
		c.texts = append(c.texts, messageText(m))
		return nil
	}
	if err := (Top{}).Handle(ctx); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "No hay datos") {
		t.Fatalf("textos=%#v", c.texts)
	}
}

func TestTopEsComandoStats(t *testing.T) {
	casos := map[string]bool{
		",top":          true,
		",ongrup":       true,
		"!top mensajes": true,
		"hola":          false,
		",otro":         false,
	}
	for texto, want := range casos {
		if got := topEsComandoStats(texto); got != want {
			t.Fatalf("topEsComandoStats(%q)=%v, esperaba %v", texto, got, want)
		}
	}
}

func TestTopStatsPath(t *testing.T) {
	prevDir := topStatsDir
	topStatsDir = func() string { return "/tmp/stats" }
	t.Cleanup(func() { topStatsDir = prevDir })

	got := topStatsPath("1205@g.us", "2026-11")
	if got != filepath.Join("/tmp/stats", "1205@g.us_2026-11.json") {
		t.Fatalf("path=%q", got)
	}
}
