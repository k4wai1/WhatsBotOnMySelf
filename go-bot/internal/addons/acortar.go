package addons

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"whatsbot-go/internal/core"
)

// Variables inyectables para pruebas sin red.
var (
	acortarHTTPClient = &http.Client{Timeout: 15 * time.Second}
	acortarAPIBase    = "https://is.gd/create.php"
)

var acortarScheme = regexp.MustCompile(`(?i)^https?://`)

// Acortar porta addons/acortar.js: .acortar y .short.
type Acortar struct{}

// Commands cumple core.Addon.
func (Acortar) Commands() []string { return []string{"acortar", "short"} }

// acortarNormalizar aplica las mismas reglas que el original: primer token,
// https:// por defecto si falta el esquema y validación con URL.
func acortarNormalizar(texto string) (string, bool) {
	texto = strings.TrimSpace(texto)
	fields := strings.Fields(texto)
	raw := ""
	if len(fields) > 0 {
		raw = fields[0]
	}
	if raw == "" {
		return "", false
	}
	if !acortarScheme.MatchString(raw) {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "", false
	}
	return raw, true
}

// Handle replica el flujo de is.gd del addon original.
func (a Acortar) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}

	texto := strings.Join(ctx.Args, " ")
	if strings.TrimSpace(texto) == "" {
		if quoted := ctx.QuotedMessage(); quoted != nil {
			texto = quoted.GetConversation()
		}
	}

	if strings.TrimSpace(texto) == "" {
		if err := ctx.React("⚠️"); err != nil {
			return err
		}
		return ctx.SendQuoted(core.TextMessage("🔗 *Acortador*\n\nUso: `.acortar https://enlace-largo.com`"), ctx.Message)
	}

	target, ok := acortarNormalizar(texto)
	if !ok {
		if err := ctx.React("❌"); err != nil {
			return err
		}
		return ctx.SendQuoted(core.TextMessage("Ese enlace no parece válido."), ctx.Message)
	}

	if err := ctx.React("⏳"); err != nil {
		return err
	}

	api := acortarAPIBase + "?format=json&url=" + url.QueryEscape(target)
	req, err := http.NewRequest(http.MethodGet, api, nil)
	if err != nil {
		return acortarError(ctx, err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	res, err := acortarHTTPClient.Do(req)
	if err != nil {
		return acortarError(ctx, err)
	}
	defer res.Body.Close()

	var parsed struct {
		ShortURL     string `json:"shorturl"`
		ErrorMessage string `json:"errormessage"`
	}
	if err := json.NewDecoder(res.Body).Decode(&parsed); err != nil {
		return acortarError(ctx, err)
	}
	if parsed.ShortURL == "" {
		msg := parsed.ErrorMessage
		if msg == "" {
			msg = "is.gd rechazó el enlace"
		}
		return acortarError(ctx, fmt.Errorf("%s", msg))
	}

	if err := ctx.SendQuoted(core.TextMessage("🔗 "+parsed.ShortURL), ctx.Message); err != nil {
		return err
	}
	return ctx.React("✅")
}

// acortarError registra el fallo y reacciona con ❌, como el catch original.
func acortarError(ctx *core.Context, err error) error {
	fmt.Fprintf(os.Stderr, "❌ [acortar]: %v\n", err)
	return ctx.React("❌")
}

func init() {
	core.Register(Acortar{})
}
