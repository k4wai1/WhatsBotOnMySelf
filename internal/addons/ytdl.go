package addons

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"

	"whatsbot-go/internal/core"
	"whatsbot-go/internal/store"
)

func init() {
	core.Register(Ytdl{})
	core.RegisterBackground("ytdl", ytdlInit)
}

const (
	ytdlWarnSize       = 60 * 1024 * 1024
	ytdlSessionTTL     = 10 * time.Minute
	ytdlTmpMaxAge      = 3 * time.Hour
	ytdlUpdateInterval = 24 * time.Hour
)

var ytdlResTiers = []int{4320, 2160, 1440, 1080, 720, 480, 360, 240, 144}

// Inyectables para pruebas.
var (
	ytdlTmpBase     = func() string { return filepath.Join(os.TempDir(), "ytdl") }
	ytdlCookiePaths = ytdlCookiePathsDefault
	ytdlFindDeno    = ytdlFindDenoDefault
	ytdlRunCommand  = ytdlRunCommandDefault
	ytdlHTTPClient  = &http.Client{Timeout: 8 * time.Second}
	ytdlNow         = time.Now
	ytdlFetchInfo   = ytdlFetchVideoInfo
	ytdlSearchFn    = ytdlSearchYoutube
	ytdlDownloadFn  = ytdlDownloadMedia
	ytdlSendMenuFn  = ytdlSendMenuWithThumbnail
	ytdlMkdirTemp   = os.MkdirTemp
	ytdlRemoveAll   = os.RemoveAll
)

var (
	ytdlMu       sync.RWMutex
	ytdlSessions = make(map[string]*ytdlSession)
	ytdlSearches = make(map[string]*ytdlSearchSession)
)

type ytdlFormat struct {
	ID             string       `json:"id"`
	URL            string       `json:"url"`
	WebpageURL     string       `json:"webpage_url"`
	Title          string       `json:"title"`
	Duration       float64      `json:"duration"`
	Channel        string       `json:"channel"`
	ChannelTitle   string       `json:"channel_title"`
	ChannelID      string       `json:"channel_id"`
	Uploader       string       `json:"uploader"`
	UploaderID     string       `json:"uploader_id"`
	Thumbnail      string       `json:"thumbnail"`
	Vcodec         string       `json:"vcodec"`
	Acodec         string       `json:"acodec"`
	Height         int          `json:"height"`
	TBR            float64      `json:"tbr"`
	Filesize       int64        `json:"filesize"`
	FilesizeApprox int64        `json:"filesize_approx"`
	Formats        []ytdlFormat `json:"formats"`
}

type ytdlInfo struct {
	ID         string       `json:"id"`
	Title      string       `json:"title"`
	Duration   float64      `json:"duration"`
	Channel    string       `json:"channel"`
	Uploader   string       `json:"uploader"`
	Thumbnail  string       `json:"thumbnail"`
	WebpageURL string       `json:"webpage_url"`
	Formats    []ytdlFormat `json:"formats"`
}

type ytdlOption struct {
	Height     int
	Label      string
	FormatSpec string
	SizeBytes  int64
	IsAudio    bool
}

type ytdlSearchResult struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	URL       string  `json:"url"`
	Duration  float64 `json:"duration"`
	Channel   string  `json:"channel"`
	Thumbnail string  `json:"thumbnail"`
}

type ytdlSession struct {
	Info      ytdlInfo
	Options   []ytdlOption
	CreatedAt time.Time
}

type ytdlSearchSession struct {
	Results   []ytdlSearchResult
	CreatedAt time.Time
}

// Ytdl porta addons/ytdl.js.
type Ytdl struct{}

// Commands cumple core.Addon (incluye los alias originales).
func (Ytdl) Commands() []string { return []string{"ytdl", "ytdlp", "yt", "youtube"} }

