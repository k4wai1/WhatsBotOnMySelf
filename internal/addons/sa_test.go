package addons

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestSaCommands(t *testing.T) {
	got := (&saAddon{}).Commands()
	if len(got) != 1 || got[0] != "sa" {
		t.Fatalf("comandos=%#v", got)
	}
}

func TestSaParseArgs(t *testing.T) {
	def := saArgs{Shape: "none", Mode: "default"}
	conDef := func(a saArgs) saArgs {
		if a.Shape == "" {
			a.Shape = "none"
		}
		if a.Mode == "" {
			a.Mode = "default"
		}
		return a
	}
	casos := []struct {
		args []string
		want saArgs
	}{
		{nil, def},
		{[]string{"0:03"}, conDef(saArgs{Start: 3})},
		{[]string{"0:03", "5"}, conDef(saArgs{Start: 3, Duration: 5})},
		{[]string{"2", "5"}, conDef(saArgs{Start: 2, Duration: 5})},
		{[]string{"1:02:03"}, conDef(saArgs{Start: 3723})},
		{[]string{"auto"}, conDef(saArgs{Auto: true})},
		{[]string{"wave"}, conDef(saArgs{Wave: true})},
		{[]string{"inspect"}, conDef(saArgs{Inspect: true})},
		{[]string{"audio"}, conDef(saArgs{ExtractAudio: true})},
		{[]string{"AUTO", "2", "4"}, conDef(saArgs{Auto: true, Start: 2, Duration: 4})},
		{[]string{"crop"}, conDef(saArgs{Crop: true})},
		{[]string{"c"}, conDef(saArgs{Crop: true})},
		{[]string{"e"}, conDef(saArgs{Stretch: true})},
		{[]string{"estirar"}, conDef(saArgs{Stretch: true})},
		{[]string{"reverse"}, conDef(saArgs{Reverse: true})},
		{[]string{"r"}, conDef(saArgs{Reverse: true})},
		{[]string{"spherical"}, conDef(saArgs{Crop: true, Shape: "spherical"})},
		{[]string{"s"}, conDef(saArgs{Crop: true, Shape: "spherical"})},
		{[]string{"border"}, conDef(saArgs{Shape: "border"})},
		{[]string{"b"}, conDef(saArgs{Shape: "border"})},
		{[]string{"high"}, conDef(saArgs{Mode: "high"})},
		{[]string{"h"}, conDef(saArgs{Mode: "high"})},
		{[]string{"fluid"}, conDef(saArgs{Mode: "fluid"})},
		{[]string{"f"}, conDef(saArgs{Mode: "fluid"})},
		{[]string{"crop", "e", "b", "0:02", "3"}, conDef(saArgs{Crop: true, Stretch: true, Shape: "border", Start: 2, Duration: 3})},
	}
	for _, caso := range casos {
		got := saParseArgs(caso.args)
		if got != caso.want {
			t.Fatalf("saParseArgs(%q)=%+v, esperaba %+v", caso.args, got, caso.want)
		}
	}
}

func TestSaParseClock(t *testing.T) {
	if v, ok := saParseClock("0:03"); !ok || v != 3 {
		t.Fatalf("0:03 → %v %v", v, ok)
	}
	if v, ok := saParseClock("1:00:00"); !ok || v != 3600 {
		t.Fatalf("1:00:00 → %v %v", v, ok)
	}
	if _, ok := saParseClock("hola"); ok {
		t.Fatal("hola no es un tiempo")
	}
	if _, ok := saParseClock("1:"); ok {
		t.Fatal("1: no es un tiempo")
	}
}

func TestSaPlanLimits(t *testing.T) {
	estatico := saPlanFor(false)
	if estatico.SizeLimit != 100_000 || estatico.MaxAudio != 10 || estatico.Animated {
		t.Fatalf("plan estático=%+v", estatico)
	}
	animado := saPlanFor(true)
	if animado.SizeLimit != 1_000_000 || animado.MaxAudio != 10 || !animado.Animated {
		t.Fatalf("plan animado=%+v", animado)
	}
}

func TestSaClampDuration(t *testing.T) {
	casos := []struct{ d, max, want float64 }{
		{0, 5, 5},
		{3, 5, 3},
		{9, 5, 5},
		{-1, 10, 10},
	}
	for _, c := range casos {
		if got := saClampDuration(c.d, c.max); got != c.want {
			t.Fatalf("saClampDuration(%v,%v)=%v, esperaba %v", c.d, c.max, got, c.want)
		}
	}
}

func TestSaCalcFPSAdaptativo(t *testing.T) {
	anterior := saCalcFPS(0.5)
	for _, dur := range []float64{1.6, 3.1, 5.0, 9.0} {
		actual := saCalcFPS(dur)
		if actual > anterior {
			t.Fatalf("fps no decrece con la duración: %v → %v", anterior, actual)
		}
		anterior = actual
	}
	if saCalcFPS(0.5) != 20 || saCalcFPS(9) != 12 {
		t.Fatalf("extremos inesperados: %d/%d", saCalcFPS(0.5), saCalcFPS(9))
	}
}

