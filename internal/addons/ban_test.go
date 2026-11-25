package addons

import (
	"path/filepath"
	"testing"
)

func TestBanCommands(t *testing.T) {
	if got := (Ban{}).Commands(); len(got) != 1 || got[0] != "ban" {
		t.Fatalf("comandos=%#v", got)
	}
}

func TestBanParseRequest(t *testing.T) {
	targets, minutos := banParseRequest([]string{"10"}, []string{"584141234567@s.whatsapp.net"}, "")
	if len(targets) != 1 || targets[0] != "584141234567@s.whatsapp.net" || minutos != 10 {
		t.Fatalf("menciones: %#v, %d", targets, minutos)
	}

	targets, minutos = banParseRequest([]string{"5"}, nil, "584222333444@s.whatsapp.net")
	if len(targets) != 1 || targets[0] != "584222333444@s.whatsapp.net" || minutos != 5 {
		t.Fatalf("citado: %#v, %d", targets, minutos)
	}

	targets, minutos = banParseRequest([]string{"0414-1234567", "30"}, nil, "")
	if len(targets) != 1 || targets[0] != "584141234567@s.whatsapp.net" || minutos != 30 {
		t.Fatalf("número: %#v, %d", targets, minutos)
	}

	if targets, _ := banParseRequest([]string{"hola"}, nil, ""); len(targets) != 0 {
		t.Fatalf("texto inválido no debe dar objetivo: %#v", targets)
	}
}

func TestBanJIDNormalize(t *testing.T) {
	casos := map[string]string{
		"584141234567@s.whatsapp.net":    "584141234567@s.whatsapp.net",
		"584141234567:12@s.whatsapp.net": "584141234567@s.whatsapp.net",
		"584141234567@lid":               "584141234567@s.whatsapp.net",
	}
	for entrada, want := range casos {
		if got := wagoJIDNormalize(entrada); got != want {
			t.Fatalf("wagoJIDNormalize(%q)=%q, esperaba %q", entrada, got, want)
		}
	}
}

func TestBanStoreRoundtrip(t *testing.T) {
	dir := t.TempDir()
	prev := banPath
	banPath = func() string { return filepath.Join(dir, "banlist.json") }
	t.Cleanup(func() { banPath = prev })

	lista := []banEntry{{ChatID: "1205@g.us", Number: "584141234567", FullJID: "584141234567@s.whatsapp.net", Until: 123}}
	if err := banWrite(lista); err != nil {
		t.Fatalf("banWrite: %v", err)
	}
	got := banRead()
	if len(got) != 1 || got[0].Number != "584141234567" || got[0].Until != 123 {
		t.Fatalf("roundtrip inesperado: %#v", got)
	}
}

func TestBanFueraDeGrupo(t *testing.T) {
	if err := (Ban{}).Handle(wagoCtx("58412345678")); err != nil {
		t.Fatalf("fuera de grupo debe ser silencioso: %v", err)
	}
}
