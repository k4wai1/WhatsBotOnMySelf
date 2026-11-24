package addons

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/disintegration/imaging"
	"go.mau.fi/whatsmeow/proto/waE2E"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"whatsbot-go/internal/core"
)

// Chat porta addons/chat.js (v4.1): agente con DeepSeek, historial, visión,
// documentos, herramientas web/MCP, clima, gráficos y edición de imágenes.
type Chat struct{}

// Commands cumple core.Addon.
func (Chat) Commands() []string { return []string{"chat", "c"} }

const (
	chatHistoryLimit      = 90
	chatMaxUserChars      = 2000
	chatQuotedTTL         = 24 * time.Hour
	chatMaxIterations     = 5
	chatRateWindow        = 15 * time.Second
	chatRateMax           = 3
	chatQueueMax          = 3
	chatQueueResponseTime = 10 * time.Second
)

// URLs y cliente inyectables para pruebas sin red.
var (
	chatHTTP           = &http.Client{Timeout: 90 * time.Second}
	chatDeepSeekBase   = "https://api.deepseek.com"
	chatYouURL         = "https://ydc-index.io/v1/search"
	chatAniListURL     = "https://graphql.anilist.co"
	chatGeocodeURL     = "https://geocoding-api.open-meteo.com/v1/search"
	chatOpenMeteoURL   = "https://api.open-meteo.com/v1/forecast"
	chatQuickChartURL  = "https://quickchart.io/chart"
	chatHistoryPathAlt string
	chatObscuraMCPAlt  string
	chatParallelMCPAlt string
	chatWeatherMCPAlt  string
)

func chatHistoryPath() string {
	if chatHistoryPathAlt != "" {
		return chatHistoryPathAlt
	}
	return core.DataPath("assets", "chatHistory.json")
}

func chatDeepSeekChatURL() string { return chatDeepSeekBase + "/chat/completions" }

// ─── Owner ─────────────────────────────────────────────────────────────────

func chatOwnerNumbers() []string {
	raw := os.Getenv("OWNER_NUMBERS")
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	for _, n := range strings.Split(raw, ",") {
		var b strings.Builder
		for _, r := range n {
			if r >= '0' && r <= '9' {
				b.WriteRune(r)
			}
		}
		if b.Len() > 0 {
			out = append(out, b.String())
		}
	}
	return out
}

func chatIsHost(ctx *core.Context) bool {
	if ctx.Message != nil && ctx.Message.Info.IsFromMe {
		return true
	}
	sender := chatDigits(chatJIDBase(ctx.SenderJID().String()))
	for _, owner := range chatOwnerNumbers() {
		if sender == owner {
			return true
		}
	}
	return false
}

func chatJIDBase(jid string) string {
	base := strings.SplitN(jid, "@", 2)[0]
	return strings.SplitN(base, ":", 2)[0]
}

func chatDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ─── Caché de mensajes citados ─────────────────────────────────────────────

type chatQuotedEntry struct {
	Text      string `json:"text"`
	Sender    string `json:"sender"`
	T         int64  `json:"t"`
	ExpiresAt int64  `json:"expiresAt"`
}

var (
	chatQuotedMu    sync.Mutex
	chatQuotedCache = make(map[string]chatQuotedEntry)
)

func chatQuotedKey(text, sender string) string {
	if sender == "" {
		sender = "?"
	}
	return sender + "::" + strings.TrimSpace(text)
}

func chatCacheQuoted(text, sender string, t int64) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if sender == "" {
		sender = "?"
	}
	now := time.Now().UnixMilli()
	chatQuotedMu.Lock()
	chatQuotedCache[chatQuotedKey(text, sender)] = chatQuotedEntry{
		Text:      text,
		Sender:    sender,
		T:         t,
		ExpiresAt: now + chatQuotedTTL.Milliseconds(),
	}
	if len(chatQuotedCache) > 500 {
		for k, v := range chatQuotedCache {
			if v.ExpiresAt < now {
				delete(chatQuotedCache, k)
			}
		}
	}
	chatQuotedMu.Unlock()
}

func chatIsQuotedCached(text, sender string) bool {
	key := chatQuotedKey(text, sender)
	chatQuotedMu.Lock()
	defer chatQuotedMu.Unlock()
	hit, ok := chatQuotedCache[key]
	if !ok {
		return false
	}
	if hit.ExpiresAt < time.Now().UnixMilli() {
		delete(chatQuotedCache, key)
		return false
	}
	return true
}

// ─── Truncado / utilidades ─────────────────────────────────────────────────

func chatTruncateUserText(text string) string {
	r := []rune(text)
	if len(r) <= chatMaxUserChars {
		return text
	}
	return string(r[:chatMaxUserChars]) +
		fmt.Sprintf("…\n\n[⚠️ Mensaje cortado a %d caracteres para no malgastar tokens de la API. Por seguridad, no se pudo leer el resto del mensaje.]", chatMaxUserChars)
}

func chatTimeAgo(ts int64) string {
	mins := int(math.Round(float64(time.Now().UnixMilli()-ts) / 60000))
	if mins < 1 {
		mins = 1
	}
	if mins < 60 {
		return fmt.Sprintf("hace %d min", mins)
	}
	hours := int(math.Round(float64(mins) / 60))
	if hours < 24 {
		return fmt.Sprintf("hace %d h", hours)
	}
	return fmt.Sprintf("hace %d d", int(math.Round(float64(hours)/24)))
}

func chatEscXML(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

func chatResolveName(ctx *core.Context, phone string) string {
	if ctx.Store != nil {
		if c, ok := ctx.Store.Get(phone + "@s.whatsapp.net"); ok {
			if v, ok := c["notify"].(string); ok && v != "" {
				return v
			}
			if v, ok := c["name"].(string); ok && v != "" {
				return v
			}
		}
	}
	return phone
}

// ─── Historial (equivalente a unread.getRecent sin duplicar el addon) ──────

type chatHistMsg struct {
	Text   string `json:"text"`
	Sender string `json:"sender"`
	T      int64  `json:"t"`
}

var (
	chatHistMu      sync.Mutex
	chatHistCache   map[string][]chatHistMsg
	chatHistModTime time.Time
	chatHistSize    int64
)

func chatGetRecent(jid string, n int) []chatHistMsg {
	path := chatHistoryPath()
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}

	chatHistMu.Lock()
	defer chatHistMu.Unlock()

	if chatHistCache == nil || !info.ModTime().Equal(chatHistModTime) || info.Size() != chatHistSize {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		loaded := make(map[string][]chatHistMsg)
		if err := json.Unmarshal(raw, &loaded); err != nil {
			return nil
		}
		chatHistCache = loaded
		chatHistModTime = info.ModTime()
		chatHistSize = info.Size()
	}

	h := chatHistCache[jid]
	if len(h) == 0 {
		return nil
	}
	if n > len(h) {
		n = len(h)
	}
	out := make([]chatHistMsg, n)
	copy(out, h[len(h)-n:])
	return out
}

// ─── Rate-limit por usuario con cola ───────────────────────────────────────

type chatQueueItem struct {
	ctx  *core.Context
	args []string
}

type chatRateState struct {
	timestamps []int64
	queue      []chatQueueItem
}

var (
	chatRateMu sync.Mutex
	chatRate   = make(map[string]*chatRateState)
)

func chatGetRateState(phone string) *chatRateState {
	st, ok := chatRate[phone]
	if !ok {
		st = &chatRateState{}
		chatRate[phone] = st
	}
	return st
}

func chatCleanTimestamps(st *chatRateState) {
	now := time.Now().UnixMilli()
	window := chatRateWindow.Milliseconds()
	kept := st.timestamps[:0]
	for _, t := range st.timestamps {
		if now-t < window {
			kept = append(kept, t)
		}
	}
	st.timestamps = kept
}

func chatScheduleQueue(phone string) {
	chatRateMu.Lock()
	st := chatGetRateState(phone)
	if len(st.queue) == 0 {
		chatRateMu.Unlock()
		return
	}
	chatCleanTimestamps(st)
	oldest := time.Now().UnixMilli()
	if len(st.timestamps) > 0 {
		oldest = st.timestamps[0]
	}
	remaining := chatRateWindow.Milliseconds() - (time.Now().UnixMilli() - oldest)
	delay := math.Max(0, math.Min(float64(remaining+100), float64(chatQueueResponseTime.Milliseconds())))
	chatRateMu.Unlock()

	time.AfterFunc(time.Duration(delay)*time.Millisecond, func() {
		chatRateMu.Lock()
		st := chatGetRateState(phone)
		chatCleanTimestamps(st)
		if len(st.queue) == 0 {
			chatRateMu.Unlock()
			return
		}
		if len(st.timestamps) >= chatRateMax {
			chatRateMu.Unlock()
			chatScheduleQueue(phone)
			return
		}
		item := st.queue[0]
		st.queue = st.queue[1:]
		st.timestamps = append(st.timestamps, time.Now().UnixMilli())
		chatRateMu.Unlock()

		if err := chatProcess(item.ctx, item.args); err != nil {
			slog.Error("❌ chat: error procesando cola:", "error", err)
		}

		chatRateMu.Lock()
		pending := len(chatGetRateState(phone).queue) > 0
		chatRateMu.Unlock()
		if pending {
			chatScheduleQueue(phone)
		}
	})
}