// Handle replica el flujo interactivo de descarga/búsqueda.
func (Ytdl) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}
	sender := ctx.SenderJID().String()
	jid := ctx.ChatJID().String()
	if sender == "" {
		sender = jid
	}

	compressFlag := false
	cleanArgs := make([]string, 0, len(ctx.Args))
	for _, a := range ctx.Args {
		if a == "c" || a == "zip" || a == "comp" {
			compressFlag = true
			continue
		}
		cleanArgs = append(cleanArgs, a)
	}

	first := ""
	if len(cleanArgs) > 0 {
		first = cleanArgs[0]
	}

	// ─── CASO 1: cancelar ───────────────────────────────────────────────
	if strings.ToLower(first) == "cancel" {
		hadSession := ytdlCancel(sender)
		if hadSession {
			return ytdlReply(ctx, "✅ Descarga/Búsqueda cancelada.")
		}
		return ytdlReply(ctx, "❌ No tienes ninguna descarga activa.")
	}

	selection, hasSelection := ytdlParseInt(first)

	// ─── CASO 2: selección de una sesión de descarga ────────────────────
	if hasSelection {
		if session, ok := ytdlTakeSession(sender); ok {
			return ytdlHandleFormatSelection(ctx, session, selection, compressFlag)
		}
	}

	// ─── CASO 2b: selección de un resultado de búsqueda ─────────────────
	if hasSelection {
		if search, ok := ytdlTakeSearch(sender); ok {
			return ytdlHandleSearchSelection(ctx, sender, jid, search, selection)
		}
	}

	// ─── CASO 3: nueva descarga o búsqueda ──────────────────────────────
	rawURL := strings.TrimSpace(strings.Join(cleanArgs, " "))
	if rawURL == "" {
		if err := ctx.React("❓"); err != nil {
			return err
		}
		return ytdlReply(ctx, ytdlHelpText)
	}

	lower := strings.ToLower(rawURL)
	isDirect := strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")

	if !isDirect {
		if err := ctx.React("🔍"); err != nil {
			return err
		}
		results, err := ytdlSearchFn(rawURL)
		if err != nil {
			if reactErr := ctx.React("❌"); reactErr != nil {
				return reactErr
			}
			return ytdlReply(ctx, "❌ Error en la búsqueda: "+ytdlTruncate(err.Error(), 200))
		}
		if len(results) == 0 {
			if reactErr := ctx.React("❌"); reactErr != nil {
				return reactErr
			}
			return ytdlReply(ctx, "❌ No se encontraron resultados para esa búsqueda.")
		}
		ytdlStoreSearch(sender, results)
		if err := ytdlReply(ctx, ytdlSearchMenu(results)); err != nil {
			return err
		}
		return ctx.React("✅")
	}

	// Verificar cookies antes de empezar.
	cookiesAvailable := false
	for _, f := range ytdlCookiePaths() {
		if _, err := os.Stat(f); err == nil {
			cookiesAvailable = true
			break
		}
	}
	if !cookiesAvailable {
		if err := ctx.React("⏳"); err != nil {
			return err
		}
		if err := ytdlReply(ctx, "⚠️ No se encontró archivo de cookies.\nVideos restringidos pueden fallar.\nGuía: coloca `assets/www.youtube.com_cookies.json` con cookies exportadas (formato JSON o Netscape)."); err != nil {
			return err
		}
	} else {
		if err := ctx.React("⏳"); err != nil {
			return err
		}
	}

	info, err := ytdlFetchInfo(rawURL)
	if err != nil {
		return ytdlSendError(ctx, err)
	}
	options := ytdlBuildFormatOptions(info)
	if len(options) == 0 {
		if reactErr := ctx.React("❌"); reactErr != nil {
			return reactErr
		}
		return ytdlReply(ctx, "❌ No se encontraron formatos descargables para este video.")
	}
	ytdlStoreSession(sender, info, options)
	if err := ytdlSendMenuFn(ctx, jid, ytdlMenuText(info, options), info.Thumbnail); err != nil {
		return err
	}
	return ctx.React("✅")
}

