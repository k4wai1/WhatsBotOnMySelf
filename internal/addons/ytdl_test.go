package addons

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"whatsbot-go/internal/core"
)

// ─── utilidades de prueba (sin mutar globales) ──────────────────────────────

type ytdlCapture struct {
	mu        sync.Mutex
	allTexts  []string
	ids       []types.MessageID
	revoked   []types.MessageID
	reactions []string
	next      int
}

func (c *ytdlCapture) ctx(args ...string) *core.Context {
	return &core.Context{
		Command: "ytdl",
		Prefix:  ".",
		Args:    args,
		Message: &events.Message{Info: types.MessageInfo{MessageSource: types.MessageSource{
			Chat:   types.NewJID("1", types.DefaultUserServer),
			Sender: types.NewJID("2", types.DefaultUserServer),
		}}},
		ReactHook: func(emoji string) error {
			c.mu.Lock()
			defer c.mu.Unlock()
			c.reactions = append(c.reactions, emoji)
			return nil
		},
		SendHook: func(msg *waE2E.Message) error {
			c.mu.Lock()
			defer c.mu.Unlock()
			c.allTexts = append(c.allTexts, ytdlTestMessageText(msg))
			return nil
		},
		SendIDHook: func(msg *waE2E.Message) (types.MessageID, error) {
			c.mu.Lock()
			defer c.mu.Unlock()
			c.next++
			id := types.MessageID("id-" + strconv.Itoa(c.next))
			c.ids = append(c.ids, id)
			c.allTexts = append(c.allTexts, ytdlTestMessageText(msg))
			return id, nil
		},
		RevokeHook: func(id types.MessageID) error {
			c.mu.Lock()
			defer c.mu.Unlock()
			c.revoked = append(c.revoked, id)
			return nil
		},
	}
}

func (c *ytdlCapture) texts() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.allTexts...)
}

func (c *ytdlCapture) revokedIDs() []types.MessageID {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]types.MessageID(nil), c.revoked...)
}

func (c *ytdlCapture) sentIDs() []types.MessageID {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]types.MessageID(nil), c.ids...)
}

func ytdlTestMessageText(msg *waE2E.Message) string {
	switch {
	case msg == nil:
		return ""
	case msg.GetConversation() != "":
		return msg.GetConversation()
	case msg.GetExtendedTextMessage() != nil:
		return msg.GetExtendedTextMessage().GetText()
	case msg.GetImageMessage() != nil:
		return msg.GetImageMessage().GetCaption()
	case msg.GetVideoMessage() != nil:
		return msg.GetVideoMessage().GetCaption()
	default:
		return ""
	}
}

func ytdlTestJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 32, 18))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{R: 200, G: 40, B: 40, A: 255}), image.Point{}, draw.Src)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 70}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type ytdlRoundTripFunc func(*http.Request) (*http.Response, error)

func (f ytdlRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func ytdlFakeHTTPClient(t *testing.T) *http.Client {
	t.Helper()
	jpegBytes := ytdlTestJPEG(t)
	return &http.Client{Timeout: 2 * time.Second, Transport: ytdlRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       ioNopCloser{bytes.NewReader(jpegBytes)},
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})}
}

type ioNopCloser struct{ *bytes.Reader }

func (ioNopCloser) Close() error { return nil }

func ytdlTestClient(t *testing.T) *ytdlClient {
	t.Helper()
	client := NewYtdlClient()
	client.CookiePaths = func() []string { return nil }
	client.FindRuntime = func() (string, string) { return "", "" }
	client.HTTPClient = ytdlFakeHTTPClient(t)
	client.TmpBase = func() string { return filepath.Join(t.TempDir(), "ytdl") }
	return client
}

func ytdlTestService(t *testing.T, client *ytdlClient) *ytdlService {
	t.Helper()
	return &ytdlService{Client: client, Sessions: NewYtdlSessionStore(time.Now)}
}

func ytdlVideoJSON(id string) string {
	return fmt.Sprintf(`{"id":%q,"title":"Video %s","duration":125,"channel":"Canal","webpage_url":"https://youtu.be/%s","thumbnail":"https://img.test/%s.jpg","formats":[{"height":720,"vcodec":"avc1","acodec":"mp4a","tbr":2000},{"vcodec":"none","acodec":"opus","tbr":128}]}`, id, id, id, id)
}

