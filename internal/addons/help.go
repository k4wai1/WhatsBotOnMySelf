package addons

import (
	"crypto/md5"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"go.mau.fi/whatsmeow"
	"golang.org/x/text/unicode/norm"

	"whatsbot-go/internal/core"
	"whatsbot-go/internal/store"
)

// Help porte de help.js: fichas simples generadas con DeepSeek, guardadas en
// help_cache.json y regeneradas automáticamente cuando cambian los addons.
type Help struct{}

// Commands cumple core.Addon.
func (Help) Commands() []string { return []string{"help", "ayuda"} }

const helpCacheVersion = 2

// Inyectables para pruebas (sin red ni depender del árbol real de addons).
var (
	helpHTTP        = &http.Client{Timeout: 60 * time.Second}
	helpAPIURL      = "https://api.deepseek.com/v1/chat/completions"
	helpCacheAlt    string
	helpSrcAlt      string
	helpLiveCommand = func() []string { return core.DefaultRouter().Commands() }
)

func helpCachePath() string {
	if helpCacheAlt != "" {
		return helpCacheAlt
	}
	return core.DataPath("help_cache.json")
}

// helpSourceFS embebe el código de los addons para que `help update` siga
// funcionando aunque el binario se ejecute sin el árbol de fuentes al lado.
// `*.go` también incluye help.go y *_test.go; helpSources los filtra.
//
//go:embed *.go
var helpSourceFS embed.FS

type helpSource struct {
	Name string
	Code []byte
}

// helpIsAnalyzed indica qué archivos se envían a DeepSeek.
func helpIsAnalyzed(name string) bool {
	return strings.HasSuffix(name, ".go") && name != "help.go" && !strings.HasSuffix(name, "_test.go")
}

// helpSources devuelve las fuentes a analizar: el directorio alternativo
// (tests) o, por defecto, las fuentes embebidas en el binario.
func helpSources() ([]helpSource, error) {
	var out []helpSource
	if helpSrcAlt != "" {
		entries, err := os.ReadDir(helpSrcAlt)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() || !helpIsAnalyzed(e.Name()) {
				continue
			}
			code, err := os.ReadFile(filepath.Join(helpSrcAlt, e.Name()))
			if err != nil {
				continue
			}
			out = append(out, helpSource{Name: e.Name(), Code: code})
		}
		return out, nil
	}

	entries, err := helpSourceFS.ReadDir(".")
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || !helpIsAnalyzed(e.Name()) {
			continue
		}
		code, err := helpSourceFS.ReadFile(e.Name())
		if err != nil {
			continue
		}
		out = append(out, helpSource{Name: e.Name(), Code: code})
	}
	return out, nil
}

// ─── Categorías fijas ──────────────────────────────────────────────────────

type helpCategory struct {
	Key   string
	Name  string
	Match []string
	Admin bool
}

var helpCategories = []helpCategory{
	{Key: "stickers", Name: "🎨 Stickers y Multimedia", Match: []string{"stickers", "sticker", "multimedia", "imagenes", "gif"}},
	{Key: "downloads", Name: "📥 Descargas", Match: []string{"descargas", "descarga", "downloads", "descargar"}},
	{Key: "ai", Name: "🤖 IA y Texto", Match: []string{"ia", "texto", "ai", "chat", "traduccion"}},
	{Key: "games", Name: "🎮 Juegos y Diversión", Match: []string{"juegos", "juego", "games", "diversion"}},
	{Key: "utils", Name: "🛠️ Utilidades", Match: []string{"utilidades", "utils", "utiles", "herramientas"}},
	{Key: "groups", Name: "🛡️ Grupos y Moderación", Match: []string{"grupos", "moderacion", "grupo"}},
	{Key: "admin", Name: "⚙️ Administración", Match: []string{"admin", "administracion"}, Admin: true},
}