func TestSaQualityRange(t *testing.T) {
	if min, max := saQualityRange(false); min != 10 || max != 100 {
		t.Fatalf("estático=(%d,%d)", min, max)
	}
	if min, max := saQualityRange(true); min != 20 || max != 100 {
		t.Fatalf("animado=(%d,%d)", min, max)
	}
}

func TestSaSessionsExpiran(t *testing.T) {
	saResetSessions()
	prev := saNow
	ahora := time.Unix(1700000000, 0)
	saNow = func() time.Time { return ahora }
	t.Cleanup(func() { saNow = prev; saResetSessions() })

	saStoreVisual("chat", "user", &saVisual{Data: []byte("webp"), Animated: true})
	s := saGetSession("chat", "user")
	if s.Visual == nil || string(s.Visual.Data) != "webp" {
		t.Fatalf("sesión sin visual: %+v", s)
	}

	saStoreAudio("chat", "user", []byte("ogg"), 4, true)
	s = saGetSession("chat", "user")
	if s.Visual == nil || len(s.Audio) != 3 || s.AudioSeconds != 4 || !s.AudioPTT {
		t.Fatalf("sesión fusionada incorrecta: %+v", s)
	}

	ahora = ahora.Add(saSessionTTL + time.Second)
	if s := saGetSession("chat", "user"); s.Visual != nil || len(s.Audio) != 0 {
		t.Fatalf("la sesión debía expirar: %+v", s)
	}

	saStoreVisual("chat", "user", &saVisual{Data: []byte("x")})
	saClearSession("chat", "user")
	if s := saGetSession("chat", "user"); s.Visual != nil {
		t.Fatalf("la sesión debía borrarse: %+v", s)
	}
}

func TestSaSesionPorUsuarioYChat(t *testing.T) {
	saResetSessions()
	t.Cleanup(saResetSessions)

	saStoreVisual("chatA", "user1", &saVisual{Data: []byte("a")})
	if s := saGetSession("chatA", "user2"); s.Visual != nil {
		t.Fatal("no debe compartirse entre usuarios")
	}
	if s := saGetSession("chatB", "user1"); s.Visual != nil {
		t.Fatal("no debe compartirse entre chats")
	}
	if s := saGetSession("chatA", "user1"); s.Visual == nil {
		t.Fatal("la sesión propia debía existir")
	}
}

func TestSaCooldown(t *testing.T) {
	saResetCooldowns()
	saResetSessions()
	prev := saNow
	ahora := time.Unix(1700000000, 0)
	saNow = func() time.Time { return ahora }
	t.Cleanup(func() { saNow = prev; saResetCooldowns(); saResetSessions() })
	t.Setenv("SA_COOLDOWN_SEC", "15")
	t.Setenv("OWNER_NUMBERS", "")

	ctx := wagoCtx("1205@g.us")
	if ok, _ := saCheckCooldown(ctx); !ok {
		t.Fatal("el primer uso debía permitirse")
	}
	ok, wait := saCheckCooldown(ctx)
	if ok || wait <= 0 || wait > 15*time.Second {
		t.Fatalf("el segundo uso debía bloquearse: ok=%v wait=%v", ok, wait)
	}
	ahora = ahora.Add(16 * time.Second)
	if ok, _ := saCheckCooldown(ctx); !ok {
		t.Fatal("tras la ventana debía permitirse")
	}
}

func TestSaCooldownDuenoExento(t *testing.T) {
	saResetCooldowns()
	prev := saNow
	ahora := time.Unix(1700000000, 0)
	saNow = func() time.Time { return ahora }
	t.Cleanup(func() { saNow = prev; saResetCooldowns() })
	t.Setenv("SA_COOLDOWN_SEC", "60")
	t.Setenv("OWNER_NUMBERS", "58412345678")

	ctx := wagoCtx("1205@g.us")
	for i := 0; i < 3; i++ {
		if ok, _ := saCheckCooldown(ctx); !ok {
			t.Fatalf("el dueño debía pasar en el intento %d", i+1)
		}
	}
}

func TestSaCooldownDesactivado(t *testing.T) {
	saResetCooldowns()
	t.Cleanup(saResetCooldowns)
	t.Setenv("SA_COOLDOWN_SEC", "0")
	t.Setenv("OWNER_NUMBERS", "")

	ctx := wagoCtx("1205@g.us")
	for i := 0; i < 3; i++ {
		if ok, _ := saCheckCooldown(ctx); !ok {
			t.Fatalf("cooldown 0 debía permitir el intento %d", i+1)
		}
	}
}

func saTestUpload(tag string) whatsmeow.UploadResponse {
	return whatsmeow.UploadResponse{
		URL:           "https://mmg.example/" + tag,
		DirectPath:    "/v/" + tag,
		FileSHA256:    []byte("sha-" + tag),
		FileEncSHA256: []byte("enc-" + tag),
		MediaKey:      []byte("key-" + tag),
		FileLength:    uint64(len(tag) * 100),
	}
}

