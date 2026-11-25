package addons

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPanteraCommands(t *testing.T) {
	if got := (Pantera{}).Commands(); len(got) != 1 || got[0] != "pantera" {
		t.Fatalf("comandos=%#v", got)
	}
}

func TestPanteraCensurado(t *testing.T) {
	sinAleatorio(t, func(int) int { return 0 })
	got := panteraCensurado()
	for _, r := range got {
		if r != '█' && r != ' ' {
			t.Fatalf("carácter inesperado %q en %q", r, got)
		}
	}
	if !strings.Contains(got, "█") {
		t.Fatalf("sin bloques: %q", got)
	}
}

func TestPanteraZalgo(t *testing.T) {
	sinAleatorio(t, func(int) int { return 0 })
	base := "ERROR"
	got := panteraZalgo(base)
	if utf8.RuneCountInString(got) <= utf8.RuneCountInString(base) {
		t.Fatalf("zalgo no añadió marcas: %q", got)
	}
	if !strings.HasPrefix(got, "E") {
		t.Fatalf("zalgo=%q", got)
	}
}

func TestPanteraRespuesta(t *testing.T) {
	sinAleatorio(t, func(int) int { return 0 })

	censurada := panteraRespuesta(0)
	if strings.ContainsAny(censurada, "abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("estilo 0 con letras: %q", censurada)
	}
	glitch := panteraRespuesta(1)
	if !strings.HasPrefix(glitch, "H") || utf8.RuneCountInString(glitch) < 20 {
		t.Fatalf("estilo 1 inesperado: %q", glitch)
	}
	mezcla := panteraRespuesta(2)
	if !strings.HasPrefix(mezcla, "E") || !strings.Contains(mezcla, "█") || !strings.Contains(mezcla, "\n") {
		t.Fatalf("estilo 2=%q", mezcla)
	}
}

func TestPanteraHandle(t *testing.T) {
	sinAleatorio(t, func(int) int { return 0 })
	c := &capture{}
	if err := (Pantera{}).Handle(c.newCtx("pantera")); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.texts) != 1 || c.texts[0] == "" {
		t.Fatalf("textos=%#v", c.texts)
	}
}
