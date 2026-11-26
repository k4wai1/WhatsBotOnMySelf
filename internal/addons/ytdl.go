package addons

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	"whatsbot-go/internal/core"
	"whatsbot-go/internal/store"
)

func init() {
	core.Register(newYtdlAddon())
	core.RegisterBackground("ytdl", ytdlInit)
}

const (
	ytdlExecTimeout    = 5 * time.Minute
	ytdlThumbTimeout   = 5 * time.Second
	ytdlSessionTTL     = 10 * time.Minute
	ytdlTmpMaxAge      = 3 * time.Hour
	ytdlUpdateInterval = 24 * time.Hour
	ytdlWarnSize       = 60 * 1024 * 1024
)

var ytdlResTiers = []int{4320, 2160, 1440, 1080, 720, 480, 360, 240, 144}

type ytdlThumbnail struct {
	URL string `json:"url"`
}

type ytdlFormat struct {
	ID               string          `json:"id"`
	URL              string          `json:"url"`
	WebpageURL       string          `json:"webpage_url"`
	Title            string          `json:"title"`
	Duration         float64         `json:"duration"`
	Channel          string          `json:"channel"`
	ChannelTitle     string          `json:"channel_title"`
	ChannelID        string          `json:"channel_id"`
	Uploader         string          `json:"uploader"`
	UploaderID       string          `json:"uploader_id"`
	PlaylistChannel  string          `json:"playlist_channel"`
	PlaylistUploader string          `json:"playlist_uploader"`
	Creators         []string        `json:"creators"`
	Thumbnail        string          `json:"thumbnail"`
	Thumbnails       []ytdlThumbnail `json:"thumbnails"`
	Vcodec           string          `json:"vcodec"`
	Acodec           string          `json:"acodec"`
	Height           int             `json:"height"`
	TBR              float64         `json:"tbr"`
	Filesize         int64           `json:"filesize"`
	FilesizeApprox   int64           `json:"filesize_approx"`
	Formats          []ytdlFormat    `json:"formats"`
}

type ytdlInfo struct {
	ID         string       `json:"id"`
	Title      string       `json:"title"`
	Channel    string       `json:"channel"`
	Thumbnail  string       `json:"thumbnail"`
	WebpageURL string       `json:"webpage_url"`
	Duration   float64      `json:"duration"`
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
	Channel   string  `json:"channel"`
	Thumbnail string  `json:"thumbnail"`
	Duration  float64 `json:"duration"`
}

type ytdlSession struct {
	Info      ytdlInfo
	Options   []ytdlOption
	MenuMsgID types.MessageID
	CreatedAt time.Time
}

type ytdlSearchSession struct {
	Results   []ytdlSearchResult
	MenuMsgID types.MessageID
	CreatedAt time.Time
}

type ytdlService struct {
	Client   *ytdlClient
	Sessions *ytdlSessionStore
}

// Ytdl porta addons/ytdl.js. El receiver por valor es requerido por el
// registro genérico de addons (registry_test.go usa Ytdl{}).
type Ytdl struct {
	Svc *ytdlService
}

var (
	defaultYtdlOnce sync.Once
	defaultYtdlSvc  *ytdlService
)

func defaultYtdlService() *ytdlService {
	defaultYtdlOnce.Do(func() {
		defaultYtdlSvc = &ytdlService{
			Client:   NewYtdlClient(),
			Sessions: NewYtdlSessionStore(time.Now),
		}
	})
	return defaultYtdlSvc
}

func newYtdlAddon() *Ytdl { return &Ytdl{Svc: defaultYtdlService()} }

func (y Ytdl) service() *ytdlService {
	if y.Svc != nil {
		return y.Svc
	}
	return defaultYtdlService()
}

// Commands cumple core.Addon (incluye los alias originales).
func (Ytdl) Commands() []string { return []string{"ytdl", "ytdlp", "yt", "youtube"} }

// ytdlPrefix compone el prefijo dinámico del router (ej. ".ytdl", "!yt").
func ytdlPrefix(ctx *core.Context) string {
	if ctx == nil {
		return ".yt"
	}
	prefix := strings.TrimSpace(ctx.Prefix + ctx.Command)
	if prefix == "" {
		return ".yt"
	}
	return prefix
}