func TestSaBuildAudioStickerWrap134(t *testing.T) {
	webp := saTestUpload("webp")
	audio := saTestUpload("ogg")
	msg := saBuildAudioSticker(webp, audio, 4, true, false, true)

	if msg.GetAudioStickerMessage() == nil {
		t.Fatal("debía venir envuelto en el campo 134")
	}
	inner := msg.GetAudioStickerMessage().GetMessage()
	if inner == nil || inner.GetStickerMessage() == nil {
		t.Fatal("falta el StickerMessage interno")
	}
	st := inner.GetStickerMessage()
	if st.GetWidth() != 512 || st.GetHeight() != 512 || !st.GetIsAnimated() {
		t.Fatalf("sticker inesperado: w=%d h=%d anim=%v", st.GetWidth(), st.GetHeight(), st.GetIsAnimated())
	}
	if st.GetURL() != webp.URL || st.GetFileLength() != webp.FileLength {
		t.Fatalf("metadatos WebP incorrectos: %+v", st)
	}
	am := st.GetAudioMessage()
	if am == nil {
		t.Fatal("falta el AudioMessage del campo 26")
	}
	if am.GetMimetype() != "audio/ogg; codecs=opus" || am.GetSeconds() != 4 {
		t.Fatalf("audio inesperado: %q %d", am.GetMimetype(), am.GetSeconds())
	}
	if am.GetPTT() {
		t.Fatal("PTT debía ser false")
	}
	if am.GetURL() != audio.URL || am.GetFileLength() != audio.FileLength {
		t.Fatalf("metadatos OGG incorrectos: %+v", am)
	}
}

func TestSaBuildAudioStickerSinWrapYPTT(t *testing.T) {
	webp := saTestUpload("w2")
	audio := saTestUpload("a2")
	msg := saBuildAudioSticker(webp, audio, 9, false, true, false)

	if msg.GetAudioStickerMessage() != nil {
		t.Fatal("no debía venir envuelto en 134")
	}
	st := msg.GetStickerMessage()
	if st == nil || st.GetAudioMessage() == nil {
		t.Fatal("falta StickerMessage con audio directo")
	}
	if !st.GetAudioMessage().GetPTT() {
		t.Fatal("PTT debía ser true")
	}
	if st.GetWidth() != 512 || st.GetHeight() != 512 || st.GetIsAnimated() {
		t.Fatalf("sticker estático inesperado: %+v", st)
	}
	if st.GetAudioMessage().GetSeconds() != 9 {
		t.Fatalf("Seconds=%d", st.GetAudioMessage().GetSeconds())
	}
}

func TestSaBuildAudioStickerSecondsMinimo(t *testing.T) {
	msg := saBuildAudioSticker(saTestUpload("w"), saTestUpload("a"), 0, false, false, false)
	if got := msg.GetStickerMessage().GetAudioMessage().GetSeconds(); got != 1 {
		t.Fatalf("Seconds=%d, esperaba 1", got)
	}
}

func TestSaInspectPayload(t *testing.T) {
	audio := &waE2E.AudioMessage{
		Mimetype: proto.String("audio/ogg; codecs=opus"),
		Seconds:  proto.Uint32(3),
		PTT:      proto.Bool(true),
		Waveform: []byte{1, 2, 3},
	}
	st := &waE2E.StickerMessage{
		Width:      proto.Uint32(512),
		Height:     proto.Uint32(512),
		IsAnimated: proto.Bool(true),
		Audio:      &waE2E.StickerMessage_AudioMessage{AudioMessage: audio},
	}

	envuelto := &waE2E.Message{AudioStickerMessage: &waE2E.FutureProofMessage{
		Message: &waE2E.Message{StickerMessage: st},
	}}
	info, ok := saInspectPayload(envuelto)
	if !ok || !info.Wrapped || !info.PTT || info.Seconds != 3 ||
		info.Mimetype != "audio/ogg; codecs=opus" || !info.Animated ||
		info.Width != 512 || info.Height != 512 || info.Waveform != 3 {
		t.Fatalf("inspección envuelta=%+v ok=%v", info, ok)
	}

	directo := &waE2E.Message{StickerMessage: st}
	info, ok = saInspectPayload(directo)
	if !ok || info.Wrapped {
		t.Fatalf("inspección directa=%+v ok=%v", info, ok)
	}

	if _, ok := saInspectPayload(&waE2E.Message{Conversation: proto.String("hola")}); ok {
		t.Fatal("un texto no debe inspeccionarse")
	}
}

