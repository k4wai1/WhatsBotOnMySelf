package addons

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestWallpaperCommands(t *testing.T) {
	got := Wallpaper{}.Commands()
	if len(got) != 2 || got[0] != "wallpaper" || got[1] != "wp" {
		t.Fatalf("comandos inesperados: %#v", got)
	}
}

func TestWallpaperSearchYFetch(t *testing.T) {
	pngBytes := func() []byte {
		img := image.NewRGBA(image.Rect(0, 0, 8, 8))
		img.Set(0, 0, color.RGBA{R: 255, A: 255})
		var buf bytes.Buffer
		_ = png.Encode(&buf, img)
		return buf.Bytes()
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("q"); got != "gatos" {
			t.Errorf("query inesperada: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"path":"` + "http://" + r.Host + `/img.png","resolution":"1920x1080","file_size":2500000,"source":"wallhaven","url":"https://wallhaven.cc/w/x"}]}`))
	})
	mux.HandleFunc("/img.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngBytes)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	prevHTTP := wallpaperHTTP
	wallpaperHTTP = srv.Client()
	t.Cleanup(func() { wallpaperHTTP = prevHTTP })

	items, err := wallpaperSearch(srv.URL + "/api?q=gatos")
	if err != nil {
		t.Fatalf("wallpaperSearch: %v", err)
	}
	if len(items) != 1 || items[0].Resolution != "1920x1080" {
		t.Fatalf("items inesperados: %#v", items)
	}

	data, err := wallpaperFetchImage(items[0].Path)
	if err != nil {
		t.Fatalf("wallpaperFetchImage: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("imagen vacía")
	}

	caption := wallpaperCaption(items[0])
	for _, want := range []string{"1920x1080", "wallhaven"} {
		if !strings.Contains(caption, want) {
			t.Fatalf("caption %q sin %q", caption, want)
		}
	}
}

func TestWallpaperSearchErrorHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	prevHTTP := wallpaperHTTP
	wallpaperHTTP = srv.Client()
	t.Cleanup(func() { wallpaperHTTP = prevHTTP })

	if _, err := wallpaperSearch(srv.URL); err == nil {
		t.Fatal("esperaba error HTTP")
	}
	if _, err := wallpaperFetchImage(srv.URL + "/x"); err == nil {
		t.Fatal("esperaba error HTTP al descargar")
	}
}

func TestWallpaperAPIURLRelevancia(t *testing.T) {
	prevInt := randIntN
	randIntN = func(int) int { return 3 }
	t.Cleanup(func() { randIntN = prevInt })

	parsed, err := url.Parse(wallpaperAPIURL("gatos cyberpunk"))
	if err != nil {
		t.Fatal(err)
	}
	q := parsed.Query()
	if q.Get("q") != "gatos cyberpunk" || q.Get("sorting") != "relevance" || q.Get("page") != "1" {
		t.Fatalf("query con término=%v", q)
	}

	parsed, err = url.Parse(wallpaperAPIURL(""))
	if err != nil {
		t.Fatal(err)
	}
	q = parsed.Query()
	if q.Get("page") != "4" || q.Get("sorting") != "" || q.Get("q") != "" {
		t.Fatalf("query sin término=%v", q)
	}
}

func TestWallpaperPickFavoreceRelevancia(t *testing.T) {
	items := make([]wallpaperItem, 30)
	for i := range items {
		items[i] = wallpaperItem{Path: "p" + strconv.Itoa(i)}
	}
	prevRand, prevInt := wallpaperRandFloat, randIntN
	t.Cleanup(func() { wallpaperRandFloat, randIntN = prevRand, prevInt })

	wallpaperRandFloat = func() float64 { return 0 }
	if got := wallpaperPick(items, "gatos"); got.Path != "p0" {
		t.Fatalf("debió elegir el primero: %q", got.Path)
	}

	wallpaperRandFloat = func() float64 { return 0.999999 }
	if got := wallpaperPick(items, "gatos"); got.Path != "p11" {
		t.Fatalf("no debía salir del top 12: %q", got.Path)
	}

	randIntN = func(n int) int { return n - 1 }
	if got := wallpaperPick(items, ""); got.Path != "p29" {
		t.Fatalf("sin término debía seguir aleatorio: %q", got.Path)
	}
}