func ytdlSearchJSON(id string) string {
	return fmt.Sprintf(`{"id":%q,"title":"Resultado %s","channel":"Canal","duration":60}`, id, id)
}

func ytdlWriteDownload(args []string) error {
	for i, arg := range args {
		if arg == "-o" && i+1 < len(args) {
			out := strings.ReplaceAll(args[i+1], "%(title)s.%(ext)s", "video.mp4")
			return os.WriteFile(out, []byte("contenido de prueba"), 0o644)
		}
	}
	return nil
}

// ─── tests ──────────────────────────────────────────────────────────────────

func TestYtdlCommands(t *testing.T) {
	got := (Ytdl{}).Commands()
	want := []string{"ytdl", "ytdlp", "yt", "youtube"}
	if len(got) != len(want) {
		t.Fatalf("comandos inesperados: %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("comando %d=%q, esperaba %q", i, got[i], want[i])
		}
	}
}

func TestYtdlFmtDurationYSize(t *testing.T) {
	durations := map[float64]string{0: "?:??", 65: "1:05", 3725: "1:02:05"}
	for input, want := range durations {
		if got := ytdlFmtDuration(input); got != want {
			t.Fatalf("ytdlFmtDuration(%v)=%q, esperaba %q", input, got, want)
		}
	}
	sizes := map[int64]string{
		0:               "? MB",
		512 * 1024:      "512 KB",
		5 * 1024 * 1024: "5 MB",
	}
	for input, want := range sizes {
		if got := ytdlFmtSize(input); got != want {
			t.Fatalf("ytdlFmtSize(%d)=%q, esperaba %q", input, got, want)
		}
	}
}

func TestYtdlBuildFormatOptions(t *testing.T) {
	info := ytdlInfo{
		Title:    "Video de prueba",
		Duration: 60,
		Formats: []ytdlFormat{
			{Height: 1080, Vcodec: "avc1", Acodec: "none", TBR: 4000},
			{Height: 720, Vcodec: "avc1", Acodec: "mp4a", TBR: 2000},
			{Vcodec: "none", Acodec: "opus", TBR: 128},
		},
	}
	options := ytdlBuildFormatOptions(info)
	if len(options) < 2 {
		t.Fatalf("opciones insuficientes: %#v", options)
	}
	labels := make([]string, 0, len(options))
	for _, opt := range options {
		labels = append(labels, opt.Label)
	}
	joined := strings.Join(labels, " | ")
	if !strings.Contains(joined, "1080p") || !strings.Contains(joined, "720p") {
		t.Fatalf("faltan resoluciones: %s", joined)
	}
	last := options[len(options)-1]
	if !last.IsAudio || last.FormatSpec != "bestaudio/best" {
		t.Fatalf("la última opción debe ser solo audio: %#v", last)
	}
	menu := ytdlMenuText(info, options, ".ytdl")
	if !strings.Contains(menu, "Video de prueba") || !strings.Contains(menu, "1080p") || !strings.Contains(menu, ".ytdl cancel") {
		t.Fatalf("menú inesperado:\n%s", menu)
	}
}

func TestYtdlPrefixDinamico(t *testing.T) {
	cases := []struct {
		prefix, command, want string
	}{
		{".", "ytdl", ".ytdl"},
		{"!", "youtube", "!youtube"},
		{"", "", ".yt"},
	}
	for _, tc := range cases {
		capture := new(ytdlCapture)
		ctx := capture.ctx()
		ctx.Prefix = tc.prefix
		ctx.Command = tc.command
		svc := ytdlTestService(t, ytdlTestClient(t))
		if err := (Ytdl{Svc: svc}).Handle(ctx); err != nil {
			t.Fatalf("Handle error: %v", err)
		}
		if got := strings.Join(capture.texts(), "\n"); !strings.Contains(got, tc.want) {
			t.Fatalf("prefijo %q: la ayuda no contiene %q:\n%s", tc.prefix, tc.want, got)
		}
	}
}