func TestSaClassifyAudioSticker(t *testing.T) {
	audio := &waE2E.AudioMessage{Mimetype: proto.String("audio/ogg"), Seconds: proto.Uint32(2)}
	st := &waE2E.StickerMessage{Audio: &waE2E.StickerMessage_AudioMessage{AudioMessage: audio}, IsAnimated: proto.Bool(false)}
	msg := &waE2E.Message{AudioStickerMessage: &waE2E.FutureProofMessage{
		Message: &waE2E.Message{StickerMessage: st},
	}}

	media := saClassify(msg)
	if media.Kind != saKindSticker || media.Animated || media.NestedAudio == nil {
		t.Fatalf("clasificación=%+v", media)
	}
	if media.NestedAudio.GetSeconds() != 2 {
		t.Fatalf("audio anidado=%+v", media.NestedAudio)
	}

	if k := saClassify(&waE2E.Message{VideoMessage: &waE2E.VideoMessage{}}).Kind; k != saKindVideo {
		t.Fatalf("vídeo=%v", k)
	}
	if k := saClassify(&waE2E.Message{AudioMessage: audio}).Kind; k != saKindAudio {
		t.Fatalf("audio=%v", k)
	}
	if k := saClassify(nil).Kind; k != saKindNone {
		t.Fatalf("nil=%v", k)
	}
}

func TestSaExifBlob(t *testing.T) {
	blob := saExifBlob("Mi Pack", "Mi Publisher")
	if !bytes.HasPrefix(blob, []byte{0x49, 0x49, 0x2A, 0x00}) {
		t.Fatal("falta la cabecera TIFF de WhatsApp")
	}
	if !bytes.Contains(blob, []byte("Mi Pack")) || !bytes.Contains(blob, []byte("Mi Publisher")) {
		t.Fatal("faltan pack name / publisher")
	}
	if !bytes.Contains(blob, []byte("sticker-pack-id")) {
		t.Fatal("falta sticker-pack-id")
	}
}

func TestSaWaveImage(t *testing.T) {
	data := saWaveImage([]byte("audio de prueba"))
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("png inválido: %v", err)
	}
	if img.Bounds().Dx() != 512 || img.Bounds().Dy() != 512 {
		t.Fatalf("tamaño=%v", img.Bounds())
	}
	// Debe ser determinista para el mismo audio.
	if !bytes.Equal(data, saWaveImage([]byte("audio de prueba"))) {
		t.Fatal("la onda debe ser determinista")
	}
}

func TestSaBuildWebPReutilizaSticker(t *testing.T) {
	prevProbe := saProbeFn
	saProbeFn = func(string) saProbeInfo { return saProbeInfo{Width: 512, Height: 512, HasVideo: true} }
	t.Cleanup(func() { saProbeFn = prevProbe })

	original := make([]byte, 40)
	copy(original[0:4], "RIFF")
	copy(original[8:12], "WEBP")
	copy(original[12:16], "VP8X")
	original[24], original[25] = 0xFF, 0x01 // 512
	original[27], original[28] = 0xFF, 0x01 // 512
	v := &saVisual{Data: original, Mimetype: "image/webp", Animated: false, IsSticker: true, IsVideo: true}
	plan := saPlanFor(false)
	budget, finalLimit, err := saWebPBudget(plan, 0)
	if err != nil {
		t.Fatal(err)
	}
	out, err := saBuildWebP(v, plan, saArgs{Shape: "none", Mode: "fluid"}, 0, 5, budget, finalLimit)
	if err != nil {
		t.Fatalf("saBuildWebP: %v", err)
	}
	if !bytes.Equal(out, original) {
		t.Fatalf("debía reutilizar el sticker: %q", out)
	}
}

func TestSaPrepareAudioVacio(t *testing.T) {
	if _, _, err := saPrepareAudio(nil, nil, 0, 5, 0); err == nil {
		t.Fatal("se esperaba error con audio vacío")
	}
}

func TestSaSniffAudioMime(t *testing.T) {
	casos := map[string]string{
		"OggSxxxx": "audio/ogg",
		"ID3xxxxx": "audio/mpeg",
		"RIFFxxxx": "audio/wav",
	}
	for entrada, want := range casos {
		if got := saSniffAudioMime([]byte(entrada)); got != want {
			t.Fatalf("saSniffAudioMime(%q)=%q, esperaba %q", entrada, got, want)
		}
	}
}

func TestSaMimeExt(t *testing.T) {
	casos := map[string]string{
		"image/webp": "webp",
		"image/jpeg": "jpg",
		"video/mp4":  "mp4",
		"audio/ogg":  "ogg",
		"audio/mpeg": "mp3",
		"text/plain": "bin",
	}
	for mime, want := range casos {
		if got := saMimeExt(mime); got != want {
			t.Fatalf("saMimeExt(%q)=%q, esperaba %q", mime, got, want)
		}
	}
}

func TestSaEnvolturaEnv(t *testing.T) {
	t.Setenv("SA_WRAP_134", "")
	t.Setenv("SA_AUDIO_PTT", "")
	if !saWrap134Enabled() || saPTTEnabled() {
		t.Fatal("valores por defecto incorrectos")
	}
	t.Setenv("SA_WRAP_134", "false")
	t.Setenv("SA_AUDIO_PTT", "true")
	if saWrap134Enabled() || !saPTTEnabled() {
		t.Fatal("las variables no se aplicaron")
	}
}

func TestSaUsageText(t *testing.T) {
	texto := saUsageText()
	for _, want := range []string{".sa", "audio", "auto", "inspect"} {
		if !strings.Contains(texto, want) {
			t.Fatalf("ayuda sin %q", want)
		}
	}
}

