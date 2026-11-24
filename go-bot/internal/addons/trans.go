package addons

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"

	"whatsbot-go/internal/core"
)

// Variables inyectables para pruebas sin red.
var (
	transHTTPClient  = &http.Client{Timeout: 60 * time.Second}
	transGroqURL     = "https://api.groq.com/openai/v1/audio/transcriptions"
	transDeepSeekURL = "https://api.deepseek.com/chat/completions"

	transDownloadMedia = func(ctx *core.Context, msg *waE2E.Message) ([]byte, error) {
		return ctx.Download(msg)
	}
)

const transDeepSeekSystemPrompt = "Eres un editor de texto experto. Tu única tarea es tomar la transcripción de voz proporcionada y limpiarla: elimina muletillas, tartamudeos y repeticiones innecesarias, y corrige errores gramaticales. Es de suma importancia que mantengas estrictamente el mismo idioma en el que está hablada la transcripción original. No añadas introducciones, explicaciones, ni comentarios extra. Solo devuelve el texto limpio."

// Trans porta addons/trans.js: .tr y .tri.
type Trans struct{}

// Commands cumple core.Addon.
func (Trans) Commands() []string { return []string{"tr", "tri"} }

// transTranscribir sube el audio a Groq y devuelve la transcripción.
func transTranscribir(data []byte) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("file", "audio.ogg")
	if err != nil {
		return "", err
	}
	if _, err := file.Write(data); err != nil {
		return "", err
	}
	if err := writer.WriteField("model", "whisper-large-v3"); err != nil {
		return "", err
	}
	if err := writer.WriteField("response_format", "json"); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequest(http.MethodPost, transGroqURL, &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("GROQ_API_KEY"))
	req.Header.Set("Content-Type", writer.FormDataContentType())

	res, err := transHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("Error en Groq: %s", http.StatusText(res.StatusCode))
	}

	var parsed struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(res.Body).Decode(&parsed); err != nil {
		return "", err
	}
	if parsed.Text == "" {
		return "", fmt.Errorf("Groq no devolvió texto.")
	}
	return parsed.Text, nil
}

// transLimpiar pide a DeepSeek la limpieza del texto (lógica .tri).
func transLimpiar(texto string) (string, error) {
	payload := map[string]any{
		"model": "deepseek-v4-flash",
		"messages": []map[string]string{
			{"role": "system", "content": transDeepSeekSystemPrompt},
			{"role": "user", "content": texto},
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest(http.MethodPost, transDeepSeekURL, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("DEEPSEEK_API_KEY"))
	req.Header.Set("Content-Type", "application/json")

	res, err := transHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("Fallo en la respuesta de DeepSeek")
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(res.Body).Decode(&parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("DeepSeek no devolvió opciones")
	}
	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
}

// transProcesar ejecuta el flujo una vez detectado el audio.
func transProcesar(ctx *core.Context, target *waE2E.Message, cmd string) error {
	if err := ctx.React("⏳"); err != nil {
		return err
	}

	buffer, err := transDownloadMedia(ctx, target)
	if err != nil {
		return err
	}

	transcription, err := transTranscribir(buffer)
	if err != nil {
		return err
	}

	if cmd == "tr" {
		if err := ctx.SendQuoted(core.TextMessage("🎙️ \n"+transcription), ctx.Message); err != nil {
			return err
		}
		return ctx.React("✅")
	}

	if cmd == "tri" {
		cleanText, dsErr := transLimpiar(transcription)
		if dsErr != nil {
			fmt.Fprintf(os.Stderr, "Error en DeepSeek: %v\n", dsErr)
			return ctx.SendQuoted(core.TextMessage("⚠️ (Fallback a transcripción cruda)\n\n"+transcription), ctx.Message)
		}
		if err := ctx.SendQuoted(core.TextMessage("✨ \n"+cleanText), ctx.Message); err != nil {
			return err
		}
		return ctx.React("✅")
	}

	return nil
}

// Handle replica el addon original, reaccionando con ❌ ante cualquier fallo.
func (t Trans) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}

	var audio *waE2E.AudioMessage
	var target *waE2E.Message
	if ctx.Message != nil && ctx.Message.Message != nil {
		audio = ctx.Message.Message.GetAudioMessage()
		if audio != nil {
			target = ctx.Message.Message
		}
	}
	if audio == nil {
		if quoted := ctx.QuotedMessage(); quoted != nil {
			if qa := quoted.GetAudioMessage(); qa != nil {
				audio = qa
				target = quoted
			}
		}
	}

	if audio == nil {
		return ctx.React("❓")
	}

	if err := transProcesar(ctx, target, ctx.Command); err != nil {
		fmt.Fprintf(os.Stderr, "Error en addon de transcripción: %v\n", err)
		return ctx.React("❌")
	}
	return nil
}

func init() {
	core.Register(Trans{})
}
