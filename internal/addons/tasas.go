package addons

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"whatsbot-go/internal/core"
)

// tasas.js — tasas de cambio del BCV (es-VE y en-US) + Binance P2P y Airtm.
//
// Variables inyectables para pruebas sin red.
var (
	tasasBCVURL     = "https://www.bcv.org.ve/"
	tasasBinanceURL = "https://p2p.binance.com/bapi/c2c/v2/friendly/c2c/adv/search"
	tasasAirtmURL   = "https://rates.airtm.io/"
	tasasHTTP       = &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			// Equivale al NODE_TLS_REJECT_UNAUTHORIZED=0 del addon original:
			// la web del BCV suele presentar certificados conflictivos.
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	tasasBinanceHTTP = &http.Client{Timeout: 15 * time.Second}
	tasasAirtmHTTP   = &http.Client{Timeout: 15 * time.Second}
)

// Tasas porta addons/tasas.js.
type Tasas struct{}

// Commands replica el array del addon original.
func (Tasas) Commands() []string {
	return []string{"dolar", "dólar", "euro", "yuan", "lira", "rublo", "tasas"}
}

type tasasMoneda struct {
	id     string
	codigo string
	nombre string
}

// tasasMonedas conserva el orden de inserción del objeto JS original.
var tasasMonedas = []tasasMoneda{
	{"dolar", "USD", "Dólar"},
	{"euro", "EUR", "Euro"},
	{"yuan", "CNY", "Yuan"},
	{"lira", "TRY", "Lira"},
	{"rublo", "RUB", "Rublo"},
}

type tasasValor struct {
	valor  string
	codigo string
	nombre string
}

// Handle replica el flujo completo del addon original.
func (Tasas) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}

	multiplicador := tasasMultiplicador(ctx.Args)
	commandUsed := tasasCommandUsed(ctx.RawText)

	if err := ctx.React("⏳"); err != nil {
		slog.Error("Error procesando el addon de tasas:", "error", err)
		_ = ctx.React("❌")
		return nil
	}

	if err := tasasProcesar(ctx, commandUsed, multiplicador); err != nil {
		slog.Error("Error procesando el addon de tasas:", "error", err)
		_ = ctx.React("❌")
	}
	return nil
}

func tasasProcesar(ctx *core.Context, commandUsed string, multiplicador float64) error {
	html, err := tasasFetchBCV()
	if err != nil {
		return err
	}

	valores := tasasParseValores(html)

	if commandUsed == "tasas" {
		if len(valores) == 0 {
			return fmt.Errorf("Estructura del BCV alterada temporalmente.")
		}
		var respuesta strings.Builder
		respuesta.WriteString("*🏛️ Tasas de Cambio Oficiales (BCV)*\n\n")
		if multiplicador != 1 {
			respuesta.WriteString("🧮 *Equivalencia para:* " + tasasJSNumber(multiplicador) + "\n\n")
		}
		for _, info := range tasasMonedas {
			data, ok := valores[info.id]
			if !ok {
				continue
			}
			total := tasasParseVes(data.valor) * multiplicador
			respuesta.WriteString("• " + data.nombre + ": " + tasasToFixed(total, 2) + " VES\n")
		}
		if fecha := tasasFecha(html); fecha != "" {
			respuesta.WriteString("\n📅 " + fecha)
		}
		if err := ctx.SendQuoted(core.TextMessage(respuesta.String()), ctx.Message); err != nil {
			return err
		}
		return ctx.React("✅")
	}

	for _, info := range tasasMonedas {
		if commandUsed != info.id {
			continue
		}
		data, ok := valores[info.id]
		if !ok {
			return fmt.Errorf("El valor de %s no está disponible en la página.", info.codigo)
		}

		precioBCV := tasasParseVes(data.valor)
		totalBCV := precioBCV * multiplicador

		if info.id == "dolar" {
			var respuesta strings.Builder
			if multiplicador != 1 {
				respuesta.WriteString("🧮 *Calculando:* " + tasasJSNumber(multiplicador) + " USD\n\n")
			}
			respuesta.WriteString("🏛 *BCV:* " + tasasToFixed(totalBCV, 2) + " VES\n")

			tasasBinance(&respuesta, precioBCV, multiplicador)
			tasasAirtm(&respuesta, multiplicador)

			if err := ctx.SendQuoted(core.TextMessage(strings.TrimSpace(respuesta.String())), ctx.Message); err != nil {
				return err
			}
		} else {
			var respuesta strings.Builder
			if multiplicador != 1 {
				respuesta.WriteString("🧮 *Calculando:* " + tasasJSNumber(multiplicador) + " " + info.codigo + "\n\n")
			}
			respuesta.WriteString("🏛 *BCV:* " + tasasToFixed(totalBCV, 2) + " VES")
			if err := ctx.SendQuoted(core.TextMessage(respuesta.String()), ctx.Message); err != nil {
				return err
			}
		}

		return ctx.React("✅")
	}

	return nil
}

