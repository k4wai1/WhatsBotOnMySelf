package addons

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func gatoSetup(t *testing.T) (string, time.Time) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "assets", "gatos_data.json")
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)

	prevPath, prevNow := gatoDBPath, gatoNow
	gatoDBPath = func() string { return path }
	gatoNow = func() time.Time { return now }
	t.Cleanup(func() { gatoDBPath, gatoNow = prevPath, prevNow })
	return path, now
}

func TestGatoCommands(t *testing.T) {
	got := Gato{}.Commands()
	if len(got) != 1 || got[0] != "gato" {
		t.Fatalf("comandos inesperados: %#v", got)
	}
}

func TestGatoCrearYNormalizar(t *testing.T) {
	gatoSetup(t)
	cat := gatoCrear("u1")
	if cat.Name != "Michi" || cat.Scene != "Sala Principal" {
		t.Fatalf("gato inesperado: %#v", cat)
	}
	if cat.Stats.Hambre != 50 || cat.Stats.Energia != 100 || cat.Stats.Felicidad != 50 {
		t.Fatalf("stats inesperados: %#v", cat.Stats)
	}
	if cat.Economia.Nivel != 1 || cat.Economia.XP != 0 {
		t.Fatalf("economía inesperada: %#v", cat.Economia)
	}

	raro := &gatoCat{Stats: &gatoStats{Hambre: 999, Energia: -5, Felicidad: 101}}
	gatoNormalizar(raro)
	if raro.Stats.Hambre != 100 || raro.Stats.Energia != 0 || raro.Stats.Felicidad != 100 {
		t.Fatalf("clamp incorrecto: %#v", raro.Stats)
	}
	if raro.Name != "Michi" || raro.Economia == nil || raro.Racha == nil || raro.Diario == nil {
		t.Fatalf("normalización incompleta: %#v", raro)
	}
}

func TestGatoAplicarDecay(t *testing.T) {
	_, now := gatoSetup(t)
	cat := gatoCrear("u1")
	cat.Stats = &gatoStats{Hambre: 80, Energia: 80, Felicidad: 80}
	cat.LastUpdate = now.Add(-4 * time.Hour).UnixMilli()

	gatoAplicarDecay(cat)

	if cat.Stats.Hambre != 64 { // 4 por hora * 4h
		t.Fatalf("hambre=%v, esperaba 64", cat.Stats.Hambre)
	}
	if cat.Stats.Felicidad != 68 { // 3/h
		t.Fatalf("felicidad=%v, esperaba 68", cat.Stats.Felicidad)
	}
	if cat.Stats.Energia != 72 { // 2/h
		t.Fatalf("energía=%v, esperaba 72", cat.Stats.Energia)
	}
	if cat.LastUpdate != now.UnixMilli() {
		t.Fatal("LastUpdate no se actualizó")
	}

	// Dormido decae menos hambre.
	dormido := gatoCrear("u2")
	dormido.Stats = &gatoStats{Hambre: 80, Energia: 80, Felicidad: 80}
	dormido.LastUpdate = now.Add(-24 * time.Hour).UnixMilli()
	dormido.Siesta = &gatoSiesta{Despierta: now.Add(2 * time.Hour).UnixMilli()}
	gatoAplicarDecay(dormido)
	if dormido.Stats.Hambre != 32 { // 4/h * 24 * 0.5
		t.Fatalf("hambre dormido=%v, esperaba 32", dormido.Stats.Hambre)
	}
	if dormido.Stats.Energia != 80 || dormido.Stats.Felicidad != 80 {
		t.Fatal("durmiendo no debe bajar energía ni felicidad")
	}
}

func TestGatoRachaYMultiplicador(t *testing.T) {
	_, now := gatoSetup(t)
	cat := gatoCrear("u1")

	if got := gatoRegistrarRacha(cat); got != 1.05 {
		t.Fatalf("primera racha=%v, esperaba 1.05", got)
	}
	ayer := now.Add(-24 * time.Hour).UTC().Format("2006-01-02")
	cat.Racha.UltimoDia = &ayer
	cat.Racha.Dias = 2
	if got := gatoRegistrarRacha(cat); got != 1.15 {
		t.Fatalf("racha continuada=%v, esperaba 1.15", got)
	}

	cat.Racha.Dias = 1000
	if got := gatoMultiplicador(cat); got != 2.0 {
		t.Fatalf("tope de multiplicador=%v, esperaba 2", got)
	}
}

func TestGatoMarcarDiario(t *testing.T) {
	gatoSetup(t)
	cat := gatoCrear("u1")

	if !gatoMarcarDiario(cat, "comida") {
		t.Fatal("la primera comida del día debe contar")
	}
	if gatoMarcarDiario(cat, "comida") {
		t.Fatal("la segunda comida del día no debe contar")
	}
	if !gatoMarcarDiario(cat, "caricia") || !gatoMarcarDiario(cat, "baño") {
		t.Fatal("caricia/baño deben contar la primera vez")
	}
	if gatoMarcarDiario(cat, "otro") {
		t.Fatal("un tipo desconocido no debe contar")
	}
}

func TestGatoOtorgarYSubirNivel(t *testing.T) {
	gatoSetup(t)
	cat := gatoCrear("u1")
	cat.Racha.Dias = 0

	ganado := gatoOtorgar(cat, 250, 5)
	if !ganado.Subio {
		t.Fatalf("con 250 xp debía subir de nivel: %#v", ganado)
	}
	if cat.Economia.XP != 150 || cat.Economia.Nivel != 2 {
		t.Fatalf("economía=%#v", cat.Economia)
	}
	if cat.Economia.Peces != 5 {
		t.Fatalf("peces=%d", cat.Economia.Peces)
	}
}

func TestGatoAnimo(t *testing.T) {
	gatoSetup(t)
	casos := []struct {
		stats gatoStats
		want  string
	}{
		{gatoStats{Hambre: 10, Felicidad: 90}, "hambriento"},
		{gatoStats{Hambre: 50, Felicidad: 10}, "triste"},
		{gatoStats{Hambre: 50, Felicidad: 90}, "feliz"},
		{gatoStats{Hambre: 50, Felicidad: 50}, ""},
	}
	for _, caso := range casos {
		cat := gatoCrear("u")
		cat.Stats = &caso.stats
		if got := gatoAnimo(cat); got != caso.want {
			t.Fatalf("ánimo(%+v)=%q, esperaba %q", caso.stats, got, caso.want)
		}
	}
}

func TestGatoGuardarYCargar(t *testing.T) {
	path, _ := gatoSetup(t)
	db := map[string]*gatoCat{"u1": gatoCrear("u1")}
	db["u1"].Name = "Bigotes"
	gatoGuardar(db)

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no se creó el JSON: %v", err)
	}
	if len(raw) == 0 || raw[0] != '{' {
		t.Fatalf("JSON inesperado: %q", string(raw[:min(20, len(raw))]))
	}

	cargado := gatoCargar()
	if len(cargado) != 1 || cargado["u1"].Name != "Bigotes" {
		t.Fatalf("roundtrip inesperado: %#v", cargado)
	}
}

func TestGatoSanitizarNombre(t *testing.T) {
	casos := map[string]string{
		"  Bigotes  ":      "Bigotes",
		"a*b_c`d~e[f]g\n":  "abcdefg",
		"Michi 123":        "Michi 123",
		"":                 "",
		"NombreMuyLargoYa": "NombreMuyL",
	}
	for entrada, want := range casos {
		if got := gatoSanitizarNombre(entrada); got != want {
			t.Fatalf("gatoSanitizarNombre(%q)=%q, esperaba %q", entrada, got, want)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
