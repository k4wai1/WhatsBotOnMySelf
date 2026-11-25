// chart_generator.go — helper interno de channel.js: genera la gráfica de tasas
// (BCV vs Binance) con QuickChart. No registra comandos.
package addons

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"whatsbot-go/internal/core"
)

type chartPunto struct {
	Date        string `json:"date"`
	BCVUSD      any    `json:"bcv_usd"`
	BinanceUSDT any    `json:"binance_usdt"`
}

type chartHistorial struct {
	Daily    []chartPunto `json:"daily"`
	Intraday []chartPunto `json:"intraday"`
}

var (
	chartHTTP     = &http.Client{Timeout: 60 * time.Second}
	chartURL      = "https://quickchart.io/chart"
	chartHistPath = func() string { return core.DataPath("assets", "statistics", "rates_history.json") }
	chartOutput   = func() string { return core.DataPath("assets", "statistics", "rates_chart.png") }
	chartNow      = time.Now
)

func init() {
	// channel.go deja la gráfica como inyectable; chart_generator la implementa.
	channelGenerateChart = wagoGenerateChart
}

func chartNumero(raw any) float64 {
	switch v := raw.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		f, _ := v.Float64()
		return f
	case string:
		return channelParseVes(v)
	}
	return 0
}

// chartSeleccionarDatos replica getHistoryData(): un registro por día, ordenado,
// últimos 30 y descarte de valores colapsados por el bug de parseo.
func chartSeleccionarDatos(h chartHistorial) []chartPunto {
	unico := map[string]chartPunto{}
	for _, item := range append(append([]chartPunto{}, h.Daily...), h.Intraday...) {
		if chartNumero(item.BCVUSD) > 0 && chartNumero(item.BinanceUSDT) > 0 {
			unico[item.Date] = item
		}
	}
	sorted := make([]chartPunto, 0, len(unico))
	for _, item := range unico {
		sorted = append(sorted, item)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Date < sorted[j].Date })

	var nums []float64
	for _, item := range sorted {
		if n := chartNumero(item.BCVUSD); n > 0 {
			nums = append(nums, n)
		}
	}
	sort.Float64s(nums)
	mediana := 0.0
	if len(nums) > 0 {
		mediana = nums[len(nums)/2]
	}
	if mediana > 100 {
		limpio := sorted[:0]
		for _, item := range sorted {
			if chartNumero(item.BCVUSD) >= 10 {
				limpio = append(limpio, item)
			}
		}
		sorted = limpio
	}
	if len(sorted) > 30 {
		sorted = sorted[len(sorted)-30:]
	}
	return sorted
}

func chartEtiqueta(fecha string) string {
	// YYYY-MM-DD → DD/MM
	if len(fecha) == 10 && fecha[4] == '-' && fecha[7] == '-' {
		return fecha[8:10] + "/" + fecha[5:7]
	}
	return fecha
}

func chartConfig(datos []chartPunto) map[string]any {
	labels := make([]string, 0, len(datos))
	bcv := make([]float64, 0, len(datos))
	binance := make([]float64, 0, len(datos))
	for _, item := range datos {
		labels = append(labels, chartEtiqueta(item.Date))
		bcv = append(bcv, chartNumero(item.BCVUSD))
		binance = append(binance, chartNumero(item.BinanceUSDT))
	}
	return map[string]any{
		"type": "line",
		"data": map[string]any{
			"labels": labels,
			"datasets": []any{
				map[string]any{
					"label": "BCV Oficial", "data": bcv,
					"borderColor": "#2962FF", "backgroundColor": "rgba(41, 98, 255, 0.1)",
					"borderWidth": 2, "fill": true, "tension": 0.4, "pointRadius": 0, "pointHitRadius": 10,
				},
				map[string]any{
					"label": "Binance P2P", "data": binance,
					"borderColor": "#F7931A", "backgroundColor": "rgba(247, 147, 26, 0.1)",
					"borderWidth": 2, "fill": true, "tension": 0.4, "pointRadius": 0, "pointHitRadius": 10,
				},
			},
		},
		"options": map[string]any{
			"title": map[string]any{
				"display": true, "text": "Dólar en Venezuela - Últimos 30 días",
				"fontColor": "#D1D4DC", "fontSize": 18, "padding": 20,
			},
			"legend": map[string]any{
				"labels":   map[string]any{"fontColor": "#D1D4DC", "usePointStyle": true, "padding": 20},
				"position": "bottom",
			},
			"scales": map[string]any{
				"xAxes": []any{map[string]any{"gridLines": map[string]any{"color": "#2B2F3A", "drawBorder": false}, "ticks": map[string]any{"fontColor": "#D1D4DC", "maxTicksLimit": 10}}},
				"yAxes": []any{map[string]any{"gridLines": map[string]any{"color": "#2B2F3A", "drawBorder": false}, "ticks": map[string]any{"fontColor": "#D1D4DC"}}},
			},
			"layout": map[string]any{"padding": map[string]any{"left": 10, "right": 20, "top": 10, "bottom": 10}},
		},
	}
}

// wagoGenerateChart descarga la gráfica PNG de QuickChart y la guarda.
func wagoGenerateChart() ([]byte, error) {
	var historial chartHistorial
	if err := core.ReadJSON(chartHistPath(), &historial); err != nil {
		return nil, nil
	}
	datos := chartSeleccionarDatos(historial)
	if len(datos) == 0 {
		return nil, nil
	}

	payload := map[string]any{
		"width":            800,
		"height":           450,
		"devicePixelRatio": 2.0,
		"format":           "png",
		"backgroundColor":  "#1E222D",
		"chart":            chartConfig(datos),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, chartURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := chartHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("error HTTP: %d", resp.StatusCode)
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return nil, err
	}
	destino := chartOutput()
	if err := os.MkdirAll(filepath.Dir(destino), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(destino, buf.Bytes(), 0o644); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// wagoChartPath devuelve la gráfica si ya existe (getChartPath).
func wagoChartPath() string {
	path := chartOutput()
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return ""
}
