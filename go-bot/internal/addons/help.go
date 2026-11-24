package addons

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"

	"whatsbot-go/internal/core"
	"whatsbot-go/internal/store"
)

// Help porta addons/help.js: menú generado por DeepSeek y cacheado en
// help_cache.json, con regeneración automática cuando cambian los addons.
type Help struct{}

// Commands cumple core.Addon.
func (Help) Commands() []string { return []string{"help", "ayuda"} }

// Inyectables para pruebas (sin red ni depender del árbol real de addons).
var (
	helpHTTP     = &http.Client{Timeout: 60 * time.Second}
	helpAPIURL   = "https://api.deepseek.com/v1/chat/completions"
	helpCacheAlt string
	helpSrcAlt   string
)

func helpCachePath() string {
	if helpCacheAlt != "" {
		return helpCacheAlt
	}
	return core.DataPath("help_cache.json")
}

func helpSourceDir() string {
	if helpSrcAlt != "" {
		return helpSrcAlt
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return core.DataPath("addons")
	}
	return filepath.Dir(file)
}

type helpGroup struct {
	Commands  []string `json:"commands"`
	Preview   string   `json:"preview"`
	Technical string   `json:"technical"`
}

type helpCacheData struct {
	GeneratedAt int64                `json:"generatedAt"`
	AddonsHash  string               `json:"addonsHash"`
	Groups      []helpGroup          `json:"groups"`
	AliasMap    map[string]helpGroup `json:"aliasMap"`
}

var (
	helpMu      sync.Mutex
	helpData    *helpCacheData
	helpRegenCh chan struct{}
)

const helpSystemPrompt = `Eres un asistente que analiza addons para un bot de WhatsApp. Agrupa los comandos que realizan la misma función (alias). Devuelve ÚNICAMENTE un array JSON de objetos con la estructura:
[
  {
    "commands": [".c", ".chat"],
    "preview": "Descripción corta (1 línea, emoji permitido).",
    "technical": "Explicación técnica detallada (uso, opciones, comportamiento)."
  },
  ...
]
Si un addon no tiene comandos, responde []. No incluyas bloques de markdown.`

func init() {
	core.Register(Help{})
	core.RegisterBackground("help", helpBackground)
}

// helpBackground replica el init(sock, store) del addon original.
func helpBackground(_ *whatsmeow.Client, _ *store.Store) error {
	if _, err := os.Stat(helpCachePath()); err != nil {
		slog.Info("📝 Generando caché de ayuda con DeepSeek...")
		go func() {
			if _, err := helpGenerateCache(false); err != nil {
				slog.Error("❌ Error generando ayuda:", "error", err)
				return
			}
			slog.Info("✅ Caché de ayuda creada.")
		}()
		return nil
	}

	helpMu.Lock()
	if helpData == nil {
		helpData = helpLoadCache()
	}
	helpMu.Unlock()

	helpRegenerateIfStale()
	return nil
}

// helpComputeHash replica el md5 de `archivo:mtimeMs:size` del addon original,
// aplicado a los archivos .go del paquete (en Node eran los .js).
func helpComputeHash() (string, error) {
	entries, err := os.ReadDir(helpSourceDir())
	if err != nil {
		return "", err
	}

	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || name == "help.go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		files = append(files, name)
	}
	sort.Strings(files)

	h := md5.New()
	for _, name := range files {
		info, err := os.Stat(filepath.Join(helpSourceDir(), name))
		if err != nil {
			continue
		}
		mtimeMs := float64(info.ModTime().UnixNano()) / 1e6
		fmt.Fprintf(h, "%s:%s:%d", name, fmt.Sprintf("%v", mtimeMs), info.Size())
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func helpLoadCache() *helpCacheData {
	raw, err := os.ReadFile(helpCachePath())
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Error("❌ [help] Error leyendo caché:", "error", err)
		}
		return nil
	}
	var data helpCacheData
	if err := json.Unmarshal(raw, &data); err != nil {
		slog.Error("❌ [help] Caché inválido:", "error", err)
		return nil
	}
	if data.AliasMap == nil {
		data.AliasMap = make(map[string]helpGroup)
	}
	return &data
}

func helpGenerateCache(force bool) (*helpCacheData, error) {
	helpMu.Lock()
	if helpData != nil && !force {
		data := helpData
		helpMu.Unlock()
		return data, nil
	}
	helpMu.Unlock()

	if os.Getenv("DEEPSEEK_API_KEY") == "" {
		return nil, fmt.Errorf("DEEPSEEK_API_KEY no está configurada en .env")
	}

	hash, err := helpComputeHash()
	if err != nil {
		return nil, err
	}

	if !force {
		helpMu.Lock()
		if helpData != nil && helpData.AddonsHash == hash {
			data := helpData
			helpMu.Unlock()
			return data, nil
		}
		helpMu.Unlock()
	}

	dir := helpSourceDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var groups []helpGroup
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || e.Name() == "help.go" || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		code, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		parsed, err := helpAnalyzeFile(string(code))
		if err != nil {
			slog.Error("❌ Error generando ayuda para "+e.Name()+":", "error", err)
			continue
		}
		groups = append(groups, parsed...)
	}

	aliasMap := make(map[string]helpGroup)
	for _, group := range groups {
		for _, cmd := range group.Commands {
			key := strings.ToLower(strings.TrimPrefix(cmd, "."))
			aliasMap[key] = group
			aliasMap["."+key] = group
		}
	}

	data := &helpCacheData{
		GeneratedAt: time.Now().UnixMilli(),
		AddonsHash:  hash,
		Groups:      groups,
		AliasMap:    aliasMap,
	}

	helpMu.Lock()
	helpData = data
	helpMu.Unlock()

	if err := core.WriteJSON(helpCachePath(), data); err != nil {
		return nil, err
	}
	return data, nil
}