func ytdlHandleFormatSelection(ctx *core.Context, session *ytdlSession, selection int, compress bool) error {
	options := session.Options
	if selection < 1 || selection > len(options) {
		if err := ctx.React("❓"); err != nil {
			return err
		}
		return ytdlReply(ctx, fmt.Sprintf("❌ Número inválido. Elige entre 1 y %d.\nUsa `.ytdl cancel` para salir.", len(options)))
	}
	selected := options[selection-1]
	if err := ctx.React("⏳"); err != nil {
		return err
	}

	tmpDir, err := ytdlMkdirTemp(ytdlTmpBase(), "dl-")
	if err != nil {
		return ytdlDownloadError(ctx, err)
	}
	defer ytdlRemoveAll(tmpDir)

	downloaded, err := ytdlDownloadFn(session.Info.WebpageURL, selected.FormatSpec, tmpDir)
	if err != nil {
		return ytdlDownloadError(ctx, err)
	}
	info, err := os.Stat(downloaded)
	if err != nil {
		return ytdlDownloadError(ctx, err)
	}
	fileSize := info.Size()

	sendPath := downloaded
	isCompressed := false
	if compress {
		zipPath := downloaded + ".zip"
		if _, _, err := ytdlRunCommand(context.Background(), "zip", "-9", "-j", zipPath, downloaded); err != nil {
			return ytdlDownloadError(ctx, err)
		}
		if st, err := os.Stat(zipPath); err == nil {
			fileSize = st.Size()
		}
		sendPath = zipPath
		isCompressed = true
	}

	data, err := os.ReadFile(sendPath)
	if err != nil {
		return ytdlDownloadError(ctx, err)
	}
	ext := strings.ToLower(filepath.Ext(sendPath))

	if isCompressed {
		if err := ctx.SendDocument(data, "video_"+session.Info.ID+".zip", ""); err != nil {
			return err
		}
	} else if selected.IsAudio {
		if err := ctx.SendAudio(data, "audio/mpeg", false, 0); err != nil {
			return err
		}
	} else if ext == ".mp4" || ext == ".webm" || ext == ".mkv" {
		actualExt := ext
		if ext == ".mkv" || ext == ".webm" {
			actualExt = ".mp4"
		}
		if fileSize > ytdlWarnSize {
			if err := ctx.SendDocument(data, "video_"+session.Info.ID+actualExt, ""); err != nil {
				return err
			}
		} else {
			if err := ctx.SendVideo(data, "📹 "+session.Info.Title, false); err != nil {
				return err
			}
		}
	} else {
		if err := ctx.SendDocument(data, filepath.Base(sendPath), ""); err != nil {
			return err
		}
	}
	return ctx.React("✅")
}

func ytdlHandleSearchSelection(ctx *core.Context, sender, jid string, search *ytdlSearchSession, selection int) error {
	results := search.Results
	if selection < 1 || selection > len(results) {
		if err := ctx.React("❓"); err != nil {
			return err
		}
		return ytdlReply(ctx, fmt.Sprintf("❌ Número inválido. Elige entre 1 y %d.\nUsa `.ytdl cancel` para salir.", len(results)))
	}
	selected := results[selection-1]
	if err := ctx.React("⏳"); err != nil {
		return err
	}
	info, err := ytdlFetchInfo(selected.URL)
	if err != nil {
		return ytdlSendError(ctx, err)
	}
	options := ytdlBuildFormatOptions(info)
	if len(options) == 0 {
		if reactErr := ctx.React("❌"); reactErr != nil {
			return reactErr
		}
		return ytdlReply(ctx, "❌ No se encontraron formatos descargables para este video.")
	}
	ytdlStoreSession(sender, info, options)
	if err := ytdlSendMenuFn(ctx, jid, ytdlMenuText(info, options), info.Thumbnail); err != nil {
		return err
	}
	return ctx.React("✅")
}

func ytdlDownloadError(ctx *core.Context, err error) error {
	if reactErr := ctx.React("❌"); reactErr != nil {
		return reactErr
	}
	return ytdlReply(ctx, "❌ Error descargando: "+ytdlTruncate(err.Error(), 250))
}

func ytdlReply(ctx *core.Context, text string) error {
	return ctx.SendQuoted(core.TextMessage(text), ctx.Message)
}

func ytdlSendError(ctx *core.Context, err error) error {
	if reactErr := ctx.React("❌"); reactErr != nil {
		return reactErr
	}
	return ytdlReply(ctx, ytdlErrorText(err.Error(), ytdlFindDeno()))
}

