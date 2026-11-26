package addons

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"whatsbot-go/internal/core"
)

func TestTwitterParseURL(t *testing.T) {
	casos := map[string]string{
		"https://x.com/ana/status/123?s=20":         "123",
		"https://twitter.com/ana/status/456":        "456",
		"https://mobile.twitter.com/ana/status/789": "789",
		"https://fxtwitter.com/ana/status/111":      "111",
		"https://vxtwitter.com/ana/status/222":      "222",
		"https://fixupx.com/ana/status/333":         "333",
		"mira https://x.com/ana/status/444/photo/1": "444",
	}
	for raw, want := range casos {
		id, _ := twitterStatusID(raw)
		if id != want {
			t.Errorf("%q → id=%q, esperaba %q", raw, id, want)
		}
	}
	if id, _ := twitterStatusID("https://youtube.com/watch?v=x"); id != "" {
		t.Fatalf("URL ajena dio id=%q", id)
	}
}

func twitterSetup(t *testing.T, n, fxStatus int) {
	t.Helper()
	img := ytdlTestJPEG(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/status/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(fxStatus)
	})
	mux.HandleFunc("/Twitter/status/", func(w http.ResponseWriter, r *http.Request) {
		urls := make([]string, n)
		for i := range urls {
			urls[i] = "http://" + r.Host + "/img/" + strconv.Itoa(i) + ".jpg"
		}
		b, _ := json.Marshal(map[string]any{
			"tweetID": "123", "user": "Ana", "user_name": "ana",
			"text": "Hola desde X", "likes": 10, "mediaURLs": urls,
		})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	})
	mux.HandleFunc("/img/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(img)
	})
	srv := httptest.NewServer(mux)
	prevHTTP, prevBases := twitterHTTP, twitterAPIBases
	twitterHTTP = srv.Client()
	twitterAPIBases = []string{srv.URL + "/status/", srv.URL + "/Twitter/status/"}
	t.Cleanup(func() {
		twitterHTTP, twitterAPIBases = prevHTTP, prevBases
		srv.Close()
	})
}

type twitterCapture struct {
	msgs   []*waE2E.Message
	reacts []string
}

func (c *twitterCapture) ctx(args ...string) *core.Context {
	return &core.Context{
		Command: "x",
		Args:    args,
		Message: &events.Message{Info: types.MessageInfo{MessageSource: types.MessageSource{
			Chat: types.NewJID("1", types.DefaultUserServer),
		}}},
		ReactHook: func(e string) error { c.reacts = append(c.reacts, e); return nil },
		SendHook:  func(m *waE2E.Message) error { c.msgs = append(c.msgs, m); return nil },
	}
}

func TestTwitterFallbackVX(t *testing.T) {
	twitterSetup(t, 2, http.StatusInternalServerError)
	c := new(twitterCapture)
	if err := (Twitter{}).Handle(c.ctx("https://x.com/ana/status/123")); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(c.msgs) != 2 {
		t.Fatalf("mensajes=%d, esperaba 2 imágenes", len(c.msgs))
	}
	for _, m := range c.msgs {
		if m.GetImageMessage() == nil {
			t.Fatalf("mensaje no imagen: %#v", m)
		}
	}
	if strings.Contains(c.msgs[0].GetImageMessage().GetCaption(), "Hola desde X") == false {
		t.Fatal("el caption debía incluir el texto del tweet")
	}
	if c.reacts[len(c.reacts)-1] != "✅" {
		t.Fatalf("reacciones=%#v", c.reacts)
	}
}