// Handle replica el flujo interactivo de descarga/búsqueda con menús revocables.
func (y Ytdl) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}
	svc := y.service()
	prefix := ytdlPrefix(ctx)

	sender := ctx.SenderJID().String()
	if sender == "" {
		sender = ctx.ChatJID().String()
	}

	compress := false
	cleanArgs := make([]string, 0, len(ctx.Args))
	for _, arg := range ctx.Args {
		if a := strings.ToLower(strings.TrimSpace(arg)); a == "c" || a == "zip" || a == "comp" {
			compress = true
			continue
		}
		cleanArgs = append(cleanArgs, arg)
	}
	first := ""
	if len(cleanArgs) > 0 {
		first = cleanArgs[0]
	}

	// ─── Cancelar ────────────────────────────────────────────────────────
	if strings.EqualFold(first, "cancel") {
		ids, had := svc.Sessions.Cancel(sender)
		for _, id := range ids {
			ytdlRevokeMenu(ctx, id)
		}
		if had {
			return ytdlReply(ctx, "✅ Descarga/Búsqueda cancelada.")
		}
		return ytdlReply(ctx, "❌ No tienes nada activo para cancelar.")
	}

	// ─── Selección numérica ──────────────────────────────────────────────
	selection, hasSelection := ytdlParseInt(first)
	if hasSelection {
		if session, ok := svc.Sessions.PeekDownload(sender); ok {
			if selection < 1 || selection > len(session.Options) {
				return ytdlInvalidSelection(ctx, len(session.Options), prefix)
			}
			ytdlRevokeMenu(ctx, session.MenuMsgID)
			if _, ok := svc.Sessions.TakeDownload(sender); !ok {
				return ytdlReply(ctx, "❌ Tu sesión expiró. Vuelve a intentarlo.")
			}
			return ytdlDownloadSelected(ctx, svc, session, selection, compress)
		}
		if search, ok := svc.Sessions.PeekSearch(sender); ok {
			if selection < 1 || selection > len(search.Results) {
				return ytdlInvalidSelection(ctx, len(search.Results), prefix)
			}
			chosen := search.Results[selection-1]
			ytdlRevokeMenu(ctx, search.MenuMsgID)
			if _, ok := svc.Sessions.TakeSearch(sender); !ok {
				return ytdlReply(ctx, "❌ Tu sesión expiró. Vuelve a intentarlo.")
			}
			return ytdlFetchAndStore(ctx, svc, sender, prefix, chosen.URL)
		}
	}

	raw := strings.TrimSpace(strings.Join(cleanArgs, " "))
	if raw == "" {
		if err := ctx.React("❓"); err != nil {
			return err
		}
		return ytdlReply(ctx, ytdlHelpText(prefix))
	}

	lower := strings.ToLower(raw)
	isDirect := strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")

	// ─── Búsqueda ────────────────────────────────────────────────────────
	if !isDirect {
		if err := ctx.React("🔍"); err != nil {
			return err
		}
		results, err := svc.Client.Search(raw)
		if err != nil {
			return ytdlSendError(ctx, svc.Client, err)
		}
		if len(results) == 0 {
			if reactErr := ctx.React("❌"); reactErr != nil {
				return reactErr
			}
			return ytdlReply(ctx, "❌ No se encontraron resultados para esa búsqueda.")
		}
		caption := ytdlSearchMenu(results, prefix)
		mosaic, mosaicErr := ytdlBuildMosaic(results, svc.Client.HTTPClient)
		var id types.MessageID
		if mosaicErr != nil || len(mosaic) == 0 {
			id, err = ctx.SendTextID(caption)
		} else {
			id, err = ctx.SendImageID(mosaic, caption)
		}
		if err != nil {
			return err
		}
		svc.Sessions.StoreSearch(sender, &ytdlSearchSession{
			Results:   results,
			MenuMsgID: id,
			CreatedAt: svc.Sessions.now(),
		})
		return ctx.React("✅")
	}

	// ─── URL directa ─────────────────────────────────────────────────────
	return ytdlFetchAndStore(ctx, svc, sender, prefix, raw)
}