// ytdlErrorText replica los mensajes específicos del catch original.
func ytdlErrorText(errMsg, denoPath string) string {
	switch {
	case strings.Contains(errMsg, "Cookies file must be Netscape"):
		return "❌ El archivo de cookies tiene formato incorrecto.\nAsegúrate de usar la extensión \"cookies_localy.txt\" que exporta en formato Netscape, o coloca el archivo JSON y el bot lo convertirá automáticamente."
	case strings.Contains(errMsg, "HTTP Error 403") || strings.Contains(errMsg, "private") || strings.Contains(errMsg, "Private video"):
		return "❌ El video es privado o requiere inicio de sesión.\nExporta tus cookies con la extensión \"cookies_localy.txt\" y colócalas en `assets/www.youtube.com_cookies.json`"
	case strings.Contains(errMsg, "n challenge solving failed") || strings.Contains(errMsg, "Requested format is not available"):
		denoHint := "❌ YouTube requiere Deno para resolver el n-challenge.\n   Instalá Deno en el servidor:\n   curl -fsSL https://deno.land/install.sh | sh\n   Después reiniciá PM2: pm2 restart index\n   O usá !restart en WhatsApp si el bot está vivo."
		if denoPath != "" {
			denoHint = "⚠️ El n-challenge de YouTube falló incluso con Deno."
		}
		return "❌ YouTube bloqueó la descarga.\n" + denoHint
	case strings.Contains(errMsg, "No video results") || strings.Contains(errMsg, "Video unavailable"):
		return "❌ Video no disponible. Puede haber sido eliminado o restringido en tu región."
	case strings.Contains(errMsg, "UNICODE") || strings.Contains(errMsg, "Emoji"):
		return "❌ Error con caracteres especiales en el título. Prueba con otro video."
	case strings.Contains(errMsg, "Premature close") || strings.Contains(errMsg, "ECONNRESET") || strings.Contains(errMsg, "ETIMEDOUT"):
		return "❌ Error de conexión con YouTube. Reintenta en unos segundos."
	}
	return "❌ Error: " + ytdlTruncate(errMsg, 300)
}

// ─── Estado de sesiones ─────────────────────────────────────────────────────

func ytdlCancel(sender string) bool {
	ytdlMu.Lock()
	defer ytdlMu.Unlock()
	_, a := ytdlSessions[sender]
	_, b := ytdlSearches[sender]
	delete(ytdlSessions, sender)
	delete(ytdlSearches, sender)
	return a || b
}

func ytdlStoreSession(sender string, info ytdlInfo, options []ytdlOption) {
	ytdlMu.Lock()
	defer ytdlMu.Unlock()
	ytdlSessions[sender] = &ytdlSession{Info: info, Options: options, CreatedAt: ytdlNow()}
}

func ytdlTakeSession(sender string) (*ytdlSession, bool) {
	ytdlMu.Lock()
	defer ytdlMu.Unlock()
	s, ok := ytdlSessions[sender]
	if !ok {
		return nil, false
	}
	delete(ytdlSessions, sender)
	if ytdlNow().Sub(s.CreatedAt) > ytdlSessionTTL {
		return nil, false
	}
	return s, true
}

func ytdlStoreSearch(sender string, results []ytdlSearchResult) {
	ytdlMu.Lock()
	defer ytdlMu.Unlock()
	ytdlSearches[sender] = &ytdlSearchSession{Results: results, CreatedAt: ytdlNow()}
}

func ytdlTakeSearch(sender string) (*ytdlSearchSession, bool) {
	ytdlMu.Lock()
	defer ytdlMu.Unlock()
	s, ok := ytdlSearches[sender]
	if !ok {
		return nil, false
	}
	delete(ytdlSearches, sender)
	if ytdlNow().Sub(s.CreatedAt) > ytdlSessionTTL {
		return nil, false
	}
	return s, true
}

func ytdlCleanupSessions() {
	now := ytdlNow()
	ytdlMu.Lock()
	defer ytdlMu.Unlock()
	for k, s := range ytdlSessions {
		if now.Sub(s.CreatedAt) > ytdlSessionTTL {
			delete(ytdlSessions, k)
		}
	}
	for k, s := range ytdlSearches {
		if now.Sub(s.CreatedAt) > ytdlSessionTTL {
			delete(ytdlSearches, k)
		}
	}
}

// ─── Formato y menús ────────────────────────────────────────────────────────

