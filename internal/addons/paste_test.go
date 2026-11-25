package addons

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"whatsbot-go/internal/core"
)

func pasteSetup(t *testing.T, rs, x0 http.HandlerFunc) {
	t.Helper()
	rsSrv := httptest.NewServer(rs)
	t.Cleanup(rsSrv.Close)
	x0Srv := httptest.NewServer(x0)
	t.Cleanup(x0Srv.Close)

	prevRs, prevX0 := pasteRsURL, paste0x0URL
	pasteRsURL = rsSrv.URL
	paste0x0URL = x0Srv.URL
	t.Cleanup(func() {
		pasteRsURL = prevRs
		paste0x0URL = prevX0
	})
}

func pasteQuotedCtx(c *capture, cmd, quoted string) *core.Context {
	ctx := c.newCtx(cmd)
	ctx.Message = &events.Message{Message: &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String(cmd),
			ContextInfo: &waE2E.ContextInfo{
				QuotedMessage: &waE2E.Message{Conversation: proto.String(quoted)},
			},
		},
	}}
	return ctx
}

func TestPasteCommands(t *testing.T) {
	got := Paste{}.Commands()
	if len(got) != 1 || got[0] != "paste" {
		t.Fatalf("comandos inesperados: %#v", got)
	}
}

func TestPasteSinTexto(t *testing.T) {
	c := &capture{}
	if err := (Paste{}).Handle(c.newCtx("paste")); err != nil {
		t.Fatalf("Handle devolvió error: %v", err)
	}
	if len(c.reactions) != 1 || c.reactions[0] != "⚠️" {
		t.Fatalf("esperaba ⚠️, obtuve %#v", c.reactions)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "📤 *Paste*") {
		t.Fatalf("texto inesperado: %#v", c.texts)
	}
}

func TestPasteArgs(t *testing.T) {
	var received string
	pasteSetup(t,
		func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			received = string(body)
			_, _ = w.Write([]byte("https://paste.rs/abc\n"))
		},
		func(w http.ResponseWriter, r *http.Request) { t.Errorf("no debía usarse el respaldo") },
	)

	c := &capture{}
	if err := (Paste{}).Handle(c.newCtx("paste", "hola", "mundo")); err != nil {
		t.Fatalf("Handle devolvió error: %v", err)
	}
	if received != "hola mundo" {
		t.Fatalf("cuerpo inesperado: %q", received)
	}
	if len(c.texts) != 1 || c.texts[0] != "📤 https://paste.rs/abc" {
		t.Fatalf("texto inesperado: %#v", c.texts)
	}
	if len(c.reactions) != 2 || c.reactions[0] != "⏳" || c.reactions[1] != "✅" {
		t.Fatalf("reacciones inesperadas: %#v", c.reactions)
	}
}

func TestPasteUsaCitado(t *testing.T) {
	pasteSetup(t,
		func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			if string(body) != "texto citado" {
				t.Errorf("cuerpo inesperado: %q", body)
			}
			_, _ = w.Write([]byte("https://paste.rs/citado"))
		},
		func(w http.ResponseWriter, r *http.Request) { t.Errorf("no debía usarse el respaldo") },
	)

	c := &capture{}
	if err := (Paste{}).Handle(pasteQuotedCtx(c, "paste", "texto citado")); err != nil {
		t.Fatalf("Handle devolvió error: %v", err)
	}
	if len(c.texts) != 1 || c.texts[0] != "📤 https://paste.rs/citado" {
		t.Fatalf("texto inesperado: %#v", c.texts)
	}
}

func TestPasteRespaldo0x0(t *testing.T) {
	pasteSetup(t,
		func(w http.ResponseWriter, r *http.Request) { http.Error(w, "boom", http.StatusInternalServerError) },
		func(w http.ResponseWriter, r *http.Request) {
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Errorf("multipart inválido: %v", err)
			}
			_, _ = w.Write([]byte("https://0x0.st/xyz\n"))
		},
	)

	c := &capture{}
	if err := (Paste{}).Handle(c.newCtx("paste", "texto")); err != nil {
		t.Fatalf("Handle devolvió error: %v", err)
	}
	if len(c.texts) != 1 || c.texts[0] != "📤 https://0x0.st/xyz" {
		t.Fatalf("texto inesperado: %#v", c.texts)
	}
	if len(c.reactions) != 2 || c.reactions[0] != "⏳" || c.reactions[1] != "✅" {
		t.Fatalf("reacciones inesperadas: %#v", c.reactions)
	}
}

func TestPasteAmbosFallan(t *testing.T) {
	pasteSetup(t,
		func(w http.ResponseWriter, r *http.Request) { http.Error(w, "boom", http.StatusInternalServerError) },
		func(w http.ResponseWriter, r *http.Request) { http.Error(w, "boom", http.StatusInternalServerError) },
	)

	c := &capture{}
	if err := (Paste{}).Handle(c.newCtx("paste", "texto")); err != nil {
		t.Fatalf("Handle devolvió error: %v", err)
	}
	if len(c.reactions) != 2 || c.reactions[0] != "⏳" || c.reactions[1] != "❌" {
		t.Fatalf("reacciones inesperadas: %#v", c.reactions)
	}
	if len(c.texts) != 0 {
		t.Fatalf("no esperaba texto, obtuve %#v", c.texts)
	}
}

func TestPasteMaximoCaracteres(t *testing.T) {
	c := &capture{}
	if err := (Paste{}).Handle(c.newCtx("paste", strings.Repeat("a", pasteMaxChars+1))); err != nil {
		t.Fatalf("Handle devolvió error: %v", err)
	}
	if len(c.reactions) != 1 || c.reactions[0] != "❌" {
		t.Fatalf("esperaba ❌, obtuve %#v", c.reactions)
	}
	if len(c.texts) != 1 || c.texts[0] != "Máximo 100000 caracteres." {
		t.Fatalf("texto inesperado: %#v", c.texts)
	}
}