func ytdlFetchAndStore(ctx *core.Context, svc *ytdlService, sender, prefix, rawURL string) error {
	if err := ctx.React("⏳"); err != nil {
		return err
	}
	info, err := svc.Client.FetchInfo(rawURL)
	if err != nil {
		return ytdlSendError(ctx, svc.Client, err)
	}
	options := ytdlBuildFormatOptions(info)
	if len(options) == 0 {
		if reactErr := ctx.React("❌"); reactErr != nil {
			return reactErr
		}
		return ytdlReply(ctx, "❌ No se encontraron formatos descargables para este video.")
	}
	menu := ytdlMenuText(info, options, prefix)
	msgID, err := ytdlSendMenu(ctx, svc.Client, menu, info.Thumbnail)
	if err != nil {
		return err
	}
	svc.Sessions.StoreDownload(sender, &ytdlSession{
		Info:      info,
		Options:   options,
		MenuMsgID: msgID,
		CreatedAt: svc.Sessions.now(),
	})
	return ctx.React("✅")
}

func ytdlDownloadSelected(ctx *core.Context, svc *ytdlService, session *ytdlSession, selection int, compress bool) error {
	selected := session.Options[selection-1]
	if err := ctx.React("⏳"); err != nil {
		return err
	}

	tmpDir, err := svc.Client.MkdirTemp(svc.Client.TmpBase(), "dl-")
	if err != nil {
		return ytdlDownloadError(ctx, err)
	}
	defer func() { _ = svc.Client.RemoveAll(tmpDir) }()

	downloaded, err := svc.Client.Download(session.Info.WebpageURL, selected.FormatSpec, tmpDir, selected.IsAudio)
	if err != nil {
		return ytdlDownloadError(ctx, err)
	}
	stat, err := os.Stat(downloaded)
	if err != nil {
		return ytdlDownloadError(ctx, err)
	}
	data, err := os.ReadFile(downloaded)
	if err != nil {
		return ytdlDownloadError(ctx, err)
	}

	if compress {
		name := filepath.Base(downloaded)
		zipData, err := YtdlZipBytes(map[string][]byte{name: data})
		if err != nil {
			return ytdlDownloadError(ctx, err)
		}
		if err := ctx.SendDocument(zipData, "video_"+session.Info.ID+".zip", ""); err != nil {
			return err
		}
		return ctx.React("✅")
	}

	if selected.IsAudio {
		if err := ctx.SendAudio(data, "audio/mpeg", false, 0); err != nil {
			return err
		}
	} else {
		ext := strings.ToLower(filepath.Ext(downloaded))
		switch ext {
		case ".mp4", ".webm", ".mkv":
			if stat.Size() > ytdlWarnSize {
				actualExt := ext
				if ext == ".mkv" || ext == ".webm" {
					actualExt = ".mp4"
				}
				if err := ctx.SendDocument(data, "video_"+session.Info.ID+actualExt, ""); err != nil {
					return err
				}
			} else {
				if err := ctx.SendVideo(data, "📹 "+session.Info.Title, false); err != nil {
					return err
				}
			}
		default:
			if err := ctx.SendDocument(data, filepath.Base(downloaded), ""); err != nil {
				return err
			}
		}
	}
	return ctx.React("✅")
}

func ytdlSendMenu(ctx *core.Context, client *ytdlClient, menu, thumbnailURL string) (types.MessageID, error) {
	if thumbnailURL != "" && client != nil && client.HTTPClient != nil {
		reqCtx, cancel := context.WithTimeout(context.Background(), ytdlThumbTimeout)
		defer cancel()
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, thumbnailURL, nil)
		if err == nil {
			resp, err := client.HTTPClient.Do(req)
			if err == nil {
				defer resp.Body.Close()
				if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
					data, readErr := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
					if readErr == nil && len(data) > 0 {
						if id, sendErr := ctx.SendImageID(data, menu); sendErr == nil {
							return id, nil
						}
					}
				}
			}
		}
	}
	return ctx.SendTextID(menu)
}