func ytdlFmtDuration(secs float64) string {
	if secs <= 0 {
		return "?:??"
	}
	total := int(secs)
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

func ytdlFmtSize(bytes int64) string {
	if bytes <= 0 {
		return "? MB"
	}
	if bytes < 1024*1024 {
		return fmt.Sprintf("%.0f KB", float64(bytes)/1024)
	}
	return fmt.Sprintf("%.0f MB", float64(bytes)/(1024*1024))
}

// ytdlBuildFormatOptions replica buildFormatOptions().
func ytdlBuildFormatOptions(info ytdlInfo) []ytdlOption {
	formats := info.Formats
	duration := info.Duration
	used := make(map[int]bool)
	results := make([]ytdlOption, 0, len(ytdlResTiers)+1)

	videoFormats := make([]ytdlFormat, 0)
	for _, f := range formats {
		if f.Vcodec != "" && f.Vcodec != "none" && f.Height >= 144 {
			videoFormats = append(videoFormats, f)
		}
	}

	audioOnly := make([]ytdlFormat, 0)
	for _, f := range formats {
		if f.Acodec != "" && f.Acodec != "none" && (f.Vcodec == "" || f.Vcodec == "none") {
			audioOnly = append(audioOnly, f)
		}
	}
	bestAudio := ytdlFormat{}
	for _, f := range audioOnly {
		if f.TBR > bestAudio.TBR {
			bestAudio = f
		}
	}

	for _, tier := range ytdlResTiers {
		candidates := make([]ytdlFormat, 0)
		for _, f := range videoFormats {
			if f.Height <= tier && f.Height > tier-80 && !used[f.Height] {
				candidates = append(candidates, f)
			}
		}
		if len(candidates) == 0 {
			continue
		}
		best := candidates[0]
		for _, cur := range candidates[1:] {
			bestScore := ytdlScore(best)
			curScore := ytdlScore(cur)
			if curScore > bestScore {
				best = cur
			}
		}
		used[best.Height] = true

		totalBitrate := best.TBR + bestAudio.TBR
		est := best.Filesize
		if est == 0 {
			est = best.FilesizeApprox
		}
		if totalBitrate > 0 && duration > 0 {
			est = int64(totalBitrate * 1000 / 8 * duration)
		}
		hasAudio := best.Acodec != "" && best.Acodec != "none"
		spec := fmt.Sprintf("bestvideo[height<=%d]+bestaudio/best[height<=%d]", best.Height, best.Height)
		if hasAudio {
			spec = fmt.Sprintf("best[height<=%d]", best.Height)
		}
		label := fmt.Sprintf("%dp", best.Height)
		if best.Height >= 2160 {
			label = fmt.Sprintf("%dp (4K)", best.Height)
		} else if best.Height >= 1440 {
			label = fmt.Sprintf("%dp (2K)", best.Height)
		}
		results = append(results, ytdlOption{Height: best.Height, Label: label, FormatSpec: spec, SizeBytes: est})
	}

	audioSize := int64(0)
	if bestAudio.TBR > 0 && duration > 0 {
		audioSize = int64(bestAudio.TBR * 1000 / 8 * duration)
	}
	results = append(results, ytdlOption{Height: 0, Label: "🎵 Solo audio (MP3)", FormatSpec: "bestaudio/best", SizeBytes: audioSize, IsAudio: true})
	return results
}

func ytdlScore(f ytdlFormat) float64 {
	score := f.TBR
	if f.Acodec != "" && f.Acodec != "none" {
		score += 1000
	}
	return score
}

func ytdlMenuText(info ytdlInfo, options []ytdlOption) string {
	var lines []string
	lines = append(lines,
		"╭━━━━━━━━━━━━━━━━━━━━",
		"┃ 📹 *"+info.Title+"*",
		"┃ ⏱  "+ytdlFmtDuration(info.Duration),
		"┃ 📺 "+info.Channel,
		"╰━━━━━━━━━━━━━━━━━━━━",
		"",
		"*Selecciona resolución:*",
		"",
	)
	for i, opt := range options {
		num := i + 1
		size := ytdlFmtSize(opt.SizeBytes)
		if opt.IsAudio {
			lines = append(lines, fmt.Sprintf("%d 〉🎵 *%s*  — %s", num, opt.Label, size))
		} else {
			warn := ""
			if opt.SizeBytes > ytdlWarnSize {
				warn = " ⚠️"
			}
			lines = append(lines, fmt.Sprintf("%d 〉*%s*  — %s%s", num, opt.Label, size, warn))
		}
	}
	lines = append(lines,
		"",
		"Responde con:  `.ytdl <núm>`  (ej: `.ytdl 3`)",
		"Agrega `c` al final para comprimir en ZIP: `.ytdl 3 c`",
		"`.ytdl cancel` para salir",
	)
	return strings.Join(lines, "\n")
}

func ytdlSearchMenu(results []ytdlSearchResult) string {
	var lines []string
	lines = append(lines, "🔍 *Resultados de búsqueda:*", "")
	for i, r := range results {
		dur := ""
		if r.Duration > 0 {
			dur = "⏱ " + ytdlFmtDuration(r.Duration)
		}
		lines = append(lines, fmt.Sprintf("%d. *%s*", i+1, r.Title))
		lines = append(lines, fmt.Sprintf("   📺 %s  %s", r.Channel, dur))
	}
	lines = append(lines,
		"",
		"Responde con:  `.ytdl <núm>`  (ej: `.ytdl 2`)",
		"`.ytdl cancel` para salir",
		"_(Búsqueda expira en 10 min)_",
	)
	return strings.Join(lines, "\n")
}

var ytdlHelpText = strings.Join([]string{
	"📹 *YouTube / TikTok / X (Twitter) Downloader*",
	"",
	"Uso:",
	"  `!ytdl <url>` — Lista resoluciones disponibles (YouTube, TikTok, X/Twitter, etc.)",
	"  `!ytdl <núm>` — Descarga la opción seleccionada",
	"  `!ytdl cancel` — Cancela la descarga actual",
	"  `!ytdl c <url>` — Descarga + comprime en ZIP",
	"  `!ytdl <núm> c` — Selección + comprime en ZIP",
	"  `!ytdl <query>` — Buscar videos en YouTube",
	"",
	"Ejemplos:",
	"  `!ytdl https://youtube.com/watch?v=xxx`",
	"  `!ytdl https://www.tiktok.com/@user/video/xxx`",
	"  `!ytdl https://x.com/user/status/xxx`",
	"  `!ytdl 3`  (selecciona la opción 3)",
	"  `!ytdl c https://youtu.be/xxx`  (comprimido)",
	"  `!ytdl curso de python`  (búsqueda)",
}, "\n")

// ─── yt-dlp ─────────────────────────────────────────────────────────────────

func ytdlCookiePathsDefault() []string {
	return []string{
		core.DataPath("assets", "www.youtube.com_cookies.json"),
		core.DataPath("www.youtube.com_cookies.json"),
		core.DataPath("assets", "www.youtube.com_cookies.txt"),
		core.DataPath("www.youtube.com_cookies.txt"),
	}
}

func ytdlFindDenoDefault() string {
	if p, err := exec.LookPath("deno"); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	if home == "" {
		home = os.Getenv("HOME")
	}
	candidates := []string{
		"/usr/local/bin/deno",
	}
	if home != "" {
		candidates = append(candidates,
			filepath.Join(home, ".deno/bin/deno"),
			filepath.Join(home, ".local/bin/deno"),
		)
	}
	if matches, _ := filepath.Glob("/home/*/.deno/bin/deno"); len(matches) > 0 {
		candidates = append(candidates, matches...)
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return c
		}
	}
	return ""
}

