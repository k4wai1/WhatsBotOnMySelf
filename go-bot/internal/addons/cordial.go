package addons

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"go.mau.fi/whatsmeow/proto/waE2E"

	"whatsbot-go/internal/core"
)

const cordialSystemPrompt = `Eres un filtro de cordialidad estricto. Recibirás una transcripción. Identifica insultos, palabras malsonantes o frases agresivas.
Genera reemplazos extremadamente amables solo para esas palabras, educados o poéticos para esas frases, invirtiendo la agresividad a bondad. .inetanta covervar un +50% del las palabras originales sn editarlas
Responde ÚNICAMENTE con un JSON con la siguiente estructura, sin markdown, sin texto adicional:
{
  "replacements": [
    {"original": "puto hocico", "replacement": "hermosa boquita"},
    {"original": "te rompo la cara", "replacement": "te doy una caricia"}
  ]
}`

// Variables inyectables: permiten probar el flujo sin ffmpeg ni red.
var (
	cordialHTTPClient  = &http.Client{Timeout: 120 * time.Second}
	cordialGroqURL     = "https://api.groq.com/openai/v1/audio/transcriptions"
	cordialDeepSeekURL = "https://api.deepseek.com/v1/chat/completions"
	cordialTTSEndpoint = "https://translate.google.com/translate_tts"

	cordialDownloadAudio = func(ctx *core.Context) ([]byte, error) {
		return ctx.DownloadQuoted()
	}
	cordialConvertMP3    = cordialDefaultConvertMP3
	cordialProbeDuration = cordialDefaultProbeDuration
	cordialBuildAudio    = cordialDefaultBuildAudio
	cordialFetchTTS      = cordialDefaultFetchTTS
)

type cordialWord struct {
	Word  string  `json:"word"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

type cordialWhisperWord struct {
	Word      string
	Start     float64
	End       float64
	CleanWord string
}

type cordialPatch struct {
	Start    float64
	End      float64
	Text     string
	Duration float64
}

type cordialReplacement struct {
	Original    string `json:"original"`
	Replacement string `json:"replacement"`
}

type cordialAnalysis struct {
	Replacements []cordialReplacement `json:"replacements"`
}

// Cordial porta addons/cordial.js: .cordial y .cord.
type Cordial struct{}

// Commands cumple core.Addon.
func (Cordial) Commands() []string { return []string{"cordial", "cord"} }

// cordialNum formatea un float como lo haría la interpolación de JS.
func cordialNum(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

// cordialCleanWord replica replace(/[^\w\sñáéíóú]/gi, ”) tras toLowerCase.
func cordialCleanWord(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == 'ñ' || r == 'á' || r == 'é' || r == 'í' || r == 'ó' || r == 'ú':
			b.WriteRune(r)
		case r == '_' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
		case unicode.IsSpace(r):
			b.WriteRune(r)
		}
	}
	return b.String()
}

// cordialAtempoChain replica buildAtempoChain del original.
func cordialAtempoChain(ratio float64) string {
	var tempos []string
	current := ratio

	if math.IsNaN(current) || math.IsInf(current, 0) {
		return "anull"
	}
	for steps := 0; current > 2.0 && steps < 100; steps++ {
		tempos = append(tempos, "atempo=2.0")
		current /= 2.0
	}
	for steps := 0; current < 0.5 && steps < 100; steps++ {
		tempos = append(tempos, "atempo=0.5")
		current /= 0.5
	}
	if current != 1 {
		tempos = append(tempos, "atempo="+strconv.FormatFloat(current, 'f', 2, 64))
	}
	if len(tempos) == 0 {
		return "anull"
	}
	return strings.Join(tempos, ",")
}

// cordialMapPatches replica el mapeo de reemplazos a timestamps de Whisper.
func cordialMapPatches(words []cordialWord, reps []cordialReplacement) []cordialPatch {
	whisper := make([]cordialWhisperWord, 0, len(words))
	for _, w := range words {
		whisper = append(whisper, cordialWhisperWord{
			Word:      w.Word,
			Start:     w.Start,
			End:       w.End,
			CleanWord: cordialCleanWord(w.Word),
		})
	}

	var patches []cordialPatch
	for _, rep := range reps {
		targetWords := strings.Split(cordialCleanWord(rep.Original), " ")
		for i := 0; i <= len(whisper)-len(targetWords); i++ {
			match := true
			for j := 0; j < len(targetWords); j++ {
				if whisper[i+j].CleanWord != targetWords[j] {
					match = false
					break
				}
			}
			if match {
				start := whisper[i].Start
				end := whisper[i+len(targetWords)-1].End
				patches = append(patches, cordialPatch{
					Start:    start,
					End:      end,
					Text:     rep.Replacement,
					Duration: end - start,
				})
				break
			}
		}
	}
	return patches
}

// cordialGoogleTTSURL arma la URL de google-tts-api (lang es, host translate).
func cordialGoogleTTSURL(text string) string {
	return cordialTTSEndpoint + "?ie=UTF-8&q=" + url.QueryEscape(text) +
		"&tl=es&client=tw-ob&ttsspeed=1&total=1&idx=0&textlen=" +
		strconv.Itoa(utf8.RuneCountInString(text))
}

// cordialTranscribir sube el MP3 a Groq y devuelve texto + timestamps.
func cordialTranscribir(mp3 []byte) (string, []cordialWord, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("file", "audio.mp3")
	if err != nil {
		return "", nil, err
	}
	if _, err := file.Write(mp3); err != nil {
		return "", nil, err
	}
	if err := writer.WriteField("model", "whisper-large-v3"); err != nil {
		return "", nil, err
	}
	if err := writer.WriteField("response_format", "verbose_json"); err != nil {
		return "", nil, err
	}
	if err := writer.WriteField("timestamp_granularities[]", "word"); err != nil {
		return "", nil, err
	}
	if err := writer.Close(); err != nil {
		return "", nil, err
	}

	req, err := http.NewRequest(http.MethodPost, cordialGroqURL, &body)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("GROQ_API_KEY"))
	req.Header.Set("Content-Type", writer.FormDataContentType())

	res, err := cordialHTTPClient.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer res.Body.Close()

	var parsed struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
		Text  string        `json:"text"`
		Words []cordialWord `json:"words"`
	}
	if err := json.NewDecoder(res.Body).Decode(&parsed); err != nil {
		return "", nil, err
	}
	if parsed.Error != nil {
		return "", nil, fmt.Errorf("Error de Groq: %s", parsed.Error.Message)
	}
	if parsed.Text == "" || parsed.Words == nil {
		return "", nil, errors.New("No se pudo transcribir el audio o no hay timestamps.")
	}
	return parsed.Text, parsed.Words, nil
}

// cordialAnalizar pide a DeepSeek los reemplazos cordiales.
func cordialAnalizar(texto string) ([]cordialReplacement, error) {
	payload := map[string]any{
		"model": "deepseek-chat",
		"messages": []map[string]string{
			{"role": "system", "content": cordialSystemPrompt},
			{"role": "user", "content": texto},
		},
		"temperature": 0.3,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, cordialDeepSeekURL, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+os.Getenv("DEEPSEEK_API_KEY"))

	res, err := cordialHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(res.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	if len(parsed.Choices) == 0 {
		return nil, errors.New("DeepSeek no devolvió opciones")
	}
	content := parsed.Choices[0].Message.Content
	content = strings.ReplaceAll(content, "```json", "")
	content = strings.ReplaceAll(content, "```", "")
	content = strings.TrimSpace(content)

	var analysis cordialAnalysis
	if err := json.Unmarshal([]byte(content), &analysis); err != nil {
		return nil, err
	}
	return analysis.Replacements, nil
}

