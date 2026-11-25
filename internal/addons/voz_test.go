package addons

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"

	"whatsbot-go/internal/core"
)

func TestVozCommands(t *testing.T) {
	got := Voz{}.Commands()
	if len(got) != 2 || got[0] != "voz" || got[1] != "tts" {
		t.Fatalf("comandos inesperados: %#v", got)
	}
}

func TestVozTextoVacio(t *testing.T) {
	vozResetCooldowns()
	c := &capture{}
	if err := (Voz{}).Handle(c.newCtx("voz")); err != nil {
		t.Fatalf("Handle devolvió error: %v", err)
	}
	if len(c.reactions) != 1 || c.reactions[0] != "⚠️" {
		t.Fatalf("esperaba ⚠️, obtuve %#v", c.reactions)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "Convierte texto a nota de voz") {
		t.Fatalf("ayuda inesperada: %#v", c.texts)
	}
}

func TestVozInvalida(t *testing.T) {
	vozResetCooldowns()
	c := &capture{}
	if err := (Voz{}).Handle(c.newCtx("voz", "@nope", "hola")); err != nil {
		t.Fatalf("Handle devolvió error: %v", err)
	}
	if len(c.reactions) != 1 || c.reactions[0] != "⚠️" {
		t.Fatalf("esperaba ⚠️, obtuve %#v", c.reactions)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "Voz inválida") {
		t.Fatalf("mensaje inesperado: %#v", c.texts)
	}
}

func TestVozListaVoces(t *testing.T) {
	vozResetCooldowns()
	c := &capture{}
	if err := (Voz{}).Handle(c.newCtx("voz", "voces")); err != nil {
		t.Fatalf("Handle devolvió error: %v", err)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "Voces recomendadas") || !strings.Contains(c.texts[0], "es-VE-SebastianNeural") {
		t.Fatalf("lista inesperada: %#v", c.texts)
	}
}

func TestVozBinarioFaltante(t *testing.T) {
	vozResetCooldowns()
	prev := vozFindEdgeTTS
	vozFindEdgeTTS = func() string { return "" }
	t.Cleanup(func() { vozFindEdgeTTS = prev })

	c := &capture{}
	if err := (Voz{}).Handle(c.newCtx("voz", "hola", "mundo")); err != nil {
		t.Fatalf("Handle devolvió error: %v", err)
	}
	if len(c.reactions) != 1 || c.reactions[0] != "❌" {
		t.Fatalf("esperaba ❌, obtuve %#v", c.reactions)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "edge-tts no está instalado") {
		t.Fatalf("mensaje inesperado: %#v", c.texts)
	}
}

func TestVozSintetizaYEnviaAudio(t *testing.T) {
	vozResetCooldowns()
	prevFind, prevRun := vozFindEdgeTTS, vozRun
	vozFindEdgeTTS = func() string { return "/fake/edge-tts" }
	vozRun = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if filepath.Base(name) == "edge-tts" {
			for i, a := range args {
				if a == "--write-media" && i+1 < len(args) {
					return nil, os.WriteFile(args[i+1], make([]byte, 300), 0o600)
				}
			}
			return nil, errors.New("sin --write-media")
		}
		if filepath.Base(name) == "ffmpeg" {
			return nil, os.WriteFile(args[len(args)-1], append([]byte("OggS"), bytes.Repeat([]byte{0x01}, 300)...), 0o600)
		}
		return nil, errors.New("binario inesperado")
	}
	t.Cleanup(func() {
		vozFindEdgeTTS = prevFind
		vozRun = prevRun
	})

	var audio *waE2E.AudioMessage
	var reactions, texts []string
	ctx := &core.Context{
		Command: "voz",
		Args:    []string{"@es-MX-DaliaNeural", "hola"},
		ReactHook: func(e string) error {
			reactions = append(reactions, e)
			return nil
		},
		SendHook: func(m *waE2E.Message) error {
			if m.GetAudioMessage() != nil {
				audio = m.GetAudioMessage()
			}
			if m.GetConversation() != "" {
				texts = append(texts, m.GetConversation())
			}
			if m.GetExtendedTextMessage() != nil {
				texts = append(texts, m.GetExtendedTextMessage().GetText())
			}
			return nil
		},
	}
	if err := (Voz{}).Handle(ctx); err != nil {
		t.Fatalf("Handle devolvió error: %v", err)
	}
	if audio == nil {
		t.Fatalf("no se envió audio; reacciones=%v textos=%v", reactions, texts)
	}
	if !audio.GetPTT() {
		t.Fatal("el audio debe ser nota de voz")
	}
	if audio.GetMimetype() != "audio/ogg; codecs=opus" {
		t.Fatalf("mimetype inesperado: %s", audio.GetMimetype())
	}
}

func TestVozErrorEdge(t *testing.T) {
	vozResetCooldowns()
	prevFind, prevRun := vozFindEdgeTTS, vozRun
	vozFindEdgeTTS = func() string { return "/fake/edge-tts" }
	vozRun = func(context.Context, string, ...string) ([]byte, error) {
		return []byte("boom"), errors.New("exit 1")
	}
	t.Cleanup(func() {
		vozFindEdgeTTS = prevFind
		vozRun = prevRun
	})

	c := &capture{}
	ctx := c.newCtx("voz", "@es-MX-DaliaNeural", "hola")
	if err := (Voz{}).Handle(ctx); err != nil {
		t.Fatalf("Handle devolvió error: %v", err)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "edge-tts falló (¿voz existente?)") {
		t.Fatalf("mensaje inesperado: %#v", c.texts)
	}
}

func TestVozCooldown(t *testing.T) {
	vozResetCooldowns()
	prevNow, prevFind := vozNow, vozFindEdgeTTS
	fixed := time.Unix(1_700_000_000, 0)
	vozNow = func() time.Time { return fixed }
	vozFindEdgeTTS = func() string { return "" }
	t.Cleanup(func() {
		vozNow = prevNow
		vozFindEdgeTTS = prevFind
	})

	ctx := &core.Context{Command: "voz", Args: []string{"hola"}}
	if err := (Voz{}).Handle(ctx); err != nil {
		t.Fatalf("primer Handle: %v", err)
	}
	var got []string
	ctx2 := &core.Context{
		Command:   "voz",
		Args:      []string{"hola"},
		ReactHook: func(e string) error { got = append(got, e); return nil },
	}
	if err := (Voz{}).Handle(ctx2); err != nil {
		t.Fatalf("segundo Handle: %v", err)
	}
	if len(got) != 1 || got[0] != "⏳" {
		t.Fatalf("esperaba ⏳ por cooldown, obtuve %#v", got)
	}
}

func TestVozFFmpegArgs(t *testing.T) {
	args := strings.Join(vozFFmpegArgs("in.mp3", "out.ogg"), " ")
	for _, want := range []string{"-i in.mp3", "-vn", "-ar 48000", "-c:a libopus", "-f ogg", "out.ogg"} {
		if !strings.Contains(args, want) {
			t.Fatalf("faltan argumentos %q en %q", want, args)
		}
	}
}