// ─── MCP genérico (JSON-RPC sobre Streamable HTTP) ─────────────────────────

type chatMcpClient struct {
	url   string
	token string
	name  string

	mu        sync.Mutex
	sessionID string
	rpcID     int
}

func chatNewMcp(urlStr, token, name string) *chatMcpClient {
	return &chatMcpClient{url: urlStr, token: token, name: name}
}

func chatParseMcpPayload(text string) map[string]any {
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "{") {
		var out map[string]any
		if json.Unmarshal([]byte(trimmed), &out) == nil {
			return out
		}
	}
	var payload map[string]any
	for _, line := range strings.Split(text, "\n") {
		l := strings.TrimSpace(line)
		if strings.HasPrefix(l, "data:") {
			var out map[string]any
			if json.Unmarshal([]byte(strings.TrimSpace(l[5:])), &out) == nil {
				payload = out
			}
		}
	}
	return payload
}

func (m *chatMcpClient) request(method string, params any, notification bool) (map[string]any, error) {
	m.mu.Lock()
	sessionID := m.sessionID
	m.mu.Unlock()

	headers := map[string]string{
		"Content-Type": "application/json",
		"Accept":       "application/json, text/event-stream",
	}
	if m.token != "" {
		headers["Authorization"] = "Bearer " + m.token
	}
	if sessionID != "" {
		headers["mcp-session-id"] = sessionID
	}

	body := map[string]any{"jsonrpc": "2.0", "method": method}
	if !notification {
		m.mu.Lock()
		m.rpcID++
		body["id"] = m.rpcID
		m.mu.Unlock()
	}
	if params != nil {
		body["params"] = params
	}
	raw, _ := json.Marshal(body)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := chatHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if sid := resp.Header.Get("mcp-session-id"); sid != "" {
		m.mu.Lock()
		m.sessionID = sid
		m.mu.Unlock()
	}

	if notification || resp.StatusCode == http.StatusAccepted {
		return nil, nil
	}

	payloadRaw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	payload := chatParseMcpPayload(string(payloadRaw))
	if payload == nil {
		return nil, fmt.Errorf("MCP %s: respuesta ilegible", m.name)
	}
	if e, ok := payload["error"].(map[string]any); ok {
		msg, _ := e["message"].(string)
		if msg == "" {
			msg = "error"
		}
		return nil, fmt.Errorf("MCP %s: %s", m.name, msg)
	}
	result, _ := payload["result"].(map[string]any)
	return result, nil
}

func (m *chatMcpClient) ensure(force bool) error {
	m.mu.Lock()
	hasSession := m.sessionID != ""
	m.mu.Unlock()
	if hasSession && !force {
		return nil
	}
	m.mu.Lock()
	m.sessionID = ""
	m.mu.Unlock()
	_, err := m.request("initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "whatsbot-self", "version": "4.1"},
	}, false)
	if err != nil {
		return err
	}
	_, err = m.request("notifications/initialized", map[string]any{}, true)
	return err
}

func (m *chatMcpClient) callTool(toolName string, args map[string]any) (map[string]any, error) {
	if err := m.ensure(false); err == nil {
		if res, err := m.request("tools/call", map[string]any{"name": toolName, "arguments": args}, false); err == nil {
			return res, nil
		}
	}
	if err := m.ensure(true); err != nil {
		return nil, err
	}
	return m.request("tools/call", map[string]any{"name": toolName, "arguments": args}, false)
}

func chatExtractTextContent(result map[string]any) string {
	content, _ := result["content"].([]any)
	var parts []string
	for _, c := range content {
		item, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if item["type"] == "text" {
			if t, ok := item["text"].(string); ok {
				parts = append(parts, t)
			}
		}
	}
	s := strings.Join(parts, "\n")
	return chatSliceRunes(s, 12000)
}

func chatExtractImageContent(result map[string]any) ([]byte, string, bool) {
	content, _ := result["content"].([]any)
	for _, c := range content {
		item, ok := c.(map[string]any)
		if !ok || item["type"] != "image" {
			continue
		}
		data, _ := item["data"].(string)
		mime, _ := item["mimeType"].(string)
		if mime == "" {
			mime = "image/png"
		}
		raw, err := base64.StdEncoding.DecodeString(data)
		if err == nil {
			return raw, mime, true
		}
	}
	return nil, "", false
}

func chatEnvOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func chatObscura() *chatMcpClient {
	u := chatObscuraMCPAlt
	if u == "" {
		u = chatEnvOr("OBSCURA_MCP_URL", "http://127.0.0.1:3000/mcp")
	}
	return chatNewMcp(u, os.Getenv("OBSCURA_MCP_TOKEN"), "Obscura")
}

func chatParallel() *chatMcpClient {
	u := chatParallelMCPAlt
	if u == "" {
		u = chatEnvOr("PARALLEL_MCP_URL", "https://search.parallel.ai/mcp")
	}
	return chatNewMcp(u, os.Getenv("PARALLEL_API_KEY"), "Parallel")
}

func chatWeather() *chatMcpClient {
	u := chatWeatherMCPAlt
	if u == "" {
		u = os.Getenv("WEATHER_MCP_URL")
	}
	if u == "" {
		return nil
	}
	return chatNewMcp(u, "", "Weather")
}

func chatObscuraNavigate(u, extract string) (string, error) {
	c := chatObscura()
	if _, err := c.callTool("browser_navigate", map[string]any{"url": u}); err != nil {
		return "", err
	}
	if extract == "snapshot" {
		r, err := c.callTool("browser_snapshot", map[string]any{"max_chars": 6000})
		if err != nil {
			return "", err
		}
		return chatExtractTextContent(r), nil
	}
	r, err := c.callTool("browser_markdown", map[string]any{"max_chars": 8000})
	if err == nil {
		if txt := chatExtractTextContent(r); txt != "" {
			return txt, nil
		}
	}
	snap, err := c.callTool("browser_snapshot", map[string]any{"max_chars": 6000})
	if err != nil {
		return "", err
	}
	return chatExtractTextContent(snap), nil
}

func chatObscuraScreenshot(u string) ([]byte, string, error) {
	c := chatObscura()
	if _, err := c.callTool("browser_navigate", map[string]any{"url": u}); err != nil {
		return nil, "", err
	}
	r, err := c.callTool("browser_screenshot", map[string]any{})
	if err != nil {
		return nil, "", err
	}
	raw, mime, ok := chatExtractImageContent(r)
	if !ok {
		return nil, "", errors.New("Obscura no devolvió imagen")
	}
	return raw, mime, nil
}

// ─── Clima (Open-Meteo) ────────────────────────────────────────────────────

const chatValencia = "Valencia, Carabobo, Venezuela"

var chatValenciaSeed = struct {
	lat, lon float64
	nombre   string
}{10.162, -68.0077, "Valencia (Carabobo, Venezuela)"}

var chatWCodes = map[int]string{
	0: "☀️ despejado", 1: "🌤 mayormente despejado", 2: "⛅ parcialmente nublado", 3: "☁️ nublado",
	45: "🌫 niebla", 48: "🌫 niebla con escarcha",
	51: "🌦 llovizna ligera", 53: "🌦 llovizna", 55: "🌦 llovizna intensa",
	56: "🌧 llovizna helada", 57: "🌧 llovizna helada intensa",
	61: "🌧 lluvia ligera", 63: "🌧 lluvia", 65: "🌧 lluvia intensa",
	66: "🌧 lluvia helada", 67: "🌧 lluvia helada intensa",
	71: "🌨 nieve ligera", 73: "🌨 nieve", 75: "🌨 nieve intensa", 77: "🌨 granizo fino",
	80: "🌦 chubascos ligeros", 81: "🌦 chubascos", 82: "⛈ chubascos fuertes",
	85: "🌨 chubascos de nieve", 86: "🌨 chubascos de nieve fuertes",
	95: "⛈ tormenta", 96: "⛈ tormenta con granizo", 99: "⛈ tormenta con granizo fuerte",
}

func chatDescWeather(code int) string {
	if v, ok := chatWCodes[code]; ok {
		return v
	}
	return fmt.Sprintf("código %d", code)
}

