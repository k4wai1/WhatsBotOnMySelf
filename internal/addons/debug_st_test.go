package addons

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"whatsbot-go/internal/core"
)

func TestDebugStCommands(t *testing.T) {
	got := (&debugStAddon{}).Commands()
	if len(got) != 2 || got[0] != "debug-st" || got[1] != "sdebug" {
		t.Fatalf("comandos=%#v", got)
	}
}

func debugStSticker() *waE2E.StickerMessage {
	return &waE2E.StickerMessage{
		URL:                proto.String("https://mmg.example/x"),
		Mimetype:           proto.String("image/webp"),
		Width:              proto.Uint32(512),
		Height:             proto.Uint32(512),
		FileLength:         proto.Uint64(12345),
		IsAnimated:         proto.Bool(true),
		IsAvatar:           proto.Bool(true),
		IsAiSticker:        proto.Bool(true),
		Emojis:             proto.String("🎵"),
		AccessibilityLabel: proto.String("sticker de prueba"),
		PngThumbnail:       []byte("thumb"),
		MediaKey:           []byte("SUPERSECRETKEY0123456789abcdefgh"),
		DirectPath:         proto.String("/v/t62/abc?token=SECRETQUERY"),
		FileSHA256:         []byte("sha256"),
		FileEncSHA256:      []byte("encsha"),
	}
}

func debugStAudio() *waE2E.AudioMessage {
	return &waE2E.AudioMessage{
		Mimetype:         proto.String("audio/ogg; codecs=opus"),
		PTT:              proto.Bool(true),
		Seconds:          proto.Uint32(3),
		FileLength:       proto.Uint64(999),
		Waveform:         []byte{1, 2, 3, 4},
		StreamingSidecar: []byte{5, 6},
		ViewOnce:         proto.Bool(false),
		DirectPath:       proto.String("/v/t62/audio?token=AUDIOSECRET"),
		MediaKey:         []byte("AUDIOSECRETKEY0123456789abcdefgh"),
		FileSHA256:       []byte("asha"),
		FileEncSHA256:    []byte("aenc"),
	}
}

func TestDebugStWrappers(t *testing.T) {
	st := debugStSticker()

	directo := debugStInspectWrappers(&waE2E.Message{StickerMessage: st})
	if directo.Sticker == nil || len(directo.Chain) != 0 {
		t.Fatalf("directo=%+v", directo)
	}

	audio := debugStInspectWrappers(&waE2E.Message{AudioStickerMessage: &waE2E.FutureProofMessage{
		Message: &waE2E.Message{StickerMessage: st},
	}})
	if audio.Sticker == nil || !strings.Contains(strings.Join(audio.Chain, " "), "134") {
		t.Fatalf("audio chain=%v", audio.Chain)
	}

	lottie := debugStInspectWrappers(&waE2E.Message{LottieStickerMessage: &waE2E.FutureProofMessage{
		Message: &waE2E.Message{StickerMessage: st},
	}})
	if lottie.Sticker == nil || !strings.Contains(strings.Join(lottie.Chain, " "), "74") {
		t.Fatalf("lottie chain=%v", lottie.Chain)
	}

	doble := debugStInspectWrappers(&waE2E.Message{EphemeralMessage: &waE2E.FutureProofMessage{
		Message: &waE2E.Message{ViewOnceMessageV2: &waE2E.FutureProofMessage{
			Message: &waE2E.Message{StickerMessage: st},
		}},
	}})
	texto := strings.Join(doble.Chain, " → ")
	if doble.Sticker == nil || !strings.Contains(texto, "Ephemeral") || !strings.Contains(texto, "ViewOnceMessageV2") {
		t.Fatalf("doble chain=%v", doble.Chain)
	}

	if w := debugStInspectWrappers(&waE2E.Message{Conversation: proto.String("hola")}); w.Sticker != nil {
		t.Fatal("un texto no debe tener sticker")
	}
}

