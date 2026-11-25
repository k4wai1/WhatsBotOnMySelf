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

// definirSetup levanta dos servidores falsos y apunta las URLs del addon.
func definirSetup(t *testing.T, rae, en http.HandlerFunc) {
	t.Helper()
	raeSrv := httptest.NewServer(rae)
	t.Cleanup(raeSrv.Close)
	enSrv := httptest.NewServer(en)
	t.Cleanup(enSrv.Close)

	prevRae, prevEn := definirRaeURL, definirEnURL
	definirRaeURL = raeSrv.URL + "/"
	definirEnURL = enSrv.URL + "/"
	t.Cleanup(func() {
		definirRaeURL = prevRae
		definirEnURL = prevEn
	})
}

func definirTexto(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func definirCtx(c *capture, cmd string, args ...string) *core.Context {
	return c.newCtx(cmd, args...)
}

func definirQuotedCtx(c *capture, cmd string, quoted string) *core.Context {
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

func TestDefinirCommands(t *testing.T) {
	got := Definir{}.Commands()
	if len(got) != 2 || got[0] != "definir" || got[1] != "def" {
		t.Fatalf("comandos inesperados: %#v", got)
	}
}

func TestDefinirRae(t *testing.T) {
	definirSetup(t,
		func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasSuffix(r.URL.Path, "/casa") {
				t.Errorf("ruta RAE inesperada: %s", r.URL.Path)
			}
			definirTexto(w, 200, `{"ok":true,"data":{"word":"casa","meanings":[{"origin":{"raw":"lat. casa"},"senses":[{"raw":"Edificio para habitar."},{"raw":"Familia."}]}]}}`)
		},
		func(w http.ResponseWriter, r *http.Request) {
			t.Errorf("no debía llamarse al respaldo inglés")
		},
	)

	c := &capture{}
	if err := (Definir{}).Handle(definirCtx(c, "definir", "casa")); err != nil {
		t.Fatalf("Handle devolvió error: %v", err)
	}
	want := "📖 *casa*\n\n🌱 _lat. casa_\n\nEdificio para habitar.\nFamilia."
	if len(c.texts) != 1 || c.texts[0] != want {
		t.Fatalf("texto inesperado:\n%q\nwant:\n%q", c.texts, want)
	}
	if len(c.reactions) != 2 || c.reactions[0] != "🔍" || c.reactions[1] != "✅" {
		t.Fatalf("reacciones inesperadas: %#v", c.reactions)
	}
}

func TestDefinirRespaldoIngles(t *testing.T) {
	definirSetup(t,
		func(w http.ResponseWriter, r *http.Request) { definirTexto(w, 404, `{"ok":false}`) },
		func(w http.ResponseWriter, r *http.Request) {
			definirTexto(w, 200, `[{"word":"dog","phonetic":"/dɒɡ/","meanings":[{"partOfSpeech":"noun","definitions":[{"definition":"animal"},{"definition":"guy"},{"definition":"tercero"}]}]}]`)
		},
	)

	c := &capture{}
	if err := (Definir{}).Handle(definirCtx(c, "def", "dog")); err != nil {
		t.Fatalf("Handle devolvió error: %v", err)
	}
	want := "📖 *dog* 🇬🇧\n🔊 _/dɒɡ/_\n\n*noun*\n• animal\n• guy"
	if len(c.texts) != 1 || c.texts[0] != want {
		t.Fatalf("texto inesperado:\n%q\nwant:\n%q", c.texts, want)
	}
}

func TestDefinirSinResultado(t *testing.T) {
	definirSetup(t,
		func(w http.ResponseWriter, r *http.Request) { definirTexto(w, 404, `{}`) },
		func(w http.ResponseWriter, r *http.Request) { definirTexto(w, 404, `[]`) },
	)

	c := &capture{}
	if err := (Definir{}).Handle(definirCtx(c, "definir", "zzzz")); err != nil {
		t.Fatalf("Handle devolvió error: %v", err)
	}
	if len(c.reactions) != 2 || c.reactions[0] != "🔍" || c.reactions[1] != "❌" {
		t.Fatalf("reacciones inesperadas: %#v", c.reactions)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], `No encontré "zzzz"`) {
		t.Fatalf("texto inesperado: %#v", c.texts)
	}
}

func TestDefinirSinArgumentos(t *testing.T) {
	c := &capture{}
	if err := (Definir{}).Handle(definirCtx(c, "definir")); err != nil {
		t.Fatalf("Handle devolvió error: %v", err)
	}
	if len(c.reactions) != 1 || c.reactions[0] != "⚠️" {
		t.Fatalf("esperaba ⚠️, obtuve %#v", c.reactions)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "Uso: `.definir <palabra>`") {
		t.Fatalf("texto inesperado: %#v", c.texts)
	}
}

func TestDefinirPalabraCitada(t *testing.T) {
	definirSetup(t,
		func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasSuffix(r.URL.Path, "/perro") {
				t.Errorf("ruta RAE inesperada: %s", r.URL.Path)
			}
			definirTexto(w, 200, `{"ok":true,"data":{"word":"perro","meanings":[{"senses":[{"raw":"Mamífero doméstico."}]}]}}`)
		},
		func(w http.ResponseWriter, r *http.Request) { t.Errorf("no debía usar inglés") },
	)

	c := &capture{}
	if err := (Definir{}).Handle(definirQuotedCtx(c, "definir", "perro ladra fuerte")); err != nil {
		t.Fatalf("Handle devolvió error: %v", err)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "📖 *perro*") {
		t.Fatalf("texto inesperado: %#v", c.texts)
	}
}