// TestSaPipelineFFmpeg valida el pipeline real (si ffmpeg/ffprobe existen):
// vídeo → WebP animado 512x512 ≤500 KB y audio OGG/Opus mono ≤5 s.
func TestSaPipelineFFmpeg(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg no disponible")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe no disponible")
	}
	dir := t.TempDir()
	videoPath := filepath.Join(dir, "in.mp4")
	cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size=160x120:rate=12",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000",
		"-t", "1.2", "-c:v", "mpeg4", "-q:v", "5", "-c:a", "aac", videoPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("no se pudo generar el vídeo de prueba: %v (%s)", err, out)
	}
	video, err := os.ReadFile(videoPath)
	if err != nil {
		t.Fatal(err)
	}

	vis := &saVisual{Data: video, Mimetype: "video/mp4", Animated: true, IsVideo: true}
	plan := saPlanFor(true)
	budget, finalLimit, err := saWebPBudget(plan, 0)
	if err != nil {
		t.Fatal(err)
	}
	webp, err := saBuildWebP(vis, plan, saArgs{Shape: "none", Mode: "fluid"}, 0, 1.2, budget, finalLimit)
	if err != nil {
		t.Fatalf("saBuildWebP: %v", err)
	}
	if len(webp) == 0 || len(webp) > saAnimatedLimit {
		t.Fatalf("WebP fuera de límite: %d bytes", len(webp))
	}
	webpPath := filepath.Join(dir, "out.webp")
	if err := os.WriteFile(webpPath, webp, 0o600); err != nil {
		t.Fatal(err)
	}
	w, h, ok := saWebPDims(webp)
	if !ok || w != 512 || h != 512 {
		t.Fatalf("WebP %dx%d ok=%v, esperaba 512x512", w, h, ok)
	}
	if !saWebPIsAnimated(webp) {
		t.Fatal("el WebP debía ser animado")
	}

	ogg, dur, err := saAudioFromVideo(video, 0, 5)
	if err != nil {
		t.Fatalf("saAudioFromVideo: %v", err)
	}
	if len(ogg) == 0 || dur <= 0 || dur > 5.01 {
		t.Fatalf("audio inesperado: %d bytes, %.2f s", len(ogg), dur)
	}
	oggPath := filepath.Join(dir, "out.ogg")
	if err := os.WriteFile(oggPath, ogg, 0o600); err != nil {
		t.Fatal(err)
	}
	info := saProbeDefault(oggPath)
	if !info.HasAudio || info.HasVideo {
		t.Fatalf("OGG inesperado: %+v", info)
	}
}

func TestSaWebPDims(t *testing.T) {
	// VP8X (animado): canvas 512x512.
	vp8x := make([]byte, 30)
	copy(vp8x[0:4], "RIFF")
	copy(vp8x[8:12], "WEBP")
	copy(vp8x[12:16], "VP8X")
	vp8x[24] = 0xFF // 512-1 = 0x1FF
	vp8x[25] = 0x01
	vp8x[27] = 0xFF
	vp8x[28] = 0x01
	if w, h, ok := saWebPDims(vp8x); !ok || w != 512 || h != 512 {
		t.Fatalf("VP8X → %dx%d ok=%v", w, h, ok)
	}

	// VP8 lossy 320x240.
	vp8 := make([]byte, 30)
	copy(vp8[0:4], "RIFF")
	copy(vp8[8:12], "WEBP")
	copy(vp8[12:16], "VP8 ")
	vp8[23], vp8[24], vp8[25] = 0x9d, 0x01, 0x2a
	vp8[26] = 0x40 // 320 = 0x140
	vp8[27] = 0x01
	vp8[28] = 0xf0 // 240 = 0x0F0
	if w, h, ok := saWebPDims(vp8); !ok || w != 320 || h != 240 {
		t.Fatalf("VP8 → %dx%d ok=%v", w, h, ok)
	}

	if _, _, ok := saWebPDims([]byte("no es webp")); ok {
		t.Fatal("datos inválidos no deben detectarse")
	}
}

// TestSaPrepareAudioOGGEntradaNoColisiona reproduce el bug de FFmpeg
// "Output ... same as Input #0": un .ogg de entrada no debe llamarse igual
// que el .ogg de salida.
func TestSaPrepareAudioOGGEntradaNoColisiona(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg no disponible")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe no disponible")
	}
	dir := t.TempDir()
	inPath := filepath.Join(dir, "entrada.ogg")
	cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "sine=frequency=330:sample_rate=48000",
		"-t", "0.8", "-c:a", "libopus", "-ac", "1", inPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("no se pudo generar el OGG de entrada: %v (%s)", err, out)
	}
	data, err := os.ReadFile(inPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("OggS")) {
		t.Fatalf("la entrada no es OGG: %x", data[:4])
	}

	ogg, seconds, err := saPrepareAudio(nil, data, 0, 5, 0)
	if err != nil {
		t.Fatalf("saPrepareAudio(.ogg): %v", err)
	}
	if len(ogg) == 0 || !bytes.HasPrefix(ogg, []byte("OggS")) {
		t.Fatalf("salida OGG inválida: %d bytes", len(ogg))
	}
	if seconds < 1 || seconds > 5 {
		t.Fatalf("duración inesperada: %d", seconds)
	}
	outPath := filepath.Join(dir, "salida.ogg")
	if err := os.WriteFile(outPath, ogg, 0o600); err != nil {
		t.Fatal(err)
	}
	info := saProbeDefault(outPath)
	if !info.HasAudio || info.HasVideo {
		t.Fatalf("OGG de salida inesperado: %+v", info)
	}
}

