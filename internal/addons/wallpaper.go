// wallpaper.go porta addons/wallpaper.js: busca en wallhaven.cc y envía un
// wallpaper aleatorio.
package addons

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/disintegration/imaging"

	"whatsbot-go/internal/core"
)

const wallpaperMaxBytes = 25 * 1024 * 1024

// Cuántos resultados de la página relevante se consideran al elegir.
const wallpaperTopResults = 12

const wallpaperUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64)"

// wallpaperHTTP y wallpaperRandFloat son inyectables en las pruebas.
var (
	wallpaperHTTP      = http.DefaultClient
	wallpaperRandFloat = rand.Float64
)

type wallpaperItem struct {
	Path       string `json:"path"`
	Resolution string `json:"resolution"`
	FileSize   int64  `json:"file_size"`
	Source     string `json:"source"`
	URL        string `json:"url"`
}

type wallpaperResponse struct {
	Data []wallpaperItem `json:"data"`
}

// Wallpaper porta .wallpaper y su alias .wp.
type Wallpaper struct{}

// Commands cumple core.Addon.
func (Wallpaper) Commands() []string { return []string{"wallpaper", "wp"} }

// Handle busca y envía un wallpaper.
func (w Wallpaper) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}
	query := strings.TrimSpace(strings.Join(ctx.Args, " "))

	if reactErr := ctx.React("🔍"); reactErr != nil {
		return reactErr
	}

	items, err := wallpaperSearch(wallpaperAPIURL(query))
	if err != nil {
		return wallpaperFail(ctx, err)
	}
	if len(items) == 0 {
		if reactErr := ctx.React("❌"); reactErr != nil {
			return reactErr
		}
		return ctx.Reply("Sin resultados para esa búsqueda.")
	}

	pick := wallpaperPick(items, query)
	if reactErr := ctx.React("⏳"); reactErr != nil {
		return reactErr
	}

	buffer, err := wallpaperFetchImage(pick.Path)
	if err != nil {
		return wallpaperFail(ctx, err)
	}

	img, _, err := image.Decode(bytes.NewReader(buffer))
	if err != nil {
		return wallpaperFail(ctx, err)
	}
	if img.Bounds().Dx() > 1600 || img.Bounds().Dy() > 1600 {
		img = imaging.Fit(img, 1600, 1600, imaging.Lanczos)
	}

	var out strings.Builder
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: 82}); err != nil {
		return wallpaperFail(ctx, err)
	}

	caption := wallpaperCaption(pick)
	if err := ctx.SendImage([]byte(out.String()), caption); err != nil {
		return wallpaperFail(ctx, err)
	}
	return ctx.React("✅")
}

// wallpaperAPIURL construye la consulta. Con término de búsqueda se pide
// orden por relevancia en la primera página; sin término se mantiene la
// exploración aleatoria original.
func wallpaperAPIURL(query string) string {
	params := url.Values{}
	if query != "" {
		params.Set("q", query)
		params.Set("sorting", "relevance")
		params.Set("order", "desc")
		params.Set("page", "1")
	} else {
		params.Set("page", strconv.Itoa(1+randIntN(5)))
	}
	return "https://wallhaven.cc/api/v1/search?" + params.Encode()
}

// wallpaperPick elige entre los primeros resultados con peso decreciente:
// los más relevantes son mucho más probables, pero no siempre el primero.
func wallpaperPick(items []wallpaperItem, query string) wallpaperItem {
	if len(items) == 0 {
		return wallpaperItem{}
	}
	if strings.TrimSpace(query) == "" || len(items) == 1 {
		return items[randIntN(len(items))]
	}

	n := len(items)
	if n > wallpaperTopResults {
		n = wallpaperTopResults
	}
	weights := make([]float64, n)
	total := 0.0
	for i := range weights {
		weights[i] = 1 / math.Pow(float64(i+1), 1.5)
		total += weights[i]
	}

	r := wallpaperRandFloat() * total
	acc := 0.0
	for i, weight := range weights {
		acc += weight
		if r < acc {
			return items[i]
		}
	}
	return items[n-1]
}

func wallpaperSearch(apiURL string) ([]wallpaperItem, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", wallpaperUserAgent)

	res, err := wallpaperHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	var parsed wallpaperResponse
	if err := json.NewDecoder(res.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	items := make([]wallpaperItem, 0, len(parsed.Data))
	for _, item := range parsed.Data {
		if item.Path != "" {
			items = append(items, item)
		}
	}
	return items, nil
}

func wallpaperFetchImage(imageURL string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", wallpaperUserAgent)

	res, err := wallpaperHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d al descargar la imagen", res.StatusCode)
	}
	buf, err := io.ReadAll(io.LimitReader(res.Body, wallpaperMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(buf) == 0 || len(buf) > wallpaperMaxBytes {
		return nil, fmt.Errorf("Imagen vacía o demasiado grande")
	}
	return buf, nil
}

func wallpaperCaption(item wallpaperItem) string {
	mb := math.Round(float64(item.FileSize)/1024/102.4) / 10
	lines := []string{
		"🖼️ *" + item.Resolution + "* · " + strconv.FormatFloat(mb, 'f', -1, 64) + " MB",
	}
	if item.Source != "" {
		lines = append(lines, "🔗 "+item.URL)
	}
	return strings.Join(lines, "\n")
}

func wallpaperFail(ctx *core.Context, err error) error {
	fmt.Printf("❌ [wallpaper]: %v\n", err)
	return ctx.React("❌")
}

func init() { core.Register(Wallpaper{}) }