func TestYtdlMosaico1200x800JPEGConFalloParcial(t *testing.T) {
	jpegBytes := ytdlTestJPEG(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "falla") {
			http.Error(w, "no disponible", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(jpegBytes)
	}))
	defer server.Close()

	results := make([]ytdlSearchResult, 6)
	for i := range results {
		name := "ok.jpg"
		if i == 2 {
			name = "falla.jpg"
		}
		results[i] = ytdlSearchResult{
			ID:        fmt.Sprintf("id%d", i),
			Title:     fmt.Sprintf("Resultado %d", i+1),
			Channel:   "Canal",
			Duration:  90,
			Thumbnail: server.URL + "/" + name,
		}
	}
	mosaic, err := ytdlBuildMosaic(results, &http.Client{Timeout: 2 * time.Second})
	if err != nil {
		t.Fatalf("ytdlBuildMosaic error: %v", err)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(mosaic))
	if err != nil {
		t.Fatalf("mosaico no es una imagen válida: %v", err)
	}
	if format != "jpeg" || cfg.Width != 1200 || cfg.Height != 800 {
		t.Fatalf("mosaico=%s %dx%d, esperaba jpeg 1200x800", format, cfg.Width, cfg.Height)
	}
}

func TestYtdlSeleccionFueraDeRangoNoDestruyeSesion(t *testing.T) {
	capture := new(ytdlCapture)
	ctx := capture.ctx("9")
	svc := ytdlTestService(t, ytdlTestClient(t))
	session := &ytdlSession{
		Info:      ytdlInfo{Title: "Video", WebpageURL: "https://youtu.be/x"},
		Options:   []ytdlOption{{Label: "720p"}, {Label: "🎵 Solo audio"}},
		MenuMsgID: "menu-1",
		CreatedAt: time.Now(),
	}
	svc.Sessions.StoreDownload(ctx.SenderJID().String(), session)

	if err := (Ytdl{Svc: svc}).Handle(ctx); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if got := strings.Join(capture.texts(), "\n"); !strings.Contains(got, "inválido") {
		t.Fatalf("falta el aviso de número inválido: %s", got)
	}
	if revoked := capture.revokedIDs(); len(revoked) != 0 {
		t.Fatalf("no se debía revocar el menú: %#v", revoked)
	}
	peeked, ok := svc.Sessions.PeekDownload(ctx.SenderJID().String())
	if !ok || peeked.MenuMsgID != "menu-1" {
		t.Fatalf("la sesión fue destruida: %#v", peeked)
	}
}

func TestYtdlCicloRevocadoBusquedaMenuDescargaYCancel(t *testing.T) {
	client := ytdlTestClient(t)
	client.Run = func(_ context.Context, _ string, args ...string) (string, string, error) {
		joined := strings.Join(args, " ")
		switch {
		case strings.Contains(joined, "ytsearch6:"):
			return ytdlSearchJSON("a") + "\n" + ytdlSearchJSON("b"), "", nil
		case strings.Contains(joined, "--dump-json"):
			return ytdlVideoJSON("a"), "", nil
		case strings.Contains(joined, "-f"):
			if err := ytdlWriteDownload(args); err != nil {
				return "", "", err
			}
			return "", "", nil
		default:
			return "", "", nil
		}
	}
	svc := ytdlTestService(t, client)
	addon := Ytdl{Svc: svc}
	capture := new(ytdlCapture)

	if err := addon.Handle(capture.ctx("curso de python")); err != nil {
		t.Fatalf("búsqueda: %v", err)
	}
	if ids := capture.sentIDs(); len(ids) != 1 {
		t.Fatalf("se esperaba un menú de búsqueda: %#v", ids)
	}
	if got := strings.Join(capture.texts(), "\n"); !strings.Contains(got, "Resultado a") {
		t.Fatalf("caption inesperado: %s", got)
	}

	if err := addon.Handle(capture.ctx("1")); err != nil {
		t.Fatalf("selección de búsqueda: %v", err)
	}
	if ids := capture.sentIDs(); len(ids) != 2 {
		t.Fatalf("se esperaba un menú de resoluciones: %#v", ids)
	}
	if revoked := capture.revokedIDs(); len(revoked) != 1 || revoked[0] != "id-1" {
		t.Fatalf("revocación del menú de búsqueda: %#v", revoked)
	}

	if err := addon.Handle(capture.ctx("1")); err != nil {
		t.Fatalf("selección de formato: %v", err)
	}
	if revoked := capture.revokedIDs(); len(revoked) != 2 || revoked[1] != "id-2" {
		t.Fatalf("revocación del menú de resoluciones: %#v", revoked)
	}

	sender := capture.ctx().SenderJID().String()
	svc.Sessions.StoreSearch(sender, &ytdlSearchSession{
		Results:   []ytdlSearchResult{{ID: "z", Title: "Pendiente"}},
		MenuMsgID: "menu-cancel",
		CreatedAt: time.Now(),
	})
	if err := addon.Handle(capture.ctx("cancel")); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if revoked := capture.revokedIDs(); len(revoked) != 3 || revoked[2] != "menu-cancel" {
		t.Fatalf("revocación al cancelar: %#v", revoked)
	}
	if got := strings.Join(capture.texts(), "\n"); !strings.Contains(got, "cancelada") {
		t.Fatalf("confirmación ausente: %s", got)
	}
}

