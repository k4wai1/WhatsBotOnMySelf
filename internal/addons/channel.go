package addons

import (
	"context"
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

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"whatsbot-go/internal/core"
	"whatsbot-go/internal/store"
)

// channelParseVes convierte cifras del BCV (es-VE: 1.009,07) o en-US
// (1,009.07) a float, igual que channel.js.
func channelParseVes(raw any) float64 {
	if v, ok := raw.(float64); ok {
		return v
	}
	s := fmt.Sprint(raw)
	var b strings.Builder
	for _, r := range s {
		if (r >= '0' && r <= '9') || r == '.' || r == ',' {
			b.WriteRune(r)
		}
	}
	s = b.String()
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

type channelValor struct {
	Valor  string `json:"valor"`
	Codigo string `json:"codigo"`
	Nombre string `json:"nombre"`
}

var (
	channelHTTP          = &http.Client{Timeout: 15 * time.Second}
	channelBCVURL        = "https://www.bcv.org.ve/"
	channelBinanceURL    = "https://p2p.binance.com/bapi/c2c/v2/friendly/c2c/adv/search"
	channelGenerateChart = func() ([]byte, error) {
		return nil, fmt.Errorf("gráfica no disponible")
	}
	channelStatePath   = func() string { return core.DataPath("assets", "statistics", "channel_state.json") }
	channelHistoryPath = func() string { return core.DataPath("assets", "statistics", "rates_history.json") }
	channelNow         = time.Now
	channelSchedule    = []int{6, 12, 15, 18}
	channelTargetID    = func() string { return os.Getenv("NEWS_CHANNEL_ID") }
)

// Channel porta addons/channel.js: comando .graf y publicación programada.
type Channel struct{}

func init() {
	core.Register(Channel{})
	core.RegisterBackground("channel", channelInit)
}

func (Channel) Commands() []string { return []string{"graf"} }

func (Channel) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}
	if err := ctx.React("📊"); err != nil {
		return err
	}
	data, err := channelObtenerMensaje()
	if err != nil || data == nil {
		return ctx.Reply("❌ Error: No pude obtener las tasas en este momento.")
	}
	if png, chartErr := channelGenerateChart(); chartErr == nil && len(png) > 0 {
		if err := ctx.SendImage(png, data.Mensaje); err != nil {
			return ctx.Reply(data.Mensaje)
		}
		return nil
	}
	return ctx.Reply(data.Mensaje)
}

type channelData struct {
	Mensaje     string
	BCVUSD      float64
	BinanceUSDT *float64
}

// channelBuildMessage arma el texto del reporte (mismo formato que channel.js).
func channelBuildMessage(valores map[string]channelValor, precioUSDT *float64, fecha string) string {
	var b strings.Builder
	b.WriteString("💵 *Actualización del Dólar & Euro*\n")
	b.WriteString("📅 " + fecha + "\n\n")
	for _, key := range []string{"dolar", "euro"} {
		dato, ok := valores[key]
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "🌍 *%s (%s)*\n🏛 BCV: %s\n", dato.Nombre, dato.Codigo, dato.Valor)
		if precioUSDT != nil {
			pBCV := channelParseVes(dato.Valor)
			etiqueta := "USDT"
			if key == "euro" {
				etiqueta = "Ref. USDT"
			}
			fmt.Fprintf(&b, "🔸 Binance (%s): %s\n", etiqueta, channelToFixed(*precioUSDT, 2))
			if !math.IsNaN(pBCV) && pBCV != 0 {
				fmt.Fprintf(&b, "📊 Brecha: %s%%\n\n", channelToFixed((*precioUSDT-pBCV)/pBCV*100, 2))
			} else {
				b.WriteString("\n")
			}
		} else {
			b.WriteString("🔸 Binance: No disponible\n\n")
		}
	}
	return b.String()
}

func channelToFixed(n float64, digits int) string {
	return strconv.FormatFloat(n, 'f', digits, 64)
}

// channelObtenerMensaje consulta BCV + Binance y arma el reporte.
func channelObtenerMensaje() (*channelData, error) {
	html, err := channelFetchBCVHTML()
	if err != nil {
		return nil, err
	}
	monedas := map[string]channelValor{
		"dolar": {Codigo: "USD", Nombre: "Dólar"},
		"euro":  {Codigo: "EUR", Nombre: "Euro"},
	}
	valores := map[string]channelValor{}
	var bcvUSD float64
	for key, info := range monedas {
		re := channelMonedaRegex(key)
		m := re.FindStringSubmatch(html)
		if m != nil {
			val := strings.ReplaceAll(m[1], " ", "")
			info.Valor = val
			valores[key] = info
			if key == "dolar" {
				bcvUSD = channelParseVes(val)
			}
		}
	}
	if len(valores) == 0 {
		return nil, fmt.Errorf("BCV sin datos")
	}

	var precio *float64
	if p, err := channelFetchBinance(); err == nil {
		precio = &p
	}

	fecha := "Hoy"
	if m := channelFechaRe.FindStringSubmatch(html); m != nil {
		fecha = strings.TrimSpace(m[1])
	}

	return &channelData{
		Mensaje:     channelBuildMessage(valores, precio, fecha),
		BCVUSD:      bcvUSD,
		BinanceUSDT: precio,
	}, nil
}

