package addons

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"whatsbot-go/internal/core"
)

var (
	cedulaHTTP = &http.Client{Timeout: 20 * time.Second}
	cedulaURL  = "https://api.cedula.com.ve/api/v1"
)

// Cedula porta addons/cedula.js.
type Cedula struct{}

func init() { core.Register(Cedula{}) }

func (Cedula) Commands() []string { return []string{"cedula", "ci"} }

// cedulaPreparar limpia el input y determina la nacionalidad.
func cedulaPreparar(raw string) (nacionalidad, limpio string, ok bool) {
	recortado := strings.TrimSpace(raw)
	limpio = strings.ReplaceAll(recortado, ".", "")
	limpio = strings.ReplaceAll(limpio, "-", "")
	limpio = strings.ReplaceAll(limpio, " ", "")
	limpio = strings.TrimSpace(limpio)
	if strings.HasPrefix(strings.ToLower(limpio), "v") {
		limpio = limpio[1:]
	} else if strings.HasPrefix(strings.ToLower(limpio), "e") {
		limpio = limpio[1:]
	}
	limpio = strings.TrimSpace(limpio)

	nacionalidad = "V"
	if strings.HasPrefix(strings.ToLower(recortado), "e") {
		nacionalidad = "E"
	} else if strings.HasPrefix(strings.ToLower(recortado), "v") {
		nacionalidad = "V"
	}

	if !regexp.MustCompile(`^\d+$`).MatchString(limpio) || len(limpio) < 3 || len(limpio) > 12 {
		return nacionalidad, limpio, false
	}
	return nacionalidad, limpio, true
}

func cedulaError(errStr string) string {
	switch errStr {
	case "INVALID_TOKEN":
		return "Token o App-ID inválido. Verifica tus credenciales en cedula.com.ve"
	case "NOT_FOUND", "RECORD_NOT_FOUND", "CEDULA_NOT_FOUND":
		return "Cédula no encontrada en los registros"
	case "INVALID_CEDULA":
		return "Número de cédula inválido"
	case "LIMIT_EXCEEDED":
		return "Límite de consultas alcanzado. Intenta más tarde"
	}
	if errStr == "" {
		return "Error: Desconocido"
	}
	return "Error: " + errStr
}

type cedulaCNE struct {
	Estado          string `json:"estado"`
	Municipio       string `json:"municipio"`
	Parroquia       string `json:"parroquia"`
	CentroElectoral string `json:"centro_electoral"`
}

type cedulaDatos struct {
	Cedula          string     `json:"cedula"`
	PrimerNombre    string     `json:"primer_nombre"`
	SegundoNombre   string     `json:"segundo_nombre"`
	PrimerApellido  string     `json:"primer_apellido"`
	SegundoApellido string     `json:"segundo_apellido"`
	RIF             string     `json:"rif"`
	FechaNac        string     `json:"fecha_nac"`
	CNE             *cedulaCNE `json:"cne"`
}

type cedulaRespuesta struct {
	Error    json.RawMessage `json:"error"`
	ErrorStr string          `json:"error_str"`
	Data     json.RawMessage `json:"data"`
}

// cedulaEsCierto interpreta tanto error:true como error:"INVALID_TOKEN".
func cedulaEsCierto(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return false
	}
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return strings.TrimSpace(x) != ""
	default:
		return x != nil
	}
}

func cedulaBodyPreview(body []byte) string {
	s := strings.TrimSpace(string(body))
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}

