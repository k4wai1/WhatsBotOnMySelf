package addons

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"whatsbot-go/internal/core"
)

type ytdlRunner func(ctx context.Context, name string, args ...string) (string, string, error)

type ytdlExecOptions struct {
	Search         bool
	UseCookies     bool
	ClientFallback bool
	PlayerClient   string
	Minimal        bool
}

type ytdlCookie struct {
	Domain         string  `json:"domain"`
	Name           string  `json:"name"`
	Value          string  `json:"value"`
	Path           string  `json:"path"`
	Secure         bool    `json:"secure"`
	ExpirationDate float64 `json:"expirationDate"`
}

type ytdlFailure struct {
	Kind   string
	Detail string
	Stderr string
}

func (e ytdlFailure) Error() string { return e.Detail }

type ytdlClient struct {
	Run         ytdlRunner
	HTTPClient  *http.Client
	Clock       func() time.Time
	CookiePaths func() []string
	TmpBase     func() string
	MkdirTemp   func(string, string) (string, error)
	RemoveAll   func(string) error
	FindRuntime func() (deno, node string)
}

func NewYtdlClient() *ytdlClient {
	return &ytdlClient{
		Run:         YtdlRunCommandDefault,
		HTTPClient:  &http.Client{Timeout: 8 * time.Second},
		Clock:       time.Now,
		CookiePaths: YtdlCookiePathsDefault,
		TmpBase: func() string {
			return filepath.Join(os.TempDir(), "ytdl")
		},
		MkdirTemp:   os.MkdirTemp,
		RemoveAll:   os.RemoveAll,
		FindRuntime: YtdlFindRuntimesDefault,
	}
}

func (c *ytdlClient) now() time.Time {
	if c == nil || c.Clock == nil {
		return time.Now()
	}
	return c.Clock()
}

func (c *ytdlClient) tmpBase() string {
	if c == nil || c.TmpBase == nil {
		return filepath.Join(os.TempDir(), "ytdl")
	}
	return c.TmpBase()
}

// ─── ejecución de yt-dlp ────────────────────────────────────────────────────

func (c *ytdlClient) exec(args []string, opts ytdlExecOptions) (string, error) {
	run := c.Run
	if run == nil {
		run = YtdlRunCommandDefault
	}

	runArgs := append([]string(nil), args...)
	var cookiesFile string
	if opts.UseCookies {
		path, temp, err := c.prepareCookies()
		if err != nil {
			slog.Warn("ytdl: no se pudieron preparar las cookies, se continúa sin ellas", "error", err)
		} else {
			cookiesFile = path
			if temp && path != "" {
				defer func() { _ = os.Remove(path) }()
			}
		}
	}

	deno, node := "", ""
	if c != nil && c.FindRuntime != nil {
		deno, node = c.FindRuntime()
	}
	runArgs = ytdlBuildArgs(runArgs, opts, cookiesFile, deno, node)

	ctx, cancel := context.WithTimeout(context.Background(), ytdlExecTimeout)
	defer cancel()

	stdout, stderr, err := run(ctx, "yt-dlp", runArgs...)
	if err != nil {
		failure := ytdlClassifyFailure(stderr, err.Error(), err)
		logStderr := failure.Stderr
		if len(logStderr) > 4000 {
			logStderr = logStderr[:4000]
		}
		slog.Warn("ytdl: yt-dlp falló",
			"kind", failure.Kind,
			"detail", failure.Detail,
			"stderr", logStderr,
			"args", runArgs,
		)
		return "", failure
	}
	return stdout, nil
}

func ytdlBuildArgs(args []string, opts ytdlExecOptions, cookiesFile, denoPath, nodePath string) []string {
	out := make([]string, 0, len(args)+8)
	if opts.UseCookies && cookiesFile != "" {
		out = append(out, "--cookies", cookiesFile)
	}
	// Las búsquedas son plane y no necesitan componentes remotos ni runtimes JS.
	if !opts.Search && !opts.Minimal {
		out = append(out, "--remote-components", "ejs:github")
		switch {
		case strings.TrimSpace(denoPath) != "":
			out = append(out, "--js-runtimes", "deno:"+denoPath)
		case strings.TrimSpace(nodePath) != "":
			out = append(out, "--js-runtimes", "node:"+nodePath)
		}
	}
	if opts.PlayerClient != "" {
		out = append(out, "--extractor-args", "youtube:player_client="+opts.PlayerClient)
	} else if opts.ClientFallback {
		out = append(out, "--extractor-args", "youtube:player_client=tv,web_safari,default")
	}
	return append(out, args...)
}

