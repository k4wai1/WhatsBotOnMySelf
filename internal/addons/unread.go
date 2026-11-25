// addons/unread.js → unread.go
// Historial de chats en assets/chatHistory.json y resumen con DeepSeek.
package addons

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"

	"whatsbot-go/internal/core"
	"whatsbot-go/internal/store"
)

const (
	unreadMaxHistory  = 5000
	unreadSaveEvery   = 200
	unreadSystemPrmpt = `Eres un asistente experto en resumir conversaciones caóticas de grupos de WhatsApp.
Se te entregará un registro de chat. Cada línea empieza con "[Hace X msgs]" indicando la antigüedad del mensaje, seguido del remitente y el contenido. Algunos mensajes indican "[Audio]", dales la misma importancia.

Tu tarea:
1. Analizar el texto e identificar los temas principales y puntos más relevantes discutidos.
2. Generar un resumen estructurado estrictamente como una lista de viñetas (bullet points).
3. Al final de cada viñeta, DEBES incluir una estimación de hace cuántos mensajes ocurrió esto (ej: "(Aprox. hace 450 msgs)").
4. Ignora comandos de bots, spam corto o saludos irrelevantes. Ve al grano.
5. Mantén un tono neutral, claro y fácil de escanear. No inventes información.`
)

type unreadEntry struct {
	Text   string `json:"text"`
	Sender string `json:"sender"`
	T      int64  `json:"t"`
}

var (
	unreadMu      sync.Mutex
	unreadOnce    sync.Once
	unreadHistory = map[string][]unreadEntry{}
	unreadCounter int
	unreadNow     = time.Now

	unreadSummarizeFn = unreadDeepSeekSummary
	unreadTranscribe  = unreadTranscribeAudio
)

func unreadHistoryPath() string { return core.DataPath("assets", "chatHistory.json") }

func unreadLoad() {
	raw, err := os.ReadFile(unreadHistoryPath())
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Error("❌ [unread] Error cargando historial", "error", err)
		}
		return
	}
	loaded := map[string][]unreadEntry{}
	if err := json.Unmarshal(raw, &loaded); err != nil {
		slog.Error("❌ [unread] Error cargando historial", "error", err)
		return
	}
	unreadMu.Lock()
	unreadHistory = loaded
	unreadMu.Unlock()
	slog.Info("✅ [unread] Historial cargado desde disco.")
}

func unreadEnsureLoaded() {
	unreadOnce.Do(unreadLoad)
}

func unreadDefaultTyping() map[string][]unreadEntry {
	out := make(map[string][]unreadEntry, len(unreadHistory))
	for jid, entries := range unreadHistory {
		out[jid] = append([]unreadEntry(nil), entries...)
	}
	return out
}

func unreadSave() {
	unreadMu.Lock()
	snapshot := unreadDefaultTyping()
	unreadMu.Unlock()

	if err := core.WriteJSON(unreadHistoryPath(), snapshot); err != nil {
		slog.Error("❌ [unread] Error guardando historial", "error", err)
	}
}

func unreadGetRecent(jid string, n int) []unreadEntry {
	unreadEnsureLoaded()
	unreadMu.Lock()
	defer unreadMu.Unlock()
	h := unreadHistory[jid]
	if len(h) == 0 {
		return nil
	}
	if n > len(h) {
		n = len(h)
	}
	out := make([]unreadEntry, n)
	copy(out, h[len(h)-n:])
	return out
}

func unreadAppend(jid string, entry unreadEntry) {
	unreadMu.Lock()
	h := unreadHistory[jid]
	h = append(h, entry)
	if len(h) > unreadMaxHistory {
		h = h[len(h)-unreadMaxHistory:]
	}
	unreadHistory[jid] = h
	unreadCounter++
	save := unreadCounter >= unreadSaveEvery
	if save {
		unreadCounter = 0
	}
	unreadMu.Unlock()

	if save {
		unreadSave()
	}
}

// unreadInit replica init(sock): carga historial y escucha mensajes entrantes.
func unreadInit(client *whatsmeow.Client, _ *store.Store) error {
	unreadEnsureLoaded()
	if client == nil {
		return nil
	}
	client.AddEventHandler(func(evt any) {
		if m, ok := evt.(*events.Message); ok {
			unreadHandleIncoming(client, m)
		}
	})
	return nil
}

func unreadHandleIncoming(client *whatsmeow.Client, msg *events.Message) {
	if msg == nil || msg.Message == nil || msg.Info.IsFromMe {
		return
	}
	jid := msg.Info.Chat.String()
	senderJID := msg.Info.Sender.String()
	if senderJID == "" {
		senderJID = jid
	}
	sender := strings.SplitN(senderJID, "@", 2)[0]

	text := msg.Message.GetConversation()
	if text == "" {
		text = msg.Message.GetExtendedTextMessage().GetText()
	}
	if text == "" {
		text = msg.Message.GetImageMessage().GetCaption()
	}
	if text == "" {
		text = msg.Message.GetVideoMessage().GetCaption()
	}

	audio := msg.Message.GetAudioMessage()
	if audio != nil && os.Getenv("GROQ_API_KEY") != "" {
		transcribed, err := unreadTranscribe(client, audio)
		if err != nil {
			slog.Error("❌ [unread] Error transcribiendo audio", "error", err)
			text = "[Audio ininteligible]"
		} else {
			text = "[Audio] " + transcribed
		}
	}

	if text == "" {
		return
	}

	unreadEnsureLoaded()
	unreadAppend(jid, unreadEntry{Text: text, Sender: sender, T: unreadNow().UnixMilli()})
}

