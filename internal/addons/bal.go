package addons

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"whatsbot-go/internal/core"
)

// balHTTP y balAPIURL son inyectables para las pruebas (sin red real).
var (
	balHTTP   = &http.Client{Timeout: 30 * time.Second}
	balAPIURL = "https://api.deepseek.com/user/balance"
)

// Bal porta addons/bal.js: .bal consulta el saldo de DeepSeek.
type Bal struct{}

// Commands cumple core.Addon.
func (Bal) Commands() []string { return []string{"bal"} }

type balResponse struct {
	BalanceInfos []struct {
		TotalBalance json.RawMessage `json:"total_balance"`
		Currency     string          `json:"currency"`
	} `json:"balance_infos"`
}

// Handle replica la petición GET y los textos del addon original.
func (Bal) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}

	if err := ctx.React("⏳"); err != nil {
		slog.Error("Error en addon .bal:", "error", err)
		_ = ctx.React("❌")
		return nil
	}

	balance, currency, err := balFetchBalance()
	if err != nil {
		slog.Error("Error en addon .bal:", "error", err)
		_ = ctx.React("❌")
		return nil
	}

	text := fmt.Sprintf("💰 *Balance DeepSeek*\n\n💵 Saldo: *%s %s*", balance, currency)
	if err := ctx.SendQuoted(core.TextMessage(text), ctx.Message); err != nil {
		slog.Error("Error en addon .bal:", "error", err)
		_ = ctx.React("❌")
		return nil
	}

	if err := ctx.React("✅"); err != nil {
		slog.Error("Error en addon .bal:", "error", err)
	}
	return nil
}

// balFetchBalance consulta api.deepseek.com/user/balance.
func balFetchBalance() (string, string, error) {
	req, err := http.NewRequest(http.MethodGet, balAPIURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("DEEPSEEK_API_KEY"))

	resp, err := balHTTP.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("Error en API HTTP: %d", resp.StatusCode)
	}

	var data balResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", "", err
	}

	balance := "0.00"
	currency := "USD"
	if len(data.BalanceInfos) > 0 {
		if v := balRawToString(data.BalanceInfos[0].TotalBalance); v != "" {
			balance = v
		}
		if data.BalanceInfos[0].Currency != "" {
			currency = data.BalanceInfos[0].Currency
		}
	}
	return balance, currency, nil
}

// balRawToString reproduce el acceso JS `total_balance || '0.00'`: los números
// se imprimen sin decimales forzados y las cadenas se respetan tal cual.
func balRawToString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		return n.String()
	}
	return ""
}

func init() { core.Register(Bal{}) }