func TestSaVisualFilterEfectos(t *testing.T) {
	base := saVisualFilter(saArgs{Shape: "none"})
	if !strings.Contains(base, "force_original_aspect_ratio=decrease") || !strings.Contains(base, "pad=512:512") {
		t.Fatalf("filtro base inesperado: %s", base)
	}
	if strings.Contains(base, "geq=") {
		t.Fatalf("el filtro base no debe tener máscara: %s", base)
	}

	crop := saVisualFilter(saArgs{Shape: "none", Crop: true})
	if !strings.Contains(crop, "crop=512:512") || strings.Contains(crop, "pad=") {
		t.Fatalf("filtro crop inesperado: %s", crop)
	}

	stretch := saVisualFilter(saArgs{Shape: "none", Stretch: true})
	if !strings.HasPrefix(stretch, "scale=512:512,format=rgba") || strings.Contains(stretch, "pad=") {
		t.Fatalf("filtro stretch inesperado: %s", stretch)
	}

	spherical := saVisualFilter(saArgs{Shape: "spherical", Crop: true})
	if !strings.Contains(spherical, "geq=") || !strings.Contains(spherical, "min(W,H)/2") {
		t.Fatalf("filtro spherical inesperado: %s", spherical)
	}

	border := saVisualFilter(saArgs{Shape: "border"})
	if !strings.Contains(border, "geq=") || !strings.Contains(border, "min(W,H)*0.1") {
		t.Fatalf("filtro border inesperado: %s", border)
	}

	if saNeedsVisualTransform(saArgs{Shape: "none"}) {
		t.Fatal("sin flags no debe requerir transformación")
	}
	for _, a := range []saArgs{{Crop: true}, {Stretch: true}, {Shape: "spherical"}, {Shape: "border"}} {
		if !saNeedsVisualTransform(a) {
			t.Fatalf("%+v debía requerir transformación", a)
		}
	}
}

func TestSaCalcFPSMode(t *testing.T) {
	if got := saCalcFPSMode("default", 0.5); got != saCalcFPS(0.5) {
		t.Fatalf("modo default=%d", got)
	}
	for _, caso := range []struct {
		dur  float64
		want int
	}{{1, 25}, {4, 20}, {9, 15}} {
		if got := saCalcFPSMode("fluid", caso.dur); got != caso.want {
			t.Fatalf("modo fluid(%.1f)=%d, esperaba %d", caso.dur, got, caso.want)
		}
	}
	if got := saCalcFPSMode("high", 0.5); got != 30 {
		t.Fatalf("modo high corto=%d, esperaba 30", got)
	}
	if got := saCalcFPSMode("high", 9); got != 18 {
		t.Fatalf("modo high largo=%d, esperaba 18", got)
	}
	if saCalcFPS(1) <= saCalcFPS(9) {
		t.Fatal("la curva debe reducir fps al alargar el video")
	}
}

// TestSaBuildWebPTransformacionesFFmpeg valida los filtros de `.st` sobre una
// imagen no cuadrada: crop, estirar, esférica y borde deben dar 512x512.
func TestSaBuildWebPTransformacionesFFmpeg(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg no disponible")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe no disponible")
	}
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "in.png")
	cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size=320x240:rate=1", "-frames:v", "1", imgPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("no se pudo generar la imagen: %v (%s)", err, out)
	}
	img, err := os.ReadFile(imgPath)
	if err != nil {
		t.Fatal(err)
	}

	casos := map[string]saArgs{
		"crop":      {Crop: true, Shape: "none", Mode: "fluid"},
		"estirar":   {Stretch: true, Shape: "none", Mode: "fluid"},
		"spherical": {Crop: true, Shape: "spherical", Mode: "fluid"},
		"border":    {Shape: "border", Mode: "fluid"},
	}
	plan := saPlanFor(false)
	budget, finalLimit, err := saWebPBudget(plan, 0)
	if err != nil {
		t.Fatal(err)
	}
	for nombre, args := range casos {
		v := &saVisual{Data: img, Mimetype: "image/png"}
		webp, err := saBuildWebP(v, plan, args, 0, 5, budget, finalLimit)
		if err != nil {
			t.Fatalf("%s: saBuildWebP: %v", nombre, err)
		}
		if len(webp) == 0 || len(webp) > saStaticLimit {
			t.Fatalf("%s: tamaño fuera de límite: %d", nombre, len(webp))
		}
		w, h, ok := saWebPDims(webp)
		if !ok || w != 512 || h != 512 {
			t.Fatalf("%s: WebP %dx%d ok=%v, esperaba 512x512", nombre, w, h, ok)
		}
	}
}