func chatGeocodeCity(city string) (float64, float64, string, bool) {
	u := chatGeocodeURL + "?name=" + url.QueryEscape(city) + "&count=1&language=es&format=json"
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, 0, "", false
	}
	resp, err := chatHTTP.Do(req)
	if err != nil {
		return 0, 0, "", false
	}
	defer resp.Body.Close()

	var data struct {
		Results []struct {
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
			Name      string  `json:"name"`
			Admin1    string  `json:"admin1"`
			Country   string  `json:"country"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil || len(data.Results) == 0 {
		return 0, 0, "", false
	}
	g := data.Results[0]
	parts := []string{}
	for _, p := range []string{g.Name, g.Admin1, g.Country} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return g.Latitude, g.Longitude, strings.Join(parts, ", "), true
}

func chatConsultarClima(city string, days int) (string, error) {
	coords := chatValenciaSeed
	nombre := chatValenciaSeed.nombre

	if strings.TrimSpace(city) != "" && strings.TrimSpace(city) != chatValencia {
		if lat, lon, name, ok := chatGeocodeCity(city); ok {
			coords.lat, coords.lon, nombre = lat, lon, name
		}
	}

	if wm := chatWeather(); wm != nil {
		if r, err := wm.callTool("get_forecast", map[string]any{
			"location": city,
			"days":     chatClampInt(days, 1, 7),
		}); err == nil {
			if txt := chatExtractTextContent(r); txt != "" {
				return txt, nil
			}
		} else {
			slog.Warn("⚠️ chat: clima MCP falló, usando Open-Meteo:", "error", err)
		}
	}

	d := chatClampInt(days, 1, 7)
	sunset := "false"
	if d > 3 {
		sunset = "true"
	}
	u := fmt.Sprintf("%s?latitude=%v&longitude=%v&current_weather=true&hourly=temperature_2m,relative_humidity_2m,precipitation_probability&daily=temperature_2m_max,temperature_2m_min,weathercode,precipitation_probability_max&sunset=%s&timezone=auto&forecast_days=%d",
		chatOpenMeteoURL, coords.lat, coords.lon, sunset, d)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	resp, err := chatHTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("Open-Meteo HTTP %d", resp.StatusCode)
	}

	var data struct {
		CurrentWeather struct {
			Temperature float64 `json:"temperature"`
			WeatherCode int     `json:"weathercode"`
			Windspeed   float64 `json:"windspeed"`
			IsDay       *int    `json:"is_day"`
		} `json:"current_weather"`
		Daily struct {
			Time                 []string  `json:"time"`
			TempMax              []float64 `json:"temperature_2m_max"`
			TempMin              []float64 `json:"temperature_2m_min"`
			WeatherCode          []int     `json:"weathercode"`
			PrecipitationProbMax []float64 `json:"precipitation_probability_max"`
		} `json:"daily"`
		Hourly struct {
			Time              []string  `json:"time"`
			RelativeHumidity  []float64 `json:"relative_humidity_2m"`
			PrecipitationProb []float64 `json:"precipitation_probability"`
		} `json:"hourly"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}

	cw := data.CurrentWeather
	lines := []string{
		"🌤️ *Clima en " + nombre + "*",
		fmt.Sprintf("Ahora: %d °C · %s · viento %v km/h", int(math.Round(cw.Temperature)), chatDescWeather(cw.WeatherCode), chatJSNumber(cw.Windspeed)),
		"",
	}

	if len(data.Daily.Time) > 0 {
		lines = append(lines, "*Pronóstico:*")
		for i := range data.Daily.Time {
			dia := chatFormatSpanishDate(data.Daily.Time[i])
			minv := chatAtFloat(data.Daily.TempMin, i)
			maxv := chatAtFloat(data.Daily.TempMax, i)
			wc := chatAtInt(data.Daily.WeatherCode, i)
			rain := chatAtFloat(data.Daily.PrecipitationProbMax, i)
			lines = append(lines, fmt.Sprintf("📅 %s: %d–%d °C · %s · lluvia %v%%",
				dia, int(math.Round(minv)), int(math.Round(maxv)), chatDescWeather(wc), chatJSNumber(rain)))
		}
	}

	if len(data.Hourly.PrecipitationProb) > 0 && cw.IsDay != nil {
		nowIdx := -1
		now := time.Now()
		for i, t := range data.Hourly.Time {
			parsed, err := time.Parse("2006-01-02T15:04", t)
			if err != nil {
				continue
			}
			if now.Sub(parsed.Add(15*time.Minute)) < time.Hour {
				nowIdx = i
				break
			}
		}
		if nowIdx >= 0 {
			hum := "?"
			if nowIdx < len(data.Hourly.RelativeHumidity) {
				hum = chatJSNumber(math.Round(data.Hourly.RelativeHumidity[nowIdx]))
			}
			rain := chatAtFloat(data.Hourly.PrecipitationProb, nowIdx)
			lines = append(lines, "", fmt.Sprintf("💧 Humedad: %s%% · lluvia próxima %v%%", hum, chatJSNumber(rain)))
		}
	}

	var filtered []string
	for _, l := range lines {
		if l != "" {
			filtered = append(filtered, l)
		}
	}
	return strings.Join(filtered, "\n"), nil
}

func chatClampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func chatAtFloat(arr []float64, i int) float64 {
	if i >= 0 && i < len(arr) {
		return arr[i]
	}
	return 0
}

func chatAtInt(arr []int, i int) int {
	if i >= 0 && i < len(arr) {
		return arr[i]
	}
	return 0
}

var (
	chatWeekdays = []string{"dom", "lun", "mar", "mié", "jue", "vie", "sáb"}
	chatMonths   = []string{"ene", "feb", "mar", "abr", "may", "jun", "jul", "ago", "sep", "oct", "nov", "dic"}
)

func chatFormatSpanishDate(raw string) string {
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		t, err = time.Parse("2006-01-02T15:04", raw)
		if err != nil {
			return raw
		}
	}
	return fmt.Sprintf("%s, %d %s", chatWeekdays[int(t.Weekday())], t.Day(), chatMonths[int(t.Month())-1])
}

func chatJSNumber(n float64) string {
	if n == math.Trunc(n) {
		return strconv.FormatFloat(n, 'f', 0, 64)
	}
	return strconv.FormatFloat(n, 'f', -1, 64)
}

// ─── Búsquedas ─────────────────────────────────────────────────────────────

type chatYouResult struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	URL         string   `json:"url"`
	Snippets    []string `json:"snippets"`
}

func chatFormatYouResults(results []chatYouResult, maxLen int) string {
	if len(results) == 0 {
		return "No se encontraron resultados."
	}
	if len(results) > 3 {
		results = results[:3]
	}
	blocks := make([]string, 0, len(results))
	for _, r := range results {
		snippet := r.Description
		if snippet == "" && len(r.Snippets) > 0 {
			snippet = r.Snippets[0]
		}
		snippet = chatSliceRunes(snippet, maxLen)
		title := r.Title
		if title == "" {
			title = "Web"
		}
		blocks = append(blocks, "📌 "+title+"\n"+snippet+"\n🔗 "+r.URL)
	}
	return strings.Join(blocks, "\n\n")
}

