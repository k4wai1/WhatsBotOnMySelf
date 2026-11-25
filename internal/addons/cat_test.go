package addons

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
)

func TestCatCommands(t *testing.T) {
	if got := (Cat{}).Commands(); len(got) != 1 || got[0] != "cat" {
		t.Fatalf("comandos=%#v", got)
	}
}

func TestCatObtenerURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "clave" {
			t.Errorf("x-api-key=%q", r.Header.Get("x-api-key"))
		}
		_, _ = w.Write([]byte(`[{"url":"https://cdn.example/cat.jpg"}]`))
	}))
	defer srv.Close()

	prevAPI, prevHTTP := catAPI, catHTTP
	catAPI = srv.URL
	catHTTP = srv.Client()
	t.Cleanup(func() { catAPI, catHTTP = prevAPI, prevHTTP })
	t.Setenv("CAT_API_KEY", "clave")

	got, err := catObtenerURL()
	if err != nil || got != "https://cdn.example/cat.jpg" {
		t.Fatalf("url=%q err=%v", got, err)
	}
}

func TestCatErrorHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	prevAPI, prevHTTP := catAPI, catHTTP
	catAPI = srv.URL
	catHTTP = srv.Client()
	t.Cleanup(func() { catAPI, catHTTP = prevAPI, prevHTTP })

	if _, err := catObtenerURL(); err == nil {
		t.Fatal("se esperaba error HTTP")
	}
}

func TestCatEsTimeout(t *testing.T) {
	if !catEsTimeout(errTimeout{}) {
		t.Fatal("deadline debe contar como timeout")
	}
	if catEsTimeout(nil) {
		t.Fatal("nil no es timeout")
	}
}

type errTimeout struct{}

func (errTimeout) Error() string { return "context deadline exceeded" }

func TestCatHandleError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer srv.Close()

	prevAPI, prevHTTP := catAPI, catHTTP
	catAPI = srv.URL
	catHTTP = srv.Client()
	t.Cleanup(func() { catAPI, catHTTP = prevAPI, prevHTTP })
	catLimite = wagoNuevoLimite(4, 10*time.Minute)
	t.Setenv("CAT_API_KEY", "")

	c := &capture{}
	ctx := wagoCtx("1205@g.us")
	ctx.SendHook = func(m *waE2E.Message) error { c.texts = append(c.texts, messageText(m)); return nil }
	ctx.ReactHook = func(e string) error { c.reactions = append(c.reactions, e); return nil }
	if err := (Cat{}).Handle(ctx); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "Error obteniendo el michi") {
		t.Fatalf("textos=%#v", c.texts)
	}
	if len(c.reactions) < 2 || c.reactions[len(c.reactions)-1] != "❌" {
		t.Fatalf("reacciones=%#v", c.reactions)
	}
}
