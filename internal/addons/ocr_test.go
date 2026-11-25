package addons

import (
	"strings"
	"testing"
)

func TestOcrCommands(t *testing.T) {
	if got := (Ocr{}).Commands(); len(got) != 1 || got[0] != "ocr" {
		t.Fatalf("comandos=%#v", got)
	}
}

func TestOcrParsearTexto(t *testing.T) {
	texto, err := ocrParsearTexto([]byte(`{"ParsedResults":[{"ParsedText":"  hola mundo  "}]}`))
	if err != nil || texto != "hola mundo" {
		t.Fatalf("texto=%q err=%v", texto, err)
	}
	texto, err = ocrParsearTexto([]byte(`{"ParsedResults":[]}`))
	if err != nil || texto != "" {
		t.Fatalf("texto=%q err=%v", texto, err)
	}
	if _, err := ocrParsearTexto([]byte("no json")); err == nil {
		t.Fatal("se esperaba error de JSON")
	}
}

func TestOcrMultipart(t *testing.T) {
	body, contentType, err := ocrMultipart([]byte("imagen"), "image/png", "clave")
	if err != nil {
		t.Fatalf("ocrMultipart: %v", err)
	}
	if !strings.HasPrefix(contentType, "multipart/form-data") {
		t.Fatalf("content-type=%q", contentType)
	}
	texto := string(body)
	for _, want := range []string{"name=\"file\"", "image.jpg", "language", "spa", "apikey", "clave", "detectOrientation"} {
		if !strings.Contains(texto, want) {
			t.Fatalf("multipart sin %q", want)
		}
	}
}

func TestOcrAPIKey(t *testing.T) {
	t.Setenv("OCR_SPACE_KEY", "")
	if got := ocrAPIKey(); got != "helloworld" {
		t.Fatalf("clave por defecto=%q", got)
	}
	t.Setenv("OCR_SPACE_KEY", "secreta")
	if got := ocrAPIKey(); got != "secreta" {
		t.Fatalf("clave=%q", got)
	}
}

func TestOcrSinImagen(t *testing.T) {
	c := &capture{}
	if err := (Ocr{}).Handle(c.newCtx("ocr")); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "envía una imagen") {
		t.Fatalf("textos=%#v", c.texts)
	}
}