// ─── Comando .unread ────────────────────────────────────────────────────────

func init() {
	core.Register(Unread{})
	core.RegisterBackground("unread", unreadInit)
}

// Unread porta addons/unread.js.
type Unread struct{}

// Commands cumple core.Addon.
func (Unread) Commands() []string { return []string{"unread"} }

// Handle resume el historial del chat actual.
func (Unread) Handle(ctx *core.Context) error {
	if ctx == nil || ctx.Message == nil {
		return nil
	}
	jid := ctx.ChatJID().String()

	requested := 0
	if len(ctx.Args) > 0 {
		requested = unreadParseIntPrefix(ctx.Args[0])
	}
	if requested == 0 {
		requested = 500
	}
	limit := requested
	if limit > unreadMaxHistory {
		limit = unreadMaxHistory
	}
	if limit < 10 {
		return ctx.React("❓")
	}

	unreadEnsureLoaded()
	unreadMu.Lock()
	history := append([]unreadEntry(nil), unreadHistory[jid]...)
	unreadMu.Unlock()

	if len(history) == 0 {
		return ctx.SendMessage(core.TextMessage("📭 No hay historial para este chat aún."))
	}

	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		return ctx.React("❌")
	}

	_ = ctx.React("⏳")

	if limit > len(history) {
		limit = len(history)
	}
	slice := history[len(history)-limit:]
	lines := make([]string, 0, len(slice))
	for i, m := range slice {
		ago := len(slice) - i
		lines = append(lines, "[Hace "+strconv.Itoa(ago)+" msgs] @"+m.Sender+": "+m.Text)
	}
	formatted := strings.Join(lines, "\n")

	summary, err := unreadSummarizeFn(apiKey, formatted)
	if err != nil {
		base := "❌ Error al generar el resumen."
		if errors.Is(err, context.DeadlineExceeded) {
			base += " La API tardó demasiado."
		} else {
			base += " " + err.Error()
		}
		return ctx.SendMessage(core.TextMessage(base))
	}

	return ctx.SendMessage(core.TextMessage("🧠 *Resumen de los últimos " + strconv.Itoa(len(slice)) + " mensajes:*\n\n" + summary))
}

func unreadParseIntPrefix(s string) int {
	s = strings.TrimSpace(s)
	i := 0
	neg := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	start := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == start {
		return 0
	}
	n, err := strconv.Atoi(s[start:i])
	if err != nil {
		return 0
	}
	if neg {
		return -n
	}
	return n
}

// ─── DeepSeek ───────────────────────────────────────────────────────────────

type unreadChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type unreadChatRequest struct {
	Model       string              `json:"model"`
	Messages    []unreadChatMessage `json:"messages"`
	Temperature float64             `json:"temperature"`
}

type unreadChatResponse struct {
	Choices []struct {
		Message unreadChatMessage `json:"message"`
	} `json:"choices"`
}

func unreadDeepSeekSummary(apiKey, formatted string) (string, error) {
	body := unreadChatRequest{
		Model: "deepseek-chat",
		Messages: []unreadChatMessage{
			{Role: "system", Content: unreadSystemPrmpt},
			{Role: "user", Content: formatted},
		},
		Temperature: 0.3,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.deepseek.com/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("DeepSeek HTTP %d", res.StatusCode)
	}
	var parsed unreadChatResponse
	if err := json.NewDecoder(res.Body).Decode(&parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", errors.New("respuesta vacía de DeepSeek")
	}
	return parsed.Choices[0].Message.Content, nil
}

// ─── Groq (transcripción de audios) ─────────────────────────────────────────

func unreadTranscribeAudio(client *whatsmeow.Client, audio *waE2E.AudioMessage) (string, error) {
	if client == nil {
		return "", errors.New("cliente no disponible")
	}
	data, err := client.Download(context.Background(), audio)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", "audio.ogg")
	if err != nil {
		return "", err
	}
	if _, err := part.Write(data); err != nil {
		return "", err
	}
	_ = writer.WriteField("model", "whisper-large-v3")
	_ = writer.WriteField("response_format", "json")
	_ = writer.WriteField("language", "es")
	if err := writer.Close(); err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.groq.com/openai/v1/audio/transcriptions", &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("GROQ_API_KEY"))
	req.Header.Set("Content-Type", writer.FormDataContentType())

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return "", fmt.Errorf("Groq HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	var parsed struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(res.Body).Decode(&parsed); err != nil {
		return "", err
	}
	return parsed.Text, nil
}