// cordialDefaultConvertMP3 convierte el audio descargado a MP3 limpio.
func cordialDefaultConvertMP3(rawAudioPath, inputAudioPath string) error {
	cmd := exec.Command("ffmpeg", "-y", "-i", rawAudioPath, "-acodec", "libmp3lame", inputAudioPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg: %w: %s", err, stderr.String())
	}
	return nil
}

// cordialDefaultProbeDuration obtiene la duración con ffprobe.
func cordialDefaultProbeDuration(filePath string) (float64, error) {
	cmd := exec.Command("ffprobe", "-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1", filePath)
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	return strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
}

// cordialDefaultFetchTTS descarga el MP3 de Google Translate.
func cordialDefaultFetchTTS(target string) ([]byte, error) {
	res, err := cordialHTTPClient.Get(target)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	return io.ReadAll(res.Body)
}

// cordialDefaultBuildAudio arma y ejecuta el complejo filtro de FFmpeg.
func cordialDefaultBuildAudio(inputAudioPath string, ttsPaths []string, patches []cordialPatch, outputAudioPath string) error {
	args := []string{"-y", "-i", inputAudioPath}
	for _, p := range ttsPaths {
		args = append(args, "-i", p)
	}

	muteSections := make([]string, 0, len(patches))
	for _, p := range patches {
		muteSections = append(muteSections, "between(t,"+cordialNum(p.Start)+","+cordialNum(p.End)+")")
	}

	filter := "[0:a]volume=enable='" + strings.Join(muteSections, "+") + "':volume=0[muted];"
	var mixInputs strings.Builder
	mixInputs.WriteString("[muted]")
	for i, patch := range patches {
		ttsDuration, err := cordialProbeDuration(ttsPaths[i])
		if err != nil {
			return err
		}
		ratio := ttsDuration / patch.Duration
		tempo := cordialAtempoChain(ratio)
		delay := int(math.Floor(patch.Start * 1000))
		filter += fmt.Sprintf("[%d:a]%s,adelay=%d|%d[patch%d];", i+1, tempo, delay, delay, i)
		mixInputs.WriteString("[patch" + strconv.Itoa(i) + "]")
	}
	filter += mixInputs.String() + "amix=inputs=" + strconv.Itoa(len(patches)+1) +
		":duration=first:dropout_transition=0:normalize=0[out]"

	args = append(args, "-filter_complex", filter, "-map", "[out]",
		"-c:a", "libopus", "-b:a", "64k", "-vbr", "on", outputAudioPath)

	cmd := exec.Command("ffmpeg", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg: %w: %s", err, stderr.String())
	}
	return nil
}