func TestTwitterZIPUsaStore(t *testing.T) {
	data, err := twitterZIP([][]byte{[]byte("uno"), []byte("dos")})
	if err != nil {
		t.Fatalf("twitterZIP: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("ZIP inválido: %v", err)
	}
	if len(zr.File) != 2 {
		t.Fatalf("entradas=%d", len(zr.File))
	}
	for _, f := range zr.File {
		if f.Method != zip.Store {
			t.Fatalf("%s usa Method=%d, esperaba Store", f.Name, f.Method)
		}
	}
}

func TestTwitterMasDeCuatroVaEnZIP(t *testing.T) {
	twitterSetup(t, 5, http.StatusInternalServerError)
	c := new(twitterCapture)
	if err := (Twitter{}).Handle(c.ctx("https://x.com/ana/status/123")); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(c.msgs) != 1 || c.msgs[0].GetDocumentMessage() == nil {
		t.Fatalf("esperaba un documento: %#v", c.msgs)
	}
	if name := c.msgs[0].GetDocumentMessage().GetFileName(); name != "twitter_123.zip" {
		t.Fatalf("nombre=%q", name)
	}
}

func TestTwitterFlagImgFuerzaImágenes(t *testing.T) {
	twitterSetup(t, 5, http.StatusInternalServerError)
	for _, flag := range []string{"-i", "img", "fotos"} {
		c := new(twitterCapture)
		if err := (Twitter{}).Handle(c.ctx(flag, "https://x.com/ana/status/123")); err != nil {
			t.Fatalf("flag %q: %v", flag, err)
		}
		if len(c.msgs) != 5 {
			t.Fatalf("flag %q: mensajes=%d, esperaba 5 imágenes", flag, len(c.msgs))
		}
		for _, m := range c.msgs {
			if m.GetImageMessage() == nil {
				t.Fatalf("flag %q: mensaje no imagen", flag)
			}
		}
	}
}

func TestYtdlRedirigeURLDeX(t *testing.T) {
	prev := twitterFetchFn
	twitterFetchFn = func(id string) (*twitterTweet, error) {
		return &twitterTweet{
			ID: id, URL: "https://x.com/ana/status/" + id,
			Text: "redirect nativo", Author: twitterAuthor{Name: "Ana"},
		}, nil
	}
	t.Cleanup(func() { twitterFetchFn = prev })

	llamadas := 0
	svc := &ytdlService{
		Client: &ytdlClient{Run: func(context.Context, string, ...string) (string, string, error) {
			llamadas++
			return "", "", fmt.Errorf("yt-dlp no debió invocarse")
		}},
		Sessions: NewYtdlSessionStore(time.Now),
	}
	c := new(twitterCapture)
	ctx := c.ctx("https://x.com/ana/status/123")
	ctx.Command = "yt"
	if err := (Ytdl{Svc: svc}).Handle(ctx); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if llamadas != 0 {
		t.Fatalf("yt-dlp invocado %d veces", llamadas)
	}
	if len(c.msgs) != 1 || !strings.Contains(c.msgs[0].GetConversation(), "redirect nativo") {
		t.Fatalf("mensajes=%#v", c.msgs)
	}
}

func TestTwitterRemuxArgs(t *testing.T) {
	copyArgs := strings.Join(twitterRemuxArgs("in.mp4", "out.mp4", true), " ")
	for _, want := range []string{"-c copy", "-movflags +faststart", "-an"} {
		if !strings.Contains(copyArgs, want) {
			t.Fatalf("remux sin %q: %s", want, copyArgs)
		}
	}
	if strings.Contains(copyArgs, "libx264") {
		t.Fatalf("remux no debía recodificar: %s", copyArgs)
	}

	recodeArgs := strings.Join(twitterRemuxArgs("in.mp4", "out.mp4", false), " ")
	for _, want := range []string{"libx264", "baseline", "yuv420p", "ultrafast", "+faststart"} {
		if !strings.Contains(recodeArgs, want) {
			t.Fatalf("fallback sin %q: %s", want, recodeArgs)
		}
	}
}

func TestTwitterPreparaVideoSegunCodec(t *testing.T) {
	prev := twitterRunCmd
	t.Cleanup(func() { twitterRunCmd = prev })

	casos := []struct {
		pixFmt  string
		wantCmd string
	}{
		{"yuv420p", "-c copy"},
		{"yuv444p", "libx264"},
	}
	for _, caso := range casos {
		var comandos []string
		twitterRunCmd = func(ctx context.Context, name string, args ...string) ([]byte, error) {
			comandos = append(comandos, name)
			if name == "ffprobe" {
				resp := `{"streams":[{"codec_type":"video","codec_name":"h264","pix_fmt":"` + caso.pixFmt + `","width":720,"height":1280,"duration":"3.5"}],"format":{"duration":"3.5"}}`
				return []byte(resp), nil
			}
			if err := os.WriteFile(args[len(args)-1], []byte("procesado"), 0o600); err != nil {
				return nil, err
			}
			return nil, nil
		}
		data, probe := twitterPrepareVideo([]byte("original"))
		if string(data) != "procesado" {
			t.Fatalf("pix_fmt=%s: datos=%q", caso.pixFmt, data)
		}
		if probe.Codec != "h264" || probe.Width != 720 || probe.Height != 1280 || probe.Seconds != 3 {
			t.Fatalf("probe=%#v", probe)
		}
		if len(comandos) != 2 || comandos[0] != "ffprobe" || comandos[1] != "ffmpeg" {
			t.Fatalf("comandos=%#v", comandos)
		}
	}
}

func twitterVideoSetup(t *testing.T, thumb []byte) (*twitterCapture, *string) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/gif.mp4", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("mp4-original"))
	})
	mux.HandleFunc("/thumb.jpg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(thumb)
	})
	srv := httptest.NewServer(mux)
	prevHTTP, prevFetch, prevRun := twitterHTTP, twitterFetchFn, twitterRunCmd
	twitterHTTP = srv.Client()
	twitterFetchFn = func(id string) (*twitterTweet, error) {
		return &twitterTweet{ID: id, Text: "gif", Author: twitterAuthor{Name: "Ana"},
			Media: twitterMediaBag{All: []twitterMedia{{
				Type: "gif", URL: srv.URL + "/gif.mp4", ThumbnailURL: srv.URL + "/thumb.jpg",
			}}}}, nil
	}
	ffmpegArgs := new(string)
	twitterRunCmd = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "ffprobe" {
			return []byte(`{"streams":[{"codec_type":"video","codec_name":"h264","pix_fmt":"yuv420p","width":720,"height":1280,"duration":"2"}],"format":{"duration":"2"}}`), nil
		}
		*ffmpegArgs = strings.Join(args, " ")
		return nil, os.WriteFile(args[len(args)-1], []byte("mp4-remux"), 0o600)
	}
	t.Cleanup(func() {
		twitterHTTP, twitterFetchFn, twitterRunCmd = prevHTTP, prevFetch, prevRun
		srv.Close()
	})
	return new(twitterCapture), ffmpegArgs
}