func debugStCtx(t *testing.T, quoted *waE2E.Message) (*capture, *core.Context) {
	t.Helper()
	c := &capture{}
	ctx := wagoCtx("1205@g.us")
	ctx.Message.Info.Type = "media"
	ctx.Message.Info.MediaType = "sticker"
	ctx.Message.Info.ID = "CURRENT1"
	ctx.Message.Message = &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text: proto.String(".debug-st"),
		ContextInfo: &waE2E.ContextInfo{
			StanzaID:      proto.String("QUOTED123"),
			Participant:   proto.String("584222333444@s.whatsapp.net"),
			QuotedMessage: quoted,
		},
	}}
	ctx.SendHook = func(m *waE2E.Message) error {
		c.texts = append(c.texts, messageText(m))
		return nil
	}
	ctx.ReactHook = func(e string) error {
		c.reactions = append(c.reactions, e)
		return nil
	}
	return c, ctx
}

func TestDebugStHandleReport(t *testing.T) {
	debugStResetCache()
	t.Cleanup(debugStResetCache)
	st := debugStSticker()
	st.Audio = &waE2E.StickerMessage_AudioMessage{AudioMessage: debugStAudio()}
	msg := &waE2E.Message{AudioStickerMessage: &waE2E.FutureProofMessage{
		Message: &waE2E.Message{StickerMessage: st},
	}}

	var logs []string
	prevLog := debugStLog
	debugStLog = func(msg string, args ...any) { logs = append(logs, msg) }
	t.Cleanup(func() { debugStLog = prevLog })

	c, ctx := debugStCtx(t, msg)
	if err := (&debugStAddon{}).Handle(ctx); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(c.texts) != 1 {
		t.Fatalf("textos=%#v", c.texts)
	}
	report := c.texts[0]

	esperado := []string{
		"DEBUG STICKER",
		"fallback QuotedMessage",
		"reenvía el sticker primero",
		"AudioStickerMessage (campo 134)",
		"Stanza (mensaje actual)",
		"MediaType: sticker",
		"QUOTED123",
		"Mimetype: image/webp",
		"Tamaño: 512x512",
		"FileLength: 12345",
		"IsAnimated: true",
		"IsAvatar: true",
		"IsAiSticker: true",
		"Emojis: 🎵",
		"AccessibilityLabel: sticker de prueba",
		"PngThumbnail: 5 bytes",
		"MediaKey: 32 bytes",
		"DirectPath: /v/t62/abc",
		"AudioMessage anidado (campo 26)",
		"PTT: true",
		"Seconds: 3",
		"Waveform: 4 bytes",
		"StreamingSidecar: 2 bytes",
		"ViewOnce: false",
		"DirectPath: /v/t62/audio",
	}
	for _, want := range esperado {
		if !strings.Contains(report, want) {
			t.Fatalf("reporte sin %q:\n%s", want, report)
		}
	}
	for _, prohibido := range []string{"SUPERSECRETKEY", "AUDIOSECRETKEY", "SECRETQUERY", "AUDIOSECRET"} {
		if strings.Contains(report, prohibido) {
			t.Fatalf("el reporte filtró %q:\n%s", prohibido, report)
		}
	}
	if strings.Contains(report, "Descarga real") {
		t.Fatal("sin `full` no debía descargar")
	}
	if len(c.reactions) != 1 || c.reactions[0] != "✅" {
		t.Fatalf("reacciones=%#v", c.reactions)
	}
	if len(logs) == 0 {
		t.Fatal("debía volcar el JSON del protobuf al log")
	}
}

func TestDebugStSinCita(t *testing.T) {
	c := &capture{}
	ctx := wagoCtx("1205@g.us")
	ctx.Command = "debug-st"
	ctx.SendHook = func(m *waE2E.Message) error {
		c.texts = append(c.texts, messageText(m))
		return nil
	}
	if err := (&debugStAddon{}).Handle(ctx); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "Cita un sticker") {
		t.Fatalf("textos=%#v", c.texts)
	}
}