// cordialFail replica el catch: aviso con el mensaje y reacción ❌.
func cordialFail(ctx *core.Context, err error) error {
	fmt.Fprintf(os.Stderr, "Error en addon /cordial: %v\n", err)
	if sendErr := ctx.SendQuoted(core.TextMessage("❌ Hubo un error de procesamiento: "+err.Error()), ctx.Message); sendErr != nil {
		return sendErr
	}
	return ctx.React("❌")
}

// cordialProcess ejecuta el pipeline completo dentro de un directorio temporal.
func cordialProcess(ctx *core.Context) error {
	tempDir, err := os.MkdirTemp("", "cordial-*")
	if err != nil {
		return cordialFail(ctx, err)
	}
	defer os.RemoveAll(tempDir)

	rawAudioPath := filepath.Join(tempDir, "raw.ogg")
	inputAudioPath := filepath.Join(tempDir, "input.mp3")
	outputAudioPath := filepath.Join(tempDir, "output.ogg")

	if err := ctx.React("👂"); err != nil {
		return err
	}

	buffer, err := cordialDownloadAudio(ctx)
	if err != nil {
		return cordialFail(ctx, err)
	}
	if err := os.WriteFile(rawAudioPath, buffer, 0o644); err != nil {
		return cordialFail(ctx, err)
	}

	if err := cordialConvertMP3(rawAudioPath, inputAudioPath); err != nil {
		return cordialFail(ctx, err)
	}

	mp3Buffer, err := os.ReadFile(inputAudioPath)
	if err != nil {
		return cordialFail(ctx, err)
	}

	texto, words, err := cordialTranscribir(mp3Buffer)
	if err != nil {
		return cordialFail(ctx, err)
	}

	if err := ctx.React("🧠"); err != nil {
		return err
	}

	replacements, err := cordialAnalizar(texto)
	if err != nil {
		return cordialFail(ctx, err)
	}

	if len(replacements) == 0 {
		if err := ctx.SendQuoted(core.TextMessage("✨ Este audio ya es bastante cordial. No hay nada que parchear."), ctx.Message); err != nil {
			return err
		}
		return ctx.React("✅")
	}

	if err := ctx.React("🛠️"); err != nil {
		return err
	}

	patches := cordialMapPatches(words, replacements)
	if len(patches) == 0 {
		return cordialFail(ctx, errors.New("DeepSeek encontró insultos pero no pude mapearlos al audio."))
	}

	ttsPaths := make([]string, 0, len(patches))
	for i, patch := range patches {
		ttsURL := cordialGoogleTTSURL(patch.Text)
		ttsBuffer, err := cordialFetchTTS(ttsURL)
		if err != nil {
			return cordialFail(ctx, err)
		}
		ttsPath := filepath.Join(tempDir, "tts_"+strconv.Itoa(i)+".mp3")
		if err := os.WriteFile(ttsPath, ttsBuffer, 0o644); err != nil {
			return cordialFail(ctx, err)
		}
		ttsPaths = append(ttsPaths, ttsPath)
	}

	if err := cordialBuildAudio(inputAudioPath, ttsPaths, patches, outputAudioPath); err != nil {
		return cordialFail(ctx, err)
	}

	finalAudio, err := os.ReadFile(outputAudioPath)
	if err != nil {
		return cordialFail(ctx, err)
	}

	seconds := uint32(0)
	if duration, probeErr := cordialProbeDuration(outputAudioPath); probeErr == nil && duration > 0 {
		seconds = uint32(math.Round(duration))
	}

	if err := ctx.SendAudio(finalAudio, "audio/ogg; codecs=opus", true, seconds); err != nil {
		return cordialFail(ctx, err)
	}
	return ctx.React("✨")
}

// Handle valida el audio citado y lanza el pipeline.
func (c Cordial) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}

	var audio *waE2E.AudioMessage
	if quoted := ctx.QuotedMessage(); quoted != nil {
		audio = quoted.GetAudioMessage()
	}
	if audio == nil {
		return ctx.SendQuoted(core.TextMessage("❌ Debes responder a un mensaje de audio."), ctx.Message)
	}
	if audio.GetSeconds() > 240 {
		return ctx.SendQuoted(core.TextMessage("❌ El audio es muy largo. Máximo 4 minutos."), ctx.Message)
	}

	return cordialProcess(ctx)
}

func init() {
	core.Register(Cordial{})
}
