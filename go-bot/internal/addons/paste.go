package addons

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"whatsbot-go/internal/core"
)

// pasteMaxChars replica MAX_CHARS.
const pasteMaxChars = 100000

// Variables inyectables para pruebas sin red.
var (
	pasteHTTPClient = &http.Client{Timeout: 20 * time.Second}
	pasteRsURL      = "https://paste.rs"
	paste0x0URL     = "https://0x0.st"
)

var pasteURLPattern = regexp.MustCompile(`^https?://`)

// Paste porta addons/paste.js: .paste.
type Paste struct{}

// Commands cumple core.Addon.
func (Paste) Commands() []string { return []string{"paste"} }

// pasteSubirRs sube el texto plano a paste.rs.
func pasteSubirRs(texto string) (string, error) {
	req, err := http.NewRequest(http.MethodPost, pasteRsURL, strings.NewReader(texto))
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	res, err := pasteHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("paste.rs HTTP %d", res.StatusCode)
	}
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return "", err
	}
	out := strings.TrimSpace(string(raw))
	if !pasteURLPattern.MatchString(out) {
		return "", fmt.Errorf("Respuesta inesperada de paste.rs")
	}
	return out, nil
}

// pasteSubir0x0 sube el texto como archivo a 0x0.st (respaldo).
func pasteSubir0x0(texto string) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="file"; filename="paste.txt"`)
	header.Set("Content-Type", "text/plain")
	part, err := writer.CreatePart(header)
	if err != nil {
		return "", err
	}
	if _, err := part.Write([]byte(texto)); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequest(http.MethodPost, paste0x0URL, &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Content-Type", writer.FormDataContentType())

	res, err := pasteHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("0x0.st HTTP %d", res.StatusCode)
	}
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return "", err
	}
	out := strings.TrimSpace(string(raw))
	if !pasteURLPattern.MatchString(out) {
		return "", fmt.Errorf("Respuesta inesperada de 0x0.st")
	}
	return out, nil
}

// pasteError registra el fallo y reacciona con ❌.
func pasteError(ctx *core.Context, err error) error {
	fmt.Fprintf(os.Stderr, "❌ [paste]: %v\n", err)
	return ctx.React("❌")
}

// Handle replica el flujo paste.rs → 0x0.st del addon original.
func (p Paste) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}

	quotedText := ""
	if quoted := ctx.QuotedMessage(); quoted != nil {
		quotedText = quoted.GetConversation()
	}
	text := strings.TrimSpace(quotedText)
	if text == "" {
		text = strings.TrimSpace(strings.Join(ctx.Args, " "))
	}

	if text == "" {
		if err := ctx.React("⚠️"); err != nil {
			return err
		}
		return ctx.SendQuoted(core.TextMessage("📤 *Paste*\n\n• `.paste <texto>`\n• Responde a un mensaje con `.paste`"), ctx.Message)
	}
	if utf8.RuneCountInString(text) > pasteMaxChars {
		if err := ctx.React("❌"); err != nil {
			return err
		}
		return ctx.SendQuoted(core.TextMessage("Máximo "+miscItoa(pasteMaxChars)+" caracteres."), ctx.Message)
	}

	if err := ctx.React("⏳"); err != nil {
		return err
	}

	link, err := pasteSubirRs(text)
	if err != nil {
		fmt.Fprintf(os.Stderr, "⚠️ [paste] respaldo a 0x0.st: %v\n", err)
		link, err = pasteSubir0x0(text)
		if err != nil {
			return pasteError(ctx, err)
		}
	}

	if err := ctx.SendQuoted(core.TextMessage("📤 "+link), ctx.Message); err != nil {
		return err
	}
	return ctx.React("✅")
}

func init() {
	core.Register(Paste{})
}