func TestSaPackDefaults(t *testing.T) {
	t.Setenv("STICKER_PACK_NAME", "")
	t.Setenv("STICKER_PACK_PUBLISHER", "")
	t.Setenv("SA_PACK_NAME", "")
	t.Setenv("SA_PACK_PUBLISHER", "")
	const pack = "˚｡•Wboms • Free • Opensource✮⋆˙"
	const pub = "github.com/k4wai1/WhatsBotOnMySelf"
	if got := saPackName(); got != pack {
		t.Fatalf("pack=%q", got)
	}
	if got := saPackPublisher(); got != pub {
		t.Fatalf("publisher=%q", got)
	}
	blob := saExifBlob(saPackName(), saPackPublisher())
	if !bytes.Contains(blob, []byte(pack)) || !bytes.Contains(blob, []byte(pub)) {
		t.Fatal("el EXIF no incluye pack/publisher por defecto")
	}
}

func TestSaPackEnvOverrides(t *testing.T) {
	t.Setenv("STICKER_PACK_NAME", "Pack Custom")
	t.Setenv("STICKER_PACK_PUBLISHER", "github.com/custom/repo")
	if got := saPackName(); got != "Pack Custom" {
		t.Fatalf("pack=%q", got)
	}
	if got := saPackPublisher(); got != "github.com/custom/repo" {
		t.Fatalf("publisher=%q", got)
	}
}

func TestSaExifBlobWhatsAppFormat(t *testing.T) {
	blob := saExifBlob("PackX", "PubY")
	if len(blob) < 22 {
		t.Fatalf("EXIF demasiado corto: %d bytes", len(blob))
	}
	want := []byte{0x49, 0x49, 0x2A, 0x00, 0x08, 0x00, 0x00, 0x00, 0x01, 0x00, 0x41, 0x57, 0x07, 0x00}
	if !bytes.Equal(blob[:14], want) {
		t.Fatalf("cabecera=%x, esperaba %x", blob[:14], want)
	}
	largo := binary.LittleEndian.Uint32(blob[14:18])
	jsonBytes := blob[22:]
	if int(largo) != len(jsonBytes) {
		t.Fatalf("longitud=%d, JSON=%d bytes", largo, len(jsonBytes))
	}
	if !json.Valid(jsonBytes) {
		t.Fatalf("JSON inválido: %q", jsonBytes)
	}
	var parsed struct {
		ID        string   `json:"sticker-pack-id"`
		Name      string   `json:"sticker-pack-name"`
		Publisher string   `json:"sticker-pack-publisher"`
		Emojis    []string `json:"emojis"`
	}
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.ID != "com.whatsbot.sa" || parsed.Name != "PackX" || parsed.Publisher != "PubY" || len(parsed.Emojis) != 1 {
		t.Fatalf("JSON inesperado: %+v", parsed)
	}
}

func TestSaApplyExifChunkWebP(t *testing.T) {
	for _, bin := range []string{"ffmpeg", "webpmux"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s no disponible", bin)
		}
	}
	dir := t.TempDir()
	in := filepath.Join(dir, "in.webp")
	cmd := exec.Command("ffmpeg", "-y", "-loglevel", "error", "-f", "lavfi", "-i", "color=c=red:s=512x512", "-frames:v", "1", in)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("ffmpeg: %v %s", err, out)
	}
	original, err := os.ReadFile(in)
	if err != nil {
		t.Fatal(err)
	}
	out := saApplyExif(original)
	if len(out) <= len(original) {
		t.Fatalf("EXIF no inyectado: %d → %d bytes", len(original), len(out))
	}
	var exif []byte
	var exifFlag byte
	i := 12
	for i+8 <= len(out) {
		id := string(out[i : i+4])
		size := int(binary.LittleEndian.Uint32(out[i+4 : i+8]))
		body := out[i+8 : i+8+size]
		if id == "EXIF" {
			exif = body
		}
		if id == "VP8X" && len(body) > 0 {
			exifFlag = body[0]
		}
		i += 8 + size + (size & 1)
	}
	if len(exif) < 22 {
		t.Fatalf("chunk EXIF ausente o corto: %d bytes", len(exif))
	}
	if exifFlag&0x08 == 0 {
		t.Fatalf("flag de EXIF no activado en VP8X: %x", exifFlag)
	}
	if !bytes.Equal(exif[:12], []byte{0x49, 0x49, 0x2A, 0x00, 0x08, 0x00, 0x00, 0x00, 0x01, 0x00, 0x41, 0x57}) {
		t.Fatalf("chunk EXIF no es formato WhatsApp: %x", exif[:12])
	}
	if largo := int(binary.LittleEndian.Uint32(exif[14:18])); largo != len(exif[22:]) {
		t.Fatalf("longitud=%d, JSON=%d bytes", largo, len(exif[22:]))
	}
	if !json.Valid(exif[22:]) {
		t.Fatalf("JSON inválido en el chunk: %q", exif[22:])
	}
}