// ytdlBuildArgs arma los argumentos base que el original agrega siempre.
func ytdlBuildArgs(args []string, cookiesFile, denoPath string) []string {
	base := []string{"--remote-components", "ejs:github"}
	if denoPath != "" {
		base = append(base, "--js-runtimes", "deno:"+denoPath)
	}
	if cookiesFile != "" {
		return append([]string{"--cookies", cookiesFile}, append(base, args...)...)
	}
	return append(base, args...)
}

type ytdlCookie struct {
	Domain         string  `json:"domain"`
	Name           string  `json:"name"`
	Value          string  `json:"value"`
	Path           string  `json:"path"`
	Secure         bool    `json:"secure"`
	ExpirationDate float64 `json:"expirationDate"`
}

// ytdlPrepareCookies convierte cookies JSON→Netscape si hace falta.
func ytdlPrepareCookies() (path string, temp bool, err error) {
	src := ""
	for _, f := range ytdlCookiePaths() {
		if _, statErr := os.Stat(f); statErr == nil {
			src = f
			break
		}
	}
	if src == "" {
		return "", false, nil
	}
	raw, err := os.ReadFile(src)
	if err != nil {
		return "", false, err
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return "", false, nil
	}

	if strings.ToLower(filepath.Ext(src)) == ".txt" {
		return src, false, nil
	}
	if strings.HasPrefix(trimmed, "#") {
		return src, false, nil
	}
	if !strings.HasPrefix(trimmed, "[") {
		return src, false, nil
	}

	var cookies []ytdlCookie
	if err := json.Unmarshal([]byte(trimmed), &cookies); err != nil {
		return "", false, nil
	}
	if len(cookies) == 0 {
		return "", false, nil
	}

	lines := []string{
		"# Netscape HTTP Cookie File",
		"# Generado automáticamente desde JSON por ytdl.js",
		"",
	}
	fallbackExpiry := ytdlNow().Unix() + 10*365*86400
	for _, c := range cookies {
		if c.Domain == "" || c.Name == "" {
			continue
		}
		domainFlag := "FALSE"
		if strings.HasPrefix(c.Domain, ".") {
			domainFlag = "TRUE"
		}
		cPath := c.Path
		if cPath == "" {
			cPath = "/"
		}
		secureFlag := "FALSE"
		if c.Secure {
			secureFlag = "TRUE"
		}
		expiry := fallbackExpiry
		if c.ExpirationDate != 0 {
			expiry = int64(c.ExpirationDate)
		}
		safeValue := strings.NewReplacer("\t", "", "\n", "", "\r", "").Replace(c.Value)
		lines = append(lines, fmt.Sprintf("%s\t%s\t%s\t%s\t%d\t%s\t%s", c.Domain, domainFlag, cPath, secureFlag, expiry, c.Name, safeValue))
	}
	if len(lines) <= 3 {
		return "", false, nil
	}

	dir := filepath.Join(ytdlTmpBase(), "cookies")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", false, err
	}
	out := filepath.Join(dir, fmt.Sprintf("cookies_%d.txt", ytdlNow().UnixNano()))
	if err := os.WriteFile(out, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		return "", false, err
	}
	return out, true, nil
}

