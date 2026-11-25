package addons

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"

	"whatsbot-go/internal/core"
)

// Resume porta addons/resume.js (comando .resu): transcribe audio/vídeo con
// Groq Whisper y resume con DeepSeek.
type Resume struct{}

// Commands cumple core.Addon.
func (Resume) Commands() []string { return []string{"resu"} }

var (
	resumeHTTP        = &http.Client{Timeout: 120 * time.Second}
	resumeGroqURL     = "https://api.groq.com/openai/v1/audio/transcriptions"
	resumeDeepSeekURL = "https://api.deepseek.com/chat/completions"
	resumeRunCmd      = resumeRunCommand
)

// resumeDownload es inyectable para pruebas sin conexión de WhatsApp.
var resumeDownload = func(ctx *core.Context, quoted *waE2E.Message) ([]byte, error) {
	if quoted != nil {
		return ctx.Download(quoted)
	}
	return ctx.DownloadMedia()
}

// resumeExtractAudio es inyectable para pruebas sin ffmpeg.
var resumeExtractAudio = resumeExtractAudioFFmpeg

const resumeSystemPrompt = "Eres un asistente experto en síntesis. Tu única tarea es leer la transcripción proporcionada y resumir su mensaje principal en exactamente UNA sola oración. Es estrictamente necesario que el resumen generado esté en el mismo idioma en el que está escrita la transcripción original. No añadas introducciones, viñetas ni comentarios extra."

// Handle replica el flujo completo del addon original.
func (Resume) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}

	var msg *waE2E.Message
	if ctx.Message != nil {
		msg = ctx.Message.Message
	}
	quoted := ctx.QuotedMessage()

	isAudio := msg != nil && msg.GetAudioMessage() != nil
	isQuotedAudio := quoted != nil && quoted.GetAudioMessage() != nil
	isVideo := msg != nil && msg.GetVideoMessage() != nil
	isQuotedVideo := quoted != nil && quoted.GetVideoMessage() != nil

	if !isAudio && !isQuotedAudio && !isVideo && !isQuotedVideo {
		return ctx.React("❓")
	}

	if err := ctx.React("⏳"); err != nil {
		slog.Error("Error en addon .resu:", "error", err)
		_ = ctx.React("❌")
		return nil
	}

	userPrompt := strings.TrimSpace(strings.Join(ctx.Args, " "))

	var downloadQuoted *waE2E.Message
	if isQuotedAudio || isQuotedVideo {
		downloadQuoted = quoted
	}

	buffer, err := resumeDownload(ctx, downloadQuoted)
	if err != nil {
		slog.Error("Error en addon .resu:", "error", err)
		_ = ctx.React("❌")
		return nil
	}

	var audio []byte
	filename := "audio.ogg"
	if isAudio || isQuotedAudio {
		audio = buffer
	} else {
		audio, err = resumeExtractAudio(buffer)
		if err != nil {
			slog.Error("Error en addon .resu:", "error", err)
			_ = ctx.React("❌")
			return nil
		}
		filename = "audio.mp3"
	}

	if len(audio) == 0 {
		slog.Error("Error en addon .resu:", "error", errors.New("El video no contiene pista de audio"))
		_ = ctx.React("❌")
		return nil
	}

	transcription, err := resumeTranscribe(audio, filename)
	if err != nil {
		slog.Error("Error en addon .resu:", "error", err)
		_ = ctx.React("❌")
		return nil
	}
	if transcription == "" {
		slog.Error("Error en addon .resu:", "error", errors.New("Groq no devolvió texto."))
		_ = ctx.React("❌")
		return nil
	}

	summary, err := resumeSummarize(transcription, userPrompt)
	if err != nil {
		slog.Error("Error en addon .resu:", "error", err)
		_ = ctx.React("❌")
		return nil
	}

	if err := ctx.SendQuoted(core.TextMessage("📝 \n"+summary), ctx.Message); err != nil {
		slog.Error("Error en addon .resu:", "error", err)
		_ = ctx.React("❌")
		return nil
	}
	return ctx.React("✅")
}

func resumeTranscribe(audio []byte, filename string) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(audio); err != nil {
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

	req, err := http.NewRequest(http.MethodPost, resumeGroqURL, &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("GROQ_API_KEY"))
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := resumeHTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("Error en Groq: %s", http.StatusText(resp.StatusCode))
	}

	var data struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}
	return data.Text, nil
}

func resumeSummarize(transcription, userPrompt string) (string, error) {
	instruction := resumeSystemPrompt
	if userPrompt != "" {
		instruction += ` Adicionalmente, el usuario ha solicitado que apliques esta condición/enfoque a tu resumen: "` + userPrompt + `".`
	}

	payload := map[string]any{
		"model": "deepseek-v4-flash",
		"messages": []map[string]string{
			{"role": "system", "content": instruction},
			{"role": "user", "content": transcription},
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest(http.MethodPost, resumeDeepSeekURL, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("DEEPSEEK_API_KEY"))
	req.Header.Set("Content-Type", "application/json")

	resp, err := resumeHTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", errors.New("Fallo en la respuesta de DeepSeek")
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
		return "", errors.New("Fallo en la respuesta de DeepSeek")
	}
	return strings.TrimSpace(data.Choices[0].Message.Content), nil
}

// resumeExtractAudioFFmpeg guarda el vídeo, valida con ffprobe que tenga pista
// de audio y extrae MP3 con ffmpeg.
func resumeExtractAudioFFmpeg(video []byte) ([]byte, error) {
	dir := os.TempDir()
	stamp := time.Now().UnixNano()
	inputPath := filepath.Join(dir, fmt.Sprintf("video_%d.mp4", stamp))
	outputPath := filepath.Join(dir, fmt.Sprintf("audio_%d.mp3", stamp))

	if err := os.WriteFile(inputPath, video, 0o600); err != nil {
		return nil, err
	}
	defer os.Remove(inputPath)
	defer os.Remove(outputPath)

	if ok, err := resumeHasAudioStream(inputPath); err == nil && !ok {
		return nil, errors.New("Error al extraer el audio del video con ffmpeg")
	}

	_, err := resumeRunCmd("ffmpeg", "-i", inputPath, "-vn", "-acodec", "libmp3lame", "-q:a", "2", "-y", outputPath)
	if err != nil {
		return nil, errors.New("Error al extraer el audio del video con ffmpeg")
	}

	audio, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, errors.New("El archivo de audio extraído no pudo ser leído")
	}
	if len(audio) == 0 {
		return nil, errors.New("El video no contiene pista de audio")
	}
	return audio, nil
}

func resumeHasAudioStream(inputPath string) (bool, error) {
	out, err := resumeRunCmd("ffprobe",
		"-v", "error",
		"-select_streams", "a:0",
		"-show_entries", "stream=codec_type",
		"-of", "csv=p=0",
		inputPath,
	)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) != "", nil
}

func resumeRunCommand(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, fmt.Errorf("%s: %w (%s)", name, err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

var _ = io.Discard

func init() { core.Register(Resume{}) }
