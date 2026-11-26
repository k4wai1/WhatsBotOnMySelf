// twitter.go descarga tweets con las APIs de embeds estilo Discord
// (fxtwitter con fallback a vxtwitter), sin depender de yt-dlp.
package addons

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/disintegration/imaging"

	"whatsbot-go/internal/core"
)

const (
	twitterUserAgent  = "Mozilla/5.0 (compatible; WhatsBotOnMySelf)"
	twitterAPITimeout = 10 * time.Second
	twitterMaxImage   = 20 << 20
	twitterMaxVideo   = 250 << 20
	twitterMaxAPI     = 2 << 20
)

var (
	twitterHTTP = http.DefaultClient

	twitterFetchFn = twitterFetch

	twitterAPIBases = []string{
		"https://api.fxtwitter.com/status/",
		"https://api.vxtwitter.com/Twitter/status/",
	}

	twitterRunCmd = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).CombinedOutput()
	}

	errTwitterNotFound = errors.New("tweet privado, borrado o inexistente")
)

var twitterURLRe = regexp.MustCompile(`(?i)https?://(?:www\.|mobile\.)?(?:x\.com|twitter\.com|fxtwitter\.com|vxtwitter\.com|fixupx\.com)/(?:i/(?:web/)?status/|([A-Za-z0-9_]+)/status/)(\d+)`)

var twitterNameParamRe = regexp.MustCompile(`name=[a-zA-Z0-9_-]+`)

// Twitter porta los comandos .x, .tw y .twitter.
type Twitter struct{}

func init() { core.Register(Twitter{}) }

func (Twitter) Commands() []string { return []string{"x", "tw", "twitter"} }

// twitterStatusID extrae el ID y el usuario de cualquier URL de X/Twitter.
func twitterStatusID(raw string) (id, user string) {
	m := twitterURLRe.FindStringSubmatch(raw)
	if m == nil {
		return "", ""
	}
	return m[2], m[1]
}

type twitterVariant struct {
	URL         string `json:"url"`
	Bitrate     int64  `json:"bitrate"`
	ContentType string `json:"content_type"`
}

type twitterMedia struct {
	Type         string           `json:"type"`
	URL          string           `json:"url"`
	FullURL      string           `json:"full_url"`
	OriginalURL  string           `json:"original_url"`
	ContentType  string           `json:"content_type"`
	ThumbnailURL string           `json:"thumbnail_url"`
	Variants     []twitterVariant `json:"variants"`
}

type twitterMediaBag struct {
	All             []twitterMedia `json:"all"`
	PhotosAndVideos []twitterMedia `json:"photos_and_videos"`
	Photos          []twitterMedia `json:"photos"`
	Videos          []twitterMedia `json:"videos"`
}

type twitterAuthor struct {
	Name       string `json:"name"`
	ScreenName string `json:"screen_name"`
	AvatarURL  string `json:"avatar_url"`
}

type twitterTweet struct {
	ID           string          `json:"id"`
	URL          string          `json:"url"`
	ThumbnailURL string          `json:"thumbnail_url"`
	Text         string          `json:"text"`
	FullText     string          `json:"full_text"`
	Author       twitterAuthor   `json:"author"`
	Likes        int64           `json:"likes"`
	Retweets     int64           `json:"retweets"`
	Replies      int64           `json:"replies"`
	Views        int64           `json:"views"`
	Quote        *twitterTweet   `json:"quote"`
	Media        twitterMediaBag `json:"media"`

	TweetID       string         `json:"tweetID"`
	User          string         `json:"user"`
	UserName      string         `json:"user_name"`
	ScreenName    string         `json:"screen_name"`
	MediaURLs     []string       `json:"mediaURLs"`
	MediaExtended []twitterMedia `json:"mediaExtended"`
}

type twitterAPIResponse struct {
	Code    int           `json:"code"`
	Message string        `json:"message"`
	Error   string        `json:"error"`
	Tweet   *twitterTweet `json:"tweet"`
	twitterTweet
}

func (r twitterAPIResponse) result() *twitterTweet {
	if r.Tweet != nil {
		return r.Tweet
	}
	t := r.twitterTweet
	if t.ID == "" {
		t.ID = t.TweetID
	}
	if t.Author.Name == "" {
		t.Author.Name = t.User
	}
	if t.Author.ScreenName == "" {
		t.Author.ScreenName = t.UserName
	}
	if t.Author.ScreenName == "" {
		t.Author.ScreenName = t.ScreenName
	}
	if t.URL == "" && t.ID != "" {
		t.URL = "https://x.com/i/status/" + t.ID
	}
	return &t
}

func (t *twitterTweet) text() string {
	if t == nil {
		return ""
	}
	if t.Text != "" {
		return t.Text
	}
	return t.FullText
}