func chatSearchYou(query string) (string, error) {
	payload, _ := json.Marshal(map[string]any{"query": query, "count": 3, "language": "es"})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, chatYouURL, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("X-API-Key", os.Getenv("YOU_API_KEY"))
	req.Header.Set("Content-Type", "application/json")

	resp, err := chatHTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var data struct {
		Results struct {
			Web []chatYouResult `json:"web"`
		} `json:"results"`
		Web struct {
			Results []chatYouResult `json:"results"`
		} `json:"web"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}
	results := data.Results.Web
	if len(results) == 0 {
		results = data.Web.Results
	}
	return chatFormatYouResults(results, 400), nil
}

func chatFormatParallelResults(parsed map[string]any, maxLen int) string {
	rawResults, _ := parsed["results"].([]any)
	if len(rawResults) == 0 {
		return "Sin resultados."
	}
	var lines []string
	total := 0
	for _, raw := range rawResults {
		r, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		title, _ := r["title"].(string)
		if title == "" {
			title = "Web"
		}
		if pd, ok := r["publish_date"].(string); ok && pd != "" {
			title += " (" + pd + ")"
		}
		var excerpts []string
		if arr, ok := r["excerpts"].([]any); ok {
			for _, e := range arr {
				if s, ok := e.(string); ok {
					excerpts = append(excerpts, s)
				}
			}
		}
		excerpt := chatSliceRunes(strings.Join(excerpts, " "), 400)
		u, _ := r["url"].(string)
		block := "📌 " + title + "\n" + excerpt + "\n🔗 " + u
		lines = append(lines, block)
		total += len([]rune(block))
		if total > maxLen {
			break
		}
	}
	return strings.Join(lines, "\n\n")
}

func chatParallelWebSearch(query string) (string, error) {
	c := chatParallel()
	result, err := c.callTool("web_search", map[string]any{
		"objective":      query,
		"search_queries": []string{query},
	})
	if err != nil {
		return "", err
	}
	raw := chatExtractRawTextContent(result)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err == nil {
		return chatFormatParallelResults(parsed, 9000), nil
	}
	if s := chatSliceRunes(raw, 12000); s != "" {
		return s, nil
	}
	return "Sin resultados.", nil
}

func chatExtractRawTextContent(result map[string]any) string {
	content, _ := result["content"].([]any)
	var parts []string
	for _, c := range content {
		item, ok := c.(map[string]any)
		if !ok || item["type"] != "text" {
			continue
		}
		if t, ok := item["text"].(string); ok {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, "\n")
}

func chatFetchAniList(query string) (string, error) {
	gql := `
    query ($search: String) {
      Media (search: $search, type: ANIME) {
        title { romaji english native }
        status episodes averageScore description siteUrl
      }
    }`
	payload, _ := json.Marshal(map[string]any{
		"query":     gql,
		"variables": map[string]any{"search": query},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, chatAniListURL, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := chatHTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", errors.New("Error en AniList")
	}

	var data struct {
		Data struct {
			Media any `json:"Media"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}
	raw, err := json.Marshal(data.Data.Media)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// ─── Visión y documentos ───────────────────────────────────────────────────

func chatAnalyzeImage(buffer []byte, mimetype, question string) (string, error) {
	if mimetype == "" {
		mimetype = "image/jpeg"
	}
	dataURL := "data:" + mimetype + ";base64," + base64.StdEncoding.EncodeToString(buffer)

	payload, _ := json.Marshal(map[string]any{
		"model": "deepseek-v4-flash-vision-exp",
		"messages": []map[string]any{
			{"role": "system", "content": "Eres un analista visual preciso. Describe lo esencial de la imagen en función de la consulta del usuario. Si contiene texto, transcríbelo completo."},
			{"role": "user", "content": []map[string]any{
				{"type": "text", "text": question},
				{"type": "image_url", "image_url": map[string]any{"url": dataURL}},
			}},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, chatDeepSeekChatURL(), bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("DEEPSEEK_API_KEY"))
	req.Header.Set("Content-Type", "application/json")

	resp, err := chatHTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("DeepSeek Vision HTTP %d", resp.StatusCode)
	}

	var data struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}
	if len(data.Choices) == 0 {
		return "", nil
	}
	return data.Choices[0].Message.Content, nil
}

// chatExtractDocumentText replica la selección de formatos del addon original.
// PDF usa pdftotext; DOCX/XLSX/PPTX se leen como ZIP+XML (sin mammoth/xlsx/
// officeparser) y TXT/CSV se devuelven tal cual.
func chatExtractDocumentText(buffer []byte, mimetype, filename string) string {
	name := strings.ToLower(filename)
	mime := strings.ToLower(mimetype)

	if strings.Contains(mime, "pdf") || strings.HasSuffix(name, ".pdf") {
		return chatExtractPDF(buffer)
	}
	if strings.Contains(mime, "wordprocessingml") || strings.HasSuffix(name, ".docx") {
		if txt, err := chatExtractDocx(buffer); err == nil {
			return txt
		} else {
			return "[No se pudo leer el documento (" + filename + "): " + err.Error() + "]"
		}
	}
	if strings.Contains(mime, "spreadsheetml") || strings.HasSuffix(name, ".xlsx") {
		if txt, err := chatExtractXlsx(buffer); err == nil {
			return txt
		} else {
			return "[No se pudo leer el documento (" + filename + "): " + err.Error() + "]"
		}
	}
	if strings.Contains(mime, "presentationml") || strings.HasSuffix(name, ".pptx") ||
		strings.Contains(mime, "officedocument") || strings.HasSuffix(name, ".ppt") || strings.HasSuffix(name, ".doc") {
		if txt, err := chatExtractPptx(buffer); err == nil {
			return txt
		} else {
			return "[No se pudo leer el documento (" + filename + "): " + err.Error() + "]"
		}
	}
	if strings.Contains(mime, "text") || strings.HasSuffix(name, ".txt") || strings.HasSuffix(name, ".csv") {
		return string(buffer)
	}

	return fmt.Sprintf("[Documento no soportado: %s. Formatos: PDF, DOCX, XLSX, PPTX, TXT, CSV.]", chatFirstNonEmpty(filename, mimetype))
}

func chatFirstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// chatPDFCommand ejecuta pdftotext (equivalente a pdf-parse en Node.js).
func chatPDFCommand(ctx context.Context, input string) *exec.Cmd {
	return exec.CommandContext(ctx, "pdftotext", "-q", input, "-")
}

func chatExtractPDF(buffer []byte) string {
	dir := os.TempDir()
	tmp, err := os.CreateTemp(dir, "chatdoc_*.pdf")
	if err != nil {
		return "[No se pudo leer el documento (PDF): " + err.Error() + "]"
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(buffer); err != nil {
		tmp.Close()
		return "[No se pudo leer el documento (PDF): " + err.Error() + "]"
	}
	tmp.Close()

	cmdCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := chatPDFCommand(cmdCtx, tmpPath)
	out, err := cmd.Output()
	if err != nil {
		return "[No se pudo leer el documento (PDF): " + err.Error() + "]"
	}
	return string(out)
}

func chatZipReader(buffer []byte) (*zip.Reader, error) {
	return zip.NewReader(bytes.NewReader(buffer), int64(len(buffer)))
}

func chatReadZipFile(zr *zip.Reader, name string) ([]byte, error) {
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("archivo %s no encontrado", name)
}

var chatXMLTagRe = regexp.MustCompile(`<[^>]+>`)

func chatStripXML(raw string) string {
	raw = strings.ReplaceAll(raw, "</w:p>", "\n")
	raw = strings.ReplaceAll(raw, "</a:p>", "\n")
	raw = chatXMLTagRe.ReplaceAllString(raw, "")
	return strings.TrimSpace(chatUnescapeXML(raw))
}

func chatUnescapeXML(s string) string {
	r := strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&apos;", "'")
	return r.Replace(s)
}

func chatExtractDocx(buffer []byte) (string, error) {
	zr, err := chatZipReader(buffer)
	if err != nil {
		return "", err
	}
	raw, err := chatReadZipFile(zr, "word/document.xml")
	if err != nil {
		return "", err
	}
	return chatStripXML(string(raw)), nil
}

func chatExtractXlsx(buffer []byte) (string, error) {
	zr, err := chatZipReader(buffer)
	if err != nil {
		return "", err
	}
	sharedData, _ := chatReadZipFile(zr, "xl/sharedStrings.xml")
	var shared []string
	if len(sharedData) > 0 {
		re := regexp.MustCompile(`(?s)<si>(.*?)</si>`)
		for _, m := range re.FindAllStringSubmatch(string(sharedData), -1) {
			shared = append(shared, chatStripXML(m[1]))
		}
	}

	var lines []string
	for _, f := range zr.File {
		if !strings.HasPrefix(f.Name, "xl/worksheets/sheet") || !strings.HasSuffix(f.Name, ".xml") {
			continue
		}
		data, err := chatReadZipFile(zr, f.Name)
		if err != nil {
			continue
		}
		lines = append(lines, "### Hoja: "+filepath.Base(f.Name))
		rowRe := regexp.MustCompile(`(?s)<row[^>]*>(.*?)</row>`)
		cellRe := regexp.MustCompile(`(?s)<c[^>]*>(.*?)</c>`)
		valRe := regexp.MustCompile(`(?s)<v>(.*?)</v>`)
		for _, row := range rowRe.FindAllStringSubmatch(string(data), -1) {
			var cells []string
			for _, cell := range cellRe.FindAllStringSubmatch(row[1], -1) {
				vm := valRe.FindStringSubmatch(cell[1])
				if vm == nil {
					cells = append(cells, "")
					continue
				}
				v := chatUnescapeXML(vm[1])
				if strings.Contains(cell[0], `t="s"`) {
					if idx, err := strconv.Atoi(v); err == nil && idx >= 0 && idx < len(shared) {
						v = shared[idx]
					}
				}
				cells = append(cells, v)
			}
			lines = append(lines, strings.Join(cells, ","))
		}
	}
	return strings.Join(lines, "\n\n"), nil
}

func chatExtractPptx(buffer []byte) (string, error) {
	zr, err := chatZipReader(buffer)
	if err != nil {
		return "", err
	}
	var texts []string
	textRe := regexp.MustCompile(`(?s)<a:t>(.*?)</a:t>`)
	for _, f := range zr.File {
		if !strings.HasPrefix(f.Name, "ppt/slides/slide") || !strings.HasSuffix(f.Name, ".xml") {
			continue
		}
		data, err := chatReadZipFile(zr, f.Name)
		if err != nil {
			continue
		}
		for _, m := range textRe.FindAllStringSubmatch(string(data), -1) {
			if t := strings.TrimSpace(chatUnescapeXML(m[1])); t != "" {
				texts = append(texts, t)
			}
		}
	}
	return strings.Join(texts, "\n"), nil
}

// ─── Manipulación de imágenes (alternativa a sharp) ────────────────────────

func chatNumParam(params map[string]any, key string, def float64) float64 {
	v, ok := params[key]
	if !ok {
		return def
	}
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	case string:
		if f, err := strconv.ParseFloat(n, 64); err == nil {
			return f
		}
	}
	return def
}

func chatStrParam(params map[string]any, key, def string) string {
	if v, ok := params[key].(string); ok && v != "" {
		return v
	}
	return def
}

func chatColorParam(s string, fallback color.NRGBA) color.NRGBA {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "#") {
		hex := strings.TrimPrefix(s, "#")
		if len(hex) == 8 {
			if n, err := strconv.ParseUint(hex, 16, 64); err == nil {
				return color.NRGBA{uint8(n >> 24), uint8(n >> 16), uint8(n >> 8), uint8(n)}
			}
		}
		if len(hex) == 6 {
			if n, err := strconv.ParseUint(hex, 16, 32); err == nil {
				return color.NRGBA{uint8(n >> 16), uint8(n >> 8), uint8(n), 255}
			}
		}
	}
	return fallback
}

