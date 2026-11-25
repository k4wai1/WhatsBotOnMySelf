package addons

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"whatsbot-go/internal/core"
)

var (
	catHTTP = &http.Client{Timeout: 20 * time.Second}
	catAPI  = "https://api.thecatapi.com/v1/images/search?limit=1"
)

// Cat porta addons/cat.js (TheCatAPI).
type Cat struct{}

func init() { core.Register(Cat{}) }

func (Cat) Commands() []string { return []string{"cat"} }

var catLimite = wagoNuevoLimite(wagoImgLimit, wagoImgVentana)

// catObtenerURL consulta TheCatAPI.
func catObtenerURL() (string, error) {
	req, err := http.NewRequest(http.MethodGet, catAPI, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	if key := os.Getenv("CAT_API_KEY"); key != "" {
		req.Header.Set("x-api-key", key)
	}
	resp, err := catHTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var data []struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}
	if len(data) == 0 || data[0].URL == "" {
		return "", fmt.Errorf("sin URL")
	}
	return data[0].URL, nil
}

func catDescargar(rawURL string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := catHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func catEsTimeout(err error) bool {
	if err == nil {
		return false
	}
	texto := strings.ToLower(err.Error())
	return strings.Contains(texto, "timeout") || strings.Contains(texto, "deadline") || strings.Contains(texto, "abort")
}

func (Cat) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}
	if !catLimite.permitir(ctx.SenderJID().String()) {
		return ctx.React("❌")
	}
	if err := ctx.React("⏳"); err != nil {
		return err
	}
	url, err := catObtenerURL()
	if err == nil {
		var data []byte
		data, err = catDescargar(url)
		if err == nil {
			if err = ctx.SendImage(data, "Meow! 🐱"); err == nil {
				return ctx.React("✅")
			}
		}
	}
	slog.Error("❌ [cat] Error:", "error", err)
	_ = ctx.React("❌")
	mensaje := "Error obteniendo el michi 😿"
	if catEsTimeout(err) {
		mensaje = "Se tardó mucho 😿 Intenta de nuevo."
	}
	return ctx.Reply(mensaje)
}