func tasasFetchBCV() (string, error) {
	reqCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, tasasBCVURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")

	resp, err := tasasHTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP BCV Falló: %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func tasasParseValores(html string) map[string]tasasValor {
	valores := make(map[string]tasasValor)
	for _, info := range tasasMonedas {
		re := regexp.MustCompile(`(?is)id=["']` + regexp.QuoteMeta(info.id) + `["'][\s\S]*?<strong[^>]*>\s*([\d.,]+)\s*<\/strong>`)
		if m := re.FindStringSubmatch(html); len(m) > 1 && m[1] != "" {
			valores[info.id] = tasasValor{
				valor:  regexp.MustCompile(`\s`).ReplaceAllString(m[1], ""),
				codigo: info.codigo,
				nombre: info.nombre,
			}
		}
	}
	return valores
}

func tasasFecha(html string) string {
	re := regexp.MustCompile(`Fecha Valor:[^>]*>([^<]+)<`)
	if m := re.FindStringSubmatch(html); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func tasasBinance(respuesta *strings.Builder, precioBCV, multiplicador float64) {
	payload := map[string]any{
		"fiat": "VES", "page": 1, "rows": 1, "tradeType": "BUY",
		"asset": "USDT", "payTypes": []any{}, "publisherType": "merchant",
	}
	raw, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, tasasBinanceURL, bytes.NewReader(raw))
	if err != nil {
		respuesta.WriteString("🔸 *Binance:* Error de conexión\n")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := tasasBinanceHTTP.Do(req)
	if err != nil {
		respuesta.WriteString("🔸 *Binance:* Error de conexión\n")
		return
	}
	defer resp.Body.Close()

	var data struct {
		Data []struct {
			Adv struct {
				Price string `json:"price"`
			} `json:"adv"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil || len(data.Data) == 0 {
		respuesta.WriteString("🔸 *Binance:* No disponible\n")
		return
	}

	precioBinance, err := strconv.ParseFloat(data.Data[0].Adv.Price, 64)
	if err != nil {
		respuesta.WriteString("🔸 *Binance:* No disponible\n")
		return
	}
	total := precioBinance * multiplicador
	var porcentaje float64
	if precioBCV != 0 {
		porcentaje = (precioBinance - precioBCV) / precioBCV * 100
	}
	respuesta.WriteString("🔸 *Binance:* " + tasasToFixed(total, 2) + " VES\n📊 *Brecha:* " + tasasToFixed(porcentaje, 2) + "%\n")
}

func tasasAirtm(respuesta *strings.Builder, multiplicador float64) {
	resp, err := tasasAirtmHTTP.Get(tasasAirtmURL)
	if err != nil {
		respuesta.WriteString("🔹 *Airtm:* Error de red\n")
		return
	}
	defer resp.Body.Close()

	var data struct {
		Data map[string]struct {
			AddValue string `json:"addValue"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		respuesta.WriteString("🔹 *Airtm:* No disponible\n")
		return
	}

	rate, ok := data.Data["ves/usd"]
	if !ok || rate.AddValue == "" {
		respuesta.WriteString("🔹 *Airtm:* No disponible\n")
		return
	}
	precio, err := strconv.ParseFloat(rate.AddValue, 64)
	if err != nil {
		respuesta.WriteString("🔹 *Airtm:* No disponible\n")
		return
	}
	respuesta.WriteString("🔹 *Airtm:* " + tasasToFixed(precio*multiplicador, 2) + " VES\n")
}

// tasasMultiplicador replica parseFloat(args[0].replace(',', '.')) con el
// recorte de seguridad de 999.999.999.
func tasasMultiplicador(args []string) float64 {
	multiplicador := 1.0
	if len(args) == 0 {
		return multiplicador
	}
	arg := strings.Replace(args[0], ",", ".", 1)
	num, err := strconv.ParseFloat(arg, 64)
	if err == nil && !math.IsNaN(num) && num > 0 {
		multiplicador = math.Min(num, 999999999)
	}
	return multiplicador
}

// tasasCommandUsed limpia el prefijo, minúsculas y acentos del primer token,
// igual que el addon original.
func tasasCommandUsed(rawText string) string {
	first := strings.Fields(strings.TrimSpace(rawText))
	if len(first) == 0 {
		return ""
	}
	token := strings.ToLower(first[0])
	if token != "" {
		if r := []rune(token); len(r) > 0 && strings.ContainsRune(".! /,", r[0]) {
			token = string(r[1:])
		}
	}
	return tasasSinAcentos(token)
}

func tasasSinAcentos(s string) string {
	decomp := norm.NFD.String(s)
	var b strings.Builder
	for _, r := range decomp {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// tasasParseVes porta parseVes() de tasas.js: distingue 1.009,07 (es-VE) de
// 1,009.07 (en-US) sin colapsar las cifras mayores que 1000.
func tasasParseVes(raw any) float64 {
	var s string
	switch v := raw.(type) {
	case nil:
		s = ""
	case float64:
		return v
	case string:
		s = v
	default:
		s = fmt.Sprint(v)
	}

	var limpio strings.Builder
	for _, r := range s {
		if (r >= '0' && r <= '9') || r == '.' || r == ',' {
			limpio.WriteRune(r)
		}
	}
	s = limpio.String()
	if s == "" {
		return math.NaN()
	}

	lastComma := strings.LastIndex(s, ",")
	lastDot := strings.LastIndex(s, ".")

	switch {
	case lastComma != -1 && lastDot != -1:
		if lastComma > lastDot {
			s = strings.ReplaceAll(s, ".", "")
			s = strings.Replace(s, ",", ".", 1)
		} else {
			s = strings.ReplaceAll(s, ",", "")
		}
	case lastComma != -1:
		s = strings.Replace(s, ",", ".", 1)
	case lastDot != -1:
		partes := strings.Split(s, ".")
		if len(partes) > 2 || len(partes[1]) == 3 {
			s = strings.Join(partes, "")
		}
	}

	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return math.NaN()
	}
	return n
}

func tasasToFixed(n float64, digits int) string {
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return "NaN"
	}
	return strconv.FormatFloat(n, 'f', digits, 64)
}

// tasasJSNumber imita `${n}` de JavaScript: sin decimales innecesarios.
func tasasJSNumber(n float64) string {
	return strconv.FormatFloat(n, 'f', -1, 64)
}

var _ = os.Getenv

func init() { core.Register(Tasas{}) }