func chatManipulateImage(buffer []byte, operacion string, params map[string]any) ([]byte, string, error) {
	src, _, err := image.Decode(bytes.NewReader(buffer))
	if err != nil {
		return nil, "", err
	}

	var out image.Image
	switch strings.ToLower(operacion) {
	case "resize":
		w := int(chatNumParam(params, "width", 0))
		h := int(chatNumParam(params, "height", 0))
		fit := strings.ToLower(chatStrParam(params, "fit", "inside"))
		b := src.Bounds()
		if w <= 0 && h <= 0 {
			w, h = b.Dx(), b.Dy()
		}
		switch fit {
		case "fill":
			out = imaging.Resize(src, w, h, imaging.Lanczos)
		case "cover":
			out = imaging.Fill(src, w, h, imaging.Center, imaging.Lanczos)
		default:
			out = imaging.Fit(src, w, h, imaging.Lanczos)
		}
	case "crop":
		left := int(math.Max(0, chatNumParam(params, "left", 0)))
		top := int(math.Max(0, chatNumParam(params, "top", 0)))
		w := int(chatNumParam(params, "width", 256))
		h := int(chatNumParam(params, "height", 256))
		out = imaging.Crop(src, image.Rect(left, top, left+w, top+h))
	case "rotate":
		angle := chatNumParam(params, "angle", 90)
		bg := chatColorParam(chatStrParam(params, "background", "#00000000"), color.NRGBA{})
		out = imaging.Rotate(src, angle, bg)
	case "grayscale", "gray":
		out = imaging.Grayscale(src)
	case "blur":
		out = imaging.Blur(src, chatNumParam(params, "sigma", 5))
	case "sharpen":
		out = imaging.Sharpen(src, 1)
	case "flip":
		out = imaging.FlipV(src)
	case "flop":
		out = imaging.FlipH(src)
	case "negate":
		out = imaging.Invert(src)
	case "normalize":
		out = imaging.AdjustContrast(src, 10)
	case "tint":
		tint := chatColorParam(chatStrParam(params, "color", "#ff0000"), color.NRGBA{255, 0, 0, 255})
		b := src.Bounds()
		layer := image.NewNRGBA(b)
		xdraw.Draw(layer, b, &image.Uniform{C: tint}, image.Point{}, xdraw.Src)
		out = imaging.Overlay(src, layer, image.Pt(0, 0), 0.45)
	case "texto":
		safeText := chatSanitizeText(chatStrParam(params, "texto", "Hola"))
		txtColor := chatColorParam(chatStrParam(params, "color", "#ffffff"), color.NRGBA{255, 255, 255, 255})
		size := chatNumParam(params, "tamano", 48)
		if size < 24 {
			size = 24
		}
		if size > 120 {
			size = 120
		}
		out, err = chatDrawText(src, safeText, txtColor, size, chatStrParam(params, "gravity", "south"))
		if err != nil {
			return nil, "", err
		}
	default:
		return nil, "", fmt.Errorf("Operación no soportada: %s. Disponibles: resize, crop, rotate, grayscale, blur, sharpen, flip, flop, negate, normalize, tint, texto", operacion)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), "image/png", nil
}

func chatSanitizeText(s string) string {
	return strings.NewReplacer("<", "", ">", "", "&", "", `"`, "", "'", "").Replace(s)
}

