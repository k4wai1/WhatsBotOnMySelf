// img.go porta addons/img.js: describe imágenes con la API multimodal de
// DeepSeek.
package addons

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"

	"whatsbot-go/internal/core"
)

const imgSystemInstruction = "Describe esta imagen detalladamente. Si la imagen contiene texto, es de suma importancia que lo extraigas y lo proporciones textualmente tal cual aparece."

// imgHTTP e imgChat son inyectables en las pruebas.
var imgHTTP = http.DefaultClient

var imgDownload = func(ctx *core.Context, quoted bool) ([]byte, error) {
	if quoted {
		return ctx.DownloadQuoted()
	}
	return ctx.DownloadMedia()
}

var imgChat = func(dataURL, prompt string) (string, error) {
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	payload := map[string]any{
		"model": "deepseek-chat",
		"messages": []map[string]any{{
			"role": "user",
			"content": []map[string]any{
				{"type": "text", "text": prompt},
				{"type": "image_url", "image_url": map[string]string{"url": dataURL}},
			},
		}},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodPost, "https://api.deepseek.com/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	res, err := imgHTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return "", err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("Fallo en DeepSeek: %d - %s", res.StatusCode, string(raw))
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("respuesta sin choices")
	}
	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
}

// Img porta los comandos .img y .vision.
type Img struct{}

// Commands cumple core.Addon.
func (Img) Commands() []string { return []string{"img", "vision"} }

// Handle describe la imagen adjunta o citada.
func (i Img) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}

	imageMsg, quoted, ok := imgPickImage(ctx)
	if !ok {
		return ctx.React("❓")
	}

	if reactErr := ctx.React("👁️"); reactErr != nil {
		return reactErr
	}

	buffer, err := imgDownload(ctx, quoted)
	if err != nil || len(buffer) == 0 {
		if err == nil {
			err = fmt.Errorf("imagen vacía")
		}
		return imgFail(ctx, err)
	}

	base64Image := base64.StdEncoding.EncodeToString(buffer)
	mimeType := imageMsg.GetMimetype()
	if mimeType == "" {
		mimeType = "image/jpeg"
	}
	dataURL := "data:" + mimeType + ";base64," + base64Image

	userArgs := strings.TrimSpace(strings.Join(ctx.Args, " "))
	finalPrompt := imgSystemInstruction
	if userArgs != "" {
		finalPrompt += "\n\nInstrucción adicional del usuario: " + userArgs
	}

	description, err := imgChat(dataURL, finalPrompt)
	if err != nil {
		return imgFail(ctx, err)
	}

	if err := ctx.Reply("📸 \n\n" + description); err != nil {
		return err
	}
	return ctx.React("✅")
}

func imgPickImage(ctx *core.Context) (*waE2E.ImageMessage, bool, bool) {
	if ctx.Message != nil && ctx.Message.Message != nil {
		if img := ctx.Message.Message.GetImageMessage(); img != nil {
			return img, false, true
		}
	}
	if quoted := ctx.QuotedMessage(); quoted != nil {
		if img := quoted.GetImageMessage(); img != nil {
			return img, true, true
		}
	}
	return nil, false, false
}

func imgFail(ctx *core.Context, err error) error {
	fmt.Printf("Error en addon de imagen (img.js): %v\n", err)
	if reactErr := ctx.React("❌"); reactErr != nil {
		return reactErr
	}
	return ctx.Reply("⚠️ No se pudo procesar la imagen.\nDetalle: " + err.Error())
}