func helpAnalyzeFile(code string) ([]helpGroup, error) {
	body := map[string]any{
		"model": "deepseek-chat",
		"messages": []map[string]any{
			{"role": "system", "content": helpSystemPrompt},
			{"role": "user", "content": "Código del addon:\n\n" + code},
		},
		"temperature": 0.2,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, helpAPIURL, strings.NewReader(string(raw)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+os.Getenv("DEEPSEEK_API_KEY"))

	resp, err := helpHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("DeepSeek HTTP %d", resp.StatusCode)
	}

	var decoded struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, err
	}
	if len(decoded.Choices) == 0 {
		return nil, fmt.Errorf("respuesta sin choices")
	}

	content := decoded.Choices[0].Message.Content
	content = strings.ReplaceAll(content, "```json", "")
	content = strings.ReplaceAll(content, "```", "")
	content = strings.TrimSpace(content)

	var groups []helpGroup
	if err := json.Unmarshal([]byte(content), &groups); err != nil {
		return nil, err
	}
	return groups, nil
}

func helpRegenerateIfStale() chan struct{} {
	helpMu.Lock()
	if helpRegenCh != nil {
		ch := helpRegenCh
		helpMu.Unlock()
		return ch
	}
	if os.Getenv("DEEPSEEK_API_KEY") == "" {
		helpMu.Unlock()
		return nil
	}
	if helpData != nil {
		if current, err := helpComputeHash(); err == nil && helpData.AddonsHash == current {
			helpMu.Unlock()
			return nil
		}
	}
	ch := make(chan struct{})
	helpRegenCh = ch
	helpMu.Unlock()

	slog.Info("📝 Cambios detectados en addons — regenerando caché de ayuda...")
	go func() {
		defer func() {
			helpMu.Lock()
			helpRegenCh = nil
			helpMu.Unlock()
			close(ch)
		}()
		if _, err := helpGenerateCache(true); err != nil {
			slog.Error("❌ Error regenerando ayuda:", "error", err)
			return
		}
		slog.Info("✅ Caché de ayuda actualizado.")
	}()
	return ch
}

// Handle replica el handler del addon original.
func (Help) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}

	subCmd := ""
	if len(ctx.Args) > 0 {
		subCmd = strings.ToLower(ctx.Args[0])
	}

	if subCmd == "update" || subCmd == "--update" || subCmd == "-u" {
		_ = ctx.React("⏳")
		_ = os.Remove(helpCachePath())
		helpMu.Lock()
		helpData = nil
		helpMu.Unlock()
		if _, err := helpGenerateCache(true); err != nil {
			slog.Error("error actualizando ayuda", "error", err)
			_ = ctx.SendQuoted(core.TextMessage("❌ Falló la actualización."), ctx.Message)
			_ = ctx.React("❌")
			return nil
		}
		_ = ctx.SendQuoted(core.TextMessage("✅ Ayuda actualizada con éxito."), ctx.Message)
		_ = ctx.React("✅")
		return nil
	}

	helpMu.Lock()
	data := helpData
	helpMu.Unlock()
	if data == nil {
		data = helpLoadCache()
		helpMu.Lock()
		helpData = data
		helpMu.Unlock()
	}
	if data == nil {
		_ = ctx.React("⏳")
		generated, err := helpGenerateCache(false)
		if err != nil {
			return ctx.SendText("❌ Error generando ayuda. Intenta más tarde.")
		}
		data = generated
	}

	helpMu.Lock()
	regen := helpRegenCh
	helpMu.Unlock()
	if regen != nil {
		<-regen
		helpMu.Lock()
		data = helpData
		helpMu.Unlock()
	}

	commandQuery := ""
	if len(ctx.Args) > 0 {
		commandQuery = strings.ToLower(ctx.Args[0])
	}

	if commandQuery == "" {
		groups := append([]helpGroup(nil), data.Groups...)
		coll := collate.New(language.Spanish)
		sort.SliceStable(groups, func(i, j int) bool {
			a := ""
			b := ""
			if len(groups[i].Commands) > 0 {
				a = groups[i].Commands[0]
			}
			if len(groups[j].Commands) > 0 {
				b = groups[j].Commands[0]
			}
			return coll.CompareString(a, b) < 0
		})

		lines := make([]string, 0, len(groups))
		for _, group := range groups {
			quoted := make([]string, 0, len(group.Commands))
			for _, c := range group.Commands {
				quoted = append(quoted, "*"+c+"*")
			}
			lines = append(lines, "• "+strings.Join(quoted, "/")+": "+group.Preview)
		}
		menu := fmt.Sprintf("📚 *Menú de Comandos* (%d)\n\n%s\n\n💡 Usa *.help <comando>* para detalles.\n🔄 Usa *.help update* para regenerar la ayuda.",
			len(lines), strings.Join(lines, "\n"))
		if err := ctx.SendQuoted(core.TextMessage(menu), ctx.Message); err != nil {
			return err
		}
		return ctx.React("📚")
	}

	searchKey := commandQuery
	if !strings.HasPrefix(searchKey, ".") {
		searchKey = "." + searchKey
	}
	group, ok := data.AliasMap[searchKey]
	if !ok {
		fallback := strings.TrimPrefix(commandQuery, ".")
		group, ok = data.AliasMap[fallback]
	}
	if !ok {
		_ = ctx.SendText(`❓ No encontré ayuda para "` + commandQuery + `".`)
		return ctx.React("❓")
	}

	aliases := strings.Join(group.Commands, ", ")
	detail := "⚙️ *" + aliases + "*\n" + group.Technical
	if err := ctx.SendQuoted(core.TextMessage(detail), ctx.Message); err != nil {
		return err
	}
	return ctx.React("ℹ️")
}
