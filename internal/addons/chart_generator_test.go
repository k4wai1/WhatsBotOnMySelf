package addons

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestChartSeleccionarDatos(t *testing.T) {
	hist := chartHistorial{
		Daily: []chartPunto{
			{Date: "2026-01-01", BCVUSD: 100.0, BinanceUSDT: 110.0},
			{Date: "2026-01-02", BCVUSD: 105.0, BinanceUSDT: 115.0},
		},
		Intraday: []chartPunto{
			{Date: "2026-01-02", BCVUSD: 106.0, BinanceUSDT: 116.0},
			{Date: "2026-01-03", BCVUSD: 0, BinanceUSDT: 120.0},
		},
	}
	got := chartSeleccionarDatos(hist)
	if len(got) != 2 {
		t.Fatalf("puntos=%#v", got)
	}
	if got[0].Date != "2026-01-01" || got[1].Date != "2026-01-02" {
		t.Fatalf("orden=%#v", got)
	}
	if chartNumero(got[1].BCVUSD) != 106 {
		t.Fatalf("no se conservó el último del día: %#v", got[1])
	}
}

func TestChartDescartaColapsados(t *testing.T) {
	hist := chartHistorial{Daily: []chartPunto{
		{Date: "2026-01-01", BCVUSD: 1200.0, BinanceUSDT: 1300.0},
		{Date: "2026-01-02", BCVUSD: 1.009, BinanceUSDT: 1300.0},
		{Date: "2026-01-03", BCVUSD: 1210.0, BinanceUSDT: 1310.0},
	}}
	got := chartSeleccionarDatos(hist)
	if len(got) != 2 {
		t.Fatalf("se esperaban 2 puntos limpios: %#v", got)
	}
	for _, p := range got {
		if chartNumero(p.BCVUSD) < 10 {
			t.Fatalf("punto colapsado conservado: %#v", p)
		}
	}
}

func TestChartLimitaTreinta(t *testing.T) {
	hist := chartHistorial{}
	for i := 1; i <= 40; i++ {
		hist.Daily = append(hist.Daily, chartPunto{
			Date:        timeDate(i),
			BCVUSD:      float64(100 + i),
			BinanceUSDT: float64(110 + i),
		})
	}
	got := chartSeleccionarDatos(hist)
	if len(got) != 30 {
		t.Fatalf("puntos=%d", len(got))
	}
	if got[0].Date != timeDate(11) {
		t.Fatalf("primer punto=%q", got[0].Date)
	}
}

func timeDate(day int) string {
	return "2026-01-" + string(rune('0'+day/10)) + string(rune('0'+day%10))
}

func TestChartEtiqueta(t *testing.T) {
	if got := chartEtiqueta("2026-11-24"); got != "24/11" {
		t.Fatalf("etiqueta=%q", got)
	}
	if got := chartEtiqueta("hoy"); got != "hoy" {
		t.Fatalf("etiqueta=%q", got)
	}
}

func TestWagoGenerateChart(t *testing.T) {
	dir := t.TempDir()
	histPath := filepath.Join(dir, "rates_history.json")
	outPath := filepath.Join(dir, "rates_chart.png")
	if err := os.WriteFile(histPath, []byte(`{"daily":[{"date":"2026-01-01","bcv_usd":100,"binance_usdt":110}]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("método=%s", r.Method)
		}
		_, _ = w.Write([]byte("PNGDATA"))
	}))
	defer srv.Close()

	prevURL, prevHTTP := chartURL, chartHTTP
	prevHist, prevOut := chartHistPath, chartOutput
	chartURL = srv.URL
	chartHTTP = srv.Client()
	chartHistPath = func() string { return histPath }
	chartOutput = func() string { return outPath }
	t.Cleanup(func() {
		chartURL, chartHTTP = prevURL, prevHTTP
		chartHistPath, chartOutput = prevHist, prevOut
	})

	data, err := wagoGenerateChart()
	if err != nil {
		t.Fatalf("wagoGenerateChart: %v", err)
	}
	if string(data) != "PNGDATA" {
		t.Fatalf("data=%q", data)
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("no se guardó la gráfica: %v", err)
	}
	if got := wagoChartPath(); got != outPath {
		t.Fatalf("wagoChartPath=%q", got)
	}
}

func TestWagoGenerateChartSinDatos(t *testing.T) {
	dir := t.TempDir()
	prevHist := chartHistPath
	chartHistPath = func() string { return filepath.Join(dir, "no-existe.json") }
	t.Cleanup(func() { chartHistPath = prevHist })

	data, err := wagoGenerateChart()
	if err != nil || data != nil {
		t.Fatalf("data=%v err=%v", data, err)
	}
}