func channelFetchBCVHTML() (string, error) {
	req, err := http.NewRequest(http.MethodGet, channelBCVURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := channelHTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("BCV caído")
	}
	raw, err := io.ReadAll(resp.Body)
	return string(raw), err
}

func channelFetchBinance() (float64, error) {
	payload := `{"fiat":"VES","page":1,"rows":1,"tradeType":"BUY","asset":"USDT","payTypes":[],"publisherType":"merchant"}`
	req, err := http.NewRequest(http.MethodPost, channelBinanceURL, strings.NewReader(payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := channelHTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var parsed struct {
		Data []struct {
			Adv struct {
				Price string `json:"price"`
			} `json:"adv"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return 0, err
	}
	if len(parsed.Data) == 0 {
		return 0, fmt.Errorf("sin ofertas")
	}
	return strconv.ParseFloat(parsed.Data[0].Adv.Price, 64)
}

func channelSaveHistory(bcv float64, binance *float64) {
	if bcv == 0 || binance == nil {
		return
	}
	history := struct {
		Daily    []any `json:"daily"`
		Intraday []any `json:"intraday"`
	}{}
	_ = core.ReadJSON(channelHistoryPath(), &history)
	if history.Daily == nil {
		history.Daily = []any{}
	}
	now := channelNow()
	history.Intraday = append(history.Intraday, map[string]any{
		"timestamp":    now.UTC().Format(time.RFC3339),
		"date":         now.UTC().Format("2006-01-02"),
		"bcv_usd":      bcv,
		"binance_usdt": *binance,
	})
	_ = core.WriteJSON(channelHistoryPath(), history)
}

func channelLastPost() string {
	var state struct {
		LastID string `json:"lastId"`
	}
	_ = core.ReadJSON(channelStatePath(), &state)
	return state.LastID
}

func channelSaveLastPost(id string) {
	_ = core.WriteJSON(channelStatePath(), map[string]string{"lastId": id})
}

func channelInit(client *whatsmeow.Client, _ *store.Store) error {
	if client == nil {
		return nil
	}
	target := channelTargetID()
	if target == "" {
		slog.Info("[CHANNEL] NEWS_CHANNEL_ID no configurado: auto-publicación desactivada")
		return nil
	}
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			channelTick(client, target)
		}
	}()
	return nil
}

func channelTick(client *whatsmeow.Client, target string) {
	now := channelNow()
	currentHour := now.Hour()
	targetHour := -1
	for _, h := range channelSchedule {
		if currentHour >= h {
			targetHour = h
		}
	}
	if targetHour == -1 {
		return
	}
	eventID := now.UTC().Format("2006-01-02") + "-" + strconv.Itoa(targetHour)
	if channelLastPost() == eventID {
		return
	}

	isLate := currentHour > targetHour || (currentHour == targetHour && now.Minute() > 20)
	data, err := channelObtenerMensaje()
	if err != nil || data == nil {
		return
	}
	channelSaveHistory(data.BCVUSD, data.BinanceUSDT)

	mensaje := data.Mensaje
	if isLate {
		mensaje = fmt.Sprintf("🙇🏻‍♂️ *Disculpa la demora*, aquí tienes el reporte correspondiente a las %d:00.\n\n%s", targetHour, data.Mensaje)
	}

	jid, err := types.ParseJID(target)
	if err != nil {
		return
	}
	if targetHour == 18 {
		if png, chartErr := channelGenerateChart(); chartErr == nil && len(png) > 0 {
			if err := channelSendImage(client, jid, png, mensaje); err == nil {
				channelSaveLastPost(eventID)
				return
			}
		}
	}
	if _, err := client.SendMessage(context.Background(), jid, core.TextMessage(mensaje)); err == nil {
		channelSaveLastPost(eventID)
	}
}

// channelSendImage sube y envía una imagen a un JID arbitrario (newsletter).
func channelSendImage(client *whatsmeow.Client, jid types.JID, data []byte, caption string) error {
	if client == nil || len(data) == 0 {
		return fmt.Errorf("sin imagen")
	}
	resp, err := client.Upload(context.Background(), data, whatsmeow.MediaImage)
	if err != nil {
		return err
	}
	msg := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
		URL:           proto.String(resp.URL),
		Mimetype:      proto.String("image/png"),
		Caption:       proto.String(caption),
		FileSHA256:    resp.FileSHA256,
		FileEncSHA256: resp.FileEncSHA256,
		FileLength:    proto.Uint64(resp.FileLength),
		MediaKey:      resp.MediaKey,
		DirectPath:    proto.String(resp.DirectPath),
	}}
	_, err = client.SendMessage(context.Background(), jid, msg)
	return err
}

var (
	channelMonedaRegex = func(id string) *regexp.Regexp {
		return regexp.MustCompile(`(?is)id=["']` + id + `["'][\s\S]*?<strong[^>]*>\s*([\d.,]+)\s*</strong>`)
	}
	channelFechaRe = regexp.MustCompile(`(?is)Fecha Valor:[^>]*>([^<]+)<`)
)