func ytdlRevokeMenu(ctx *core.Context, id types.MessageID) {
	if id == "" {
		return
	}
	if err := ctx.RevokeMessage(id); err != nil {
		slog.Debug("ytdl: no se pudo revocar el menú", "id", id, "error", err)
	}
}

func ytdlReply(ctx *core.Context, text string) error {
	return ctx.SendText(text)
}

func ytdlSendError(ctx *core.Context, client *ytdlClient, err error) error {
	if reactErr := ctx.React("❌"); reactErr != nil {
		return reactErr
	}
	return ytdlReply(ctx, YtdlErrorText(err, ytdlRuntimeHint(client)))
}

func ytdlRuntimeHint(client *ytdlClient) string {
	if client == nil || client.FindRuntime == nil {
		return ""
	}
	deno, node := client.FindRuntime()
	switch {
	case strings.TrimSpace(deno) != "":
		return "Deno: " + deno
	case strings.TrimSpace(node) != "":
		return "Node: " + node
	default:
		return ""
	}
}

func ytdlInvalidSelection(ctx *core.Context, max int, prefix string) error {
	if err := ctx.React("❓"); err != nil {
		return err
	}
	return ytdlReply(ctx, fmt.Sprintf("❌ Número inválido. Elige entre 1 y %d con `%s <núm>` o `%s cancel`.", max, prefix, prefix))
}

func ytdlDownloadError(ctx *core.Context, err error) error {
	if reactErr := ctx.React("❌"); reactErr != nil {
		return reactErr
	}
	return ytdlReply(ctx, "❌ Error descargando: "+ytdlTruncate(err.Error(), 250))
}

// ─── Helpers puros ──────────────────────────────────────────────────────────

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

