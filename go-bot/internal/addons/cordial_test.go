package addons

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"whatsbot-go/internal/core"
)

type cordialCapture struct {
	msgs      []*waE2E.Message
	texts     []string
	reactions []string
}

func (c *cordialCapture) ctx(cmd string) *core.Context {
	return &core.Context{
		Command: cmd,
		SendHook: func(m *waE2E.Message) error {
			c.msgs = append(c.msgs, m)
			c.texts = append(c.texts, messageText(m))
			return nil
		},
		ReactHook: func(emoji string) error {
			c.reactions = append(c.reactions, emoji)
			return nil
		},
	}
}

func cordialSetup(t *testing.T, groq, deepseek http.HandlerFunc) {
	t.Helper()
	groqSrv := httptest.NewServer(groq)
	t.Cleanup(groqSrv.Close)
	dsSrv := httptest.NewServer(deepseek)
	t.Cleanup(dsSrv.Close)

	prevGroq, prevDS := cordialGroqURL, cordialDeepSeekURL
	cordialGroqURL = groqSrv.URL
	cordialDeepSeekURL = dsSrv.URL
	t.Cleanup(func() {
		cordialGroqURL = prevGroq
		cordialDeepSeekURL = prevDS
	})
}

func cordialStubPipeline(t *testing.T) {
	t.Helper()
	prevDownload := cordialDownloadAudio
	prevConvert := cordialConvertMP3
	prevFetch := cordialFetchTTS
	prevBuild := cordialBuildAudio
	prevProbe := cordialProbeDuration

	cordialDownloadAudio = func(*core.Context) ([]byte, error) { return []byte("raw"), nil }
	cordialConvertMP3 = func(raw, out string) error { return os.WriteFile(out, []byte("mp3"), 0o644) }
	cordialFetchTTS = func(string) ([]byte, error) { return []byte("tts"), nil }
	cordialBuildAudio = func(input string, tts []string, patches []cordialPatch, out string) error {
		return os.WriteFile(out, []byte("ogg"), 0o644)
	}
	cordialProbeDuration = func(string) (float64, error) { return 12, nil }

	t.Cleanup(func() {
		cordialDownloadAudio = prevDownload
		cordialConvertMP3 = prevConvert
		cordialFetchTTS = prevFetch
		cordialBuildAudio = prevBuild
		cordialProbeDuration = prevProbe
	})
}

func cordialDeepSeekBody(content string) string {
	raw, _ := json.Marshal(map[string]any{
		"choices": []any{
			map[string]any{"message": map[string]any{"content": content}},
		},
	})
	return string(raw)
}

func cordialAudioCtx(c *cordialCapture, cmd string, seconds uint32) *core.Context {
	ctx := c.ctx(cmd)
	ctx.Message = &events.Message{Message: &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String(cmd),
			ContextInfo: &waE2E.ContextInfo{
				QuotedMessage: &waE2E.Message{
					AudioMessage: &waE2E.AudioMessage{Seconds: proto.Uint32(seconds)},
				},
			},
		},
	}}
	return ctx
}

func TestCordialCommands(t *testing.T) {
	got := Cordial{}.Commands()
	if len(got) != 2 || got[0] != "cordial" || got[1] != "cord" {
		t.Fatalf("comandos inesperados: %#v", got)
	}
}

func TestCordialSinAudio(t *testing.T) {
	c := &cordialCapture{}
	if err := (Cordial{}).Handle(c.ctx("cordial")); err != nil {
		t.Fatalf("Handle devolvió error: %v", err)
	}
	if len(c.texts) != 1 || c.texts[0] != "❌ Debes responder a un mensaje de audio." {
		t.Fatalf("texto inesperado: %#v", c.texts)
	}
	if len(c.reactions) != 0 {
		t.Fatalf("no esperaba reacciones, obtuve %#v", c.reactions)
	}
}

func TestCordialAudioLargo(t *testing.T) {
	c := &cordialCapture{}
	if err := (Cordial{}).Handle(cordialAudioCtx(c, "cord", 241)); err != nil {
		t.Fatalf("Handle devolvió error: %v", err)
	}
	if len(c.texts) != 1 || c.texts[0] != "❌ El audio es muy largo. Máximo 4 minutos." {
		t.Fatalf("texto inesperado: %#v", c.texts)
	}
}

func TestCordialSinReemplazos(t *testing.T) {
	cordialSetup(t,
		func(w http.ResponseWriter, r *http.Request) {
			transJSON(w, `{"text":"hola que tal","words":[{"word":"hola","start":0,"end":0.5}]}`)
		},
		func(w http.ResponseWriter, r *http.Request) {
			transJSON(w, `{"choices":[{"message":{"content":"{\"replacements\":[]}"}}]}`)
		},
	)
	cordialStubPipeline(t)

	c := &cordialCapture{}
	if err := (Cordial{}).Handle(cordialAudioCtx(c, "cordial", 10)); err != nil {
		t.Fatalf("Handle devolvió error: %v", err)
	}
	if len(c.texts) != 1 || c.texts[0] != "✨ Este audio ya es bastante cordial. No hay nada que parchear." {
		t.Fatalf("texto inesperado: %#v", c.texts)
	}
	wantReactions := []string{"👂", "🧠", "✅"}
	if len(c.reactions) != len(wantReactions) {
		t.Fatalf("reacciones inesperadas: %#v", c.reactions)
	}
	for i, want := range wantReactions {
		if c.reactions[i] != want {
			t.Fatalf("reacciones inesperadas: %#v", c.reactions)
		}
	}
}

