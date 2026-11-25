package addons

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
)

func TestSpeedCommands(t *testing.T) {
	if got := (Speed{}).Commands(); len(got) != 1 || got[0] != "speed" {
		t.Fatalf("comandos=%#v", got)
	}
}

func TestSpeedMeasure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, 1024*1024))
	}))
	defer srv.Close()

	prevURL, prevHTTP := speedURL, speedHTTP
	speedURL = srv.URL
	speedHTTP = srv.Client()
	t.Cleanup(func() { speedURL, speedHTTP = prevURL, prevHTTP })

	res, err := speedMeasure()
	if err != nil {
		t.Fatalf("speedMeasure: %v", err)
	}
	if res.Mbps <= 0 {
		t.Fatalf("Mbps inválido: %#v", res)
	}
	if res.Latency < 0 {
		t.Fatalf("latencia inválida: %#v", res)
	}
}

func TestSpeedNoOwner(t *testing.T) {
	t.Setenv("OWNER_NUMBERS", "584000000000")
	c := &capture{}
	if err := (Speed{}).Handle(c.newCtx("speed")); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.reactions) != 1 || c.reactions[0] != "⛔" {
		t.Fatalf("reacciones=%#v", c.reactions)
	}
}

func TestSpeedHandlerTexto(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, 256*1024))
	}))
	defer srv.Close()

	prevURL, prevHTTP := speedURL, speedHTTP
	speedURL = srv.URL
	speedHTTP = srv.Client()
	t.Cleanup(func() { speedURL, speedHTTP = prevURL, prevHTTP })

	t.Setenv("OWNER_NUMBERS", "58412345678")
	c := &capture{}
	ctx := wagoCtx("58412345678")
	ctx.Command = "speed"
	ctx.SendHook = func(m *waE2E.Message) error {
		c.texts = append(c.texts, messageText(m))
		return nil
	}
	ctx.ReactHook = func(e string) error { c.reactions = append(c.reactions, e); return nil }
	if err := (Speed{}).Handle(ctx); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.texts) < 2 || !strings.Contains(c.texts[len(c.texts)-1], "Resultado Speedtest") {
		t.Fatalf("textos=%#v", c.texts)
	}
}