func (t *twitterTweet) medias() []twitterMedia {
	if t == nil {
		return nil
	}
	out := make([]twitterMedia, 0)
	seen := make(map[string]bool)
	add := func(m twitterMedia) {
		u := m.bestURL()
		if u == "" || seen[u] {
			return
		}
		seen[u] = true
		out = append(out, m)
	}
	for _, m := range t.Media.All {
		add(m)
	}
	for _, m := range t.Media.PhotosAndVideos {
		add(m)
	}
	for _, m := range t.Media.Photos {
		add(m)
	}
	for _, m := range t.Media.Videos {
		add(m)
	}
	for _, m := range t.MediaExtended {
		add(m)
	}
	for _, u := range t.MediaURLs {
		add(twitterMedia{URL: u})
	}
	return out
}

func twitterMediaEsVideo(m twitterMedia) bool {
	tipo := strings.ToLower(m.Type + " " + m.ContentType)
	if strings.Contains(tipo, "video") || strings.Contains(tipo, "gif") || strings.Contains(tipo, "animated") {
		return true
	}
	for _, u := range []string{m.URL, m.OriginalURL, m.FullURL} {
		if strings.Contains(strings.ToLower(u), ".mp4") {
			return true
		}
	}
	return false
}

func (m twitterMedia) bestURL() string {
	if len(m.Variants) > 0 {
		best := ""
		var bitrate int64 = -1
		for _, v := range m.Variants {
			if v.URL == "" || strings.Contains(v.URL, ".m3u8") {
				continue
			}
			ct := strings.ToLower(v.ContentType)
			if ct != "" && !strings.Contains(ct, "mp4") && !strings.Contains(ct, "video") {
				continue
			}
			if v.Bitrate >= bitrate {
				best, bitrate = v.URL, v.Bitrate
			}
		}
		if best != "" {
			return best
		}
	}
	for _, u := range []string{m.URL, m.OriginalURL, m.FullURL} {
		if u == "" {
			continue
		}
		if twitterMediaEsVideo(m) {
			return u
		}
		return twitterNameParamRe.ReplaceAllString(u, "name=orig")
	}
	return ""
}