func chatDrawText(src image.Image, text string, col color.NRGBA, size float64, gravity string) (image.Image, error) {
	f, err := opentype.Parse(goregular.TTF)
	if err != nil {
		return nil, err
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	img := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	xdraw.Draw(img, img.Bounds(), src, b.Min, xdraw.Src)

	width := font.MeasureString(face, text)
	ascent := face.Metrics().Ascent
	lineH := int(math.Round(size * 1.6))
	totalH := int(math.Round(size * 1.6))
	x := (b.Dx() - width.Ceil()) / 2
	y := b.Dy() - totalH + int(math.Round(float64(ascent)/64))
	switch strings.ToLower(gravity) {
	case "north":
		y = int(math.Round(float64(ascent) / 64))
	case "center":
		y = b.Dy()/2 + int(math.Round(float64(ascent)/64))/2
	}
	_ = lineH

	d := &font.Drawer{Dst: img, Src: &image.Uniform{C: col}, Face: face, Dot: fixed.P(x, y)}
	d.DrawString(text)
	return img, nil
}

// ─── DSML (tool-calls emitidas como texto) ─────────────────────────────────

var (
	chatDsmlInvokeRe = regexp.MustCompile(`(?s)<[｜|]*\s*DSML[｜|]*\s*invoke\s+name=["']([^"']+)["'](.*?)</[｜|]*\s*DSML[｜|]*\s*invoke>`)
	chatDsmlParamRe  = regexp.MustCompile(`(?s)<[｜|]*\s*DSML[｜|]*\s*parameter\s+name=["']([^"']+)["'][^>]*>(.*?)</[｜|]*\s*DSML[｜|]*\s*parameter>`)
	chatDsmlTagRe    = regexp.MustCompile(`</?[｜|]*\s*DSML[｜|]*\s*[^>]*>`)
)

func chatParseDsmlToolCalls(content string) []map[string]any {
	var calls []map[string]any
	for _, m := range chatDsmlInvokeRe.FindAllStringSubmatch(content, -1) {
		args := make(map[string]string)
		for _, p := range chatDsmlParamRe.FindAllStringSubmatch(m[2], -1) {
			args[p[1]] = strings.TrimSpace(p[2])
		}
		argsJSON, _ := json.Marshal(args)
		calls = append(calls, map[string]any{
			"id":   fmt.Sprintf("dsml_%d_%d", time.Now().UnixMilli(), len(calls)),
			"type": "function",
			"function": map[string]any{
				"name":      m[1],
				"arguments": string(argsJSON),
			},
		})
	}
	return calls
}

func chatSanitizeDsmlText(text string) string {
	return strings.TrimSpace(chatDsmlTagRe.ReplaceAllString(text, ""))
}

func chatNormalizeChartArgs(a map[string]any) (string, bool) {
	cfg, ok := a["chartConfig"]
	if !ok {
		cfg = a["chart_config"]
	}
	if cfg != nil {
		switch v := cfg.(type) {
		case string:
			if strings.HasPrefix(strings.TrimSpace(v), "{") {
				var probe any
				if json.Unmarshal([]byte(v), &probe) == nil {
					return strings.TrimSpace(v), true
				}
			}
		case map[string]any:
			raw, err := json.Marshal(v)
			if err == nil {
				return string(raw), true
			}
		}
	}

	data, ok := a["datos"]
	if !ok {
		data = a["data"]
	}
	if data == nil {
		data = a["datasets"]
	}
	if data == nil {
		return "", false
	}

	dataObj := data
	if s, ok := data.(string); ok {
		var parsed any
		if json.Unmarshal([]byte(s), &parsed) != nil {
			return "", false
		}
		dataObj = parsed
	}
	if arr, ok := dataObj.([]any); ok {
		dataObj = map[string]any{"labels": []any{}, "datasets": []any{map[string]any{"label": "", "data": arr}}}
	}

	chartType := "bar"
	if t, ok := a["tipo"].(string); ok && t != "" {
		chartType = t
	} else if t, ok := a["type"].(string); ok && t != "" {
		chartType = t
	}
	chartType = strings.NewReplacer(`"`, "", `'`, "", `\`, "").Replace(chartType)
	if chartType == "" {
		chartType = "bar"
	}

	chart := map[string]any{"type": chartType, "data": dataObj}
	if title, ok := a["titulo"]; ok && title != nil {
		chart["options"] = map[string]any{"title": map[string]any{"display": true, "text": fmt.Sprint(title)}}
	} else if title, ok := a["title"]; ok && title != nil {
		chart["options"] = map[string]any{"title": map[string]any{"display": true, "text": fmt.Sprint(title)}}
	}
	raw, err := json.Marshal(chart)
	if err != nil {
		return "", false
	}
	return string(raw), true
}

func chatSendLongText(ctx *core.Context, text string) error {
	const maxLen = 4000
	s := strings.TrimSpace(text)
	if s == "" {
		return nil
	}
	r := []rune(s)
	if len(r) <= maxLen {
		return ctx.SendQuoted(core.TextMessage(s), ctx.Message)
	}
	var parts []string
	rest := r
	for len(rest) > maxLen {
		cut := -1
		for i := maxLen; i >= 0; i-- {
			if rest[i] == '\n' {
				cut = i
				break
			}
		}
		if cut < maxLen/2 {
			for i := maxLen; i >= 0; i-- {
				if rest[i] == ' ' {
					cut = i
					break
				}
			}
		}
		if cut < maxLen/2 {
			cut = maxLen
		}
		parts = append(parts, string(rest[:cut]))
		rest = []rune(strings.TrimLeftFunc(string(rest[cut:]), func(r rune) bool { return r == ' ' || r == '\n' || r == '\t' || r == '\r' }))
	}
	if len(rest) > 0 {
		parts = append(parts, string(rest))
	}
	for _, part := range parts {
		if err := ctx.SendQuoted(core.TextMessage(part), ctx.Message); err != nil {
			return err
		}
	}
	return nil
}

func chatSliceRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

// ─── Núcleo: procesamiento ─────────────────────────────────────────────────

type chatImageToSend struct {
	buffer   []byte
	url      string
	mimetype string
	caption  string
}

func chatArgsText(args []string) string {
	return strings.TrimSpace(strings.Join(args, " "))
}

func chatMessageText(m *waE2E.Message) string {
	return core.ExtractText(m)
}

func chatProcess(ctx *core.Context, args []string) error {
	if ctx == nil || ctx.Message == nil || ctx.Message.Message == nil {
		return nil
	}
	msg := ctx.Message.Message
	quoted := ctx.QuotedMessage()

	userPrompt := chatArgsText(args)
	if userPrompt == "" {
		userPrompt = chatQuotedCaption(quoted)
	}
	if userPrompt == "" {
		userPrompt = chatCurrentCaption(msg)
	}

	imageMsg := msg.GetImageMessage()
	if imageMsg == nil && quoted != nil {
		imageMsg = quoted.GetImageMessage()
	}
	docMsg := msg.GetDocumentMessage()
	docFromQuoted := false
	if docMsg == nil && quoted != nil {
		docMsg = quoted.GetDocumentMessage()
		docFromQuoted = docMsg != nil
	}
	imageFromQuoted := imageMsg != nil && msg.GetImageMessage() == nil

	if userPrompt == "" && imageMsg == nil && docMsg == nil {
		return ctx.React("❓")
	}

	if err := ctx.React("🧠"); err != nil {
		return err
	}

	requesterPhone := chatJIDBase(ctx.SenderJID().String())
	requesterName := chatResolveName(ctx, requesterPhone)

	rawHistory := chatGetRecent(ctx.ChatJID().String(), chatHistoryLimit)
	history := make([]chatHistMsg, 0, len(rawHistory))
	for _, m := range rawHistory {
		senderNorm := strings.SplitN(m.Sender, ":", 2)[0]
		if chatIsQuotedCached(m.Text, senderNorm) {
			continue
		}
		if senderNorm == requesterPhone && m.Text == userPrompt && math.Abs(float64(time.Now().UnixMilli()-m.T)) < 5000 {
			continue
		}
		history = append(history, m)
	}

	var histLines []string
	for _, m := range history {
		sender := strings.SplitN(m.Sender, ":", 2)[0]
		histLines = append(histLines, fmt.Sprintf(`<msg de="%s" num="%s" cuando="%s">%s</msg>`,
			chatEscXML(chatResolveName(ctx, sender)), chatEscXML(sender), chatTimeAgo(m.T), chatEscXML(chatTruncateUserText(m.Text))))
	}

	quotedText := chatQuotedText(quoted)
	if strings.TrimSpace(quotedText) != "" {
		quotedSender := requesterPhone
		if ci := ctx.ContextInfo(); ci != nil && ci.GetParticipant() != "" {
			quotedSender = chatJIDBase(ci.GetParticipant())
		}
		chatCacheQuoted(quotedText, quotedSender, time.Now().UnixMilli())
	}

	documentText := ""
	if docMsg != nil {
		_ = ctx.React("📄")
		data, err := chatDownloadDocument(ctx, quoted, docFromQuoted)
		if err == nil {
			documentText = chatExtractDocumentText(data, docMsg.GetMimetype(), docMsg.GetFileName())
			if len([]rune(documentText)) > 4000 {
				documentText = string([]rune(documentText)[:4000]) + "\n\n[📄 Documento truncado a 4000 caracteres para no malgastar tokens.]"
			}
		} else {
			documentText = "[No se pudo procesar el documento: " + err.Error() + "]"
		}
	}

	var imageBuffer []byte
	imageMimetype := "image/jpeg"
	imageAnalysis := ""
	if imageMsg != nil {
		_ = ctx.React("📡")
		buf, err := chatDownloadImage(ctx, quoted, imageFromQuoted)
		if err == nil && len(buf) > 0 {
			imageBuffer = buf
			if imageMsg.GetMimetype() != "" {
				imageMimetype = imageMsg.GetMimetype()
			}
			visionQuestion := fmt.Sprintf(`Consulta del usuario "%s": %s`, requesterName, chatFirstNonEmpty(userPrompt, "Describe esta imagen."))
			if quotedText != "" {
				visionQuestion += "\nContexto del citado: " + chatSliceRunes(quotedText, 500)
			}
			analysis, aerr := chatAnalyzeImage(imageBuffer, imageMimetype, visionQuestion)
			if aerr != nil {
				slog.Warn("⚠️ chat: visión falló:", "error", aerr)
				imageAnalysis = "(No se pudo analizar la imagen: " + aerr.Error() + ")"
			} else {
				imageAnalysis = analysis
			}
		} else {
			reason := "imagen vacía/expirada"
			if err != nil {
				reason = err.Error()
			}
			slog.Warn("⚠️ chat: visión falló:", "error", reason)
			imageAnalysis = "(No se pudo analizar la imagen: " + reason + ")"
		}
	}

	var contextParts []string
	if len(histLines) > 0 {
		contextParts = append(contextParts, fmt.Sprintf(`<historial_chat ultimos="%d" nota="Solo contexto. El autor relevante es quien firma <peticion_actual>.">`+"\n%s\n</historial_chat>", len(history), strings.Join(histLines, "\n")))
	}
	if strings.TrimSpace(quotedText) != "" {
		quotedAuthor := requesterPhone
		if ci := ctx.ContextInfo(); ci != nil && ci.GetParticipant() != "" {
			quotedAuthor = chatJIDBase(ci.GetParticipant())
		}
		contextParts = append(contextParts, fmt.Sprintf(`<mensaje_citado de="%s" num="%s">`+"\n%s\n</mensaje_citado>",
			chatEscXML(chatResolveName(ctx, quotedAuthor)), chatEscXML(quotedAuthor), chatEscXML(chatTruncateUserText(quotedText))))
	}
	if documentText != "" {
		contextParts = append(contextParts, fmt.Sprintf(`<documento_adjunto nombre="%s" tipo="%s">`+"\n%s\n</documento_adjunto>",
			chatEscXML(chatFirstNonEmpty(docMsg.GetFileName(), "documento")), chatEscXML(docMsg.GetMimetype()), chatEscXML(documentText)))
	}
	if imageAnalysis != "" {
		fuente := "adjunta"
		if imageFromQuoted {
			fuente = "citada"
		}
		contextParts = append(contextParts, fmt.Sprintf(`<analisis_imagen fuente="%s">`+"\n%s\n</analisis_imagen>", fuente, chatEscXML(chatSliceRunes(imageAnalysis, 4000))))
	}
	contextParts = append(contextParts, fmt.Sprintf(`<peticion_actual de="%s" num="%s">%s</peticion_actual>`,
		chatEscXML(requesterName), chatEscXML(requesterPhone),
		chatEscXML(chatTruncateUserText(chatFirstNonEmpty(userPrompt, "Analiza el contenido y cuéntame qué ves.")))))

	userContent := "<contexto>\n" + strings.Join(contextParts, "\n") + "\n</contexto>"

	systemPrompt := chatSystemPromptText()

	messages := []any{
		map[string]any{"role": "system", "content": systemPrompt},
		map[string]any{"role": "user", "content": userContent},
	}
	tools := chatTools()

	callDeepSeek := func(withTools bool) (map[string]any, error) {
		return chatCallDeepSeek(messages, tools, withTools)
	}

	finished := false
	iterations := 0
	var imagesToSend []chatImageToSend

	for !finished && iterations < chatMaxIterations {
		responseMessage, err := callDeepSeek(true)
		if err != nil {
			return err
		}

		if _, hasTools := responseMessage["tool_calls"]; !hasTools {
			if content, ok := responseMessage["content"].(string); ok && strings.Contains(content, "DSML") {
				rescued := chatParseDsmlToolCalls(content)
				if len(rescued) > 0 {
					slog.Warn(fmt.Sprintf("⚠️ chat: %d tool-call(s) rescatada(s) de DSML", len(rescued)))
					responseMessage["tool_calls"] = rescued
					sanitized := chatSanitizeDsmlText(chatDsmlInvokeRe.ReplaceAllString(content, ""))
					responseMessage["content"] = sanitized
				}
			}
		}

		messages = append(messages, responseMessage)

		rawToolCalls, _ := responseMessage["tool_calls"].([]any)
		if len(rawToolCalls) > 0 {
			iterations++
			_ = ctx.React("⚙️")

			for _, rawCall := range rawToolCalls {
				toolCall, _ := rawCall.(map[string]any)
				fn, _ := toolCall["function"].(map[string]any)
				fnName, _ := fn["name"].(string)
				argsStr, _ := fn["arguments"].(string)
				toolArgs := map[string]any{}
				if strings.TrimSpace(argsStr) != "" {
					_ = json.Unmarshal([]byte(argsStr), &toolArgs)
				}
				callID, _ := toolCall["id"].(string)

				toolResult, toolErr := chatRunTool(ctx, fnName, toolArgs, &imagesToSend, imageBuffer)
				if toolErr != nil {
					slog.Warn("⚠️ chat: error en herramienta "+fnName+":", "error", toolErr)
					toolResult = "Error al ejecutar la herramienta: " + toolErr.Error()
				}

				messages = append(messages, map[string]any{
					"role":         "tool",
					"tool_call_id": callID,
					"content":      toolResult,
				})
			}
		} else {
			finished = true
		}
	}

	finalMessage := ""
	if len(messages) > 0 {
		if last, ok := messages[len(messages)-1].(map[string]any); ok {
			if s, ok := last["content"].(string); ok {
				finalMessage = s
			}
		}
	}
	if !finished || strings.TrimSpace(finalMessage) == "" {
		messages = append(messages, map[string]any{
			"role":    "user",
			"content": "[SISTEMA] Límite de herramientas alcanzado. Entrega AHORA tu respuesta final con la información reunida, sin llamar más herramientas.",
		})
		forced, err := callDeepSeek(false)
		if err != nil {
			return err
		}
		finalMessage, _ = forced["content"].(string)
		if finalMessage == "" {
			finalMessage = "No pude completar el análisis, inténtalo de nuevo."
		}
	}

	for _, img := range imagesToSend {
		data := img.buffer
		if len(data) == 0 && img.url != "" {
			fetched, err := chatFetchURLBytes(img.url)
			if err != nil {
				slog.Warn("⚠️ chat: no se pudo descargar la imagen:", "error", err)
				continue
			}
			data = fetched
		}
		if len(data) > 0 {
			if err := ctx.SendImage(data, img.caption); err != nil {
				return err
			}
		}
	}

	if strings.TrimSpace(finalMessage) != "" {
		if err := chatSendLongText(ctx, chatSanitizeDsmlText(finalMessage)); err != nil {
			return err
		}
	}

	return ctx.React("✅")
}

func chatDownloadImage(ctx *core.Context, quoted *waE2E.Message, fromQuoted bool) ([]byte, error) {
	if fromQuoted && quoted != nil {
		return ctx.Download(quoted)
	}
	return ctx.DownloadMedia()
}

func chatDownloadDocument(ctx *core.Context, quoted *waE2E.Message, fromQuoted bool) ([]byte, error) {
	if fromQuoted && quoted != nil {
		return ctx.Download(quoted)
	}
	return ctx.DownloadMedia()
}

func chatCurrentCaption(msg *waE2E.Message) string {
	if msg == nil {
		return ""
	}
	if q := msg.GetImageMessage(); q != nil && q.GetCaption() != "" {
		return q.GetCaption()
	}
	if q := msg.GetDocumentMessage(); q != nil && q.GetCaption() != "" {
		return q.GetCaption()
	}
	if q := msg.GetVideoMessage(); q != nil && q.GetCaption() != "" {
		return q.GetCaption()
	}
	return ""
}

func chatQuotedCaption(quoted *waE2E.Message) string {
	if quoted == nil {
		return ""
	}
	if q := quoted.GetImageMessage(); q != nil && q.GetCaption() != "" {
		return q.GetCaption()
	}
	if q := quoted.GetDocumentMessage(); q != nil && q.GetCaption() != "" {
		return q.GetCaption()
	}
	if q := quoted.GetVideoMessage(); q != nil && q.GetCaption() != "" {
		return q.GetCaption()
	}
	return ""
}

func chatQuotedText(quoted *waE2E.Message) string {
	if quoted == nil {
		return ""
	}
	return core.ExtractText(quoted)
}

func chatFetchURLBytes(u string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := chatHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func chatRunTool(ctx *core.Context, fn string, a map[string]any, imagesToSend *[]chatImageToSend, imageBuffer []byte) (string, error) {
	switch fn {
	case "buscar_en_internet":
		return chatSearchYou(chatStrArg(a, "query"))
	case "buscar_web_parallel":
		return chatParallelWebSearch(chatStrArg(a, "query"))
	case "buscar_anime":
		return chatFetchAniList(chatStrArg(a, "query"))
	case "navegar_web":
		return chatObscuraNavigate(chatStrArg(a, "url"), chatFirstNonEmpty(chatStrArg(a, "extract"), "markdown"))
	case "captura_web":
		u := chatStrArg(a, "url")
		raw, mime, err := chatObscuraScreenshot(u)
		if err != nil {
			return "", err
		}
		*imagesToSend = append(*imagesToSend, chatImageToSend{buffer: raw, mimetype: mime, caption: "📸 " + u})
		return "Captura de " + u + " enviada al chat.", nil
	case "consultar_clima":
		city := chatStrArg(a, "ciudad")
		if city == "" {
			city = chatValencia
		}
		days := 3
		if v, ok := a["dias"]; ok {
			if f, ok := chatToFloat(v); ok {
				days = int(f)
			}
		}
		return chatConsultarClima(city, days)
	case "generar_grafico":
		var configs []string
		if arr, ok := a["charts"].([]any); ok {
			for i, c := range arr {
				if i >= 3 {
					break
				}
				if s, ok := c.(string); ok {
					if norm, ok := chatNormalizeChartArgs(map[string]any{"chartConfig": s}); ok {
						configs = append(configs, norm)
					}
				}
			}
		} else if norm, ok := chatNormalizeChartArgs(a); ok {
			configs = append(configs, norm)
		}
		if len(configs) == 0 {
			return "Argumentos inválidos. Usa chartConfig (string JSON de Chart.js) o charts (array de strings).", nil
		}
		w := chatNumParam(a, "width", 800)
		h := chatNumParam(a, "height", 400)
		bkg := chatStrParam(a, "backgroundColor", "white")
		for _, cfg := range configs {
			u := fmt.Sprintf("%s?c=%s&w=%v&h=%v&bkg=%s",
				chatQuickChartURL, url.QueryEscape(cfg), chatJSNumber(w), chatJSNumber(h), url.QueryEscape(bkg))
			*imagesToSend = append(*imagesToSend, chatImageToSend{url: u, caption: "📊 Gráfico generado"})
		}
		return fmt.Sprintf("Se generaron %d gráfico(s). Explica brevemente los datos; no pegues la URL.", len(configs)), nil
	case "manipular_imagen":
		if len(imageBuffer) == 0 {
			return "No hay imagen adjunta o citada para manipular.", nil
		}
		params, _ := a["parametros"].(map[string]any)
		if params == nil {
			params = map[string]any{}
		}
		edited, mime, err := chatManipulateImage(imageBuffer, chatStrArg(a, "operacion"), params)
		if err != nil {
			return "", err
		}
		*imagesToSend = append(*imagesToSend, chatImageToSend{buffer: edited, mimetype: mime, caption: "🖼️ Imagen editada (" + chatStrArg(a, "operacion") + ")"})
		return `Imagen editada con "` + chatStrArg(a, "operacion") + `" y enviada al chat.`, nil
	}
	return "Herramienta desconocida: " + fn, nil
}

func chatStrArg(a map[string]any, key string) string {
	if v, ok := a[key].(string); ok {
		return v
	}
	return ""
}

func chatToFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil
	}
	return 0, false
}

func chatCallDeepSeek(messages []any, tools []map[string]any, withTools bool) (map[string]any, error) {
	body := map[string]any{"model": "deepseek-v4-flash", "messages": messages}
	if withTools {
		body["tools"] = tools
		body["tool_choice"] = "auto"
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, chatDeepSeekChatURL(), bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("DEEPSEEK_API_KEY"))
	req.Header.Set("Content-Type", "application/json")

	resp, err := chatHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("DeepSeek error: %d", resp.StatusCode)
	}

	var data struct {
		Choices []struct {
			Message map[string]any `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	if len(data.Choices) == 0 {
		return nil, errors.New("DeepSeek sin choices")
	}
	return data.Choices[0].Message, nil
}

func chatSystemPromptText() string {
	return fmt.Sprintf(`Eres el núcleo de razonamiento de un asistente avanzado de WhatsApp. Eres directo, lógico y amigable. No usas lenguaje robótico.

CONTEXTO ESTRUCTURADO:
Recibirás un bloque <contexto> con XML. Dentro puede haber:
- <historial_chat>: últimos mensajes reales del chat (solo contexto). Los mensajes están truncados a %d caracteres por seguridad y ahorro de tokens.
- <mensaje_citado>: mensaje específico citado al invocarte.
- <documento_adjunto>: texto extraído de un PDF/Word/Excel/PPT.
- <analisis_imagen>: descripción visual de una imagen.
- <peticion_actual>: LA CONSULTA A RESPONDER. Tiene prioridad absoluta.
El único autor relevante es quien firma <peticion_actual>.

HERRAMIENTAS:
- buscar_en_internet (You.com)
- buscar_web_parallel (Parallel Search MCP)
- buscar_anime (AniList)
- navegar_web (Obscura MCP): navega una URL y devuelve markdown/texto legible (JS incluido).
- captura_web (Obscura MCP): navega una URL y envía una captura de pantalla al chat.
- consultar_clima: clima actual y pronóstico vía Open-Meteo. Por defecto Valencia, Carabobo, Venezuela.
- generar_grafico (QuickChart): gráficos Chart.js. 'chartConfig' (string JSON) o 'charts' (array de hasta 3 strings JSON).
- manipular_imagen (sharp): edita la imagen adjunta/citada. Operaciones: resize, crop, rotate, grayscale, blur, sharpen, flip, flop, negate, normalize, tint, texto.

REGLAS CRÍTICAS:
- Usa SOLO function calling nativo. NUNCA escribas llamadas en XML/DSML/texto.
- Para generar_grafico usa 'chartConfig' (JSON string de Chart.js completo) o 'charts' (array). No pegues URLs en la respuesta final.
- Si un mensaje fue truncado y la petición requiere ese contenido, di: "Por seguridad, no se pudo leer el resto del mensaje".
- No inventes datos. Si no sabes, busca o di que no lo sabes.
- Máximo %d rondas de herramientas; si no necesitas datos actuales, responde directo sin herramientas.

FORMATO WHATSAPP:
- *negritas* solo para datos clave.
- Listas con guiones.
- Sé breve; ve directo al grano.`, chatMaxUserChars, chatMaxIterations)
}

func chatTools() []map[string]any {
	return []map[string]any{
		{"type": "function", "function": map[string]any{
			"name":        "buscar_en_internet",
			"description": "Busca en internet noticias, definiciones o datos en tiempo real (You.com).",
			"parameters":  map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}}, "required": []string{"query"}},
		}},
		{"type": "function", "function": map[string]any{
			"name":        "buscar_web_parallel",
			"description": "Búsqueda web alternativa vía Parallel Search MCP. Segunda opción ante fallos de You.com.",
			"parameters":  map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}}, "required": []string{"query"}},
		}},
		{"type": "function", "function": map[string]any{
			"name":        "buscar_anime",
			"description": "Datos exactos, sinopsis y puntuación de un anime.",
			"parameters":  map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}}, "required": []string{"query"}},
		}},
		{"type": "function", "function": map[string]any{
			"name":        "navegar_web",
			"description": "Navega una URL con el navegador Obscura y devuelve el contenido legible (markdown/texto). Útil para páginas que requieren JS.",
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url":     map[string]any{"type": "string"},
					"extract": map[string]any{"type": "string", "enum": []string{"markdown", "snapshot"}},
				},
				"required": []string{"url"},
			},
		}},
		{"type": "function", "function": map[string]any{
			"name":        "captura_web",
			"description": "Navega una URL con Obscura y envía una captura de pantalla al chat.",
			"parameters":  map[string]any{"type": "object", "properties": map[string]any{"url": map[string]any{"type": "string"}}, "required": []string{"url"}},
		}},
		{"type": "function", "function": map[string]any{
			"name":        "consultar_clima",
			"description": "Clima actual y pronóstico (Open-Meteo). Por defecto Valencia, Carabobo, Venezuela.",
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"ciudad": map[string]any{"type": "string", "description": "Ciudad. Por defecto Valencia, Carabobo, Venezuela."},
					"dias":   map[string]any{"type": "number", "description": "Días de pronóstico (1-7)"},
				},
			},
		}},
		{"type": "function", "function": map[string]any{
			"name":        "generar_grafico",
			"description": "Genera uno o varios gráficos Chart.js y los envía como imágenes.",
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"chartConfig":     map[string]any{"type": "string", "description": "JSON string de Chart.js para un gráfico."},
					"charts":          map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Array de configs JSON de Chart.js (máx 3)."},
					"width":           map[string]any{"type": "number"},
					"height":          map[string]any{"type": "number"},
					"backgroundColor": map[string]any{"type": "string"},
				},
			},
		}},
		{"type": "function", "function": map[string]any{
			"name":        "manipular_imagen",
			"description": "Edita la imagen adjunta o citada (sharp). Operaciones: resize, crop, rotate, grayscale, blur, sharpen, flip, flop, negate, normalize, tint, texto.",
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"operacion":  map[string]any{"type": "string"},
					"parametros": map[string]any{"type": "object", "description": "Parámetros según operación: width, height, angle, sigma, color, texto, tamano, gravity, left, top, fit, background."},
				},
				"required": []string{"operacion"},
			},
		}},
	}
}

