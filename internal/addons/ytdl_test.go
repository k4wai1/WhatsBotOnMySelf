package addons

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"whatsbot-go/internal/core"
)

func TestYtdlCommands(t *testing.T) {
	got := Ytdl{}.Commands()
	want := []string{"ytdl", "ytdlp", "yt", "youtube"}
	if len(got) != len(want) {
		t.Fatalf("comandos inesperados: %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("comando %d=%q, esperaba %q", i, got[i], want[i])
		}
	}
}

func TestYtdlFmtDurationYSize(t *testing.T) {
	dur := map[float64]string{0: "?:??", 65: "1:05", 3725: "1:02:05"}
	for entrada, want := range dur {
		if got := ytdlFmtDuration(entrada); got != want {
			t.Fatalf("ytdlFmtDuration(%v)=%q, esperaba %q", entrada, got, want)
		}
	}

	tam := map[int64]string{
		0:               "? MB",
		512 * 1024:      "512 KB",
		5 * 1024 * 1024: "5 MB",
	}
	for entrada, want := range tam {
		if got := ytdlFmtSize(entrada); got != want {
			t.Fatalf("ytdlFmtSize(%d)=%q, esperaba %q", entrada, got, want)
		}
	}
}

func TestYtdlBuildFormatOptions(t *testing.T) {
	info := ytdlInfo{
		Title:    "Video de prueba",
		Duration: 60,
		Formats: []ytdlFormat{
			{Height: 1080, Vcodec: "avc1", Acodec: "none", TBR: 4000},
			{Height: 720, Vcodec: "avc1", Acodec: "mp4a", TBR: 2000},
			{Vcodec: "none", Acodec: "opus", TBR: 128},
		},
	}
	options := ytdlBuildFormatOptions(info)
	if len(options) < 2 {
		t.Fatalf("opciones insuficientes: %#v", options)
	}
	var etiquetas []string
	for _, opt := range options {
		etiquetas = append(etiquetas, opt.Label)
	}
	unidas := strings.Join(etiquetas, " | ")
	if !strings.Contains(unidas, "1080p") || !strings.Contains(unidas, "720p") {
		t.Fatalf("faltan resoluciones: %s", unidas)
	}
	ultima := options[len(options)-1]
	if !ultima.IsAudio || ultima.FormatSpec != "bestaudio/best" {
		t.Fatalf("la última opción debe ser solo audio: %#v", ultima)
	}

	menu := ytdlMenuText(info, options)
	if !strings.Contains(menu, "Video de prueba") || !strings.Contains(menu, "1080p") {
		t.Fatalf("menú inesperado:\n%s", menu)
	}
}

func TestYtdlSesiones(t *testing.T) {
	prev := ytdlNow
	ytdlNow = func() time.Time { return time.UnixMilli(1700000000000) }
	t.Cleanup(func() { ytdlNow = prev })

	ytdlMu.Lock()
	ytdlSessions = map[string]*ytdlSession{}
	ytdlSearches = map[string]*ytdlSearchSession{}
	ytdlMu.Unlock()
	t.Cleanup(func() {
		ytdlMu.Lock()
		ytdlSessions = map[string]*ytdlSession{}
		ytdlSearches = map[string]*ytdlSearchSession{}
		ytdlMu.Unlock()
	})

	ytdlStoreSession("u1", ytdlInfo{Title: "x"}, nil)
	if _, ok := ytdlTakeSession("u1"); !ok {
		t.Fatal("la sesión debía existir")
	}
	if _, ok := ytdlTakeSession("u1"); ok {
		t.Fatal("la sesión se consume una sola vez")
	}
	if ytdlCancel("u1") {
		t.Fatal("no debía haber sesión que cancelar")
	}

	ytdlStoreSession("u2", ytdlInfo{Title: "y"}, nil)
	if !ytdlCancel("u2") {
		t.Fatal("debía cancelar la sesión")
	}
}

