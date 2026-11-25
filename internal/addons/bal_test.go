package addons

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBalCommands(t *testing.T) {
	got := Bal{}.Commands()
	if len(got) != 1 || got[0] != "bal" {
		t.Fatalf("comandos inesperados: %#v", got)
	}
}

func TestBalHandleSaldo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Bearer") {
			t.Errorf("falta el bearer token: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"balance_infos":[{"total_balance":"12.34","currency":"USD"}]}`))
	}))
	defer srv.Close()

	prevURL, prevHTTP := balAPIURL, balHTTP
	balAPIURL = srv.URL
	balHTTP = srv.Client()
	t.Cleanup(func() { balAPIURL, balHTTP = prevURL, prevHTTP })

	c := &capture{}
	if err := (Bal{}).Handle(c.newCtx("bal")); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.reactions) != 2 || c.reactions[0] != "⏳" || c.reactions[1] != "✅" {
		t.Fatalf("reacciones=%#v", c.reactions)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "12.34 USD") || !strings.Contains(c.texts[0], "Balance DeepSeek") {
		t.Fatalf("texto inesperado: %#v", c.texts)
	}
}

func TestBalHandleErrorHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	prevURL, prevHTTP := balAPIURL, balHTTP
	balAPIURL = srv.URL
	balHTTP = srv.Client()
	t.Cleanup(func() { balAPIURL, balHTTP = prevURL, prevHTTP })

	c := &capture{}
	if err := (Bal{}).Handle(c.newCtx("bal")); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.reactions) == 0 || c.reactions[len(c.reactions)-1] != "❌" {
		t.Fatalf("reacciones=%#v", c.reactions)
	}
	if len(c.texts) != 0 {
		t.Fatalf("no debía enviar texto: %#v", c.texts)
	}
}