func twitterNotFoundText(s string) bool {
	s = strings.ToLower(s)
	for _, needle := range []string{
		"not found", "no encontrado", "private", "privado", "deleted",
		"borrado", "unavailable", "no longer", "does not exist", "inexistente",
	} {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

func twitterFetchOne(baseURL, statusID string) (*twitterTweet, error) {
	ctx, cancel := context.WithTimeout(context.Background(), twitterAPITimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+statusID, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", twitterUserAgent)
	res, err := twitterHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		if res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusForbidden {
			return nil, errTwitterNotFound
		}
		return nil, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	var parsed twitterAPIResponse
	if err := json.NewDecoder(io.LimitReader(res.Body, twitterMaxAPI)).Decode(&parsed); err != nil {
		return nil, err
	}
	msg := parsed.Message
	if msg == "" {
		msg = parsed.Error
	}
	if parsed.Code != 0 && parsed.Code != 200 {
		if parsed.Code == 404 || parsed.Code == 403 || twitterNotFoundText(msg) {
			return nil, errTwitterNotFound
		}
		return nil, fmt.Errorf("API %d: %s", parsed.Code, msg)
	}
	if twitterNotFoundText(msg) {
		return nil, errTwitterNotFound
	}
	tweet := parsed.result()
	if tweet == nil || (tweet.text() == "" && len(tweet.medias()) == 0) {
		return nil, fmt.Errorf("respuesta vacía")
	}
	return tweet, nil
}

func twitterFetch(statusID string) (*twitterTweet, error) {
	var lastErr error
	notFound := false
	for _, base := range twitterAPIBases {
		tweet, err := twitterFetchOne(base, statusID)
		if err == nil {
			return tweet, nil
		}
		lastErr = err
		if errors.Is(err, errTwitterNotFound) {
			notFound = true
		}
	}
	if notFound {
		return nil, errTwitterNotFound
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("sin respuesta de las APIs")
	}
	return nil, lastErr
}

func twitterCount(n int64) string {
	switch {
	case n >= 1_000_000:
		return strconv.FormatFloat(float64(n)/1e6, 'f', 1, 64) + "M"
	case n >= 1000:
		return strconv.FormatFloat(float64(n)/1e3, 'f', 1, 64) + "K"
	default:
		return strconv.FormatInt(n, 10)
	}
}

func twitterCaption(t *twitterTweet) string {
	if t == nil {
		return ""
	}
	nombre := t.Author.Name
	if nombre == "" {
		nombre = t.User
	}
	handle := t.Author.ScreenName
	if handle == "" {
		handle = t.UserName
	}
	lineas := []string{"🐦 *" + nombre + "*"}
	if handle != "" {
		lineas[0] += " (@" + handle + ")"
	}
	if txt := t.text(); txt != "" {
		lineas = append(lineas, "", txt)
	}
	if t.Quote != nil && t.Quote.text() != "" {
		lineas = append(lineas, "", "🔁 *Citado:* "+t.Quote.text())
	}
	metricas := []string{}
	if t.Likes > 0 {
		metricas = append(metricas, "❤️ "+twitterCount(t.Likes))
	}
	if t.Retweets > 0 {
		metricas = append(metricas, "🔁 "+twitterCount(t.Retweets))
	}
	if t.Replies > 0 {
		metricas = append(metricas, "💬 "+twitterCount(t.Replies))
	}
	if t.Views > 0 {
		metricas = append(metricas, "👁 "+twitterCount(t.Views))
	}
	if len(metricas) > 0 {
		lineas = append(lineas, "", strings.Join(metricas, " · "))
	}
	if t.URL != "" {
		lineas = append(lineas, "", t.URL)
	}
	return strings.Join(lineas, "\n")
}

type twitterFlags struct {
	Images bool
	Zip    bool
	Rest   []string
}

func twitterParseFlags(args []string) twitterFlags {
	var f twitterFlags
	for _, arg := range args {
		switch strings.ToLower(strings.TrimSpace(arg)) {
		case "-i", "--img", "img", "foto", "fotos", "images":
			f.Images = true
		case "c", "zip", "comp":
			f.Zip = true
		default:
			f.Rest = append(f.Rest, arg)
		}
	}
	return f
}

func twitterDownload(url string, limite int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", twitterUserAgent)
	res, err := twitterHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("HTTP %d al descargar", res.StatusCode)
	}
	buf, err := io.ReadAll(io.LimitReader(res.Body, limite+1))
	if err != nil {
		return nil, err
	}
	if int64(len(buf)) > limite {
		return nil, fmt.Errorf("archivo demasiado grande")
	}
	return buf, nil
}

func twitterDownloadImages(items []twitterMedia) [][]byte {
	out := make([][]byte, len(items))
	errs := make([]error, len(items))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i, m := range items {
		wg.Add(1)
		go func(i int, m twitterMedia) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out[i], errs[i] = twitterDownload(m.bestURL(), twitterMaxImage)
		}(i, m)
	}
	wg.Wait()
	result := make([][]byte, 0, len(items))
	for i, data := range out {
		if errs[i] == nil && len(data) > 0 {
			result = append(result, data)
		}
	}
	return result
}

func twitterZIP(images [][]byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for i, data := range images {
		header := &zip.FileHeader{Name: fmt.Sprintf("%02d.jpg", i+1), Method: zip.Store}
		header.SetModTime(time.Now())
		w, err := zw.CreateHeader(header)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func twitterTakeCaption(cap *string) string {
	c := *cap
	*cap = ""
	return c
}

func twitterSend(ctx *core.Context, tweet *twitterTweet, flags twitterFlags) error {
	medias := tweet.medias()
	if tweet.Quote != nil {
		medias = append(medias, tweet.Quote.medias()...)
	}
	caption := twitterCaption(tweet)
	if len(medias) == 0 {
		return ctx.SendText(caption)
	}
	videos := make([]twitterMedia, 0)
	fotos := make([]twitterMedia, 0)
	for _, m := range medias {
		if twitterMediaEsVideo(m) {
			videos = append(videos, m)
		} else {
			fotos = append(fotos, m)
		}
	}
	for _, video := range videos {
		data, err := twitterDownload(video.bestURL(), twitterMaxVideo)
		if err != nil || len(data) == 0 {
			continue
		}
		esGIF := strings.Contains(strings.ToLower(video.Type), "gif")
		probe := twitterVideoProbe{}
		if esGIF {
			var preparado []byte
			preparado, probe = twitterPrepareVideo(data)
			if len(preparado) > 0 {
				data = preparado
			}
		}
		if len(data) > ytdlWarnSize {
			err = ctx.SendDocument(data, "twitter_"+tweet.ID+".mp4", twitterTakeCaption(&caption))
		} else {
			meta := core.VideoMeta{
				JPEGThumbnail: twitterThumbnail(video, tweet),
				Width:         probe.Width,
				Height:        probe.Height,
				Seconds:       probe.Seconds,
			}
			err = ctx.SendVideoWithMeta(data, twitterTakeCaption(&caption), esGIF, meta)
		}
		if err != nil {
			return err
		}
	}
	if len(fotos) == 0 {
		return nil
	}
	images := twitterDownloadImages(fotos)
	if len(images) == 0 {
		return nil
	}
	if flags.Zip || (len(images) > 4 && !flags.Images) {
		zipData, err := twitterZIP(images)
		if err != nil {
			return err
		}
		return ctx.SendDocument(zipData, "twitter_"+tweet.ID+".zip", caption)
	}
	for i, data := range images {
		cap := ""
		if i == 0 {
			cap = caption
		}
		if err := ctx.SendImage(data, cap); err != nil {
			return err
		}
	}
	return nil
}

func (Twitter) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}
	flags := twitterParseFlags(ctx.Args)
	raw := strings.Join(flags.Rest, " ")
	if strings.TrimSpace(raw) == "" {
		raw = ctx.RawText
	}
	id, _ := twitterStatusID(raw)
	if id == "" {
		return ctx.Reply("🐦 Pasa un enlace de X/Twitter: `!x <url>`")
	}
	if err := ctx.React("⏳"); err != nil {
		return err
	}
	tweet, err := twitterFetchFn(id)
	if err != nil {
		if errors.Is(err, errTwitterNotFound) {
			_ = ctx.React("❌")
			return ctx.Reply("❌ El tweet es privado, fue borrado o no existe.")
		}
		return twitterFail(ctx, err)
	}
	if err := twitterSend(ctx, tweet, flags); err != nil {
		return twitterFail(ctx, err)
	}
	return ctx.React("✅")
}

func twitterFail(ctx *core.Context, err error) error {
	fmt.Printf("❌ [twitter]: %v\n", err)
	_ = ctx.React("❌")
	return ctx.Reply("❌ No se pudo descargar el tweet: " + err.Error())
}

type twitterVideoProbe struct {
	Codec   string
	PixFmt  string
	Width   uint32
	Height  uint32
	Seconds uint32
}

func twitterProbeVideo(path string) (twitterVideoProbe, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, err := twitterRunCmd(ctx, "ffprobe", "-v", "error", "-print_format", "json", "-show_streams", "-show_format", path)
	if err != nil {
		return twitterVideoProbe{}, fmt.Errorf("ffprobe: %w", err)
	}
	var parsed struct {
		Streams []struct {
			CodecType string `json:"codec_type"`
			CodecName string `json:"codec_name"`
			PixFmt    string `json:"pix_fmt"`
			Width     uint32 `json:"width"`
			Height    uint32 `json:"height"`
			Duration  string `json:"duration"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return twitterVideoProbe{}, err
	}
	probe := twitterVideoProbe{}
	for _, s := range parsed.Streams {
		if s.CodecType != "video" {
			continue
		}
		probe.Codec, probe.PixFmt = s.CodecName, s.PixFmt
		probe.Width, probe.Height = s.Width, s.Height
		if d, err := strconv.ParseFloat(s.Duration, 64); err == nil {
			probe.Seconds = uint32(d)
		}
		break
	}
	if probe.Seconds == 0 {
		if d, err := strconv.ParseFloat(parsed.Format.Duration, 64); err == nil {
			probe.Seconds = uint32(d)
		}
	}
	return probe, nil
}

func twitterRemuxArgs(in, out string, compatible bool) []string {
	if compatible {
		return []string{"-y", "-i", in, "-c", "copy", "-movflags", "+faststart", "-an", out}
	}
	return []string{
		"-y", "-i", in, "-c:v", "libx264", "-profile:v", "baseline",
		"-level", "3.0", "-pix_fmt", "yuv420p", "-preset", "ultrafast",
		"-movflags", "+faststart", "-an", out,
	}
}

func twitterPrepareVideo(data []byte) ([]byte, twitterVideoProbe) {
	probe := twitterVideoProbe{}
	tmpDir, err := os.MkdirTemp("", "twvideo-")
	if err != nil {
		return data, probe
	}
	defer os.RemoveAll(tmpDir)
	in := filepath.Join(tmpDir, "in.mp4")
	if err := os.WriteFile(in, data, 0o600); err != nil {
		return data, probe
	}
	probe, err = twitterProbeVideo(in)
	if err != nil {
		return data, twitterVideoProbe{}
	}
	compatible := probe.Codec == "h264" && probe.PixFmt == "yuv420p"
	out := filepath.Join(tmpDir, "out.mp4")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if _, err := twitterRunCmd(ctx, "ffmpeg", twitterRemuxArgs(in, out, compatible)...); err != nil {
		return data, probe
	}
	processed, err := os.ReadFile(out)
	if err != nil || len(processed) == 0 {
		return data, probe
	}
	return processed, probe
}

func twitterThumbnail(video twitterMedia, tweet *twitterTweet) []byte {
	thumbURL := video.ThumbnailURL
	if thumbURL == "" && tweet != nil {
		thumbURL = tweet.ThumbnailURL
	}
	if thumbURL == "" {
		return nil
	}
	data, err := twitterDownload(thumbURL, 4<<20)
	if err != nil || len(data) == 0 {
		return nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	if img.Bounds().Dx() > 320 || img.Bounds().Dy() > 320 {
		img = imaging.Fit(img, 320, 320, imaging.Lanczos)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		return nil
	}
	return buf.Bytes()
}
