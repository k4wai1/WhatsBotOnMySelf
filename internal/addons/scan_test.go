package addons

import (
	"strings"
	"testing"
)

func TestScanCommands(t *testing.T) {
	if got := (Scan{}).Commands(); len(got) != 1 || got[0] != "scan" {
		t.Fatalf("comandos=%#v", got)
	}
}

func TestScanSinImagen(t *testing.T) {
	c := &capture{}
	if err := (Scan{}).Handle(c.newCtx("scan")); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.reactions) != 1 || c.reactions[0] != "⚠️" {
		t.Fatalf("reacciones=%#v", c.reactions)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "Envía una imagen") {
		t.Fatalf("textos=%#v", c.texts)
	}
}

func TestScanBufferInvalido(t *testing.T) {
	if got := scanDecodeBuffer([]byte("no es una imagen")); got != "" {
		t.Fatalf("se esperaba vacío, got=%q", got)
	}
}

func TestScanClean(t *testing.T) {
	got := scanClean("\x00hola\r\nmundo\r")
	if got != "hola\nmundo" {
		t.Fatalf("scanClean=%q", got)
	}
}