func TestCordialParcheaAudio(t *testing.T) {
	cordialSetup(t,
		func(w http.ResponseWriter, r *http.Request) {
			transJSON(w, `{"text":"eres tonto","words":[{"word":"eres","start":0,"end":0.5},{"word":"tonto","start":0.5,"end":1.0}]}`)
		},
		func(w http.ResponseWriter, r *http.Request) {
			transJSON(w, cordialDeepSeekBody("```json\n"+`{"replacements":[{"original":"tonto","replacement":"amable"}]}`+"\n```"))
		},
	)
	cordialStubPipeline(t)

	c := &cordialCapture{}
	if err := (Cordial{}).Handle(cordialAudioCtx(c, "cordial", 10)); err != nil {
		t.Fatalf("Handle devolvió error: %v", err)
	}
	wantReactions := []string{"👂", "🧠", "🛠️", "✨"}
	if len(c.reactions) != len(wantReactions) {
		t.Fatalf("reacciones inesperadas: %#v", c.reactions)
	}
	for i, want := range wantReactions {
		if c.reactions[i] != want {
			t.Fatalf("reacciones inesperadas: %#v", c.reactions)
		}
	}
	if len(c.msgs) == 0 {
		t.Fatal("no se envió ningún mensaje")
	}
	last := c.msgs[len(c.msgs)-1]
	audio := last.GetAudioMessage()
	if audio == nil {
		t.Fatalf("el último mensaje no es audio: %#v", last)
	}
	if !audio.GetPTT() {
		t.Fatal("el audio debía enviarse como nota de voz")
	}
	if audio.GetSeconds() != 12 {
		t.Fatalf("segundos inesperados: %d", audio.GetSeconds())
	}
}

func TestCordialErrorDeMapeo(t *testing.T) {
	cordialSetup(t,
		func(w http.ResponseWriter, r *http.Request) {
			transJSON(w, `{"text":"eres tonto","words":[{"word":"eres","start":0,"end":0.5}]}`)
		},
		func(w http.ResponseWriter, r *http.Request) {
			transJSON(w, cordialDeepSeekBody(`{"replacements":[{"original":"grosero","replacement":"amable"}]}`))
		},
	)
	cordialStubPipeline(t)

	c := &cordialCapture{}
	if err := (Cordial{}).Handle(cordialAudioCtx(c, "cordial", 10)); err != nil {
		t.Fatalf("Handle devolvió error: %v", err)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "no pude mapearlos al audio") {
		t.Fatalf("texto inesperado: %#v", c.texts)
	}
	if len(c.reactions) != 4 || c.reactions[3] != "❌" {
		t.Fatalf("reacciones inesperadas: %#v", c.reactions)
	}
}

func TestCordialAtempoChain(t *testing.T) {
	cases := map[float64]string{
		1:    "anull",
		1.5:  "atempo=1.50",
		0.25: "atempo=0.5,atempo=0.50",
		4:    "atempo=2.0,atempo=2.00",
	}
	for ratio, want := range cases {
		if got := cordialAtempoChain(ratio); got != want {
			t.Errorf("ratio %v: got %q want %q", ratio, got, want)
		}
	}
}

func TestCordialCleanWord(t *testing.T) {
	cases := map[string]string{
		"¡Tonto!":       "tonto",
		"  Mañana, sí.": "  mañana sí",
		"a_b-1":         "a_b1",
	}
	for in, want := range cases {
		if got := cordialCleanWord(in); got != want {
			t.Errorf("cleanWord(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCordialMapPatches(t *testing.T) {
	words := []cordialWord{
		{Word: "eres", Start: 0, End: 0.4},
		{Word: "muy", Start: 0.4, End: 0.7},
		{Word: "tonto", Start: 0.7, End: 1.2},
	}
	reps := []cordialReplacement{{Original: "muy tonto", Replacement: "muy amable"}}
	patches := cordialMapPatches(words, reps)
	if len(patches) != 1 {
		t.Fatalf("esperaba 1 parche, obtuve %#v", patches)
	}
	p := patches[0]
	if p.Start != 0.4 || p.End != 1.2 || p.Text != "muy amable" {
		t.Fatalf("parche inesperado: %#v", p)
	}
	if p.Duration < 0.79 || p.Duration > 0.81 {
		t.Fatalf("duración inesperada: %v", p.Duration)
	}
}