func TestDebugStCitaNoSticker(t *testing.T) {
	debugStResetCache()
	t.Cleanup(debugStResetCache)
	c, ctx := debugStCtx(t, &waE2E.Message{Conversation: proto.String("hola")})
	if err := (&debugStAddon{}).Handle(ctx); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "no contiene un StickerMessage") {
		t.Fatalf("textos=%#v", c.texts)
	}
}

// ─── EXIF ──────────────────────────────────────────────────────────────────

func debugStWebPWithChunk(fourcc string, payload []byte) []byte {
	var body bytes.Buffer
	body.WriteString("WEBP")
	body.WriteString(fourcc)
	var size [4]byte
	binary.LittleEndian.PutUint32(size[:], uint32(len(payload)))
	body.Write(size[:])
	body.Write(payload)
	if len(payload)%2 == 1 {
		body.WriteByte(0)
	}
	var out bytes.Buffer
	out.WriteString("RIFF")
	binary.LittleEndian.PutUint32(size[:], uint32(body.Len()))
	out.Write(size[:])
	out.Write(body.Bytes())
	return out.Bytes()
}

func TestDebugStEXIFJSON(t *testing.T) {
	exif := saExifBlob("PackX", "PubY")
	webp := debugStWebPWithChunk("EXIF", exif)

	texto, n, ok := debugStEXIFJSON(webp)
	if !ok || n != len(exif) {
		t.Fatalf("ok=%v n=%d esperaba %d", ok, n, len(exif))
	}
	if !strings.Contains(texto, "sticker-pack-name") || !strings.Contains(texto, "PackX") || !strings.Contains(texto, "PubY") {
		t.Fatalf("JSON EXIF inesperado: %q", texto)
	}

	// Fallback: JSON plano dentro del chunk, sin cabecera TIFF.
	plano := []byte(`{"ok":true,"pack":"prueba"}`)
	webp2 := debugStWebPWithChunk("EXIF", plano)
	texto, _, ok = debugStEXIFJSON(webp2)
	if !ok || texto != string(plano) {
		t.Fatalf("fallback JSON=%q ok=%v", texto, ok)
	}

	if _, _, ok := debugStEXIFJSON(debugStWebPWithChunk("VP8X", []byte{1, 2, 3})); ok {
		t.Fatal("no debía encontrar EXIF")
	}
}

// ─── ffprobe ───────────────────────────────────────────────────────────────

func TestDebugStParseProbe(t *testing.T) {
	raw := []byte(`{"format":{"duration":"2.50"},"streams":[{"codec_type":"video","codec_name":"h264"},{"codec_type":"audio","codec_name":"opus","channels":1,"sample_rate":"48000"}]}`)
	res, err := debugStParseProbe(raw)
	if err != nil {
		t.Fatalf("debugStParseProbe: %v", err)
	}
	if res.Codec != "opus" || res.Channels != 1 || res.SampleRate != "48000" || res.Duration != 2.5 {
		t.Fatalf("resultado=%+v", res)
	}

	if _, err := debugStParseProbe([]byte(`{"streams":[{"codec_type":"video"}]}`)); err == nil {
		t.Fatal("se esperaba error sin pista de audio")
	}
	if _, err := debugStParseProbe([]byte("no json")); err == nil {
		t.Fatal("se esperaba error de JSON")
	}
}

func TestDebugStProbeAudioReal(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg no disponible")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe no disponible")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "sine.ogg")
	cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000",
		"-t", "0.5", "-c:a", "libopus", "-ac", "1", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("no se pudo generar el OGG: %v (%s)", err, out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	res, err := debugStProbeAudio(data)
	if err != nil {
		t.Fatalf("debugStProbeAudio: %v", err)
	}
	if res.Codec != "opus" || res.Channels != 1 || res.SampleRate != "48000" || res.Duration <= 0 {
		t.Fatalf("resultado=%+v", res)
	}
}