func (c *ytdlClient) runWithFallback(args []string, opts ytdlExecOptions) (string, error) {
	useCookies := opts.UseCookies
	out, err := c.exec(args, ytdlExecOptions{Search: opts.Search, UseCookies: useCookies})
	if err == nil {
		return out, nil
	}
	kind := ytdlFailureKind(err)

	// 2) Reintento con clientes alternativos de YouTube.
	if kind != "private" {
		out, err = c.exec(args, ytdlExecOptions{Search: opts.Search, UseCookies: useCookies, ClientFallback: true})
		if err == nil {
			return out, nil
		}
		kind = ytdlFailureKind(err)
	}

	// 3) Último recurso: conservar las cookies y probar perfiles web_safari/web.
	// Quitar las cookies disparaba el bot-check de YouTube, así que ya no se hace.
	last := ytdlExecOptions{Search: opts.Search, UseCookies: useCookies, PlayerClient: "web_safari,web,default"}
	out, err = c.exec(args, last)
	if err == nil {
		return out, nil
	}

	// 4) Perfil mínimo: sin componentes remotos ni runtime JS (variante más estable).
	out, err = c.exec(args, ytdlExecOptions{UseCookies: useCookies, Minimal: true})
	if err == nil {
		return out, nil
	}
	return "", err
}

func ytdlFailureKind(err error) string {
	if err == nil {
		return ""
	}
	var failure *ytdlFailure
	if errors.As(err, &failure) {
		return failure.Kind
	}
	if v, ok := err.(ytdlFailure); ok {
		return v.Kind
	}
	return "other"
}

func ytdlErrorDetail(stderr, fallback string) string {
	for _, line := range strings.Split(stderr, "\n") {
		if idx := strings.Index(line, "ERROR:"); idx >= 0 {
			detail := strings.TrimSpace(line[idx:])
			if detail != "" {
				return ytdlTruncate(detail, 400)
			}
		}
	}
	detail := strings.TrimSpace(fallback)
	if detail == "" {
		detail = "error desconocido de yt-dlp"
	}
	return ytdlTruncate(detail, 400)
}

func ytdlClassifyFailure(stderr, fallback string, err error) *ytdlFailure {
	detail := ytdlErrorDetail(stderr, fallback)
	_ = err
	lower := strings.ToLower(stderr + "\n" + fallback)
	kind := "other"
	switch {
	case strings.Contains(lower, "private video") ||
		strings.Contains(lower, "video unavailable") ||
		strings.Contains(lower, "has made this video private"):
		kind = "private"
	case strings.Contains(lower, "sign in to confirm") ||
		strings.Contains(lower, "http error 403") ||
		strings.Contains(lower, "http 403") ||
		strings.Contains(lower, "bot check") ||
		strings.Contains(lower, "cookies file must be netscape"):
		kind = "cookies"
	case strings.Contains(lower, "requested format is not available"):
		kind = "format"
	case strings.Contains(lower, "n challenge") ||
		strings.Contains(lower, "n-challenge") ||
		strings.Contains(lower, "nchallenge") ||
		strings.Contains(lower, "challenge solving failed") ||
		strings.Contains(lower, "unable to extract"):
		kind = "challenge"
	case strings.Contains(lower, "econnreset") ||
		strings.Contains(lower, "etimedout") ||
		strings.Contains(lower, "connection reset") ||
		strings.Contains(lower, "premature close") ||
		strings.Contains(lower, "network is unreachable") ||
		strings.Contains(lower, "temporary failure in name resolution"):
		kind = "network"
	}
	return &ytdlFailure{Kind: kind, Detail: detail, Stderr: stderr}
}

// ─── API pública del cliente ────────────────────────────────────────────────

func (c *ytdlClient) FetchInfo(rawURL string) (ytdlInfo, error) {
	stdout, err := c.runWithFallback(
		[]string{"--dump-json", "--no-warnings", "--no-playlist", rawURL},
		ytdlExecOptions{UseCookies: true},
	)
	if err != nil {
		return ytdlInfo{}, err
	}
	var data ytdlFormat
	if err := json.Unmarshal([]byte(stdout), &data); err != nil {
		return ytdlInfo{}, err
	}
	title := strings.TrimSpace(data.Title)
	if title == "" {
		title = "Sin título"
	}
	webpage := strings.TrimSpace(data.WebpageURL)
	if webpage == "" {
		webpage = rawURL
	}
	return ytdlInfo{
		ID:         data.ID,
		Title:      title,
		Channel:    ytdlAutor(data),
		Thumbnail:  ytdlThumbnailURL(data),
		WebpageURL: webpage,
		Duration:   data.Duration,
		Formats:    data.Formats,
	}, nil
}

