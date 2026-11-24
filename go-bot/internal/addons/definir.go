package addons

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"whatsbot-go/internal/core"
)

// definirMaxSentidos replica MAX_SENTIDOS.
const definirMaxSentidos = 4

// Variables inyectables para las pruebas (sin red).
var (
	definirHTTPClient = &http.Client{Timeout: 15 * time.Second}
	definirRaeURL     = "https://rae-api.com/api/words/"
	definirEnURL      = "https://api.dictionaryapi.dev/api/v2/entries/en/"
)

type definirRaeSense struct {
	Raw string `json:"raw"`
}

type definirRaeOrigin struct {
	Raw string `json:"raw"`
}

type definirRaeMeaning struct {
	Origin *definirRaeOrigin `json:"origin"`
	Senses []definirRaeSense `json:"senses"`
}

type definirRaeData struct {
	Word     string              `json:"word"`
	Meanings []definirRaeMeaning `json:"meanings"`
}

type definirRaeResponse struct {
	OK   *bool           `json:"ok"`
	Data *definirRaeData `json:"data"`
}

type definirEnDefinition struct {
	Definition string `json:"definition"`
}

type definirEnMeaning struct {
	PartOfSpeech string                `json:"partOfSpeech"`
	Definitions  []definirEnDefinition `json:"definitions"`
}

type definirEnEntry struct {
	Word     string             `json:"word"`
	Phonetic string             `json:"phonetic"`
	Meanings []definirEnMeaning `json:"meanings"`
}

// Definir porta addons/definir.js: .definir y .def.
type Definir struct{}

// Commands cumple core.Addon.
func (Definir) Commands() []string { return []string{"definir", "def"} }

// definirFormatearRae replica formatearRae(data, palabra).
func definirFormatearRae(data *definirRaeData, palabra string) *string {
	if data == nil {
		data = &definirRaeData{}
	}
	word := data.Word
	if word == "" {
		word = palabra
	}
	out := "📖 *" + word + "*\n"
	if len(data.Meanings) > 0 && data.Meanings[0].Origin != nil && data.Meanings[0].Origin.Raw != "" {
		out += "\n🌱 _" + data.Meanings[0].Origin.Raw + "_\n"
	}

	count := 0
	for _, meaning := range data.Meanings {
		if meaning.Senses == nil {
			continue
		}
		for _, sense := range meaning.Senses {
			if sense.Raw == "" || count >= definirMaxSentidos {
				break
			}
			out += "\n" + sense.Raw
			count++
		}
		if count >= definirMaxSentidos {
			break
		}
	}
	if count == 0 {
		return nil
	}
	trimmed := strings.TrimSpace(out)
	return &trimmed
}

// definirFormatearEn replica definirEn del original.
func definirFormatearEn(entry *definirEnEntry) *string {
	if entry == nil {
		return nil
	}
	out := "📖 *" + entry.Word + "* 🇬🇧\n"
	if entry.Phonetic != "" {
		out += "🔊 _" + entry.Phonetic + "_\n"
	}

	count := 0
	for _, m := range entry.Meanings {
		out += "\n*" + m.PartOfSpeech + "*\n"
		defs := m.Definitions
		if len(defs) > 2 {
			defs = defs[:2]
		}
		for _, def := range defs {
			if count >= definirMaxSentidos {
				break
			}
			out += "• " + def.Definition + "\n"
			count++
		}
		if count >= definirMaxSentidos {
			break
		}
	}
	if count == 0 {
		return nil
	}
	trimmed := strings.TrimSpace(out)
	return &trimmed
}

// definirFetchJSON hace la petición y decodifica solo si el estado es 2xx.
func definirFetchJSON(target string, userAgent string, out any) bool {
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return false
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	res, err := definirHTTPClient.Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return false
	}
	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		return false
	}
	return true
}

// definirBuscarRae consulta rae-api.com.
func definirBuscarRae(palabra string) *string {
	target := definirRaeURL + url.PathEscape(strings.ToLower(palabra))
	var parsed definirRaeResponse
	if !definirFetchJSON(target, "Mozilla/5.0", &parsed) {
		return nil
	}
	if parsed.OK != nil && !*parsed.OK {
		return nil
	}
	if parsed.Data == nil {
		return nil
	}
	return definirFormatearRae(parsed.Data, palabra)
}

// definirBuscarEn consulta dictionaryapi.dev.
func definirBuscarEn(palabra string) *string {
	target := definirEnURL + url.PathEscape(palabra)
	var entries []definirEnEntry
	if !definirFetchJSON(target, "", &entries) {
		return nil
	}
	if len(entries) == 0 {
		return nil
	}
	return definirFormatearEn(&entries[0])
}

// Handle replica el flujo RAE → inglés → aviso del addon original.
func (d Definir) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}

	palabra := strings.TrimSpace(strings.Join(ctx.Args, " "))
	if palabra == "" {
		if quoted := ctx.QuotedMessage(); quoted != nil {
			txt := strings.TrimSpace(quoted.GetConversation())
			fields := strings.Fields(txt)
			if len(fields) > 0 {
				palabra = fields[0]
			}
		}
	}
	if palabra == "" || strings.ContainsAny(palabra, " \t\r\n") {
		if err := ctx.React("⚠️"); err != nil {
			return err
		}
		return ctx.SendQuoted(core.TextMessage("📚 *Diccionario*\n\nUso: `.definir <palabra>`"), ctx.Message)
	}

	if err := ctx.React("🔍"); err != nil {
		return err
	}

	resultado := definirBuscarRae(palabra)
	if resultado == nil {
		resultado = definirBuscarEn(palabra)
	}

	if resultado == nil {
		if err := ctx.React("❌"); err != nil {
			return err
		}
		return ctx.SendQuoted(core.TextMessage("No encontré \""+palabra+"\" en el diccionario."), ctx.Message)
	}

	if err := ctx.SendQuoted(core.TextMessage(*resultado), ctx.Message); err != nil {
		return err
	}
	return ctx.React("✅")
}

func init() {
	core.Register(Definir{})
}