func TestDebugStFull(t *testing.T) {
	debugStResetCache()
	t.Cleanup(debugStResetCache)
	st := debugStSticker()
	st.Audio = &waE2E.StickerMessage_AudioMessage{AudioMessage: debugStAudio()}
	msg := &waE2E.Message{AudioStickerMessage: &waE2E.FutureProofMessage{
		Message: &waE2E.Message{StickerMessage: st},
	}}

	webp := debugStWebPWithChunk("EXIF", saExifBlob("PackFull", "PubFull"))
	ogg := []byte("OggS-fake")

	prevDownload, prevProbe, prevLog := debugStDownloadFn, debugStProbeFn, debugStLog
	debugStDownloadFn = func(_ *core.Context, media whatsmeow.DownloadableMessage) ([]byte, error) {
		switch media.(type) {
		case *waE2E.StickerMessage:
			return webp, nil
		case *waE2E.AudioMessage:
			return ogg, nil
		}
		return nil, nil
	}
	debugStProbeFn = func([]byte) (debugStProbeResult, error) {
		return debugStProbeResult{Codec: "opus", Channels: 1, SampleRate: "48000", Duration: 3}, nil
	}
	var logs []string
	debugStLog = func(msg string, args ...any) { logs = append(logs, msg) }
	t.Cleanup(func() { debugStDownloadFn, debugStProbeFn, debugStLog = prevDownload, prevProbe, prevLog })

	c, ctx := debugStCtx(t, msg)
	ctx.Args = []string{"full"}
	if err := (&debugStAddon{}).Handle(ctx); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(c.texts) != 1 {
		t.Fatalf("textos=%#v", c.texts)
	}
	report := c.texts[0]
	for _, want := range []string{
		"Descarga real + EXIF + ffprobe",
		"WebP: ✅ descifrado OK",
		"EXIF: encontrado",
		"PackFull",
		"Audio: ✅ descifrado OK",
		"codec=opus, canales=1, sample_rate=48000 Hz, duración=3.00 s",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("reporte full sin %q:\n%s", want, report)
		}
	}
	if len(logs) < 2 {
		t.Fatalf("debía registrar protobuf y EXIF: %#v", logs)
	}
}

func TestDebugStFullErrorDescarga(t *testing.T) {
	debugStResetCache()
	t.Cleanup(debugStResetCache)
	st := debugStSticker()
	st.Audio = &waE2E.StickerMessage_AudioMessage{AudioMessage: debugStAudio()}
	msg := &waE2E.Message{StickerMessage: st}

	prevDownload, prevLog := debugStDownloadFn, debugStLog
	debugStDownloadFn = func(*core.Context, whatsmeow.DownloadableMessage) ([]byte, error) {
		return nil, os.ErrPermission
	}
	debugStLog = func(string, ...any) {}
	t.Cleanup(func() { debugStDownloadFn, debugStLog = prevDownload, prevLog })

	c, ctx := debugStCtx(t, msg)
	ctx.Args = []string{"full"}
	if err := (&debugStAddon{}).Handle(ctx); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	report := c.texts[0]
	if !strings.Contains(report, "WebP: ❌ error al descargar") {
		t.Fatalf("reporte=%s", report)
	}
	if !strings.Contains(report, "Audio: ❌ error al descargar") {
		t.Fatalf("reporte=%s", report)
	}
}

func TestDebugStSecretYPath(t *testing.T) {
	if got := debugStSecret(nil); got != "no" {
		t.Fatalf("nil=%q", got)
	}
	got := debugStSecret([]byte{0xde, 0xad, 0xbe, 0xef, 0x01})
	if got != "5 bytes (prefijo deadbeef)" {
		t.Fatalf("secret=%q", got)
	}
	if got := debugStPath("/v/t62/x?token=abc"); got != "/v/t62/x" {
		t.Fatalf("path=%q", got)
	}
	if got := debugStPath(""); got != "no" {
		t.Fatalf("path vacío=%q", got)
	}
}

// ─── Caché de protobuf original ────────────────────────────────────────────