func TestSaLimitesStickerAudioAnimado(t *testing.T) {
	if saAudioBitrateKbps != 64 || saMaxVideoSeconds != 10 {
		t.Fatalf("bitrate=%v, tope=%v, esperaba 64/10", saAudioBitrateKbps, saMaxVideoSeconds)
	}
	if saClampDuration(12, saMaxVideoSeconds) != 10 {
		t.Fatal("la duración animada debía acotarse a 10 s")
	}
}

func TestSaExifReserve(t *testing.T) {
	reserve := saExifReserve()
	if reserve < saMinExifReserve || reserve < len(saExifBlob(saPackName(), saPackPublisher()))+64 {
		t.Fatalf("reserva insuficiente: %d", reserve)
	}
}

func TestSaCurvaCalidad10s(t *testing.T) {
	if saCalcFPS(1) <= saCalcFPS(10) {
		t.Fatal("la curva debe bajar fps al alargar el video")
	}
	if saCalcFPSMode("high", 1) <= saCalcFPSMode("high", 10) {
		t.Fatal("el modo high debe bajar fps al alargar el video")
	}
	if _, max := saQualityRange(true); max != 100 {
		t.Fatalf("calidad máxima=%d, esperaba 100", max)
	}
}

func TestSaEncodeAnimatedWebPRespetaPresupuesto(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg no disponible")
	}
	dir := t.TempDir()
	video := filepath.Join(dir, "in.mp4")
	cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=512x512:rate=30", "-t", "2",
		"-c:v", "libx264", "-preset", "ultrafast", video)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("ffmpeg: %v %s", err, out)
	}
	plan := saPlan{Animated: true, SizeLimit: 80 * 1024, MaxAudio: 2}
	budget, finalLimit, err := saWebPBudget(plan, 0)
	if err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(dir, "out.webp")
	webp, err := saEncodeAnimatedWebP(video, outPath, 0, 2, budget, saArgs{Shape: "none", Mode: "high"})
	if err != nil {
		t.Fatalf("saEncodeAnimatedWebP: %v", err)
	}
	if len(webp) == 0 || len(webp) > budget {
		t.Fatalf("WebP fuera del presupuesto: %d bytes, límite %d", len(webp), budget)
	}
	if len(saApplyExif(webp)) > finalLimit {
		t.Fatal("el WebP con EXIF supera el límite")
	}
}

func TestSaWebPBudget(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg no disponible")
	}
	dir := t.TempDir()
	oggPath := filepath.Join(dir, "in.ogg")
	cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000", "-t", "10",
		"-vn", "-c:a", "libopus", "-ac", "1", "-ar", "48000", "-b:a", "64k", oggPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("ffmpeg: %v %s", err, out)
	}
	ogg, err := os.ReadFile(oggPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(ogg) < 50_000 {
		t.Fatalf("audio de prueba demasiado corto: %d bytes", len(ogg))
	}

	reserve := saExifReserve()
	margin := int(float64(saAnimatedLimit) * saSafetyMarginPct / 100)
	plan := saPlanFor(true)

	t.Setenv("SA_COMBINED_BUDGET", "")
	if saCombinedBudgetEnabled() {
		t.Fatal("el modo combinado debe estar desactivado por defecto")
	}
	budget, finalLimit, err := saWebPBudget(plan, len(ogg))
	if err != nil {
		t.Fatal(err)
	}
	if finalLimit != saAnimatedLimit || budget != saAnimatedLimit-reserve-margin {
		t.Fatalf("presupuesto=%d límite=%d, esperaba %d/%d", budget, finalLimit, saAnimatedLimit-reserve-margin, saAnimatedLimit)
	}

	t.Setenv("SA_COMBINED_BUDGET", "true")
	combinedBudget, combinedLimit, err := saWebPBudget(plan, len(ogg))
	if err != nil {
		t.Fatal(err)
	}
	wantLimit := saAnimatedLimit - len(ogg)
	if combinedLimit != wantLimit || combinedBudget != wantLimit-reserve-margin {
		t.Fatalf("presupuesto=%d límite=%d, esperaba %d/%d", combinedBudget, combinedLimit, wantLimit-reserve-margin, wantLimit)
	}
	if combinedBudget >= budget {
		t.Fatal("el modo combinado debe reducir el presupuesto del WebP")
	}

	staticPlan := saPlanFor(false)
	staticBudget, staticLimit, err := saWebPBudget(staticPlan, len(ogg))
	if err != nil {
		t.Fatal(err)
	}
	wantStatic := saStaticLimit - saExifReserve() - int(float64(saStaticLimit)*saSafetyMarginPct/100)
	if staticLimit != saStaticLimit || staticBudget != wantStatic {
		t.Fatalf("estático: presupuesto=%d límite=%d, esperaba %d/%d", staticBudget, staticLimit, wantStatic, saStaticLimit)
	}
}