func ytdlTruncate(s string, n int) string {
	if n < 0 {
		n = 0
	}
	if len(s) <= n {
		return s
	}
	return s[:n]
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

// ytdlAutor prioriza los campos que yt-dlp rellena con más frecuencia.
func ytdlAutor(d ytdlFormat) string {
	for _, v := range []string{d.Channel, d.Uploader, d.ChannelTitle, d.UploaderID, d.ChannelID, d.PlaylistChannel, d.PlaylistUploader} {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	for _, creator := range d.Creators {
		if s := strings.TrimSpace(creator); s != "" {
			return s
		}
	}
	return "YouTube"
}

func ytdlScore(f ytdlFormat) float64 {
	score := f.TBR
	if f.Acodec != "" && f.Acodec != "none" {
		score += 1000
	}
	return score
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
			if ytdlScore(cur) > ytdlScore(best) {
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

func ytdlMenuText(info ytdlInfo, options []ytdlOption, prefix string) string {
	lines := []string{
		"╭━━━━━━━━━━━━━━━━━━━━",
		"┃ 📹 *" + info.Title + "*",
		"┃ ⏱  " + ytdlFmtDuration(info.Duration),
		"┃ 📺 " + info.Channel,
		"╰━━━━━━━━━━━━━━━━━━━━",
		"",
		"*Selecciona resolución:*",
		"",
	}
	for i, opt := range options {
		num := i + 1
		size := ytdlFmtSize(opt.SizeBytes)
		if opt.IsAudio {
			lines = append(lines, fmt.Sprintf("%d 〉🎵 *%s*  — %s", num, opt.Label, size))
			continue
		}
		warn := ""
		if opt.SizeBytes > ytdlWarnSize {
			warn = " ⚠️"
		}
		lines = append(lines, fmt.Sprintf("%d 〉*%s*  — %s%s", num, opt.Label, size, warn))
	}
	lines = append(lines,
		"",
		fmt.Sprintf("Responde con: `%s <núm>`  (ej: `%s 3`)", prefix, prefix),
		fmt.Sprintf("Agrega `c` al final para comprimir en ZIP: `%s 3 c`", prefix),
		fmt.Sprintf("`%s cancel` para salir", prefix),
	)
	return strings.Join(lines, "\n")
}

func ytdlSearchMenu(results []ytdlSearchResult, prefix string) string {
	lines := []string{"🔍 *Resultados de búsqueda:*", ""}
	for i, r := range results {
		if i >= 6 {
			break
		}
		dur := ""
		if r.Duration > 0 {
			dur = " ⏱ " + ytdlFmtDuration(r.Duration)
		}
		channel := strings.TrimSpace(r.Channel)
		if channel == "" {
			channel = "YouTube"
		}
		lines = append(lines, fmt.Sprintf("%d. *%s*", i+1, r.Title))
		lines = append(lines, fmt.Sprintf("   📺 %s%s", channel, dur))
	}
	lines = append(lines,
		"",
		fmt.Sprintf("Responde `%s <núm>` (1..%d) · `%s cancel`", prefix, min(len(results), 6), prefix),
	)
	return strings.Join(lines, "\n")
}

func ytdlHelpText(prefix string) string {
	if strings.TrimSpace(prefix) == "" {
		prefix = ".yt"
	}
	return strings.Join([]string{
		"📹 *YouTube / TikTok / X (Twitter) Downloader*",
		"",
		"Uso:",
		fmt.Sprintf("  `%s <url>` — Lista resoluciones disponibles", prefix),
		fmt.Sprintf("  `%s <núm>` — Descarga la opción seleccionada", prefix),
		fmt.Sprintf("  `%s cancel` — Cancela la descarga actual", prefix),
		fmt.Sprintf("  `%s c <url>` — Descarga + comprime en ZIP", prefix),
		fmt.Sprintf("  `%s <núm> c` — Selección + comprime en ZIP", prefix),
		fmt.Sprintf("  `%s <query>` — Buscar videos en YouTube", prefix),
		"",
		"Ejemplos:",
		fmt.Sprintf("  `%s https://youtube.com/watch?v=xxx`", prefix),
		fmt.Sprintf("  `%s 3`  (selecciona la opción 3)", prefix),
		fmt.Sprintf("  `%s curso de python`  (búsqueda)", prefix),
	}, "\n")
}

// ─── Segundo plano: limpieza y auto-actualización ───────────────────────────

func ytdlInit(_ *whatsmeow.Client, _ *store.Store) error {
	svc := defaultYtdlService()
	go func() {
		ytdlSweepTmp(svc.Client)
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			ytdlSweepTmp(svc.Client)
		}
	}()
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			svc.Sessions.Cleanup()
		}
	}()
	go func() {
		time.Sleep(time.Minute)
		ytdlAutoUpdate(svc.Client)
		ticker := time.NewTicker(ytdlUpdateInterval)
		defer ticker.Stop()
		for range ticker.C {
			ytdlAutoUpdate(svc.Client)
		}
	}()
	return nil
}

func ytdlSweepTmp(client *ytdlClient) {
	base := client.TmpBase()
	if err := os.MkdirAll(base, 0o755); err != nil {
		return
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		return
	}
	now := client.Clock()
	var freed int64
	for _, entry := range entries {
		full := filepath.Join(base, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) > ytdlTmpMaxAge {
			freed += ytdlSizeOf(full)
			_ = client.RemoveAll(full)
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

func ytdlAutoUpdate(client *ytdlClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	stdout, _, err := client.Run(ctx, "yt-dlp", "-U")
	if err != nil {
		slog.Warn("⚠️ ytdl: auto-actualización falló (no crítico)", "error", err)
		return
	}
	out := strings.TrimSpace(stdout)
	if strings.Contains(strings.ToLower(out), "pip or using the wheel") {
		ctx2, cancel2 := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel2()
		if _, _, err := client.Run(ctx2, "python3", "-m", "pip", "install", "--user", "--break-system-packages", "-U", "yt-dlp"); err != nil {
			slog.Warn("⚠️ ytdl: actualización vía pip falló", "error", err)
			return
		}
		slog.Info("⬆️ ytdl: yt-dlp actualizado vía pip.")
		return
	}
	lines := strings.Split(out, "\n")
	slog.Info("⬆️ ytdl: " + lines[len(lines)-1])
}
