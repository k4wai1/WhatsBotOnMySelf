package addons

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"time"

	"whatsbot-go/internal/core"
)

var (
	ocrHTTP = &http.Client{Timeout: 60 * time.Second}
	ocrURL  = "https://api.ocr.space/parse/image"
)

// Ocr porta addons/ocr.js (api.ocr.space).
type Ocr struct{}

func init() { core.Register(Ocr{}) }

func (Ocr) Commands() []string { return []string{"ocr"} }

func ocrAPIKey() string {
	if v := os.Getenv("OCR_SPACE_KEY"); v != "" {
		return v
	}
	return "helloworld"
}

type ocrParsedResult struct {
	ParsedText string `json:"ParsedText"`
}

type ocrRespuesta struct {
	ParsedResults []ocrParsedResult `json:"ParsedResults"`
	ErrorMessage  string            `json:"ErrorMessage"`
}

// ocrMultipart construye el formulario igual que ocr.js.
func ocrMultipart(buffer []byte, mimetype, apiKey string) ([]byte, string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if mimetype == "" {
		mimetype = "image/jpeg"
	}
	part, err := writer.CreateFormFile("file", "image.jpg")
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(buffer); err != nil {
		return nil, "", err
	}
	campos := map[string]string{
		"language":          "spa",
		"detectOrientation": "true",
		"isOverlayRequired": "false",
		"apikey":            apiKey,
	}
	for clave, valor := range campos {
		if err := writer.WriteField(clave, valor); err != nil {
			return nil, "", err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return body.Bytes(), writer.FormDataContentType(), nil
}

// ocrParsearTexto extrae el primer resultado textual no vacío.
func ocrParsearTexto(raw []byte) (string, error) {
	var parsed ocrRespuesta
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", err
	}
	if len(parsed.ParsedResults) == 0 {
		return "", nil
	}
	return strings.TrimSpace(parsed.ParsedResults[0].ParsedText), nil
}

func (Ocr) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}

	var buffer []byte
	var mimetype string
	var err error
	esCitada := false

	if ctx.Message != nil && ctx.Message.Message != nil && ctx.Message.Message.GetImageMessage() != nil {
		mimetype = ctx.Message.Message.GetImageMessage().GetMimetype()
		buffer, err = ctx.DownloadMedia()
	} else if quoted := ctx.QuotedMessage(); quoted != nil && quoted.GetImageMessage() != nil {
		esCitada = true
		mimetype = quoted.GetImageMessage().GetMimetype()
		buffer, err = ctx.DownloadQuoted()
	}
	_ = esCitada

	if err != nil || len(buffer) == 0 {
		return ctx.Reply("❌ Por favor envía una imagen o responde a un mensaje con imagen usando el comando ocr")
	}

	body, contentType, err := ocrMultipart(buffer, mimetype, ocrAPIKey())
	if err != nil {
		_ = ctx.React("❌")
		return ctx.Reply("❌ Error al procesar: " + err.Error())
	}
	req, err := http.NewRequest(http.MethodPost, ocrURL, bytes.NewReader(body))
	if err != nil {
		_ = ctx.React("❌")
		return ctx.Reply("❌ Error al procesar: " + err.Error())
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := ocrHTTP.Do(req)
	if err != nil {
		_ = ctx.React("❌")
		return ctx.Reply("❌ Error al procesar: " + err.Error())
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		_ = ctx.React("❌")
		return ctx.Reply("❌ Error al procesar: " + err.Error())
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = ctx.React("❌")
		return ctx.Reply(fmt.Sprintf("❌ Error al procesar: HTTP error! status: %d", resp.StatusCode))
	}

	texto, err := ocrParsearTexto(raw)
	if err != nil {
		_ = ctx.React("❌")
		return ctx.Reply("❌ Error al procesar la imagen.")
	}
	if texto == "" {
		_ = ctx.React("❌")
		return ctx.Reply("❌ No se detectó texto en la imagen.")
	}
	if err := ctx.React("✅"); err != nil {
		return err
	}
	return ctx.Reply("📝 *Texto extraído:*\n\n" + texto)
}
