package addons

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

func TestFxCommands(t *testing.T) {
	got := Fx{}.Commands()
	if len(got) != 2 || got[0] != "fx" || got[1] != "efecto" {
		t.Fatalf("comandos inesperados: %#v", got)
	}
}

func TestFxAplicarEfectos(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			src.Set(x, y, color.NRGBA{R: uint8(10 * x), G: uint8(20 * y), B: 30, A: 255})
		}
	}

	gris, err := fxAplicar(src, "gris")
	if err != nil {
		t.Fatalf("fxAplicar(gris): %v", err)
	}
	r, g, b, _ := gris.At(1, 1).RGBA()
	if r != g || g != b {
		t.Fatalf("gris no es monocromo: %d %d %d", r, g, b)
	}

	sepia, err := fxAplicar(src, "sepia")
	if err != nil {
		t.Fatalf("fxAplicar(sepia): %v", err)
	}
	sr, sg, sb, _ := sepia.At(2, 2).RGBA()
	if !(sr >= sg && sg >= sb) {
		t.Fatalf("sepia inesperado: %d %d %d", sr, sg, sb)
	}

	espejo, err := fxAplicar(src, "espejo")
	if err != nil {
		t.Fatalf("fxAplicar(espejo): %v", err)
	}
	want := src.At(3, 0)
	got := espejo.At(0, 0)
	if got != want {
		t.Fatalf("espejo no invirtió: got %v want %v", got, want)
	}

	blur, err := fxAplicar(src, "blur")
	if err != nil {
		t.Fatalf("fxAplicar(blur): %v", err)
	}
	if blur.Bounds() != src.Bounds() {
		t.Fatalf("blur cambió dimensiones: %v", blur.Bounds())
	}

	if _, err := fxAplicar(src, "nope"); err == nil {
		t.Fatal("esperaba error para efecto desconocido")
	}
}

func TestFxEfectoInvalido(t *testing.T) {
	c := &capture{}
	err := (Fx{}).Handle(c.newCtx("fx", "brillo"))
	if err != nil {
		t.Fatalf("Handle devolvió error: %v", err)
	}
	if len(c.reactions) != 1 || c.reactions[0] != "⚠️" {
		t.Fatalf("esperaba ⚠️, obtuve %#v", c.reactions)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "Efectos de imagen") {
		t.Fatalf("esperaba ayuda, obtuve %#v", c.texts)
	}
}

func TestFxSinImagen(t *testing.T) {
	c := &capture{}
	err := (Fx{}).Handle(c.newCtx("fx", "blur"))
	if err != nil {
		t.Fatalf("Handle devolvió error: %v", err)
	}
	if len(c.reactions) != 2 || c.reactions[0] != "🔄" || c.reactions[1] != "❌" {
		t.Fatalf("reacciones inesperadas: %#v", c.reactions)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "No encontré imagen") {
		t.Fatalf("mensaje inesperado: %#v", c.texts)
	}
}

func TestFxEfectoValido(t *testing.T) {
	for _, e := range []string{"blur", "gris", "sepia", "espejo"} {
		if !fxEfectoValido(e) {
			t.Fatalf("%q debería ser válido", e)
		}
	}
	if fxEfectoValido("") || fxEfectoValido("BLUR") {
		t.Fatal("efectos inválidos aceptados")
	}
}
