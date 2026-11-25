package addons

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCedulaCommands(t *testing.T) {
	got := (Cedula{}).Commands()
	if len(got) != 2 || got[0] != "cedula" || got[1] != "ci" {
		t.Fatalf("comandos=%#v", got)
	}
}

func TestCedulaPreparar(t *testing.T) {
	casos := []struct {
		raw    string
		nac    string
		limpio string
		ok     bool
	}{
		{"32832480", "V", "32832480", true},
		{"V-12.345.678", "V", "12345678", true},
		{"E-12.345.678", "E", "12345678", true},
		{"v 123", "V", "123", true},
		{"12", "V", "12", false},
		{"abc", "V", "abc", false},
		{"1234567890123", "V", "1234567890123", false},
	}
	for _, caso := range casos {
		nac, limpio, ok := cedulaPreparar(caso.raw)
		if nac != caso.nac || limpio != caso.limpio || ok != caso.ok {
			t.Fatalf("cedulaPreparar(%q)=(%q,%q,%v), esperaba (%q,%q,%v)", caso.raw, nac, limpio, ok, caso.nac, caso.limpio, caso.ok)
		}
	}
}

func TestCedulaError(t *testing.T) {
	casos := map[string]string{
		"INVALID_TOKEN":    "Token o App-ID inválido. Verifica tus credenciales en cedula.com.ve",
		"NOT_FOUND":        "Cédula no encontrada en los registros",
		"RECORD_NOT_FOUND": "Cédula no encontrada en los registros",
		"LIMIT_EXCEEDED":   "Límite de consultas alcanzado. Intenta más tarde",
		"OTRO":             "Error: OTRO",
		"":                 "Error: Desconocido",
	}
	for entrada, want := range casos {
		if got := cedulaError(entrada); got != want {
			t.Fatalf("cedulaError(%q)=%q, esperaba %q", entrada, got, want)
		}
	}
}

func TestCedulaSinCredenciales(t *testing.T) {
	t.Setenv("CEDULA_APP_ID", "")
	t.Setenv("CEDULA_TOKEN", "")
	c := &capture{}
	if err := (Cedula{}).Handle(c.newCtx("cedula", "12345678")); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "no configurada") {
		t.Fatalf("textos=%#v", c.texts)
	}
}

func TestCedulaInvalida(t *testing.T) {
	t.Setenv("CEDULA_APP_ID", "app")
	t.Setenv("CEDULA_TOKEN", "token")
	c := &capture{}
	if err := (Cedula{}).Handle(c.newCtx("cedula", "abc")); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "inválido") {
		t.Fatalf("textos=%#v", c.texts)
	}
}

func TestCedulaConsultaOK(t *testing.T) {
	t.Setenv("CEDULA_APP_ID", "app")
	t.Setenv("CEDULA_TOKEN", "token")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("nacionalidad") != "V" || q.Get("cedula") != "12345678" {
			t.Errorf("query inesperada: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":false,"data":{"cedula":"12345678","primer_nombre":"Ana","segundo_nombre":"María","primer_apellido":"Pérez","segundo_apellido":"Gómez","rif":"V12345678","fecha_nac":"1990-01-01","cne":{"estado":"Distrito Capital","municipio":"Libertador","parroquia":"Catedral","centro_electoral":"Escuela"}}}`))
	}))
	defer srv.Close()

	prevURL, prevHTTP := cedulaURL, cedulaHTTP
	cedulaURL = srv.URL
	cedulaHTTP = srv.Client()
	t.Cleanup(func() { cedulaURL, cedulaHTTP = prevURL, prevHTTP })

	c := &capture{}
	if err := (Cedula{}).Handle(c.newCtx("cedula", "V-12.345.678")); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.reactions) != 2 || c.reactions[0] != "🔍" || c.reactions[1] != "✅" {
		t.Fatalf("reacciones=%#v", c.reactions)
	}
	if len(c.texts) != 1 {
		t.Fatalf("textos=%#v", c.texts)
	}
	for _, want := range []string{"Datos de Cédula", "V-12345678", "Ana María", "Pérez Gómez", "Distrito Capital"} {
		if !strings.Contains(c.texts[0], want) {
			t.Fatalf("texto sin %q:\n%s", want, c.texts[0])
		}
	}
}
