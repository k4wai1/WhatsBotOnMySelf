package addons

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"whatsbot-go/internal/core"
)

func acortarSetup(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	prev, prevFallback := acortarAPIBase, acortarFallbackBase
	acortarAPIBase = srv.URL
	acortarFallbackBase = srv.URL + "/fallback"
	t.Cleanup(func() { acortarAPIBase, acortarFallbackBase = prev, prevFallback })
}

func acortarQuotedCtx(c *capture, cmd, quoted string) *core.Context {
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

func TestAcortarCommands(t *testing.T) {
	got := Acortar{}.Commands()
	if len(got) != 2 || got[0] != "acortar" || got[1] != "short" {
		t.Fatalf("comandos inesperados: %#v", got)
	}
}

func TestAcortarSinArgumentos(t *testing.T) {
	c := &capture{}
	if err := (Acortar{}).Handle(c.newCtx("acortar")); err != nil {
		t.Fatalf("Handle devolvió error: %v", err)
	}
	if len(c.reactions) != 1 || c.reactions[0] != "⚠️" {
		t.Fatalf("esperaba ⚠️, obtuve %#v", c.reactions)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "Uso: `.acortar https://enlace-largo.com`") {
		t.Fatalf("texto inesperado: %#v", c.texts)
	}
}

func TestAcortarEnlaceInvalido(t *testing.T) {
	c := &capture{}
	if err := (Acortar{}).Handle(c.newCtx("acortar", "http://")); err != nil {
		t.Fatalf("Handle devolvió error: %v", err)
	}
	if len(c.reactions) != 1 || c.reactions[0] != "❌" {
		t.Fatalf("esperaba ❌, obtuve %#v", c.reactions)
	}
	if len(c.texts) != 1 || c.texts[0] != "Ese enlace no parece válido." {
		t.Fatalf("texto inesperado: %#v", c.texts)
	}
}

func TestAcortarOk(t *testing.T) {
	acortarSetup(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("format"); got != "json" {
			t.Errorf("format inesperado: %q", got)
		}
		if got := r.URL.Query().Get("url"); got != "https://example.com/largo" {
			t.Errorf("url inesperada: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"shorturl":"https://is.gd/abc"}`))
	})

	c := &capture{}
	if err := (Acortar{}).Handle(c.newCtx("acortar", "example.com/largo")); err != nil {
		t.Fatalf("Handle devolvió error: %v", err)
	}
	if len(c.texts) != 1 || c.texts[0] != "🔗 https://is.gd/abc" {
		t.Fatalf("texto inesperado: %#v", c.texts)
	}
	if len(c.reactions) != 2 || c.reactions[0] != "⏳" || c.reactions[1] != "✅" {
		t.Fatalf("reacciones inesperadas: %#v", c.reactions)
	}
}

func TestAcortarErrorAPI(t *testing.T) {
	acortarSetup(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errormessage":"rate limit"}`))
	})

	c := &capture{}
	if err := (Acortar{}).Handle(c.newCtx("short", "https://example.com")); err != nil {
		t.Fatalf("Handle devolvió error: %v", err)
	}
	if len(c.reactions) != 2 || c.reactions[0] != "⏳" || c.reactions[1] != "❌" {
		t.Fatalf("reacciones inesperadas: %#v", c.reactions)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "No pude acortar") {
		t.Fatalf("texto inesperado: %#v", c.texts)
	}
}

func TestAcortarFallbackHtmlDeIsGd(t *testing.T) {
	acortarSetup(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/fallback") {
			if got := r.URL.Query().Get("url"); got != "https://example.com/largo" {
				t.Errorf("url inesperada en fallback: %q", got)
			}
			_, _ = w.Write([]byte("https://tinyurl.com/abc123\n"))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>502 proxy</html>"))
	})

	c := &capture{}
	if err := (Acortar{}).Handle(c.newCtx("acortar", "https://example.com/largo")); err != nil {
		t.Fatalf("Handle devolvió error: %v", err)
	}
	if len(c.texts) != 1 || c.texts[0] != "🔗 https://tinyurl.com/abc123" {
		t.Fatalf("texto inesperado: %#v", c.texts)
	}
	if len(c.reactions) != 2 || c.reactions[0] != "⏳" || c.reactions[1] != "✅" {
		t.Fatalf("reacciones inesperadas: %#v", c.reactions)
	}
}

func TestAcortarTextoCitado(t *testing.T) {
	acortarSetup(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("url"); got != "https://example.com/x" {
			t.Errorf("url inesperada: %q", got)
		}
		_, _ = w.Write([]byte(`{"shorturl":"https://is.gd/x"}`))
	})

	c := &capture{}
	if err := (Acortar{}).Handle(acortarQuotedCtx(c, "acortar", "https://example.com/x")); err != nil {
		t.Fatalf("Handle devolvió error: %v", err)
	}
	if len(c.texts) != 1 || c.texts[0] != "🔗 https://is.gd/x" {
		t.Fatalf("texto inesperado: %#v", c.texts)
	}
}

func TestAcortarNormalizar(t *testing.T) {
	got, ok := acortarNormalizar("  example.com/a  ")
	if !ok || got != "https://example.com/a" {
		t.Fatalf("normalización inesperada: %q / %v", got, ok)
	}
	u, err := url.Parse(got)
	if err != nil || u.Host != "example.com" {
		t.Fatalf("URL inesperada: %v / %v", u, err)
	}
}