func TestYtdlSinArgumentosMuestraAyuda(t *testing.T) {
	var textos []string
	var reacciones []string
	ctx := &core.Context{
		Command: "ytdl",
		Message: &events.Message{
			Info: types.MessageInfo{MessageSource: types.MessageSource{
				Chat:   types.NewJID("1", types.DefaultUserServer),
				Sender: types.NewJID("2", types.DefaultUserServer),
			}},
		},
		SendHook: func(m *waE2E.Message) error {
			if m.GetConversation() != "" {
				textos = append(textos, m.GetConversation())
			}
			return nil
		},
		ReactHook: func(e string) error { reacciones = append(reacciones, e); return nil },
	}
	if err := (Ytdl{}).Handle(ctx); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(reacciones) != 1 || reacciones[0] != "❓" {
		t.Fatalf("reacciones=%#v", reacciones)
	}
	if len(textos) != 1 || !strings.Contains(textos[0], "ytdl") {
		t.Fatalf("ayuda inesperada: %#v", textos)
	}
}

func TestYtdlCancelSinSesion(t *testing.T) {
	ytdlMu.Lock()
	ytdlSessions = map[string]*ytdlSession{}
	ytdlSearches = map[string]*ytdlSearchSession{}
	ytdlMu.Unlock()

	var textos []string
	ctx := &core.Context{
		Command: "ytdl",
		Args:    []string{"cancel"},
		Message: &events.Message{
			Info: types.MessageInfo{MessageSource: types.MessageSource{
				Chat:   types.NewJID("1", types.DefaultUserServer),
				Sender: types.NewJID("2", types.DefaultUserServer),
			}},
		},
		SendHook: func(m *waE2E.Message) error {
			if m.GetConversation() != "" {
				textos = append(textos, m.GetConversation())
			}
			return nil
		},
		ReactHook: func(string) error { return nil },
	}
	if err := (Ytdl{}).Handle(ctx); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(textos) != 1 || !strings.Contains(textos[0], "No tienes ninguna descarga activa") {
		t.Fatalf("textos=%#v", textos)
	}
}

func TestYtdlAutorFallback(t *testing.T) {
	casos := []struct {
		d    ytdlFormat
		want string
	}{
		{ytdlFormat{Channel: "Saintontas", Uploader: "Otro"}, "Saintontas"},
		{ytdlFormat{ChannelTitle: "Wuthering Waves"}, "Wuthering Waves"},
		{ytdlFormat{UploaderID: "@wutheringwaves"}, "@wutheringwaves"},
		{ytdlFormat{ChannelID: "UC123"}, "UC123"},
		{ytdlFormat{}, "Desconocido"},
	}
	for i, c := range casos {
		if got := ytdlAutor(c.d); got != c.want {
			t.Fatalf("caso %d: ytdlAutor=%q, esperaba %q", i, got, c.want)
		}
	}
}

func TestYtdlSearchUsaAutorFallback(t *testing.T) {
	prevRun, prevFind, prevCookies := ytdlRunCommand, ytdlFindDeno, ytdlCookiePaths
	ytdlFindDeno = func() string { return "" }
	ytdlCookiePaths = func() []string { return nil }
	ytdlRunCommand = func(_ context.Context, _ string, _ ...string) (string, string, error) {
		return `{"id":"abc","title":"Video","uploader":"Subidor"}`, "", nil
	}
	t.Cleanup(func() { ytdlRunCommand, ytdlFindDeno, ytdlCookiePaths = prevRun, prevFind, prevCookies })

	results, err := ytdlSearchYoutube("prueba")
	if err != nil {
		t.Fatalf("ytdlSearchYoutube error: %v", err)
	}
	if len(results) != 1 || results[0].Channel != "Subidor" {
		t.Fatalf("resultados=%#v", results)
	}
}

func TestYtdlFindDenoDesdeHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("PATH", "")
	denoDir := filepath.Join(dir, ".deno", "bin")
	if err := os.MkdirAll(denoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(denoDir, "deno")
	if err := os.WriteFile(want, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := ytdlFindDenoDefault(); got != want {
		t.Fatalf("deno=%q, esperaba %q", got, want)
	}
}
