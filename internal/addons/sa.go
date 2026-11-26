// sa.go — addon `.sa`: stickers con audio (dual-blob: WebP + OGG/Opus).
//
// El sticker con sonido de WhatsApp no incrusta el audio dentro del WebP:
// sube dos archivos cifrados por separado y los une en el protobuf
// StickerMessage.AudioMessage (campo oneof 26), normalmente envuelto en
// Message.AudioStickerMessage (campo 134, FutureProofMessage).
package addons

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"whatsbot-go/internal/core"
	"whatsbot-go/internal/store"
)

const (
	saSize             = 512
	saStaticLimit      = 100 * 1024
	saAnimatedLimit    = 500 * 1024
	saStaticMaxAudio   = 10.0
	saAnimatedMaxAudio = 5.0
	saMaxVideoSeconds  = 5.0
	saSessionTTL       = 2 * time.Minute
	saDefaultCooldown  = 15 * time.Second
)

// ─── Inyectables para pruebas ───────────────────────────────────────────────

var (
	saNow       = time.Now
	saExec      = exec.CommandContext
	saLookPath  = exec.LookPath
	saMkdirTemp = os.MkdirTemp
	saRemoveAll = os.RemoveAll
	saProbeFn   = saProbeDefault

	saUploadFn = func(client *whatsmeow.Client, data []byte, mt whatsmeow.MediaType) (whatsmeow.UploadResponse, error) {
		return client.Upload(context.Background(), data, mt)
	}
	saDownloadFn = func(ctx *core.Context, media whatsmeow.DownloadableMessage) ([]byte, error) {
		if ctx == nil || ctx.Client == nil || media == nil {
			return nil, fmt.Errorf("cliente de WhatsApp no disponible")
		}
		return ctx.Client.Download(context.Background(), media)
	}
)

// ─── Registro ───────────────────────────────────────────────────────────────

type saAddon struct{}

func init() {
	core.Register(&saAddon{})
	core.RegisterBackground("sa", saInit)
}

func (*saAddon) Commands() []string { return []string{"sa"} }

// ─── Sesiones temporales (2 pasos, TTL 2 min) ───────────────────────────────

type saVisual struct {
	Data      []byte
	Mimetype  string
	Animated  bool
	IsSticker bool
	IsVideo   bool
}

type saSession struct {
	Visual       *saVisual
	Audio        []byte
	AudioSeconds float64
	AudioPTT     bool
	Expires      time.Time
}

var (
	saMu       sync.Mutex
	saSessions = map[string]saSession{}
)

func saSessionKey(chat, user string) string { return chat + "|" + user }

// saGetSession devuelve la sesión vigente (o vacía si expiró).
func saGetSession(chat, user string) saSession {
	saMu.Lock()
	defer saMu.Unlock()
	key := saSessionKey(chat, user)
	s, ok := saSessions[key]
	if !ok {
		return saSession{}
	}
	if !s.Expires.IsZero() && saNow().After(s.Expires) {
		delete(saSessions, key)
		return saSession{}
	}
	return s
}

func saPutSession(chat, user string, s saSession) {
	saMu.Lock()
	defer saMu.Unlock()
	s.Expires = saNow().Add(saSessionTTL)
	saSessions[saSessionKey(chat, user)] = s
}

func saStoreVisual(chat, user string, v *saVisual) {
	s := saGetSession(chat, user)
	s.Visual = v
	saPutSession(chat, user, s)
}

func saStoreAudio(chat, user string, data []byte, seconds float64, ptt bool) {
	s := saGetSession(chat, user)
	s.Audio = data
	s.AudioSeconds = seconds
	s.AudioPTT = ptt
	saPutSession(chat, user, s)
}

func saClearSession(chat, user string) {
	saMu.Lock()
	delete(saSessions, saSessionKey(chat, user))
	saMu.Unlock()
}

func saResetSessions() {
	saMu.Lock()
	saSessions = map[string]saSession{}
	saMu.Unlock()
}

// ─── Cooldown (15 s por usuario, dueños exentos) ────────────────────────────

var (
	saCooldownMu sync.Mutex
	saCooldowns  = map[string]time.Time{}
)

