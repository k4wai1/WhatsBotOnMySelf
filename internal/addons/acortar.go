package addons

import (
	"encoding/json"
	"fmt"
	"io"
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
	acortarHTTPClient   = &http.Client{Timeout: 15 * time.Second}
	acortarAPIBase      = "https://is.gd/create.php"
	acortarFallbackBase = "https://tinyurl.com/api-create.php"
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

func acortarRecortar(s string, max int) string {
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

func acortarGet(target string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "*/*")
	return acortarHTTPClient.Do(req)
}

func acortarIsGd(target string) (string, error) {
	api := acortarAPIBase + "?format=json&url=" + url.QueryEscape(target)
	res, err := acortarGet(api)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 8192))
	if err != nil {
		return "", err
	}
	var parsed struct {
		ShortURL     string `json:"shorturl"`
		ErrorMessage string `json:"errormessage"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("is.gd responded HTTP %d with non-JSON: %s", res.StatusCode, acortarRecortar(strings.TrimSpace(string(body)), 80))
	}
	if parsed.ShortURL == "" {
		if parsed.ErrorMessage == "" {
			parsed.ErrorMessage = "is.gd rechazó el enlace"
		}
		return "", fmt.Errorf("%s", parsed.ErrorMessage)
	}
	return parsed.ShortURL, nil
}

func acortarAlternativo(target string) (string, error) {
	api := acortarFallbackBase + "?url=" + url.QueryEscape(target)
	res, err := acortarGet(api)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 4096))
	if err != nil {
		return "", err
	}
	short := strings.TrimSpace(string(body))
	if !acortarScheme.MatchString(short) {
		return "", fmt.Errorf("alternativo responded HTTP %d with unexpected body: %q", res.StatusCode, acortarRecortar(short, 80))
	}
	return short, nil
}

// Handle replica el flujo de is.gd del addon original, con respaldo si falla.
func (a Acortar) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}

	texto := strings.Join(ctx.Args, " ")
	if strings.TrimSpace(texto) == "" {
		if quoted := ctx.QuotedMessage(); quoted != nil {
			texto = quoted.GetConversation()
			if texto == "" && quoted.GetExtendedTextMessage() != nil {
				texto = quoted.GetExtendedTextMessage().GetText()
			}
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

	short, err := acortarIsGd(target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "⚠️ [acortar] is.gd falló (%v); usando alternativo\n", err)
		short, err = acortarAlternativo(target)
	}
	if err != nil || short == "" {
		if err == nil {
			err = fmt.Errorf("sin respuesta de los acortadores")
		}
		return acortarError(ctx, err)
	}

	if err := ctx.SendQuoted(core.TextMessage("🔗 "+short), ctx.Message); err != nil {
		return err
	}
	return ctx.React("✅")
}

// acortarError registra el fallo, reacciona con ❌ y avisa al usuario por texto.
func acortarError(ctx *core.Context, err error) error {
	fmt.Fprintf(os.Stderr, "❌ [acortar]: %v\n", err)
	_ = ctx.React("❌")
	return ctx.SendQuoted(core.TextMessage("❌ No pude acortar el enlace. Inténtalo de nuevo en unos minutos."), ctx.Message)
}

func init() {
	core.Register(Acortar{})
}