func ytdlRunCommandDefault(ctx context.Context, name string, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func ytdlYtdlp(args []string) (string, error) {
	cookiesFile, isTemp, err := ytdlPrepareCookies()
	if err != nil {
		return "", err
	}
	if isTemp && cookiesFile != "" {
		defer os.Remove(cookiesFile)
	}
	allArgs := ytdlBuildArgs(args, cookiesFile, ytdlFindDeno())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	stdout, stderr, err := ytdlRunCommand(ctx, "yt-dlp", allArgs...)
	if err != nil {
		msg := ""
		for _, line := range strings.Split(stderr, "\n") {
			if strings.Contains(line, "ERROR:") {
				msg = line
				break
			}
		}
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s", ytdlTruncate(msg, 400))
	}
	return stdout, nil
}

// ytdlAutor prioriza los campos que yt-dlp rellena con más frecuencia.
func ytdlAutor(d ytdlFormat) string {
	for _, v := range []string{d.Channel, d.Uploader, d.ChannelTitle, d.UploaderID, d.ChannelID} {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return "Desconocido"
}

func ytdlFetchVideoInfo(rawURL string) (ytdlInfo, error) {
	stdout, err := ytdlYtdlp([]string{"--dump-json", "--no-warnings", "--no-playlist", rawURL})
	if err != nil {
		return ytdlInfo{}, err
	}
	var data ytdlFormat
	if err := json.Unmarshal([]byte(stdout), &data); err != nil {
		return ytdlInfo{}, err
	}
	title := data.Title
	if title == "" {
		title = "Sin título"
	}
	channel := ytdlAutor(data)
	webpage := data.WebpageURL
	if webpage == "" {
		webpage = rawURL
	}
	return ytdlInfo{
		ID:         data.ID,
		Title:      title,
		Duration:   data.Duration,
		Channel:    channel,
		Thumbnail:  data.Thumbnail,
		WebpageURL: webpage,
		Formats:    data.Formats,
	}, nil
}

func ytdlSearchYoutube(query string) ([]ytdlSearchResult, error) {
	stdout, err := ytdlYtdlp([]string{"ytsearch5:" + query, "--dump-json", "--no-warnings", "--flat-playlist", "--no-playlist"})
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	out := make([]ytdlSearchResult, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var d ytdlFormat
		if err := json.Unmarshal([]byte(line), &d); err != nil {
			continue
		}
		title := d.Title
		if title == "" {
			title = "Sin título"
		}
		channel := ytdlAutor(d)
		u := d.URL
		if u == "" {
			u = d.WebpageURL
		}
		if u == "" {
			u = "https://youtu.be/" + d.ID
		}
		out = append(out, ytdlSearchResult{
			ID:        d.ID,
			Title:     title,
			URL:       u,
			Duration:  d.Duration,
			Channel:   channel,
			Thumbnail: d.Thumbnail,
		})
	}
	return out, nil
}

func ytdlDownloadMedia(rawURL, formatSpec, outputDir string) (string, error) {
	outputTemplate := filepath.Join(outputDir, "%(title)s.%(ext)s")
	if _, err := ytdlYtdlp([]string{
		"-f", formatSpec,
		"--merge-output-format", "mp4",
		"--no-warnings",
		"--no-playlist",
		"--no-part",
		"--no-mtime",
		"-o", outputTemplate,
		rawURL,
	}); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == "." || name == ".." || strings.HasPrefix(name, ".") || entry.IsDir() {
			continue
		}
		return filepath.Join(outputDir, name), nil
	}
	return "", fmt.Errorf("No se encontró el archivo descargado.")
}

func ytdlSendMenuWithThumbnail(ctx *core.Context, jid, menuText, thumbnailURL string) error {
	if thumbnailURL != "" {
		resp, err := ytdlHTTPClient.Get(thumbnailURL)
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				data, readErr := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
				if readErr == nil && len(data) > 0 {
					if err := ctx.SendImage(data, menuText); err == nil {
						return nil
					}
				}
			}
		}
	}
	return ytdlReply(ctx, menuText)
}