func saCooldownDuration() time.Duration {
	raw := strings.TrimSpace(os.Getenv("SA_COOLDOWN_SEC"))
	if raw == "" {
		return saDefaultCooldown
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || v < 0 {
		return saDefaultCooldown
	}
	return time.Duration(v * float64(time.Second))
}

func saIsOwner(ctx *core.Context) bool {
	if ctx == nil || ctx.Message == nil {
		return false
	}
	ownerNumbers := wagoOwnerNumbers()
	for _, jid := range wagoSenderJIDs(ctx) {
		if wagoContains(ownerNumbers, jid.User) {
			return true
		}
	}
	return false
}

// saCheckCooldown devuelve si el usuario puede usar el comando y, si no,
// cuánto falta para poder reintentar.
func saCheckCooldown(ctx *core.Context) (bool, time.Duration) {
	if saIsOwner(ctx) {
		return true, 0
	}
	d := saCooldownDuration()
	if d <= 0 {
		return true, 0
	}
	key := ctx.SenderJID().String()
	now := saNow()
	saCooldownMu.Lock()
	defer saCooldownMu.Unlock()
	if until, ok := saCooldowns[key]; ok && now.Before(until) {
		return false, until.Sub(now)
	}
	saCooldowns[key] = now.Add(d)
	return true, 0
}

func saResetCooldowns() {
	saCooldownMu.Lock()
	saCooldowns = map[string]time.Time{}
	saCooldownMu.Unlock()
}

// ─── Parseo de argumentos ───────────────────────────────────────────────────

type saArgs struct {
	Start        float64
	Duration     float64
	Auto         bool
	Wave         bool
	Inspect      bool
	ExtractAudio bool

	// Efectos visuales compartidos con `.st`.
	Crop    bool
	Reverse bool
	Stretch bool
	Shape   string // "none" | "spherical" | "border"
	Mode    string // "fluid" | "high"
}

func saParseArgs(args []string) saArgs {
	out := saArgs{Shape: "none", Mode: "fluid"}
	var numbers []float64
	for _, raw := range args {
		for _, token := range strings.Fields(strings.ToLower(raw)) {
			token = strings.TrimSpace(token)
			if token == "" {
				continue
			}
			switch token {
			case "auto":
				out.Auto = true
				continue
			case "wave", "onda":
				out.Wave = true
				continue
			case "inspect", "inspeccionar":
				out.Inspect = true
				continue
			case "audio", "mp3", "sonido":
				out.ExtractAudio = true
				continue
			case "crop", "c":
				out.Crop = true
				continue
			case "reverse", "r":
				out.Reverse = true
				continue
			case "e", "estirar":
				out.Stretch = true
				continue
			case "spherical", "s":
				out.Shape = "spherical"
				continue
			case "border", "b":
				out.Shape = "border"
				continue
			case "h", "high":
				out.Mode = "high"
				continue
			case "f", "fluid":
				out.Mode = "fluid"
				continue
			}
			if v, ok := saParseClock(token); ok {
				numbers = append(numbers, v)
				continue
			}
			if v, err := strconv.ParseFloat(token, 64); err == nil && v >= 0 {
				numbers = append(numbers, v)
			}
		}
	}
	if len(numbers) > 0 {
		out.Start = numbers[0]
	}
	if len(numbers) > 1 {
		out.Duration = numbers[1]
	}
	// Igual que `.st`: la máscara esférica necesita recorte previo.
	if out.Shape == "spherical" {
		out.Crop = true
	}
	return out
}

// saParseClock acepta "SS", "M:SS" y "H:MM:SS".
func saParseClock(token string) (float64, bool) {
	parts := strings.Split(token, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, false
	}
	total := 0.0
	for _, p := range parts {
		if p == "" {
			return 0, false
		}
		v, err := strconv.ParseFloat(p, 64)
		if err != nil || v < 0 {
			return 0, false
		}
		total = total*60 + v
	}
	return total, true
}

// ─── Plan visual (límites y fps adaptativos) ────────────────────────────────

type saPlan struct {
	Animated  bool
	SizeLimit int
	MaxAudio  float64
}

func saPlanFor(animated bool) saPlan {
	if animated {
		return saPlan{Animated: true, SizeLimit: saAnimatedLimit, MaxAudio: saAnimatedMaxAudio}
	}
	return saPlan{Animated: false, SizeLimit: saStaticLimit, MaxAudio: saStaticMaxAudio}
}

// saCalcFPS replica la compresión adaptativa de stickers.go, acotada a 5 s.
func saCalcFPS(duration float64) int {
	switch {
	case duration < 1.5:
		return 20
	case duration < 3.0:
		return 16
	case duration < 5.0:
		return 12
	default:
		return 10
	}
}

// saCalcFPSMode aplica el modo `high` de `.st` (más fps) sobre el esquema
// adaptativo de `.sa`, siempre acotado al tope de 5 s.
func saCalcFPSMode(mode string, duration float64) int {
	if mode != "high" {
		return saCalcFPS(duration)
	}
	switch {
	case duration < 1.5:
		return 30
	case duration < 3.0:
		return 24
	case duration < 5.0:
		return 20
	default:
		return 18
	}
}

func saQualityRange(animated bool) (int, int) {
	if animated {
		return 0, 100
	}
	return 10, 100
}

// saClampDuration recorta a la duración máxima; si no hay duración usa el tope.
func saClampDuration(d, max float64) float64 {
	if d <= 0 || d > max {
		return max
	}
	return d
}

// ─── ffprobe / ffmpeg ──────────────────────────────────────────────────────

type saProbeInfo struct {
	Duration float64
	FPS      float64
	Width    int
	Height   int
	HasVideo bool
	HasAudio bool
}

func saProbeDefault(path string) saProbeInfo {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := saExec(ctx, "ffprobe", "-v", "error", "-print_format", "json",
		"-show_format", "-show_streams", path).Output()
	if err != nil {
		return saProbeInfo{}
	}
	var parsed struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			CodecType  string `json:"codec_type"`
			RFrameRate string `json:"r_frame_rate"`
			Width      int    `json:"width"`
			Height     int    `json:"height"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return saProbeInfo{}
	}
	info := saProbeInfo{FPS: 24}
	if v, err := strconv.ParseFloat(parsed.Format.Duration, 64); err == nil {
		info.Duration = v
	}
	for _, s := range parsed.Streams {
		switch s.CodecType {
		case "video":
			info.HasVideo = true
			if info.Width == 0 {
				info.Width, info.Height = s.Width, s.Height
			}
			if s.RFrameRate != "" {
				parts := strings.Split(s.RFrameRate, "/")
				if len(parts) == 2 && parts[1] != "0" {
					a, errA := strconv.ParseFloat(parts[0], 64)
					b, errB := strconv.ParseFloat(parts[1], 64)
					if errA == nil && errB == nil && b != 0 {
						info.FPS = a / b
					}
				}
			}
		case "audio":
			info.HasAudio = true
		}
	}
	if info.FPS <= 0 || math.IsNaN(info.FPS) {
		info.FPS = 24
	}
	return info
}

func saRunFFmpeg(args ...string) error {
	out, err := saExec(context.Background(), "ffmpeg", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func saFormatFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// saVisualFilter construye la cadena de filtros FFmpeg. Porta
// stickersFilterChain: escala, recorte, estirado y máscaras esférica/borde.
func saVisualFilter(a saArgs) string {
	rgb := "r='p(X,Y)':g='p(X,Y)':b='p(X,Y)'"
	shape := ""
	switch a.Shape {
	case "spherical":
		shape = ",geq=" + rgb + ":a='p(X,Y)*lte((X-W/2)*(X-W/2)+(Y-H/2)*(Y-H/2),(min(W,H)/2)*(min(W,H)/2))'"
	case "border":
		r := "(min(W,H)*0.1)"
		dx := "max(0,abs(X-W/2)-(W/2-" + r + "))"
		dy := "max(0,abs(Y-H/2)-(H/2-" + r + "))"
		shape = ",geq=" + rgb + ":a='p(X,Y)*lte(" + dx + "*" + dx + "+" + dy + "*" + dy + "," + r + "*" + r + ")'"
	}

	if a.Stretch {
		return "scale=512:512,format=rgba" + shape
	}
	if a.Crop {
		return "scale='max(512,iw*512/ih)':'max(512,ih*512/iw)',format=rgba,crop=512:512" + shape
	}
	return "scale=512:512:force_original_aspect_ratio=decrease,format=rgba" + shape + ",pad=512:512:(ow-iw)/2:(oh-ih)/2:color=0x00000000"
}

// saNeedsVisualTransform indica si los flags exigen recodificar aunque el
// sticker de entrada ya mida 512x512.
func saNeedsVisualTransform(a saArgs) bool {
	return a.Crop || a.Stretch || (a.Shape != "" && a.Shape != "none")
}

// ─── Codificación WebP adaptativa ───────────────────────────────────────────

// saEncodeStaticWebP busca la mejor calidad que quepa en el límite.
func saEncodeStaticWebP(inputPath, outputPath string, limit int, filter string) ([]byte, error) {
	qMin, qMax := saQualityRange(false)
	target := float64(limit) * 0.90
	var best []byte
	bestDiff := math.Inf(1)

	for i := 0; i < 4; i++ {
		q := (qMin + qMax) / 2
		args := []string{
			"-y", "-err_detect", "ignore_err", "-i", inputPath,
			"-vcodec", "libwebp", "-lossless", "0", "-q:v", strconv.Itoa(q),
			"-compression_level", "6", "-an", "-vf", filter, outputPath,
		}
		if err := saRunFFmpeg(args...); err != nil {
			return nil, err
		}
		current, err := os.ReadFile(outputPath)
		if err != nil {
			return nil, err
		}
		size := float64(len(current))
		diff := math.Abs(target - size)
		if len(current) <= limit && diff < bestDiff {
			best = current
			bestDiff = diff
		}
		if size > target {
			qMax = q - 1
		} else {
			qMin = q + 1
		}
		if qMax < qMin {
			break
		}
	}
	if best != nil {
		return best, nil
	}
	return os.ReadFile(outputPath)
}

// saEncodeAnimatedWebP extrae frames a fps adaptativo y los recodifica.
func saEncodeAnimatedWebP(inputPath, outputPath string, start, duration float64, limit int, a saArgs) ([]byte, error) {
	workDir, err := saMkdirTemp("", "sa_anim_")
	if err != nil {
		return nil, err
	}
	defer saRemoveAll(workDir)

	framesDir := filepath.Join(workDir, "frames")
	if err := os.MkdirAll(framesDir, 0o755); err != nil {
		return nil, err
	}
	if duration <= 0 {
		duration = saMaxVideoSeconds
	}
	// Al invertir el bucle la duración final se duplica: recortamos la fuente
	// para no superar el tope de 5 s.
	if a.Reverse && duration > saMaxVideoSeconds/2 {
		duration = saMaxVideoSeconds / 2
	}
	duration = saClampDuration(duration, saMaxVideoSeconds)
	finalDuration := duration
	if a.Reverse {
		finalDuration = duration * 2
	}
	fps := saCalcFPSMode(a.Mode, finalDuration)
	filter := saVisualFilter(a)
	framesPattern := filepath.Join(framesDir, "frame_%04d.png")

	args := []string{"-y", "-err_detect", "ignore_err"}
	if start > 0 {
		args = append(args, "-ss", saFormatFloat(start))
	}
	args = append(args, "-i", inputPath, "-t", saFormatFloat(duration),
		"-c:v", "png", "-pix_fmt", "rgba",
		"-vf", fmt.Sprintf("fps=%d,%s", fps, filter), framesPattern)

	if err := saRunFFmpeg(args...); err != nil {
		return nil, err
	}

	framePrefix := "frame_%04d.png"
	if a.Reverse {
		if _, err := os.Stat(framesPattern); err != nil {
			return nil, fmt.Errorf("no se extrajeron frames del vídeo")
		}
		if err := stickersReverseFrames(framesDir); err != nil {
			return nil, err
		}
		framePrefix = "loop_%04d.png"
	}

	files, _ := filepath.Glob(filepath.Join(framesDir, "*.png"))
	if len(files) == 0 {
		return nil, fmt.Errorf("no se extrajeron frames del vídeo")
	}
	encodePattern := filepath.Join(framesDir, framePrefix)

	qMin, qMax := saQualityRange(true)
	target := float64(limit) * 0.90
	margin := float64(limit) * 0.05
	var best []byte
	bestDiff := math.Inf(1)

	for i := 0; i < 5; i++ {
		q := (qMin + qMax) / 2
		enc := []string{
			"-y", "-framerate", strconv.Itoa(fps), "-i", encodePattern,
			"-c:v", "libwebp", "-lossless", "0", "-q:v", strconv.Itoa(q),
			"-compression_level", "4", "-loop", "0", "-an", outputPath,
		}
		if err := saRunFFmpeg(enc...); err != nil {
			return nil, err
		}
		current, err := os.ReadFile(outputPath)
		if err != nil {
			return nil, err
		}
		size := float64(len(current))
		diff := math.Abs(target - size)
		if len(current) <= limit && diff < bestDiff {
			best = current
			bestDiff = diff
		}
		if size >= target-margin && size <= target+margin {
			break
		}
		if size > target {
			qMax = q - 1
		} else {
			qMin = q + 1
		}
		if qMax < qMin {
			break
		}
	}
	if best != nil {
		return best, nil
	}
	return os.ReadFile(outputPath)
}

// saWebPDims lee las dimensiones de un WebP sin depender de ffprobe.
// Soporta VP8X (animado/alfa), VP8 (lossy) y VP8L (lossless).
func saWebPDims(data []byte) (int, int, bool) {
	if len(data) < 30 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return 0, 0, false
	}
	switch string(data[12:16]) {
	case "VP8X":
		w := 1 + int(uint32(data[24])|uint32(data[25])<<8|uint32(data[26])<<16)
		h := 1 + int(uint32(data[27])|uint32(data[28])<<8|uint32(data[29])<<16)
		return w, h, true
	case "VP8 ":
		if data[23] == 0x9d && data[24] == 0x01 && data[25] == 0x2a {
			w := int(binary.LittleEndian.Uint16(data[26:28]) & 0x3fff)
			h := int(binary.LittleEndian.Uint16(data[28:30]) & 0x3fff)
			return w, h, true
		}
	case "VP8L":
		if data[20] == 0x2f {
			bits := binary.LittleEndian.Uint32(data[21:25])
			w := int(bits&0x3fff) + 1
			h := int((bits>>14)&0x3fff) + 1
			return w, h, true
		}
	}
	return 0, 0, false
}

// saWebPIsAnimated detecta el chunk ANMF de los WebP animados.
func saWebPIsAnimated(data []byte) bool { return bytes.Contains(data, []byte("ANMF")) }

// ─── Audio OGG/Opus ────────────────────────────────────────────────────────

func saExtractOGG(inputPath, outputPath string, start, duration float64) error {
	args := []string{"-y", "-err_detect", "ignore_err"}
	if start > 0 {
		args = append(args, "-ss", saFormatFloat(start))
	}
	args = append(args, "-i", inputPath,
		"-t", saFormatFloat(duration), "-vn", "-map", "0:a:0",
		"-c:a", "libopus", "-ac", "1", "-ar", "48000", "-b:a", "64k",
		"-avoid_negative_ts", "make_zero", outputPath)
	return saRunFFmpeg(args...)
}

// saMimeExt elige una extensión para el archivo temporal de entrada.
func saMimeExt(mimetype string) string {
	m := strings.ToLower(mimetype)
	switch {
	case strings.Contains(m, "webp"):
		return "webp"
	case strings.Contains(m, "gif"):
		return "gif"
	case strings.Contains(m, "png"):
		return "png"
	case strings.Contains(m, "jpeg"), strings.Contains(m, "jpg"):
		return "jpg"
	case strings.Contains(m, "mp4"), strings.HasPrefix(m, "video/"):
		return "mp4"
	case strings.Contains(m, "ogg"), strings.Contains(m, "opus"):
		return "ogg"
	case strings.Contains(m, "mpeg"), strings.Contains(m, "mp3"):
		return "mp3"
	case strings.Contains(m, "wav"):
		return "wav"
	}
	return "bin"
}

// ─── EXIF opcional (pack name / publisher) ────────────────────────────────

func saPackName() string      { return wagoGetenv("SA_PACK_NAME", "WhatsBotOnMySelf") }
func saPackPublisher() string { return wagoGetenv("SA_PACK_PUBLISHER", "WhatsBot") }

// saExifBlob construye un EXIF mínimo con UserComment en ASCII.
func saExifBlob(pack, publisher string) []byte {
	payload, _ := json.Marshal(map[string]any{
		"sticker-pack-id":        "com.whatsbot.sa",
		"sticker-pack-name":      pack,
		"sticker-pack-publisher": publisher,
		"emojis":                 []string{"🎵"},
	})
	userComment := append([]byte("ASCII\x00\x00\x00"), payload...)

	buf := make([]byte, 0, 26+len(userComment))
	// Cabecera TIFF little-endian.
	buf = append(buf, 'I', 'I', 0x2A, 0x00, 0x08, 0x00, 0x00, 0x00)
	// IFD0 con una sola entrada.
	var count [2]byte
	binary.LittleEndian.PutUint16(count[:], 1)
	buf = append(buf, count[:]...)
	entry := make([]byte, 12)
	binary.LittleEndian.PutUint16(entry[0:], 0x9286) // UserComment
	binary.LittleEndian.PutUint16(entry[2:], 7)      // UNDEFINED
	binary.LittleEndian.PutUint32(entry[4:], uint32(len(userComment)))
	binary.LittleEndian.PutUint32(entry[8:], 26) // offset tras la IFD
	buf = append(buf, entry...)
	buf = append(buf, 0x00, 0x00, 0x00, 0x00) // siguiente IFD
	buf = append(buf, userComment...)
	return append([]byte("Exif\x00\x00"), buf...)
}

// saApplyExif inyecta metadatos si webpmux está disponible; si no, no-op.
func saApplyExif(webp []byte) []byte {
	if _, err := saLookPath("webpmux"); err != nil {
		return webp
	}
	dir, err := saMkdirTemp("", "sa_exif_")
	if err != nil {
		return webp
	}
	defer saRemoveAll(dir)

	inPath := filepath.Join(dir, "in.webp")
	exifPath := filepath.Join(dir, "meta.exif")
	outPath := filepath.Join(dir, "out.webp")
	if err := os.WriteFile(inPath, webp, 0o600); err != nil {
		return webp
	}
	if err := os.WriteFile(exifPath, saExifBlob(saPackName(), saPackPublisher()), 0o600); err != nil {
		return webp
	}
	cmd := saExec(context.Background(), "webpmux", "-set", "exif", exifPath, inPath, "-o", outPath)
	if err := cmd.Run(); err != nil {
		return webp
	}
	out, err := os.ReadFile(outPath)
	if err != nil || len(out) == 0 {
		return webp
	}
	return out
}

// ─── Clasificación y descarga de medios ────────────────────────────────────

type saKind int

const (
	saKindNone saKind = iota
	saKindImage
	saKindVideo
	saKindSticker
	saKindAudio
)

type saMedia struct {
	Kind        saKind
	Animated    bool
	PTT         bool
	Download    whatsmeow.DownloadableMessage
	NestedAudio *waE2E.AudioMessage
}

// saUnwrapMessage desenvuelve contenedores habituales (efímeros, view-once,
// document-with-caption y AudioStickerMessage 134).
func saUnwrapMessage(msg *waE2E.Message) *waE2E.Message {
	for i := 0; i < 8 && msg != nil; i++ {
		switch {
		case msg.GetEphemeralMessage() != nil:
			msg = msg.GetEphemeralMessage().GetMessage()
		case msg.GetViewOnceMessage() != nil:
			msg = msg.GetViewOnceMessage().GetMessage()
		case msg.GetViewOnceMessageV2() != nil:
			msg = msg.GetViewOnceMessageV2().GetMessage()
		case msg.GetViewOnceMessageV2Extension() != nil:
			msg = msg.GetViewOnceMessageV2Extension().GetMessage()
		case msg.GetDocumentWithCaptionMessage() != nil:
			msg = msg.GetDocumentWithCaptionMessage().GetMessage()
		case msg.GetAudioStickerMessage() != nil:
			msg = msg.GetAudioStickerMessage().GetMessage()
		default:
			return msg
		}
	}
	return msg
}

func saClassify(msg *waE2E.Message) saMedia {
	msg = saUnwrapMessage(msg)
	if msg == nil {
		return saMedia{Kind: saKindNone}
	}
	switch {
	case msg.GetImageMessage() != nil:
		return saMedia{Kind: saKindImage, Download: msg.GetImageMessage()}
	case msg.GetVideoMessage() != nil:
		return saMedia{Kind: saKindVideo, Animated: true, Download: msg.GetVideoMessage()}
	case msg.GetAudioMessage() != nil:
		return saMedia{Kind: saKindAudio, PTT: msg.GetAudioMessage().GetPTT(), Download: msg.GetAudioMessage()}
	case msg.GetStickerMessage() != nil:
		st := msg.GetStickerMessage()
		return saMedia{
			Kind:        saKindSticker,
			Animated:    st.GetIsAnimated(),
			Download:    st,
			NestedAudio: st.GetAudioMessage(),
		}
	}
	return saMedia{Kind: saKindNone}
}

func saIsVisual(k saKind) bool {
	return k == saKindImage || k == saKindVideo || k == saKindSticker
}

// ─── Inspector ─────────────────────────────────────────────────────────────

type saInspection struct {
	Wrapped  bool
	PTT      bool
	Seconds  uint32
	Mimetype string
	Animated bool
	Width    uint32
	Height   uint32
	Waveform int
}

// saInspectPayload detecta stickers con audio y extrae sus parámetros.
func saInspectPayload(msg *waE2E.Message) (saInspection, bool) {
	if msg == nil {
		return saInspection{}, false
	}
	wrapped := msg.GetAudioStickerMessage() != nil
	inner := saUnwrapMessage(msg)
	if inner == nil {
		return saInspection{}, false
	}
	st := inner.GetStickerMessage()
	if st == nil {
		return saInspection{}, false
	}
	audio := st.GetAudioMessage()
	if audio == nil {
		return saInspection{}, false
	}
	return saInspection{
		Wrapped:  wrapped,
		PTT:      audio.GetPTT(),
		Seconds:  audio.GetSeconds(),
		Mimetype: audio.GetMimetype(),
		Animated: st.GetIsAnimated(),
		Width:    st.GetWidth(),
		Height:   st.GetHeight(),
		Waveform: len(audio.GetWaveform()),
	}, true
}

func saInspectionText(i saInspection) string {
	return fmt.Sprintf("🔎 *Inspector .sa*\n"+
		"• Envoltura 134: %v\n• PTT: %v\n• Seconds: %d\n• Mimetype: %s\n"+
		"• IsAnimated: %v\n• Tamaño: %dx%d\n• Waveform: %d bytes",
		i.Wrapped, i.PTT, i.Seconds, i.Mimetype, i.Animated, i.Width, i.Height, i.Waveform)
}

func saInspect(ctx *core.Context, cur, quoted *waE2E.Message) error {
	if info, ok := saInspectPayload(cur); ok {
		slog.Info("🔎 [sa] inspector", "wrapped134", info.Wrapped, "ptt", info.PTT,
			"seconds", info.Seconds, "mimetype", info.Mimetype, "animated", info.Animated,
			"width", info.Width, "height", info.Height, "waveform_bytes", info.Waveform)
		return ctx.Reply(saInspectionText(info))
	}
	if info, ok := saInspectPayload(quoted); ok {
		slog.Info("🔎 [sa] inspector (citado)", "wrapped134", info.Wrapped, "ptt", info.PTT,
			"seconds", info.Seconds, "mimetype", info.Mimetype, "animated", info.Animated,
			"width", info.Width, "height", info.Height, "waveform_bytes", info.Waveform)
		return ctx.Reply(saInspectionText(info))
	}
	return ctx.Reply("🔎 No encontré un sticker con audio en este mensaje ni en el citado.")
}

func saInit(client *whatsmeow.Client, _ *store.Store) error {
	if client == nil {
		return nil
	}
	client.AddEventHandler(func(evt any) {
		if os.Getenv("SA_DEBUG_CAPTURE") != "1" {
			return
		}
		msg, ok := evt.(*events.Message)
		if !ok || msg == nil || msg.Message == nil {
			return
		}
		if info, ok := saInspectPayload(msg.Message); ok {
			slog.Info("🔎 [sa] AudioSticker capturado", "wrapped134", info.Wrapped,
				"ptt", info.PTT, "seconds", info.Seconds, "mimetype", info.Mimetype,
				"animated", info.Animated, "width", info.Width, "height", info.Height,
				"waveform_bytes", info.Waveform)
		}
	})
	return nil
}

// ─── Imagen de onda (modo auto/wave) ───────────────────────────────────────

// saWaveImage dibuja una onda de audio determinista en 512x512.
func saWaveImage(audio []byte) []byte {
	var seed uint32 = 2166136261
	for _, b := range audio {
		seed ^= uint32(b)
		seed *= 16777619
	}

	img := image.NewRGBA(image.Rect(0, 0, saSize, saSize))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.RGBA{0x10, 0x14, 0x1e, 0xff}}, image.Point{}, draw.Src)

	// Resplandor circular.
	cx, cy := saSize/2, saSize/2
	for y := 0; y < saSize; y++ {
		for x := 0; x < saSize; x++ {
			dx, dy := float64(x-cx), float64(y-cy)
			dist := math.Hypot(dx, dy)
			if dist > 200 {
				continue
			}
			alpha := uint8(math.Max(0, (1-dist/200)) * 70)
			img.SetRGBA(x, y, color.RGBA{0x22, 0x7a, 0xc8, alpha})
		}
	}

	// Barras de onda.
	bars := 41
	barW := 7
	gap := 2
	totalW := bars*barW + (bars-1)*gap
	startX := (saSize - totalW) / 2
	state := seed
	for i := 0; i < bars; i++ {
		state = state*1664525 + 1013904223
		amp := 0.12 + 0.78*math.Abs(math.Sin(float64(i)*0.55+float64(state%97)/97.0))
		h := int(amp * 300)
		x := startX + i*(barW+gap)
		y0 := cy - h/2
		for y := y0; y < y0+h; y++ {
			for x2 := x; x2 < x+barW; x2++ {
				if x2 < 0 || y < 0 || x2 >= saSize || y >= saSize {
					continue
				}
				t := float64(y-y0) / math.Max(1, float64(h))
				img.SetRGBA(x2, y, color.RGBA{
					R: uint8(120 + 135*t),
					G: uint8(200 - 60*t),
					B: uint8(255 - 40*t),
					A: 0xff,
				})
			}
		}
	}

	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil
	}
	return out.Bytes()
}

// ─── Construcción del protobuf ─────────────────────────────────────────────

func saWrap134Enabled() bool {
	return strings.ToLower(wagoGetenv("SA_WRAP_134", "true")) != "false"
}

func saPTTEnabled() bool {
	return strings.ToLower(wagoGetenv("SA_AUDIO_PTT", "false")) == "true"
}

// saBuildAudioSticker une los metadatos de subida en StickerMessage + Audio.
func saBuildAudioSticker(webp, audio whatsmeow.UploadResponse, seconds uint32, animated, ptt, wrap bool) *waE2E.Message {
	if seconds == 0 {
		seconds = 1
	}
	audioMsg := &waE2E.AudioMessage{
		URL:           proto.String(audio.URL),
		Mimetype:      proto.String("audio/ogg; codecs=opus"),
		FileSHA256:    audio.FileSHA256,
		FileLength:    proto.Uint64(audio.FileLength),
		Seconds:       proto.Uint32(seconds),
		PTT:           proto.Bool(ptt),
		MediaKey:      audio.MediaKey,
		FileEncSHA256: audio.FileEncSHA256,
		DirectPath:    proto.String(audio.DirectPath),
	}
	sticker := &waE2E.StickerMessage{
		Audio:         &waE2E.StickerMessage_AudioMessage{AudioMessage: audioMsg},
		URL:           proto.String(webp.URL),
		Mimetype:      proto.String("image/webp"),
		FileSHA256:    webp.FileSHA256,
		FileEncSHA256: webp.FileEncSHA256,
		MediaKey:      webp.MediaKey,
		Height:        proto.Uint32(saSize),
		Width:         proto.Uint32(saSize),
		DirectPath:    proto.String(webp.DirectPath),
		FileLength:    proto.Uint64(webp.FileLength),
		IsAnimated:    proto.Bool(animated),
	}
	inner := &waE2E.Message{StickerMessage: sticker}
	if wrap {
		return &waE2E.Message{AudioStickerMessage: &waE2E.FutureProofMessage{Message: inner}}
	}
	return inner
}

// ─── Flujo principal ───────────────────────────────────────────────────────

func (a *saAddon) Handle(ctx *core.Context) error {
	if ctx == nil || ctx.Message == nil || ctx.Message.Message == nil {
		return nil
	}

	args := saParseArgs(ctx.Args)
	cur := saUnwrapMessage(ctx.Message.Message)
	quoted := saUnwrapMessage(ctx.QuotedMessage())

	if args.Inspect {
		return saInspect(ctx, cur, quoted)
	}

	// Extraer el audio de un sticker sonoro.
	if args.ExtractAudio {
		media := saClassify(quoted)
		if media.NestedAudio == nil {
			media = saClassify(cur)
		}
		if media.NestedAudio == nil {
			return ctx.Reply("⚠️ Cita un sticker con audio para extraer su sonido.")
		}
		if err := ctx.React("⏳"); err != nil {
			return err
		}
		data, err := saDownloadFn(ctx, media.NestedAudio)
		if err != nil || len(data) == 0 {
			_ = ctx.React("❌")
			return ctx.Reply("❌ No pude descargar el audio del sticker.")
		}
		if err := ctx.SendAudio(data, "audio/ogg; codecs=opus",
			media.NestedAudio.GetPTT(), media.NestedAudio.GetSeconds()); err != nil {
			return err
		}
		return ctx.React("✅")
	}

	curMedia := saClassify(cur)
	quoMedia := saClassify(quoted)

	chat := ctx.ChatJID().String()
	user := ctx.SenderJID().String()
	session := saGetSession(chat, user)

	// El cooldown no aplica al segundo paso de una sesión pendiente.
	pendingSession := session.Visual != nil || len(session.Audio) > 0
	if !pendingSession {
		if ok, wait := saCheckCooldown(ctx); !ok {
			return ctx.Reply("⏳ Espera " + strconv.Itoa(int(math.Ceil(wait.Seconds()))) + " s antes de volver a usar .sa.")
		}
	}

	// 1. Visual (el actual tiene prioridad sobre el citado).
	var visual *saVisual
	if saIsVisual(curMedia.Kind) {
		v, err := saVisualFrom(ctx, curMedia)
		if err != nil {
			return saFail(ctx, err)
		}
		visual = v
	} else if saIsVisual(quoMedia.Kind) {
		v, err := saVisualFrom(ctx, quoMedia)
		if err != nil {
			return saFail(ctx, err)
		}
		visual = v
	}

	// 2. Audio explícito (mensaje de audio o audio anidado en un sticker).
	var audioData []byte
	var audioSeconds float64
	var audioPTT bool
	var explicitAudio *saMedia
	switch {
	case curMedia.Kind == saKindAudio:
		m := curMedia
		explicitAudio = &m
	case curMedia.NestedAudio != nil:
		m := curMedia
		explicitAudio = &m
	case quoMedia.Kind == saKindAudio:
		m := quoMedia
		explicitAudio = &m
	case quoMedia.NestedAudio != nil:
		m := quoMedia
		explicitAudio = &m
	}
	if explicitAudio != nil {
		target := explicitAudio.Download
		if explicitAudio.NestedAudio != nil {
			target = explicitAudio.NestedAudio
		}
		data, err := saDownloadFn(ctx, target)
		if err != nil || len(data) == 0 {
			return saFail(ctx, fmt.Errorf("no pude descargar el audio: %w", err))
		}
		audioData = data
		audioPTT = explicitAudio.PTT
		if explicitAudio.NestedAudio != nil {
			audioPTT = explicitAudio.NestedAudio.GetPTT()
			audioSeconds = float64(explicitAudio.NestedAudio.GetSeconds())
		}
	}

	// 3. Si no hay audio explícito, usar el del vídeo (visual actual o citado).
	if audioData == nil {
		videoMedia := saMedia{Kind: saKindNone}
		if curMedia.Kind == saKindVideo {
			videoMedia = curMedia
		} else if quoMedia.Kind == saKindVideo {
			videoMedia = quoMedia
		}
		if videoMedia.Kind == saKindVideo {
			var videoBytes []byte
			if visual != nil && visual.IsVideo {
				videoBytes = visual.Data
			} else {
				data, err := saDownloadFn(ctx, videoMedia.Download)
				if err != nil {
					return saFail(ctx, fmt.Errorf("no pude descargar el vídeo: %w", err))
				}
				videoBytes = data
			}
			if visual == nil {
				visual = &saVisual{Data: videoBytes, Mimetype: "video/mp4", Animated: true, IsVideo: true}
			}
			videoAudioMax := saAnimatedMaxAudio
			if args.Duration > 0 && args.Duration < videoAudioMax {
				videoAudioMax = args.Duration
			}
			ogg, dur, err := saAudioFromVideo(videoBytes, args.Start, videoAudioMax)
			if err == nil && len(ogg) > 0 {
				audioData = ogg
				audioSeconds = dur
			}
		}
	}

	// 4. Resolver con la sesión temporal de 2 pasos.
	if visual == nil && audioData == nil {
		if session.Visual == nil && len(session.Audio) == 0 {
			return ctx.Reply(saUsageText())
		}
		return ctx.Reply("⚠️ Tienes una sesión .sa pendiente: envía el archivo que falta (o espera 2 min y caducará).")
	}

	if visual != nil && audioData == nil {
		if len(session.Audio) > 0 {
			audioData = session.Audio
			audioSeconds = session.AudioSeconds
			audioPTT = session.AudioPTT
		} else {
			saStoreVisual(chat, user, visual)
			return ctx.Reply("🖼️ Base visual guardada. Ahora envíame o cita el *audio* con `.sa` (tienes 2 minutos).")
		}
	}

	if visual == nil && audioData != nil {
		if args.Auto || args.Wave {
			visual = &saVisual{Data: saWaveImage(audioData), Mimetype: "image/png"}
		} else if session.Visual != nil {
			visual = session.Visual
			session.Visual = nil
		} else {
			saStoreAudio(chat, user, audioData, audioSeconds, audioPTT)
			return ctx.Reply("🎵 Audio guardado. Ahora envíame o cita la *imagen, vídeo o sticker* con `.sa` (tienes 2 minutos).\n💡 O usa `.sa auto` para generar una onda.")
		}
	}

	plan := saPlanFor(visual.Animated)
	audioMax := plan.MaxAudio
	visualDuration := args.Duration
	if visualDuration <= 0 || visualDuration > saMaxVideoSeconds {
		visualDuration = saMaxVideoSeconds
	}
	if args.Duration > 0 && args.Duration < audioMax {
		audioMax = args.Duration
	}

	if err := ctx.React("⏳"); err != nil {
		return err
	}

	webp, err := saBuildWebP(visual, plan, args, args.Start, visualDuration)
	if err != nil {
		return saFail(ctx, err)
	}

	ogg, seconds, err := saPrepareAudio(ctx, audioData, audioSeconds, audioMax, args.Start)
	if err != nil {
		return saFail(ctx, err)
	}

	webpResp, err := saUploadFn(ctx.Client, webp, whatsmeow.MediaImage)
	if err != nil {
		return saFail(ctx, fmt.Errorf("subiendo WebP: %w", err))
	}
	audioResp, err := saUploadFn(ctx.Client, ogg, whatsmeow.MediaAudio)
	if err != nil {
		return saFail(ctx, fmt.Errorf("subiendo OGG: %w", err))
	}

	msg := saBuildAudioSticker(webpResp, audioResp, seconds, visual.Animated, saPTTEnabled(), saWrap134Enabled())
	if err := ctx.SendMessage(msg); err != nil {
		return saFail(ctx, err)
	}

	saClearSession(chat, user)
	return ctx.React("✅")
}

func saFail(ctx *core.Context, err error) error {
	slog.Error("❌ [sa] Error:", "error", err)
	_ = ctx.React("❌")
	return ctx.Reply("❌ No pude crear el sticker con audio: " + err.Error())
}

func saUsageText() string {
	return "🎵 *Stickers con audio (.sa)*\n" +
		"• Vídeo con `.sa` → sticker animado + su audio.\n" +
		"• Imagen/sticker + audio citado → sticker estático + audio.\n" +
		"• Responde a un audio con `.sa` y luego manda la imagen.\n" +
		"• `.sa auto` con un audio → onda generada.\n" +
		"• `.sa audio` citando un sticker sonoro → extrae el sonido.\n" +
		"• `.sa 0:03` o `.sa 2 5` → recorte de inicio/duración.\n" +
		"• Efectos: `crop`/`c`, `e`/`estirar`, `s`/`spherical`, `b`/`border`, `r`/`reverse`, `h`/`high`.\n" +
		"• `.sa inspect` → inspecciona el sticker citado."
}

// saVisualFrom descarga el medio visual ya clasificado.
func saVisualFrom(ctx *core.Context, media saMedia) (*saVisual, error) {
	data, err := saDownloadFn(ctx, media.Download)
	if err != nil {
		return nil, fmt.Errorf("no pude descargar el medio: %w", err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("el medio visual está vacío")
	}
	v := &saVisual{
		Data:      data,
		Animated:  media.Kind == saKindVideo || media.Animated,
		IsSticker: media.Kind == saKindSticker,
		IsVideo:   media.Kind == saKindVideo,
	}
	if media.Kind == saKindVideo {
		v.Mimetype = "video/mp4"
	} else if media.Kind == saKindSticker {
		v.Mimetype = "image/webp"
	} else {
		v.Mimetype = "image/jpeg"
	}
	return v, nil
}

// saBuildWebP reutiliza el WebP de un sticker si ya cumple, o lo recodifica.
func saBuildWebP(v *saVisual, plan saPlan, args saArgs, start, duration float64) ([]byte, error) {
	if v == nil {
		return nil, fmt.Errorf("falta el medio visual")
	}
	if len(v.Data) > 100*1024*1024 {
		return nil, fmt.Errorf("el medio visual supera los 100 MB")
	}
	workDir, err := saMkdirTemp("", "sa_webp_")
	if err != nil {
		return nil, err
	}
	defer saRemoveAll(workDir)

	// Solo se reutiliza el WebP original si no hay transformaciones pedidas.
	if v.IsSticker && !saNeedsVisualTransform(args) && !args.Reverse {
		if w, h, ok := saWebPDims(v.Data); ok && w == saSize && h == saSize && len(v.Data) <= plan.SizeLimit {
			if out := saApplyExif(v.Data); len(out) <= plan.SizeLimit {
				return out, nil
			}
			return v.Data, nil
		}
	}

	ext := saMimeExt(v.Mimetype)
	inputPath := saWriteTempFile(workDir, "in_visual."+ext, v.Data)
	outputPath := filepath.Join(workDir, "out_visual.webp")

	filter := saVisualFilter(args)
	var webp []byte
	if plan.Animated {
		encStart, encDur := start, duration
		if v.IsSticker {
			// Un sticker animado ya trae su propio bucle: no se recorta.
			encStart, encDur = 0, saMaxVideoSeconds
		}
		webp, err = saEncodeAnimatedWebP(inputPath, outputPath, encStart, encDur, plan.SizeLimit, args)
	} else {
		webp, err = saEncodeStaticWebP(inputPath, outputPath, plan.SizeLimit, filter)
	}
	if err != nil {
		return nil, err
	}
	if len(webp) > plan.SizeLimit {
		return nil, fmt.Errorf("el WebP (%d KB) supera el límite de %d KB", len(webp)/1024, plan.SizeLimit/1024)
	}

	if out := saApplyExif(webp); len(out) > 0 && len(out) <= plan.SizeLimit {
		webp = out
	}
	return webp, nil
}

func saWriteTempFile(dir, name string, data []byte) string {
	path := filepath.Join(dir, name)
	_ = os.WriteFile(path, data, 0o600)
	return path
}

// saPrepareAudio normaliza a OGG/Opus mono y aplica el tope de duración.
func saPrepareAudio(ctx *core.Context, data []byte, sourceSeconds, maxSeconds, start float64) ([]byte, uint32, error) {
	if len(data) == 0 {
		return nil, 0, fmt.Errorf("audio vacío")
	}
	if len(data) > 30*1024*1024 {
		return nil, 0, fmt.Errorf("el audio supera los 30 MB")
	}
	workDir, err := saMkdirTemp("", "sa_audio_")
	if err != nil {
		return nil, 0, err
	}
	defer saRemoveAll(workDir)

	inputPath := saWriteTempFile(workDir, "in_audio."+saMimeExt(saSniffAudioMime(data)), data)
	if sourceSeconds <= 0 {
		info := saProbeFn(inputPath)
		sourceSeconds = info.Duration
	}
	if sourceSeconds <= 0 {
		sourceSeconds = maxSeconds
	}
	dur := saClampDuration(sourceSeconds, maxSeconds)
	if start < 0 {
		start = 0
	}
	if start >= sourceSeconds {
		start = 0
	}
	if remain := sourceSeconds - start; remain > 0 && remain < dur {
		dur = remain
	}
	outputPath := filepath.Join(workDir, "out_audio.ogg")
	if err := saExtractOGG(inputPath, outputPath, start, dur); err != nil {
		return nil, 0, fmt.Errorf("convirtiendo audio a OGG/Opus: %w", err)
	}
	ogg, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, 0, err
	}
	if len(ogg) == 0 {
		return nil, 0, fmt.Errorf("el OGG resultante está vacío")
	}
	seconds := uint32(math.Ceil(dur))
	if seconds == 0 {
		seconds = 1
	}
	return ogg, seconds, nil
}

// saAudioFromVideo extrae y normaliza el audio del vídeo en una sola pasada.
func saAudioFromVideo(video []byte, start, maxSeconds float64) ([]byte, float64, error) {
	workDir, err := saMkdirTemp("", "sa_video_")
	if err != nil {
		return nil, 0, err
	}
	defer saRemoveAll(workDir)

	inputPath := saWriteTempFile(workDir, "in_visual.mp4", video)
	info := saProbeFn(inputPath)
	if !info.HasAudio {
		return nil, 0, fmt.Errorf("el vídeo no tiene pista de audio")
	}
	available := info.Duration - start
	if available <= 0 {
		start = 0
		available = info.Duration
	}
	dur := saClampDuration(available, maxSeconds)
	outputPath := filepath.Join(workDir, "out_audio.ogg")
	if err := saExtractOGG(inputPath, outputPath, start, dur); err != nil {
		return nil, 0, err
	}
	ogg, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, 0, err
	}
	return ogg, dur, nil
}

// saSniffAudioMime reconoce un par de formatos por su cabecera.
func saSniffAudioMime(data []byte) string {
	switch {
	case len(data) >= 4 && string(data[:4]) == "OggS":
		return "audio/ogg"
	case len(data) >= 3 && string(data[:3]) == "ID3":
		return "audio/mpeg"
	case len(data) >= 2 && data[0] == 0xFF && data[1]&0xE0 == 0xE0:
		return "audio/mpeg"
	case len(data) >= 12 && string(data[4:8]) == "ftyp":
		return "audio/mp4"
	case len(data) >= 4 && string(data[:4]) == "RIFF":
		return "audio/wav"
	}
	return "audio/ogg"
}
