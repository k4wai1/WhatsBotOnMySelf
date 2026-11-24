package addons

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"whatsbot-go/internal/core"
)

type resumeCapture struct {
	texts     []string
	reactions []string
}

func resumeCtx(msg *waE2E.Message) (*core.Context, *resumeCapture) {
	c := &resumeCapture{}
	return &core.Context{
		Command: "resu",
		Message: &events.Message{
			Info:    types.MessageInfo{MessageSource: types.MessageSource{Chat: types.NewJID("1", types.DefaultUserServer)}},
			Message: msg,
		},
		SendHook: func(m *waE2E.Message) error {
			if m.GetConversation() != "" {
				c.texts = append(c.texts, m.GetConversation())
			}
			if m.GetExtendedTextMessage() != nil {
				c.texts = append(c.texts, m.GetExtendedTextMessage().GetText())
			}
			return nil
		},
		ReactHook: func(e string) error {
			c.reactions = append(c.reactions, e)
			return nil
		},
	}, c
}

func TestResumeCommands(t *testing.T) {
	got := Resume{}.Commands()
	if len(got) != 1 || got[0] != "resu" {
		t.Fatalf("comandos inesperados: %#v", got)
	}
}

func TestResumeSinMedia(t *testing.T) {
	ctx, c := resumeCtx(&waE2E.Message{Conversation: proto.String("resu")})
	if err := (Resume{}).Handle(ctx); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.reactions) != 1 || c.reactions[0] != "❓" {
		t.Fatalf("reacciones=%#v", c.reactions)
	}
}

func TestResumeFlujoAudio(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "transcriptions"):
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Errorf("multipart inválido: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"text": "esta es la transcripción del audio"})
		default:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{"message": map[string]string{"content": "Resumen en una oración."}}},
			})
		}
	}))
	defer srv.Close()

	prevHTTP, prevGroq, prevDeep, prevDownload := resumeHTTP, resumeGroqURL, resumeDeepSeekURL, resumeDownload
	resumeHTTP = srv.Client()
	resumeGroqURL = srv.URL + "/openai/v1/audio/transcriptions"
	resumeDeepSeekURL = srv.URL + "/chat/completions"
	resumeDownload = func(*core.Context, *waE2E.Message) ([]byte, error) { return []byte("OggS audio"), nil }
	t.Cleanup(func() {
		resumeHTTP, resumeGroqURL, resumeDeepSeekURL, resumeDownload = prevHTTP, prevGroq, prevDeep, prevDownload
	})

	ctx, c := resumeCtx(&waE2E.Message{AudioMessage: &waE2E.AudioMessage{Mimetype: proto.String("audio/ogg")}})
	if err := (Resume{}).Handle(ctx); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.reactions) != 2 || c.reactions[0] != "⏳" || c.reactions[1] != "✅" {
		t.Fatalf("reacciones=%#v", c.reactions)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "Resumen en una oración.") {
		t.Fatalf("textos=%#v", c.texts)
	}
}

func TestResumeFallaTranscripcion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	prevHTTP, prevGroq, prevDownload := resumeHTTP, resumeGroqURL, resumeDownload
	resumeHTTP = srv.Client()
	resumeGroqURL = srv.URL
	resumeDownload = func(*core.Context, *waE2E.Message) ([]byte, error) { return []byte("audio"), nil }
	t.Cleanup(func() { resumeHTTP, resumeGroqURL, resumeDownload = prevHTTP, prevGroq, prevDownload })

	ctx, c := resumeCtx(&waE2E.Message{AudioMessage: &waE2E.AudioMessage{}})
	if err := (Resume{}).Handle(ctx); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.reactions) == 0 || c.reactions[len(c.reactions)-1] != "❌" {
		t.Fatalf("reacciones=%#v", c.reactions)
	}
}
