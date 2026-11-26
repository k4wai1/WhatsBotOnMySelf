package addons

import (
	"strings"
	"testing"
)

func TestStickersCommands(t *testing.T) {
	got := Stickers{}.Commands()
	if len(got) != 3 || got[0] != "st" || got[1] != "sticker" || got[2] != "s" {
		t.Fatalf("comandos inesperados: %#v", got)
	}
}

func TestStickersParseArgs(t *testing.T) {
	args := stickersNormalizeArgs([]string{"CROP  High", "reverse", "b", "e"})
	cfg := stickersParseArgs(args)
	if !cfg.Crop || !cfg.Reverse || !cfg.Stretch {
		t.Fatalf("flags inesperados: %#v", cfg)
	}
	if cfg.Shape != "border" {
		t.Fatalf("shape=%q", cfg.Shape)
	}
	if cfg.Mode != "high" {
		t.Fatalf("mode=%q", cfg.Mode)
	}

	porDefecto := stickersParseArgs(stickersNormalizeArgs(nil))
	if porDefecto.Crop || porDefecto.Reverse || porDefecto.Stretch || porDefecto.Shape != "none" || porDefecto.Mode != "default" {
		t.Fatalf("defaults inesperados: %#v", porDefecto)
	}

	fluid := stickersParseArgs(stickersNormalizeArgs([]string{"f"}))
	if fluid.Mode != "fluid" {
		t.Fatalf("f debe activar el modo fluid con más frames: %#v", fluid)
	}
	fluidLargo := stickersParseArgs(stickersNormalizeArgs([]string{"fluid"}))
	if fluidLargo.Mode != "fluid" {
		t.Fatalf("fluid debe activar el modo fluid con más frames: %#v", fluidLargo)
	}

	esferico := stickersParseArgs(stickersNormalizeArgs([]string{"s"}))
	if esferico.Shape != "spherical" {
		t.Fatalf("shape=%q", esferico.Shape)
	}
}

func TestStickersFilterChain(t *testing.T) {
	normal := stickersFilterChain(false, "none", false)
	if !strings.Contains(normal, "scale=512:512:force_original_aspect_ratio=decrease") || !strings.Contains(normal, "pad=512:512") {
		t.Fatalf("filtro normal inesperado: %s", normal)
	}

	crop := stickersFilterChain(true, "none", false)
	if !strings.Contains(crop, "crop=512:512") {
		t.Fatalf("filtro crop inesperado: %s", crop)
	}

	stretch := stickersFilterChain(false, "none", true)
	if !strings.HasPrefix(stretch, "scale=512:512,format=rgba") {
		t.Fatalf("filtro stretch inesperado: %s", stretch)
	}

	esfera := stickersFilterChain(false, "spherical", false)
	if !strings.Contains(esfera, "geq=") {
		t.Fatalf("filtro esférico inesperado: %s", esfera)
	}

	borde := stickersFilterChain(false, "border", false)
	if !strings.Contains(borde, "geq=") {
		t.Fatalf("filtro borde inesperado: %s", borde)
	}
}

func TestStickersCalcTargetFps(t *testing.T) {
	casos := []struct {
		mode     string
		duration float64
		source   float64
		want     int
	}{
		{"default", 1, 30, 24},
		{"default", 3, 30, 16},
		{"default", 5, 30, 12},
		{"default", 10, 30, 8},
		{"fluid", 1, 30, 30},
		{"fluid", 3, 30, 20},
		{"fluid", 5, 30, 15},
		{"fluid", 10, 30, 10},
		{"high", 1, 25, 25},
		{"high", 2, 60, 24},
		{"high", 5, 60, 20},
		{"high", 10, 60, 18},
	}
	for _, caso := range casos {
		if got := stickersCalcTargetFps(caso.mode, caso.duration, caso.source); got != caso.want {
			t.Fatalf("fps(%q, %v, %v)=%d, esperaba %d", caso.mode, caso.duration, caso.source, got, caso.want)
		}
	}
}

func TestStickersReverseFrameSequence(t *testing.T) {
	seq := stickersReverseFrameSequence(20)
	if len(seq) < 2 {
		t.Fatalf("secuencia demasiado corta: %#v", seq)
	}
	if seq[0] != 0 || seq[len(seq)-1] != 0 {
		t.Fatalf("la animación empieza y termina en el primer frame: %#v", seq)
	}
	tope := 0
	for _, v := range seq {
		if v > tope {
			tope = v
		}
	}
	if tope != 19 {
		t.Fatalf("no llega al último frame: %#v", seq)
	}
	if got := stickersReverseFrameSequence(0); got != nil {
		t.Fatalf("total 0 debe devolver nil: %#v", got)
	}
}

func TestStickersArgBuilders(t *testing.T) {
	static := strings.Join(stickersStaticArgs("in.webp", "out.webp", "scale=512:512", 80), " ")
	for _, want := range []string{"-vcodec libwebp", "-q:v 80", "-an", "-vf scale=512:512"} {
		if !strings.Contains(static, want) {
			t.Fatalf("args estáticos sin %q: %s", want, static)
		}
	}

	frames := strings.Join(stickersExtractFramesArgs("in.webp", "f_%04d.png", 16, 3.5, "scale=512:512", "webp"), " ")
	for _, want := range []string{"-t 3.5", "fps=16", "-pix_fmt rgba"} {
		if !strings.Contains(frames, want) {
			t.Fatalf("args de frames sin %q: %s", want, frames)
		}
	}

	encode := strings.Join(stickersEncodeArgs("f_%04d.png", "out.webp", 12, 70), " ")
	for _, want := range []string{"-framerate 12", "-loop 0", "-q:v 70"} {
		if !strings.Contains(encode, want) {
			t.Fatalf("args de encode sin %q: %s", want, encode)
		}
	}
}