func (Cedula) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}
	appID := os.Getenv("CEDULA_APP_ID")
	token := os.Getenv("CEDULA_TOKEN")
	if appID == "" || token == "" {
		return ctx.Reply("⚙️ *API de cédulas no configurada*\n\nPara usar este comando necesitas obtener credenciales gratis en:\nhttps://cedula.com.ve\n\nLuego agrega en tu archivo *.env*:\n```\nCEDULA_APP_ID=tu_app_id\nCEDULA_TOKEN=tu_token\n```")
	}

	raw := strings.TrimSpace(strings.Join(ctx.Args, " "))
	if raw == "" {
		return ctx.Reply("📋 *Consulta de Cédula*\n\nUso: `.cedula <número>`\nEjemplo: `.cedula 32832480`\n\nTambién puedes usar: `.ci <número>`")
	}

	nacionalidad, limpio, ok := cedulaPreparar(raw)
	if !ok {
		return ctx.Reply("⚠️ Número de cédula inválido. Solo se permiten dígitos (3-12 caracteres).")
	}

	if err := ctx.React("🔍"); err != nil {
		return err
	}

	consulta := fmt.Sprintf("%s?app_id=%s&token=%s&nacionalidad=%s&cedula=%s",
		cedulaURL, url.QueryEscape(appID), url.QueryEscape(token), url.QueryEscape(nacionalidad), url.QueryEscape(limpio))
	req, err := http.NewRequest(http.MethodGet, consulta, nil)
	if err != nil {
		slog.Error("❌ [cedula] error creando petición", "error", err)
		_ = ctx.React("❌")
		return ctx.Reply("❌ Error al consultar la API. Verifica tu conexión e intenta de nuevo.")
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; WhatsBotOnMySelf)")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Referer", "https://cedula.com.ve/")
	resp, err := cedulaHTTP.Do(req)
	if err != nil {
		slog.Error("❌ [cedula] error de red", "error", err)
		_ = ctx.React("❌")
		return ctx.Reply("❌ Error al consultar la API. Verifica tu conexión e intenta de nuevo.")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("❌ [cedula] error leyendo respuesta", "status", resp.StatusCode, "error", err)
		_ = ctx.React("❌")
		return ctx.Reply("❌ Error al consultar la API. Verifica tu conexión e intenta de nuevo.")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Error("❌ [cedula] HTTP inesperado", "status", resp.StatusCode, "body", cedulaBodyPreview(body))
		_ = ctx.React("❌")
		return ctx.Reply(fmt.Sprintf("❌ Error al consultar la API (HTTP %d).", resp.StatusCode))
	}
	var parsed cedulaRespuesta
	if err := json.Unmarshal(body, &parsed); err != nil {
		slog.Error("❌ [cedula] JSON inválido", "status", resp.StatusCode, "body", cedulaBodyPreview(body), "error", err)
		_ = ctx.React("❌")
		return ctx.Reply("❌ Error al consultar la API. Respuesta inesperada del proveedor.")
	}

	var datos *cedulaDatos
	if len(parsed.Data) > 0 {
		if err := json.Unmarshal(parsed.Data, &datos); err != nil {
			slog.Warn("⚠️ [cedula] data no es un objeto", "data", cedulaBodyPreview(parsed.Data), "error", err)
		}
	}
	if datos == nil || cedulaEsCierto(parsed.Error) || parsed.ErrorStr != "" {
		slog.Warn("⚠️ [cedula] consulta fallida", "status", resp.StatusCode, "error_str", parsed.ErrorStr, "error", cedulaBodyPreview(parsed.Error), "data", cedulaBodyPreview(parsed.Data))
		_ = ctx.React("❌")
		return ctx.Reply("❌ *Consulta fallida*\n\n" + cedulaError(parsed.ErrorStr))
	}
	d := datos
	nombre := strings.TrimSpace(strings.Join([]string{d.PrimerNombre, d.SegundoNombre}, " "))
	apellido := strings.TrimSpace(strings.Join([]string{d.PrimerApellido, d.SegundoApellido}, " "))
	rif := d.RIF
	if rif == "" {
		rif = "N/A"
	}

	var b strings.Builder
	b.WriteString("🪪 *Datos de Cédula*\n")
	b.WriteString("━━━━━━━━━━━━━━━━━\n")
	fmt.Fprintf(&b, "📄 *Cédula:* %s-%s\n", nacionalidad, d.Cedula)
	fmt.Fprintf(&b, "👤 *Nombre:* %s\n", nombre)
	fmt.Fprintf(&b, "👥 *Apellido:* %s\n", apellido)
	fmt.Fprintf(&b, "🏢 *RIF:* %s\n", rif)
	if d.FechaNac != "" {
		fmt.Fprintf(&b, "🎂 *Fecha Nac:* %s\n", d.FechaNac)
	}
	if d.CNE != nil {
		b.WriteString("\n🗳️ *Datos Electorales*\n")
		fmt.Fprintf(&b, "📍 Estado: %s\n", valorONA(d.CNE.Estado))
		fmt.Fprintf(&b, "🏘️ Municipio: %s\n", valorONA(d.CNE.Municipio))
		fmt.Fprintf(&b, "📌 Parroquia: %s\n", valorONA(d.CNE.Parroquia))
		fmt.Fprintf(&b, "🏛️ Centro: %s\n", valorONA(d.CNE.CentroElectoral))
	}

	if err := ctx.React("✅"); err != nil {
		return err
	}
	return ctx.Reply(b.String())
}

func valorONA(v string) string {
	if v == "" {
		return "N/A"
	}
	return v
}