func (c *ytdlClient) Search(query string) ([]ytdlSearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("ytdl: búsqueda vacía")
	}
	stdout, err := c.exec(
		[]string{"ytsearch6:" + query, "--dump-json", "--no-warnings", "--flat-playlist", "--no-playlist"},
		ytdlExecOptions{Search: true},
	)
	if err != nil {
		return nil, err
	}
	out := make([]ytdlSearchResult, 0, 6)
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var data ytdlFormat
		if err := json.Unmarshal([]byte(line), &data); err != nil {
			continue
		}
		title := strings.TrimSpace(data.Title)
		if title == "" {
			title = "Sin título"
		}
		url := strings.TrimSpace(data.URL)
		if url == "" {
			url = strings.TrimSpace(data.WebpageURL)
		}
		if url == "" {
			url = "https://youtu.be/" + data.ID
		}
		out = append(out, ytdlSearchResult{
			ID:        data.ID,
			Title:     title,
			URL:       url,
			Channel:   ytdlAutor(data),
			Thumbnail: ytdlThumbnailURL(data),
			Duration:  data.Duration,
		})
		if len(out) >= 6 {
			break
		}
	}
	return out, nil
}

func ytdlThumbnailURL(d ytdlFormat) string {
	if thumb := strings.TrimSpace(d.Thumbnail); thumb != "" {
		return thumb
	}
	for i := len(d.Thumbnails) - 1; i >= 0; i-- {
		if thumb := strings.TrimSpace(d.Thumbnails[i].URL); thumb != "" {
			return thumb
		}
	}
	if strings.TrimSpace(d.ID) == "" {
		return ""
	}
	return "https://i.ytimg.com/vi/" + d.ID + "/hqdefault.jpg"
}

func (c *ytdlClient) Download(rawURL, spec, outputDir string, audio bool) (string, error) {
	args := []string{"-f", spec}
	if audio {
		args = append(args, "-x", "--audio-format", "mp3", "--audio-quality", "0")
	} else {
		args = append(args, "--merge-output-format", "mp4")
	}
	args = append(args,
		"--no-warnings", "--no-playlist", "--no-part", "--no-mtime",
		"-o", filepath.Join(outputDir, "%(title)s.%(ext)s"),
		rawURL,
	)
	if _, err := c.runWithFallback(args, ytdlExecOptions{UseCookies: true}); err != nil {
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
	return "", fmt.Errorf("ytdl: no se encontró el archivo descargado")
}

// ─── cookies ────────────────────────────────────────────────────────────────

func YtdlCookiePathsDefault() []string {
	return []string{
		core.DataPath("assets", "www.youtube.com_cookies.json"),
		core.DataPath("www.youtube.com_cookies.json"),
		core.DataPath("assets", "www.youtube.com_cookies.txt"),
		core.DataPath("www.youtube.com_cookies.txt"),
	}
}

func (c *ytdlClient) prepareCookies() (path string, temp bool, err error) {
	if c == nil || c.CookiePaths == nil {
		return "", false, nil
	}
	source := ""
	for _, candidate := range c.CookiePaths() {
		if candidate == "" {
			continue
		}
		if _, statErr := os.Stat(candidate); statErr == nil {
			source = candidate
			break
		}
	}
	if source == "" {
		return "", false, nil
	}

	raw, err := os.ReadFile(source)
	if err != nil {
		return "", false, err
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return "", false, nil
	}
	// Netscape tal cual: extensión .txt o cabecera con '#'.
	if strings.EqualFold(filepath.Ext(source), ".txt") || strings.HasPrefix(trimmed, "#") || !strings.HasPrefix(trimmed, "[") {
		return source, false, nil
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
		"# Generado automáticamente desde JSON por yt-dlp",
		"",
	}
	fallbackExpiry := c.now().Unix() + 10*365*86400
	clean := strings.NewReplacer("\t", "", "\n", "", "\r", "")
	for _, cookie := range cookies {
		if cookie.Domain == "" || cookie.Name == "" {
			continue
		}
		domainFlag := "FALSE"
		if strings.HasPrefix(cookie.Domain, ".") {
			domainFlag = "TRUE"
		}
		cookiePath := cookie.Path
		if cookiePath == "" {
			cookiePath = "/"
		}
		secureFlag := "FALSE"
		if cookie.Secure {
			secureFlag = "TRUE"
		}
		expiry := fallbackExpiry
		if cookie.ExpirationDate != 0 {
			expiry = int64(cookie.ExpirationDate)
		}
		lines = append(lines, fmt.Sprintf("%s\t%s\t%s\t%s\t%d\t%s\t%s",
			cookie.Domain, domainFlag, cookiePath, secureFlag, expiry, cookie.Name, clean.Replace(cookie.Value)))
	}
	if len(lines) <= 3 {
		return "", false, nil
	}

	dir := filepath.Join(c.tmpBase(), "cookies")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", false, err
	}
	out := filepath.Join(dir, fmt.Sprintf("cookies_%d.txt", c.now().UnixNano()))
	if err := os.WriteFile(out, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		return "", false, err
	}
	return out, true, nil
}