func TestYtdlFallbackChallengeYFormato(t *testing.T) {
	newCookieFile := func(t *testing.T) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "cookies.txt")
		if err := os.WriteFile(path, []byte("# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tFALSE\t0\tSID\tx\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}

	t.Run("challance_con_cookies_y_extractor_args", func(t *testing.T) {
		client := ytdlTestClient(t)
		client.CookiePaths = func() []string { return []string{newCookieFile(t)} }
		var calls [][]string
		attempt := 0
		client.Run = func(_ context.Context, _ string, args ...string) (string, string, error) {
			calls = append(calls, append([]string(nil), args...))
			attempt++
			if attempt < 3 {
				return "", "ERROR: [youtube] n challenge solving failed", fmt.Errorf("exit status 1")
			}
			return ytdlVideoJSON("abc"), "", nil
		}
		info, err := client.FetchInfo("https://youtu.be/abc")
		if err != nil {
			t.Fatalf("FetchInfo error: %v", err)
		}
		if info.ID != "abc" {
			t.Fatalf("info inesperada: %#v", info)
		}
		if len(calls) != 3 {
			t.Fatalf("esperaba 3 intentos, hubo %d", len(calls))
		}
		if contains(calls[0], "--extractor-args") || !contains(calls[0], "--cookies") {
			t.Fatalf("primer intento inesperado: %#v", calls[0])
		}
		if !contains(calls[1], "--extractor-args") || !contains(calls[1], "--cookies") {
			t.Fatalf("segundo intento inesperado: %#v", calls[1])
		}
		if !contains(calls[2], "--extractor-args") || !contains(calls[2], "--cookies") {
			t.Fatalf("último intento no debía quitar las cookies: %#v", calls[2])
		}
		if !strings.Contains(strings.Join(calls[2], " "), "player_client=web_safari,web,default") {
			t.Fatalf("falta el perfil web en el último intento: %#v", calls[2])
		}
		if !strings.Contains(strings.Join(calls[1], " "), "player_client=tv,web_safari,default") {
			t.Fatalf("falta el fallback de clientes: %#v", calls[1])
		}
	})

	t.Run("formato_no_se_clasifica_como_challenge", func(t *testing.T) {
		failure := ytdlClassifyFailure("ERROR: Requested format is not available", "exit status 1", nil)
		if failure.Kind != "format" {
			t.Fatalf("kind=%q, esperaba format", failure.Kind)
		}
		if strings.Contains(YtdlErrorText(failure, ""), "n-challenge") {
			t.Fatalf("el error de formato se mezcló con challenge: %s", YtdlErrorText(failure, ""))
		}

		client := ytdlTestClient(t)
		var calls [][]string
		attempt := 0
		client.Run = func(_ context.Context, _ string, args ...string) (string, string, error) {
			calls = append(calls, append([]string(nil), args...))
			attempt++
			if attempt == 1 {
				return "", "ERROR: Requested format is not available", fmt.Errorf("exit status 1")
			}
			return ytdlVideoJSON("xyz"), "", nil
		}
		if _, err := client.FetchInfo("https://youtu.be/xyz"); err != nil {
			t.Fatalf("FetchInfo error: %v", err)
		}
		if len(calls) != 2 || !contains(calls[1], "--extractor-args") {
			t.Fatalf("fallback de formato inesperado: %#v", calls)
		}
	})
}

func contains(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func TestYtdlSoloAudioUsaMP3(t *testing.T) {
	client := ytdlTestClient(t)
	var captured []string
	client.Run = func(_ context.Context, _ string, args ...string) (string, string, error) {
		captured = append([]string(nil), args...)
		if err := ytdlWriteDownload(args); err != nil {
			return "", "", err
		}
		return "", "", nil
	}
	dir := t.TempDir()
	downloaded, err := client.Download("https://youtu.be/abc", "bestaudio/best", dir, true)
	if err != nil {
		t.Fatalf("Download error: %v", err)
	}
	if downloaded == "" {
		t.Fatal("no devolvió el archivo descargado")
	}
	for _, want := range []string{"-x", "--audio-format", "mp3", "--audio-quality", "0"} {
		if !contains(captured, want) {
			t.Fatalf("falta %q en los argumentos: %#v", want, captured)
		}
	}
	if contains(captured, "--merge-output-format") {
		t.Fatalf("el audio no debe llevar merge-output-format: %#v", captured)
	}
}

func TestYtdlZipBytes(t *testing.T) {
	data, err := YtdlZipBytes(map[string][]byte{"video.mp4": []byte("contenido zip")})
	if err != nil {
		t.Fatalf("YtdlZipBytes error: %v", err)
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("ZIP inválido: %v", err)
	}
	if len(reader.File) != 1 || reader.File[0].Name != "video.mp4" {
		t.Fatalf("entradas inesperadas: %#v", reader.File)
	}
	entry, err := reader.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	defer entry.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(entry); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "contenido zip" {
		t.Fatalf("contenido inesperado: %q", buf.String())
	}
}

func TestYtdlAutorFallbackYouTube(t *testing.T) {
	cases := []struct {
		format ytdlFormat
		want   string
	}{
		{ytdlFormat{Channel: "Saintontas", Uploader: "Otro"}, "Saintontas"},
		{ytdlFormat{ChannelTitle: "Wuthering Waves"}, "Wuthering Waves"},
		{ytdlFormat{UploaderID: "@wutheringwaves"}, "@wutheringwaves"},
		{ytdlFormat{ChannelID: "UC123"}, "UC123"},
		{ytdlFormat{PlaylistChannel: "IGN"}, "IGN"},
		{ytdlFormat{PlaylistUploader: "Wuthering Waves"}, "Wuthering Waves"},
		{ytdlFormat{Creators: []string{"Canal Creador"}}, "Canal Creador"},
		{ytdlFormat{}, "YouTube"},
	}
	for i, tc := range cases {
		if got := ytdlAutor(tc.format); got != tc.want {
			t.Fatalf("caso %d: ytdlAutor=%q, esperaba %q", i, got, tc.want)
		}
	}
}

func TestYtdlThumbnailFallbackHQDefault(t *testing.T) {
	client := ytdlTestClient(t)
	client.Run = func(_ context.Context, _ string, _ ...string) (string, string, error) {
		return `{"id":"abc","title":"Sin miniatura"}`, "", nil
	}
	results, err := client.Search("prueba")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("resultados inesperados: %#v", results)
	}
	want := "https://i.ytimg.com/vi/abc/hqdefault.jpg"
	if results[0].Thumbnail != want {
		t.Fatalf("thumbnail=%q, esperaba %q", results[0].Thumbnail, want)
	}
	if results[0].Channel != "YouTube" {
		t.Fatalf("autor=%q, esperaba YouTube", results[0].Channel)
	}
}

func TestYtdlCookiesJSONANetscape(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "cookies.json")
	fixed := time.Unix(1700000000, 0)
	raw := fmt.Sprintf(`[{"domain":".youtube.com","name":"SID","value":"a\tb\nc","path":"/","secure":true}]`)
	if err := os.WriteFile(source, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	client := ytdlTestClient(t)
	client.CookiePaths = func() []string { return []string{source} }
	client.TmpBase = func() string { return dir }
	client.Clock = func() time.Time { return fixed }

	path, temp, err := client.prepareCookies()
	if err != nil {
		t.Fatalf("prepareCookies error: %v", err)
	}
	if !temp || path == "" {
		t.Fatalf("esperaba un archivo temporal: path=%q temp=%v", path, temp)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	wantExpiry := fixed.Unix() + 10*365*86400
	if !strings.Contains(text, fmt.Sprintf(".youtube.com\tTRUE\t/\tTRUE\t%d\tSID\tabc\n", wantExpiry)) {
		t.Fatalf("cookies convertidos incorrectamente: %q", text)
	}
}

func TestYtdlFallbackPerfilMinimo(t *testing.T) {
	client := ytdlTestClient(t)
	var calls [][]string
	attempt := 0
	client.Run = func(_ context.Context, _ string, args ...string) (string, string, error) {
		calls = append(calls, append([]string(nil), args...))
		attempt++
		if attempt < 4 {
			return "", "ERROR: Requested format is not available", fmt.Errorf("exit status 1")
		}
		return ytdlVideoJSON("minimal"), "", nil
	}
	if _, err := client.FetchInfo("https://youtu.be/minimal"); err != nil {
		t.Fatalf("FetchInfo error: %v", err)
	}
	if len(calls) != 4 {
		t.Fatalf("esperaba 4 intentos, hubo %d", len(calls))
	}
	joined := strings.Join(calls[3], " ")
	if strings.Contains(joined, "--remote-components") || strings.Contains(joined, "--js-runtimes") {
		t.Fatalf("el último intento debía ser mínimo: %#v", calls[3])
	}
}

func TestYtdlFindBinary(t *testing.T) {
	t.Setenv("YTDLP_BIN", "/ruta/custom/yt-dlp")
	if got := YtdlFindBinary(); got != "/ruta/custom/yt-dlp" {
		t.Fatalf("YTDLP_BIN ignorado: %q", got)
	}
	t.Setenv("YTDLP_BIN", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "yt-dlp")
	if err := os.WriteFile(want, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := YtdlFindBinary(); got != want {
		t.Fatalf("binario=%q, esperaba %q", got, want)
	}
}

func TestYtdlCancelSinMenuID(t *testing.T) {
	s := NewYtdlSessionStore(time.Now)
	s.StoreDownload("u1", &ytdlSession{CreatedAt: time.Now()})
	ids, had := s.Cancel("u1")
	if !had || len(ids) != 0 {
		t.Fatalf("ids=%#v had=%v: la sesión existía aunque no tuviera MenuMsgID", ids, had)
	}
}

func TestYtdlCommandEnvSinNodeIPC(t *testing.T) {
	t.Setenv("NODE_CHANNEL_FD", "3")
	t.Setenv("NODE_CHANNEL_SERIALIZATION_MODE", "json")
	env := ytdlCommandEnv()
	foundPath := false
	for _, entry := range env {
		if strings.HasPrefix(entry, "NODE_CHANNEL_FD=") || strings.HasPrefix(entry, "NODE_CHANNEL_SERIALIZATION_MODE=") {
			t.Fatalf("IPC de PM2 filtrado al subproceso: %q", entry)
		}
		if strings.HasPrefix(entry, "PATH=") {
			foundPath = true
			if !strings.Contains(entry, "/usr/local/bin") {
				t.Fatalf("PATH sin /usr/local/bin: %q", entry)
			}
		}
	}
	if !foundPath {
		t.Fatal("PATH no presente en el entorno del subproceso")
	}
}

func TestYtdlSeleccionBusquedaMuestraMenuConFallbackTexto(t *testing.T) {
	client := ytdlTestClient(t)
	client.HTTPClient = &http.Client{Transport: ytdlRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 500, Body: ioNopCloser{bytes.NewReader(nil)}, Header: make(http.Header), Request: req}, nil
	})}
	client.Run = func(_ context.Context, _ string, args ...string) (string, string, error) {
		if strings.Contains(strings.Join(args, " "), "--dump-json") {
			return ytdlVideoJSON("a"), "", nil
		}
		return "", "", nil
	}
	svc := ytdlTestService(t, client)
	capture := new(ytdlCapture)
	svc.Sessions.StoreSearch(capture.ctx().SenderJID().String(), &ytdlSearchSession{
		Results:   []ytdlSearchResult{{ID: "a", Title: "Resultado a", URL: "https://youtu.be/a"}},
		MenuMsgID: "mosaico-viejo", CreatedAt: time.Now(),
	})
	if err := (Ytdl{Svc: svc}).Handle(capture.ctx("1")); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	textos := strings.Join(capture.texts(), "\n")
	if !strings.Contains(textos, "Selecciona resolución") || !strings.Contains(textos, "720p") {
		t.Fatalf("no se envió el menú de resoluciones: %s", textos)
	}
	if len(capture.sentIDs()) != 1 {
		t.Fatalf("debía enviarse un menú por texto: %#v", capture.sentIDs())
	}
	rev := capture.revokedIDs()
	if len(rev) != 1 || rev[0] != "mosaico-viejo" {
		t.Fatalf("revocación incorrecta: %#v", rev)
	}
}
