package addons

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestChannelCommands(t *testing.T) {
	if got := (Channel{}).Commands(); len(got) != 1 || got[0] != "graf" {
		t.Fatalf("comandos=%#v", got)
	}
}

func TestChannelParseVes(t *testing.T) {
	casos := []struct {
		entrada string
		want    float64
	}{
		{"1.009,07", 1009.07},
		{"1,009.07", 1009.07},
		{"36,50", 36.5},
		{"36.50", 36.5},
	}
	for _, caso := range casos {
		got := channelParseVes(caso.entrada)
		if got < caso.want-0.001 || got > caso.want+0.001 {
			t.Fatalf("channelParseVes(%q)=%v, esperaba %v", caso.entrada, got, caso.want)
		}
	}
}

func TestChannelBuildMessage(t *testing.T) {
	valores := map[string]channelValor{
		"dolar": {Valor: "1.009,07", Codigo: "USD", Nombre: "Dólar"},
		"euro":  {Valor: "1.100,50", Codigo: "EUR", Nombre: "Euro"},
	}
	precio := 1200.5
	msg := channelBuildMessage(valores, &precio, "24/11/2026")
	for _, want := range []string{"Actualización del Dólar & Euro", "24/11/2026", "Dólar (USD)", "BCV: 1.009,07", "Binance (USDT): 1200.50", "Brecha:", "Euro (EUR)", "Binance (Ref. USDT)"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("mensaje sin %q:\n%s", want, msg)
		}
	}

	sinBinance := channelBuildMessage(valores, nil, "Hoy")
	if !strings.Contains(sinBinance, "Binance: No disponible") {
		t.Fatalf("sin binance inesperado:\n%s", sinBinance)
	}
}

func TestChannelLastPost(t *testing.T) {
	dir := t.TempDir()
	prev := channelStatePath
	channelStatePath = func() string { return filepath.Join(dir, "channel_state.json") }
	t.Cleanup(func() { channelStatePath = prev })

	if got := channelLastPost(); got != "" {
		t.Fatalf("estado inicial=%q", got)
	}
	channelSaveLastPost("2026-11-24-18")
	if got := channelLastPost(); got != "2026-11-24-18" {
		t.Fatalf("estado guardado=%q", got)
	}
}