func TestDebugStRememberYCacheFIFO(t *testing.T) {
	debugStResetCache()
	t.Cleanup(debugStResetCache)

	if debugStRemember(&events.Message{Info: types.MessageInfo{ID: "texto"}, Message: &waE2E.Message{Conversation: proto.String("hola")}}) {
		t.Fatal("un texto no debe cachearse")
	}
	if debugStRemember(nil) || debugStRemember(&events.Message{Info: types.MessageInfo{ID: "vacio"}}) {
		t.Fatal("mensajes inválidos no deben cachearse")
	}

	for i := 0; i < debugStCacheLimit+5; i++ {
		msg := &events.Message{
			Info: types.MessageInfo{
				ID:        types.MessageID(fmt.Sprintf("ID%03d", i)),
				Type:      "media",
				MediaType: "sticker",
			},
			Message: &waE2E.Message{StickerMessage: &waE2E.StickerMessage{
				Mimetype: proto.String("image/webp"),
			}},
		}
		if !debugStRemember(msg) {
			t.Fatalf("no se cacheó el sticker %d", i)
		}
	}
	if got := debugStCacheLen(); got != debugStCacheLimit {
		t.Fatalf("caché=%d, esperaba %d", got, debugStCacheLimit)
	}
	if _, ok := debugStCacheGet("ID000"); ok {
		t.Fatal("el más viejo debía haberse evictado (FIFO)")
	}
	item, ok := debugStCacheGet("ID005")
	if !ok || item == nil || item.Type != "media" || item.MediaType != "sticker" {
		t.Fatalf("entrada reciente=%+v ok=%v", item, ok)
	}
	if _, ok := debugStCacheGet("NO-EXISTE"); ok {
		t.Fatal("no debía existir")
	}
}

func TestDebugStUsaCacheOriginal(t *testing.T) {
	debugStResetCache()
	t.Cleanup(debugStResetCache)

	st := debugStSticker()
	st.Audio = &waE2E.StickerMessage_AudioMessage{AudioMessage: debugStAudio()}
	fullMsg := &waE2E.Message{AudioStickerMessage: &waE2E.FutureProofMessage{
		Message: &waE2E.Message{StickerMessage: st},
	}}
	const id = "CACHED-ORIGINAL-1"
	debugStRemember(&events.Message{
		Info: types.MessageInfo{
			ID:        types.MessageID(id),
			Type:      "media",
			MediaType: "sticker",
		},
		Message: fullMsg,
	})

	// La cita viene recortada: sticker sin AudioMessage.
	trimmed := &waE2E.Message{StickerMessage: &waE2E.StickerMessage{
		Mimetype: proto.String("image/webp"),
		Width:    proto.Uint32(512),
		Height:   proto.Uint32(512),
	}}

	webp := debugStWebPWithChunk("EXIF", saExifBlob("PackCache", "PubCache"))
	prevDownload, prevProbe, prevLog := debugStDownloadFn, debugStProbeFn, debugStLog
	debugStDownloadFn = func(_ *core.Context, media whatsmeow.DownloadableMessage) ([]byte, error) {
		switch media.(type) {
		case *waE2E.StickerMessage:
			return webp, nil
		case *waE2E.AudioMessage:
			return []byte("OggS-cache"), nil
		}
		return nil, nil
	}
	debugStProbeFn = func([]byte) (debugStProbeResult, error) {
		return debugStProbeResult{Codec: "opus", Channels: 1, SampleRate: "48000", Duration: 3}, nil
	}
	debugStLog = func(string, ...any) {}
	t.Cleanup(func() { debugStDownloadFn, debugStProbeFn, debugStLog = prevDownload, prevProbe, prevLog })

	c, ctx := debugStCtx(t, trimmed)
	ctx.Message.Message.GetExtendedTextMessage().ContextInfo.StanzaID = proto.String(id)
	ctx.Args = []string{"full"}
	if err := (&debugStAddon{}).Handle(ctx); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	report := c.texts[0]
	for _, want := range []string{
		"caché original en vivo",
		"Tipo: media (original cacheado)",
		"MediaType: sticker (original cacheado)",
		"AudioStickerMessage (campo 134)",
		"AudioMessage anidado (campo 26)",
		"Seconds: 3",
		"Descarga real + EXIF + ffprobe",
		"codec=opus",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("reporte con caché sin %q:\n%s", want, report)
		}
	}
}