// ─── runtime y proceso ──────────────────────────────────────────────────────

func YtdlRunCommandDefault(ctx context.Context, name string, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = ytdlCommandEnv()
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func ytdlCommandEnv() []string {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = os.Getenv("HOME")
	}
	parts := make([]string, 0, 3)
	if home != "" {
		parts = append(parts, filepath.Join(home, ".deno", "bin"))
	}
	parts = append(parts, "/usr/local/bin")
	if path := os.Getenv("PATH"); path != "" {
		parts = append(parts, path)
	}
	env := make([]string, 0, len(os.Environ())+1)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "PATH=") {
			continue
		}
		env = append(env, entry)
	}
	return append(env, "PATH="+strings.Join(parts, ":"))
}

func YtdlFindRuntimesDefault() (deno, node string) {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = os.Getenv("HOME")
	}
	denoCandidates := make([]string, 0, 5)
	if home != "" {
		denoCandidates = append(denoCandidates, filepath.Join(home, ".deno", "bin", "deno"))
	}
	denoCandidates = append(denoCandidates, "/usr/local/bin/deno")
	if matches, _ := filepath.Glob("/home/*/.deno/bin/deno"); len(matches) > 0 {
		denoCandidates = append(denoCandidates, matches...)
	}
	deno = ytdlFindExecutable("deno", denoCandidates...)
	node = ytdlFindExecutable("node", "/usr/local/bin/node")
	return deno, node
}

func ytdlFindExecutable(name string, candidates ...string) string {
	if found, err := exec.LookPath(name); err == nil && found != "" {
		return found
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate
		}
	}
	return ""
}

// ─── errores y ZIP ──────────────────────────────────────────────────────────

func YtdlErrorText(err error, runtimeHint string) string {
	if err == nil {
		return "❌ Error desconocido."
	}
	detail := err.Error()
	kind := "other"
	var failure *ytdlFailure
	if errors.As(err, &failure) {
		kind = failure.Kind
		detail = failure.Detail
	} else if value, ok := err.(ytdlFailure); ok {
		kind = value.Kind
		detail = value.Detail
	}
	detail = ytdlTruncate(detail, 400)
	hint := strings.TrimSpace(runtimeHint)

	switch kind {
	case "challenge":
		advice := "Instala Deno (curl -fsSL https://deno.land/install.sh | sh) o Node.js y reinicia el bot."
		if hint != "" {
			advice = "Runtime detectado: " + hint + ". El challenge sigue fallando: actualiza yt-dlp."
		}
		return "❌ YouTube bloqueó la descarga (n-challenge).\n" + advice + "\nDetalle: " + detail
	case "format":
		return "❌ El formato solicitado no está disponible para este video.\nDetalle: " + detail
	case "cookies":
		return "❌ YouTube pide inicio de sesión o detectó un bot.\nExporta cookies Netscape a `assets/www.youtube.com_cookies.json`.\nDetalle: " + detail
	case "private":
		return "❌ El video es privado o no está disponible en tu región.\nDetalle: " + detail
	case "network":
		return "❌ Error de conexión con el proveedor del video. Reintenta en unos segundos.\nDetalle: " + detail
	default:
		return "❌ Error: " + detail
	}
}

func YtdlZipBytes(files map[string][]byte) ([]byte, error) {
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	names := make([]string, 0, len(files))
	for name := range files {
		if name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		entry, err := writer.CreateHeader(header)
		if err != nil {
			_ = writer.Close()
			return nil, err
		}
		if _, err := entry.Write(files[name]); err != nil {
			_ = writer.Close()
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
