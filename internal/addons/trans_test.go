package addons

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"whatsbot-go/internal/core"
)

func transSetup(t *testing.T, groq, deepseek http.HandlerFunc) {
	t.Helper()
	groqSrv := httptest.NewServer(groq)
	t.Cleanup(groqSrv.Close)
	dsSrv := httptest.NewServer(deepseek)
	t.Cleanup(dsSrv.Close)

	prevGroq, prevDS := transGroqURL, transDeepSeekURL
	transGroqURL = groqSrv.URL
	transDeepSeekURL = dsSrv.URL
	prevDownload := transDownloadMedia
	transDownloadMedia = func(*core.Context, *waE2E.Message) ([]byte, error) {
		return []byte("audio"), nil
	}
	t.Cleanup(func() {
		transGroqURL = prevGroq
		transDeepSeekURL = prevDS
		transDownloadMedia = prevDownload
	})
}

func transJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(body))
}

func transAudioCtx(c *capture, cmd string) *core.Context {
	ctx := c.newCtx(cmd)
	ctx.Message = &events.Message{Message: &waE2E.Message{
		AudioMessage: &waE2E.AudioMessage{Mimetype: proto.String("audio/ogg")},
	}}
	return ctx
}

func transQuotedAudioCtx(c *capture, cmd string) *core.Context {
	ctx := c.newCtx(cmd)
	ctx.Message = &events.Message{Message: &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String(cmd),
			ContextInfo: &waE2E.ContextInfo{
				QuotedMessage: &waE2E.Message{
					AudioMessage: &waE2E.AudioMessage{Mimetype: proto.String("audio/ogg")},
				},
			},
		},
	}}
	return ctx
}

func TestTransCommands(t *testing.T) {
	got := Trans{}.Commands()
	if len(got) != 2 || got[0] != "tr" || got[1] != "tri" {
		t.Fatalf("comandos inesperados: %#v", got)
	}
}

func TestTransSinAudio(t *testing.T) {
	c := &capture{}
	if err := (Trans{}).Handle(c.newCtx("tr")); err != nil {
		t.Fatalf("Handle devolvió error: %v", err)
	}
	if len(c.reactions) != 1 || c.reactions[0] != "❓" {
		t.Fatalf("esperaba ❓, obtuve %#v", c.reactions)
	}
}

func TestTransTr(t *testing.T) {
	transSetup(t,
		func(w http.ResponseWriter, r *http.Request) {
			if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Bearer") {
				t.Errorf("falta Authorization: %q", got)
			}
			transJSON(w, `{"text":"hola mundo"}`)
		},
		func(w http.ResponseWriter, r *http.Request) { t.Errorf("no debía llamarse a DeepSeek") },
	)

	c := &capture{}
	if err := (Trans{}).Handle(transAudioCtx(c, "tr")); err != nil {
		t.Fatalf("Handle devolvió error: %v", err)
	}
	if len(c.texts) != 1 || c.texts[0] != "🎙️ \nhola mundo" {
		t.Fatalf("texto inesperado: %#v", c.texts)
	}
	if len(c.reactions) != 2 || c.reactions[0] != "⏳" || c.reactions[1] != "✅" {
		t.Fatalf("reacciones inesperadas: %#v", c.reactions)
	}
}

func TestTransTri(t *testing.T) {
	transSetup(t,
		func(w http.ResponseWriter, r *http.Request) { transJSON(w, `{"text":"eh hola eh"}`) },
		func(w http.ResponseWriter, r *http.Request) {
			transJSON(w, `{"choices":[{"message":{"content":"  hola  "}}]}`)
		},
	)

	c := &capture{}
	if err := (Trans{}).Handle(transAudioCtx(c, "tri")); err != nil {
		t.Fatalf("Handle devolvió error: %v", err)
	}
	if len(c.texts) != 1 || c.texts[0] != "✨ \nhola" {
		t.Fatalf("texto inesperado: %#v", c.texts)
	}
	if len(c.reactions) != 2 || c.reactions[0] != "⏳" || c.reactions[1] != "✅" {
		t.Fatalf("reacciones inesperadas: %#v", c.reactions)
	}
}

func TestTransTriFallback(t *testing.T) {
	transSetup(t,
		func(w http.ResponseWriter, r *http.Request) { transJSON(w, `{"text":"eh hola eh"}`) },
		func(w http.ResponseWriter, r *http.Request) { http.Error(w, "boom", http.StatusInternalServerError) },
	)

	c := &capture{}
	if err := (Trans{}).Handle(transAudioCtx(c, "tri")); err != nil {
		t.Fatalf("Handle devolvió error: %v", err)
	}
	want := "⚠️ (Fallback a transcripción cruda)\n\neh hola eh"
	if len(c.texts) != 1 || c.texts[0] != want {
		t.Fatalf("texto inesperado: %#v", c.texts)
	}
	if len(c.reactions) != 1 || c.reactions[0] != "⏳" {
		t.Fatalf("reacciones inesperadas: %#v", c.reactions)
	}
}

func TestTransAudioCitado(t *testing.T) {
	transSetup(t,
		func(w http.ResponseWriter, r *http.Request) { transJSON(w, `{"text":"citado"}`) },
		func(w http.ResponseWriter, r *http.Request) { t.Errorf("no debía llamarse a DeepSeek") },
	)

	c := &capture{}
	if err := (Trans{}).Handle(transQuotedAudioCtx(c, "tr")); err != nil {
		t.Fatalf("Handle devolvió error: %v", err)
	}
	if len(c.texts) != 1 || c.texts[0] != "🎙️ \ncitado" {
		t.Fatalf("texto inesperado: %#v", c.texts)
	}
}

func TestTransGroqFalla(t *testing.T) {
	transSetup(t,
		func(w http.ResponseWriter, r *http.Request) { http.Error(w, "boom", http.StatusInternalServerError) },
		func(w http.ResponseWriter, r *http.Request) {},
	)

	c := &capture{}
	if err := (Trans{}).Handle(transAudioCtx(c, "tr")); err != nil {
		t.Fatalf("Handle devolvió error: %v", err)
	}
	if len(c.reactions) != 2 || c.reactions[0] != "⏳" || c.reactions[1] != "❌" {
		t.Fatalf("reacciones inesperadas: %#v", c.reactions)
	}
}