func ytdlParseInt(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	i := 0
	if s[0] == '+' || s[0] == '-' {
		i++
	}
	start := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == start {
		return 0, false
	}
	n, err := strconv.Atoi(s[:i])
	if err != nil {
		return 0, false
	}
	return n, true
}

func ytdlTruncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// ─── Segundo plano: limpieza y auto-actualización ───────────────────────────

func ytdlInit(client *whatsmeow.Client, st *store.Store) error {
	go func() {
		ytdlSweepTmp()
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			ytdlSweepTmp()
		}
	}()
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			ytdlCleanupSessions()
		}
	}()
	go func() {
		time.Sleep(time.Minute)
		ytdlAutoUpdate()
		ticker := time.NewTicker(ytdlUpdateInterval)
		defer ticker.Stop()
		for range ticker.C {
			ytdlAutoUpdate()
		}
	}()
	return nil
}

func ytdlSweepTmp() {
	base := ytdlTmpBase()
	if err := os.MkdirAll(base, 0o755); err != nil {
		return
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		return
	}
	now := ytdlNow()
	var freed int64
	for _, entry := range entries {
		full := filepath.Join(base, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) > ytdlTmpMaxAge {
			freed += ytdlSizeOf(full)
			_ = os.RemoveAll(full)
		}
	}
	if freed > 0 {
		slog.Info("🧹 ytdl: sweep liberó temporales huérfanos", "bytes", freed)
	}
}

func ytdlSizeOf(path string) int64 {
	var total int64
	_ = filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}

func ytdlAutoUpdate() {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	stdout, _, err := ytdlRunCommand(ctx, "yt-dlp", "-U")
	if err != nil {
		slog.Warn("⚠️ ytdl: auto-actualización falló (no crítico)", "error", err)
		return
	}
	out := strings.TrimSpace(stdout)
	if strings.Contains(strings.ToLower(out), "pip or using the wheel") {
		ctx2, cancel2 := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel2()
		if _, _, err := ytdlRunCommand(ctx2, "python3", "-m", "pip", "install", "--user", "--break-system-packages", "-U", "yt-dlp"); err != nil {
			slog.Warn("⚠️ ytdl: actualización vía pip falló", "error", err)
			return
		}
		slog.Info("⬆️ ytdl: yt-dlp actualizado vía pip.")
		return
	}
	lines := strings.Split(out, "\n")
	slog.Info("⬆️ ytdl: " + lines[len(lines)-1])
}