// ─── Handler con rate-limit ────────────────────────────────────────────────

// Handle replica el handler del addon original.
func (Chat) Handle(ctx *core.Context) error {
	if ctx == nil || ctx.Message == nil || ctx.Message.Message == nil {
		return nil
	}

	msg := ctx.Message.Message
	quoted := ctx.QuotedMessage()

	userPrompt := chatArgsText(ctx.Args)
	if userPrompt == "" {
		userPrompt = chatCurrentCaption(msg)
	}
	hasMedia := msg.GetImageMessage() != nil || msg.GetDocumentMessage() != nil ||
		(quoted != nil && (quoted.GetImageMessage() != nil || quoted.GetDocumentMessage() != nil))

	if userPrompt == "" && !hasMedia {
		return ctx.React("❓")
	}

	if chatIsHost(ctx) {
		return chatProcess(ctx, ctx.Args)
	}

	requesterPhone := chatJIDBase(ctx.SenderJID().String())

	chatRateMu.Lock()
	state := chatGetRateState(requesterPhone)
	chatCleanTimestamps(state)

	msgID := ""
	if ctx.Message != nil {
		msgID = string(ctx.Message.Info.ID)
	}
	for _, q := range state.queue {
		if q.ctx != nil && q.ctx.Message != nil && string(q.ctx.Message.Info.ID) == msgID {
			chatRateMu.Unlock()
			return ctx.React("⏳")
		}
	}

	if len(state.timestamps) < chatRateMax {
		state.timestamps = append(state.timestamps, time.Now().UnixMilli())
		chatRateMu.Unlock()
		return chatProcess(ctx, ctx.Args)
	}

	if len(state.queue) >= chatQueueMax {
		chatRateMu.Unlock()
		_ = ctx.React("⚠️")
		return ctx.SendQuoted(core.TextMessage("⚠️ Demasiadas peticiones en cola. Espera unos segundos e intenta de nuevo."), ctx.Message)
	}

	state.queue = append(state.queue, chatQueueItem{ctx: ctx, args: ctx.Args})
	chatRateMu.Unlock()

	if err := ctx.React("⏳"); err != nil {
		return err
	}
	chatScheduleQueue(requesterPhone)
	return nil
}

var _ = chatPDFCommand
