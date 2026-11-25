package addons

import (
	"math"
	"testing"
)

func TestTasasCommands(t *testing.T) {
	got := Tasas{}.Commands()
	want := []string{"dolar", "dólar", "euro", "yuan", "lira", "rublo", "tasas"}
	if len(got) != len(want) {
		t.Fatalf("comandos=%#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("comando %d = %q, esperaba %q", i, got[i], want[i])
		}
	}
}

func TestTasasParseVes(t *testing.T) {
	casos := []struct {
		entrada string
		want    float64
	}{
		{"1.009,07", 1009.07}, // es-VE >1000 (el bug original lo colapsaba a 1.009)
		{"1,009.07", 1009.07}, // en-US >1000
		{"1.009", 1009},       // es-VE sin decimales
		{"36,50", 36.5},       // coma decimal
		{"36.50", 36.5},       // punto decimal con 2 dígitos
		{"0,85", 0.85},
		{"  1.234.567,89 ", 1234567.89},
		{"abc", math.NaN()},
		{"", math.NaN()},
	}
	for _, caso := range casos {
		got := tasasParseVes(caso.entrada)
		if math.IsNaN(caso.want) {
			if !math.IsNaN(got) {
				t.Fatalf("tasasParseVes(%q)=%v, esperaba NaN", caso.entrada, got)
			}
			continue
		}
		if math.Abs(got-caso.want) > 0.0001 {
			t.Fatalf("tasasParseVes(%q)=%v, esperaba %v", caso.entrada, got, caso.want)
		}
	}

	if got := tasasParseVes(42.5); got != 42.5 {
		t.Fatalf("números deben pasar tal cual, obtuve %v", got)
	}
}

func TestTasasMultiplicador(t *testing.T) {
	casos := map[string]float64{
		"":       1,
		"2":      2,
		"2,5":    2.5,
		"0":      1,
		"-3":     1,
		"abc":    1,
		"1e9999": 1,
	}
	for entrada, want := range casos {
		var args []string
		if entrada != "" {
			args = []string{entrada}
		}
		if got := tasasMultiplicador(args); got != want {
			t.Fatalf("tasasMultiplicador(%q)=%v, esperaba %v", entrada, got, want)
		}
	}
}

func TestTasasCommandUsed(t *testing.T) {
	casos := map[string]string{
		",dolar":   "dolar",
		"!DÓLAR":   "dolar",
		"/euro":    "euro",
		".tasas":   "tasas",
		"  ,yuan ": "yuan",
		"":         "",
		"rublo":    "rublo",
	}
	for entrada, want := range casos {
		if got := tasasCommandUsed(entrada); got != want {
			t.Fatalf("tasasCommandUsed(%q)=%q, esperaba %q", entrada, got, want)
		}
	}
}

func TestTasasSinAcentos(t *testing.T) {
	if got := tasasSinAcentos("Dólar Día Ñandú"); got != "Dolar Dia Nandu" {
		t.Fatalf("sin acentos=%q", got)
	}
}