func helpNormalize(s string) string {
	s = strings.ToLower(norm.NFD.String(s))
	var b strings.Builder
	for _, r := range s {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func helpCategoryByKey(key string) (helpCategory, bool) {
	q := helpNormalize(key)
	for _, c := range helpCategories {
		if helpNormalize(c.Key) == q || helpNormalize(c.Name) == q {
			return c, true
		}
		for _, m := range c.Match {
			if helpNormalize(m) == q {
				return c, true
			}
		}
	}
	return helpCategory{}, false
}

func helpResolveCategory(query string) (helpCategory, bool) {
	q := helpNormalize(query)
	if q == "" {
		return helpCategory{}, false
	}
	for _, c := range helpCategories {
		if helpNormalize(c.Key) == q || helpNormalize(c.Name) == q {
			return c, true
		}
		for _, m := range c.Match {
			if helpNormalize(m) == q {
				return c, true
			}
		}
	}
	for _, c := range helpCategories {
		full := helpNormalize(c.Name)
		if strings.Contains(full, q) || strings.Contains(q, full) {
			return c, true
		}
	}
	return helpCategory{}, false
}

func helpCanonicalCategory(raw string) string {
	if c, ok := helpCategoryByKey(raw); ok {
		return c.Key
	}
	return "utils"
}

func helpCategoryEmoji(key string) string {
	if c, ok := helpCategoryByKey(key); ok {
		if i := strings.IndexByte(c.Name, ' '); i > 0 {
			return c.Name[:i]
		}
		return c.Name
	}
	return "🤖"
}

var helpInternalCommands = map[string]bool{
	"debug": true, "debug-st": true, "debugpp": true, "debug-pp": true,
	"debug-info": true, "sdebug": true, "sysinfo": true, "sistema": true,
	"server": true, "fetch": true, "speed": true, "prefix": true,
	"setprefix": true, "getpp": true, "restart": true, "looger": true,
	"cor": true, "info": true, "files": true, "repo": true, "git": true,
}

func helpIsInternal(command string) bool {
	return helpInternalCommands[helpNormalize(command)]
}

// ─── Caché tipada ──────────────────────────────────────────────────────────

type helpEntry struct {
	Command     string   `json:"command"`
	Aliases     []string `json:"aliases"`
	Title       string   `json:"title"`
	Category    string   `json:"category"`
	Description string   `json:"description"`
	HowTo       []string `json:"how_to"`
	Options     []string `json:"options"`
	Examples    []string `json:"examples"`
	AdminOnly   bool     `json:"admin_only"`
	Hidden      bool     `json:"hidden"`
}

type helpCacheData struct {
	Version     int            `json:"version"`
	GeneratedAt int64          `json:"generatedAt"`
	AddonsHash  string         `json:"addonsHash"`
	Entries     []helpEntry    `json:"entries"`
	AliasMap    map[string]int `json:"aliasMap"`
}

var (
	helpMu      sync.Mutex
	helpData    *helpCacheData
	helpRegenCh chan struct{}
)

const helpSystemPrompt = `Eres el redactor de la ayuda de un bot de WhatsApp. Tu público es una persona cualquiera que NO sabe programar, NO sabe cómo funciona un bot y NO conoce comandos de Linux.

Devuelve ÚNICAMENTE un array JSON válido, sin texto ni bloques markdown, con un objeto por comando o grupo de comandos que hagan lo mismo:
[
  {
    "command": "sa",
    "aliases": ["ss", "audio"],
    "title": "Stickers con sonido",
    "category": "stickers",
    "description": "Una o dos frases muy simples sobre lo que hace, para una persona normal.",
    "how_to": ["Paso corto uno usando {p} si hace falta.", "Paso corto dos."],
    "options": ["crop — recorta en cuadrado", "circular — forma redonda"],
    "examples": ["{p}sa respondiendo a un video", "{p}sa crop 0:02 0:05"],
    "admin_only": false,
    "hidden": false
  }
]

REGLAS ESTRICTAS:
- Usa siempre {p} donde vaya el prefijo del comando (ej: {p}st, {p}yt). Nunca escribas un punto, una exclamación ni una barra fijo delante del comando.
- "command" es el nombre real del comando sin prefijo. "aliases" son los otros nombres que lo invocan.
- "title" máximo 30 caracteres. "description" máximo 2 frases. "how_to" de 1 a 3 pasos. "examples" de 1 a 3.
- En "options" incluye TODOS los subcomandos, modos y opciones reales que el usuario puede pasar; no los recortes. Si el comando tiene muchas, lista hasta unas 12 de forma compacta.
- Si una opción o subcomando tiene versión larga y corta (o sinónimos), muestra SIEMPRE todas juntas separadas por / antes del guion. Ejemplos: crop / c — recorta en cuadrado; estirar / e — estira la imagen al cuadrado; spherical / s — forma redonda; border / b — borde redondeado; reverse / r — reproduce al revés; high / h — más calidad; auto / wave / onda — genera una onda visual desde un audio; vertical / v — formato vertical; horizontal / h — formato horizontal; color / c — mantiene la foto a color.
- El encabezado de la ficha mostrará los alias del comando principal cuando existan; por eso completa siempre "aliases" con todos los nombres cortos y sinónimos reales.
- "category" debe ser exactamente una de estas claves: stickers, downloads, ai, games, utils, groups, admin.
- Marca "admin_only": true en comandos internos de diagnóstico, del servidor o del dueño (debug-st, debug-info, debug-pp, sdebug, sysinfo, sistema, server, fetch, speed, prefix, setprefix, restart, getpp, cor, info, files, repo, git). Marca "hidden": true en los que el usuario normal nunca debe ver.
- Si un límite importa al usuario, exprésalo en lenguaje humano: "videos de hasta 5 segundos", nunca en kilobytes ni nombres de variables.

PROHIBIDO en cualquier texto que devuelvas: ffmpeg, ffprobe, webpmux, yt-dlp, Protobuf, oneof, dual-blob, WebP, EXIF, API, JSON, SQLite, PM2, Linux, variables de entorno, SA_*, *_API_KEY, addon, handler, búsqueda binaria, sesión temporal, errores internos, reacciones técnicas ni palabras en inglés. Tampoco menciones archivos, carpetas ni comandos de terminal.
Si un addon no tiene comandos, responde [].`

func init() {
	core.Register(Help{})
	core.RegisterBackground("help", helpBackground)
}

// helpBackground replica el init(sock, store) del addon original.
func helpBackground(_ *whatsmeow.Client, _ *store.Store) error {
	helpMu.Lock()
	if helpData == nil {
		helpData = helpLoadCache()
	}
	helpMu.Unlock()

	helpRegenerateIfStale()
	return nil
}

// helpComputeHash calcula un md5 estable a partir del contenido de las fuentes
// analizadas. No usa mtime: las fuentes embebidas en el binario no conservan
// esa información.
func helpComputeHash() (string, error) {
	sources, err := helpSources()
	if err != nil {
		return "", err
	}
	h := md5.New()
	for _, src := range sources {
		fmt.Fprintf(h, "%s:%d:", src.Name, len(src.Code))
		h.Write(src.Code)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func helpBuildAliasMap(entries []helpEntry) map[string]int {
	out := make(map[string]int)
	for i, e := range entries {
		for _, k := range append([]string{e.Command}, e.Aliases...) {
			k = helpNormalize(k)
			if k == "" {
				continue
			}
			out[k] = i
			out["."+k] = i
		}
	}
	return out
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
	if data.Version != helpCacheVersion {
		return nil
	}
	if data.AliasMap == nil {
		data.AliasMap = helpBuildAliasMap(data.Entries)
	}
	return &data
}

func helpCleanCommand(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	return strings.TrimLeft(s, ".!/")
}

func helpLimit(list []string, max int) []string {
	if len(list) > max {
		list = append([]string(nil), list[:max]...)
	}
	out := make([]string, 0, len(list))
	for _, s := range list {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func helpNormalizeEntry(e helpEntry) helpEntry {
	e.Command = helpCleanCommand(e.Command)
	for i, a := range e.Aliases {
		e.Aliases[i] = helpCleanCommand(a)
	}
	e.Title = strings.TrimSpace(e.Title)
	e.Description = strings.TrimSpace(e.Description)
	e.Category = helpCanonicalCategory(e.Category)
	e.HowTo = helpLimit(e.HowTo, 3)
	e.Options = helpLimit(e.Options, 12)
	e.Examples = helpLimit(e.Examples, 3)
	if e.Title == "" {
		e.Title = e.Command
	}
	if e.Description == "" {
		e.Description = helpApplyPrefix("Muestra lo que puedes hacer con {p}"+e.Command+".", ".")
	}
	if helpIsInternal(e.Command) || e.Category == "admin" {
		e.AdminOnly = true
	}
	return e
}

func helpCommandExists(command string, live []string) bool {
	c := helpNormalize(command)
	for _, l := range live {
		if helpNormalize(l) == c {
			return true
		}
	}
	return false
}

func helpParseLLMEntries(content string) ([]helpEntry, error) {
	content = strings.ReplaceAll(content, "```json", "")
	content = strings.ReplaceAll(content, "```", "")
	content = strings.TrimSpace(content)
	if i := strings.IndexByte(content, '['); i >= 0 {
		if j := strings.LastIndexByte(content, ']'); j > i {
			content = content[i : j+1]
		}
	}
	var entries []helpEntry
	if err := json.Unmarshal([]byte(content), &entries); err != nil {
		return nil, err
	}
	return entries, nil
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

	sources, err := helpSources()
	if err != nil {
		return nil, err
	}
	live := helpLiveCommand()

	entries := make([]helpEntry, 0, len(sources))
	seen := make(map[string]bool)
	for _, src := range sources {
		parsed, err := helpAnalyzeFile(string(src.Code))
		if err != nil {
			slog.Error("❌ Error generando ayuda para "+src.Name+":", "error", err)
			continue
		}
		for _, e := range parsed {
			e = helpNormalizeEntry(e)
			if e.Command == "" || seen[e.Command] || !helpCommandExists(e.Command, live) {
				continue
			}
			seen[e.Command] = true
			entries = append(entries, e)
		}
	}

	sort.SliceStable(entries, func(i, j int) bool {
		ci, cj := entries[i].Category, entries[j].Category
		pi, pj := helpCategoryOrder(ci), helpCategoryOrder(cj)
		if pi != pj {
			return pi < pj
		}
		return entries[i].Command < entries[j].Command
	})

	data := &helpCacheData{
		Version:     helpCacheVersion,
		GeneratedAt: time.Now().UnixMilli(),
		AddonsHash:  hash,
		Entries:     entries,
		AliasMap:    helpBuildAliasMap(entries),
	}

	helpMu.Lock()
	helpData = data
	helpMu.Unlock()

	if err := core.WriteJSON(helpCachePath(), data); err != nil {
		return nil, err
	}
	return data, nil
}

func helpCategoryOrder(key string) int {
	for i, c := range helpCategories {
		if c.Key == key {
			return i
		}
	}
	return len(helpCategories)
}

func helpAnalyzeFile(code string) ([]helpEntry, error) {
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
	return helpParseLLMEntries(decoded.Choices[0].Message.Content)
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

// ─── Plantillas fijas ──────────────────────────────────────────────────────

func helpApplyPrefix(text, prefix string) string {
	if prefix == "" {
		prefix = "."
	}
	return strings.ReplaceAll(text, "{p}", prefix)
}

func helpCommandLabel(command, prefix string) string {
	return helpApplyPrefix("{p}"+command, prefix)
}

func helpEntriesInCategory(data *helpCacheData, cat helpCategory, includeAdmin bool) []helpEntry {
	if data == nil {
		return nil
	}
	var out []helpEntry
	for _, e := range data.Entries {
		if e.Hidden || (e.AdminOnly && !includeAdmin) {
			continue
		}
		if cat.Key == "admin" && !e.AdminOnly {
			continue
		}
		if helpCanonicalCategory(e.Category) == cat.Key {
			out = append(out, e)
		}
	}
	return out
}

func helpRenderMenu(data *helpCacheData, prefix string) string {
	var b strings.Builder
	b.WriteString("📖 Comandos del bot")
	for _, cat := range helpCategories {
		if cat.Admin {
			continue
		}
		entries := helpEntriesInCategory(data, cat, false)
		if len(entries) == 0 {
			continue
		}
		labels := make([]string, 0, len(entries))
		for _, e := range entries {
			labels = append(labels, helpCommandLabel(e.Command, prefix))
		}
		b.WriteString("\n\n" + cat.Name + "\n" + strings.Join(labels, " • "))
	}
	b.WriteString("\n\n" + helpApplyPrefix("💡 Escribe {p}help <comando> para ver cómo usarlo (ej: {p}help st).", prefix))
	b.WriteString("\n" + helpApplyPrefix("📂 También puedes ver una categoría: {p}help stickers.", prefix))
	return b.String()
}

func helpRenderCategory(data *helpCacheData, prefix string, cat helpCategory) string {
	entries := helpEntriesInCategory(data, cat, true)
	var b strings.Builder
	b.WriteString(cat.Name)
	if len(entries) == 0 {
		b.WriteString("\n\nAún no hay comandos en esta categoría.")
	} else {
		for _, e := range entries {
			b.WriteString("\n" + helpCommandLabel(e.Command, prefix) + " — " + e.Title)
		}
	}
	b.WriteString("\n\n" + helpApplyPrefix("💡 Escribe {p}help <comando> para ver cómo usarlo.", prefix))
	return b.String()
}

func helpRenderEntry(e helpEntry, prefix string) string {
	var b strings.Builder
	b.WriteString(helpCategoryEmoji(e.Category) + " " + helpCommandLabel(e.Command, prefix))
	aliases := make([]string, 0, len(e.Aliases))
	for _, a := range e.Aliases {
		if a != e.Command {
			aliases = append(aliases, helpCommandLabel(a, prefix))
		}
	}
	if len(aliases) > 0 {
		b.WriteString(" (también: " + strings.Join(aliases, ", ") + ")")
	}
	b.WriteString(" — " + e.Title)
	if e.Description != "" {
		b.WriteString("\n" + helpApplyPrefix(e.Description, prefix))
	}
	if len(e.HowTo) > 0 {
		b.WriteString("\n\nCómo usarlo:")
		for _, step := range e.HowTo {
			b.WriteString("\n• " + helpApplyPrefix(step, prefix))
		}
	}
	if len(e.Options) > 0 {
		b.WriteString("\n\nOpciones útiles:")
		for _, opt := range e.Options {
			b.WriteString("\n• " + helpApplyPrefix(opt, prefix))
		}
	}
	if len(e.Examples) > 0 {
		b.WriteString("\n\nEjemplos:")
		for _, ex := range e.Examples {
			b.WriteString("\n• " + helpApplyPrefix(ex, prefix))
		}
	}
	return b.String()
}

type helpResolution struct {
	EntryIndex int
	Category   *helpCategory
}

// helpResolveQuery prioriza comandos y alias sobre los nombres de categoría.
func helpResolveQuery(data *helpCacheData, query string) helpResolution {
	if data != nil {
		key := helpNormalize(query)
		if idx, ok := data.AliasMap[key]; ok && idx >= 0 && idx < len(data.Entries) {
			return helpResolution{EntryIndex: idx}
		}
	}
	if cat, ok := helpResolveCategory(query); ok {
		return helpResolution{EntryIndex: -1, Category: &cat}
	}
	return helpResolution{EntryIndex: -1}
}

func helpRenderUnknown(query, prefix string) string {
	return "❓ No encontré ayuda para \"" + query + "\".\n" +
		helpApplyPrefix("💡 Escribe {p}help para ver todo lo que puedes hacer.", prefix)
}

// Handle replica el handler del addon original.
func (Help) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}

	prefix := ctx.Prefix
	if prefix == "" {
		prefix = "."
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
			_ = ctx.SendQuoted(core.TextMessage("❌ No pude actualizar la ayuda ahora mismo."), ctx.Message)
			_ = ctx.React("❌")
			return nil
		}
		_ = ctx.SendQuoted(core.TextMessage("✅ La ayuda ya está al día."), ctx.Message)
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
			return ctx.SendText("❌ La ayuda no está lista todavía. Inténtalo en un momento.")
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

	if subCmd == "" {
		if err := ctx.SendQuoted(core.TextMessage(helpRenderMenu(data, prefix)), ctx.Message); err != nil {
			return err
		}
		return ctx.React("📖")
	}

	res := helpResolveQuery(data, subCmd)
	if res.EntryIndex >= 0 {
		if err := ctx.SendQuoted(core.TextMessage(helpRenderEntry(data.Entries[res.EntryIndex], prefix)), ctx.Message); err != nil {
			return err
		}
		return ctx.React("ℹ️")
	}
	if res.Category != nil {
		if err := ctx.SendQuoted(core.TextMessage(helpRenderCategory(data, prefix, *res.Category)), ctx.Message); err != nil {
			return err
		}
		return ctx.React("📂")
	}

	_ = ctx.SendQuoted(core.TextMessage(helpRenderUnknown(ctx.Args[0], prefix)), ctx.Message)
	return ctx.React("❓")
}
