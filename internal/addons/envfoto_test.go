package addons

import (
	"bytes"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"

	"whatsbot-go/internal/core"
)

func TestEnvfotoCommands(t *testing.T) {
	if got := (Envfoto{}).Commands(); len(got) != 1 || got[0] != "envfoto" {
		t.Fatalf("comandos=%#v", got)
	}
}

func TestEnvfotoGenerarImagen(t *testing.T) {
	data, err := envfotoGenerarImagen(time.Date(2026, 11, 24, 10, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("envfotoGenerarImagen: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("png inválido: %v", err)
	}
	if img.Bounds().Dx() != 800 || img.Bounds().Dy() != 400 {
		t.Fatalf("tamaño=%v", img.Bounds())
	}
}

func TestEnvfotoSinCanal(t *testing.T) {
	t.Setenv("NEWS_CHANNEL_ID", "")
	c := &capture{}
	if err := (Envfoto{}).Handle(c.newCtx("envfoto")); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "NEWS_CHANNEL_ID") {
		t.Fatalf("textos=%#v", c.texts)
	}
}

func TestEnvfotoModoInvalido(t *testing.T) {
	t.Setenv("NEWS_CHANNEL_ID", "120363000000000000@newsletter")
	c := &capture{}
	ctx := wagoCtx("1205@g.us")
	ctx.Command = "envfoto"
	ctx.Args = []string{"9"}
	ctx.SendHook = func(m *waE2E.Message) error { c.texts = append(c.texts, messageText(m)); return nil }
	ctx.ReactHook = func(e string) error { c.reactions = append(c.reactions, e); return nil }

	if err := (Envfoto{}).Handle(ctx); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "modo inválido") {
		t.Fatalf("textos=%#v", c.texts)
	}
	if len(c.reactions) != 2 || c.reactions[0] != "⏳" || c.reactions[1] != "✅" {
		t.Fatalf("reacciones=%#v", c.reactions)
	}
}

func TestEnvfotoModo1Envia(t *testing.T) {
	t.Setenv("NEWS_CHANNEL_ID", "120363000000000000@newsletter")
	var enviado bool
	prev := envfotoSendFn
	envfotoSendFn = func(_ *core.Context, _ types.JID, data []byte, caption string) error {
		enviado = true
		if len(data) == 0 || !strings.Contains(caption, "modo 1") {
			t.Errorf("envío inesperado: len=%d caption=%q", len(data), caption)
		}
		return nil
	}
	t.Cleanup(func() { envfotoSendFn = prev })

	c := &capture{}
	ctx := wagoCtx("1205@g.us")
	ctx.Command = "envfoto"
	ctx.Args = []string{"1"}
	ctx.SendHook = func(m *waE2E.Message) error { c.texts = append(c.texts, messageText(m)); return nil }
	ctx.ReactHook = func(e string) error { c.reactions = append(c.reactions, e); return nil }

	if err := (Envfoto{}).Handle(ctx); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if !enviado {
		t.Fatal("no se envió la imagen")
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "Imagen enviada al canal (modo 1)") {
		t.Fatalf("textos=%#v", c.texts)
	}
}

func TestEnvfotoModo3Archivo(t *testing.T) {
	t.Setenv("NEWS_CHANNEL_ID", "120363000000000000@newsletter")
	dir := t.TempDir()
	ruta := filepath.Join(dir, "imagen.png")
	if err := os.WriteFile(ruta, []byte("datos"), 0o644); err != nil {
		t.Fatal(err)
	}
	prev := envfotoSendFn
	var caption string
	envfotoSendFn = func(_ *core.Context, _ types.JID, _ []byte, cap string) error {
		caption = cap
		return nil
	}
	t.Cleanup(func() { envfotoSendFn = prev })

	c := &capture{}
	ctx := wagoCtx("1205@g.us")
	ctx.Command = "envfoto"
	ctx.Args = []string{"3", ruta}
	ctx.SendHook = func(m *waE2E.Message) error { c.texts = append(c.texts, messageText(m)); return nil }
	ctx.ReactHook = func(e string) error { c.reactions = append(c.reactions, e); return nil }

	if err := (Envfoto{}).Handle(ctx); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if !strings.Contains(caption, "imagen.png") {
		t.Fatalf("caption=%q", caption)
	}
}

func TestEnvfotoDescargar(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("User-Agent"), "Mozilla") {
			t.Errorf("UA=%q", r.Header.Get("User-Agent"))
		}
		_, _ = w.Write([]byte("IMAGEN"))
	}))
	defer srv.Close()

	prev := envfotoHTTP
	envfotoHTTP = srv.Client()
	t.Cleanup(func() { envfotoHTTP = prev })

	data, err := envfotoDescargar(srv.URL)
	if err != nil || string(data) != "IMAGEN" {
		t.Fatalf("data=%q err=%v", data, err)
	}
}