func TestTwitterGIFVideoMessageConMeta(t *testing.T) {
	c, ffmpegArgs := twitterVideoSetup(t, ytdlTestJPEG(t))
	if err := (Twitter{}).Handle(c.ctx("https://x.com/ana/status/123")); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !strings.Contains(*ffmpegArgs, "-c copy") {
		t.Fatalf("debía hacer remux: %q", *ffmpegArgs)
	}
	if len(c.msgs) != 1 || c.msgs[0].GetVideoMessage() == nil {
		t.Fatalf("mensajes=%#v", c.msgs)
	}
	vm := c.msgs[0].GetVideoMessage()
	if vm.GetMimetype() != "video/mp4" || !vm.GetGifPlayback() {
		t.Fatalf("mimetype=%q gif=%v", vm.GetMimetype(), vm.GetGifPlayback())
	}
	if vm.GetGifAttribution() != waE2E.VideoMessage_NONE {
		t.Fatalf("gifAttribution=%v", vm.GetGifAttribution())
	}
	if len(vm.JPEGThumbnail) == 0 {
		t.Fatal("JPEGThumbnail vacío")
	}
	if vm.GetWidth() != 720 || vm.GetHeight() != 1280 || vm.GetSeconds() != 2 {
		t.Fatalf("meta=%dx%d %ds", vm.GetWidth(), vm.GetHeight(), vm.GetSeconds())
	}
}
