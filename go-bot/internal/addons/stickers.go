package addons

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"

	"whatsbot-go/internal/core"
	"whatsbot-go/internal/store"
)

func init() {
	core.Register(Stickers{})
	core.RegisterBackground("stickers", stickersInit)
}

const (
	stickersStaticLimit   = 100000
	stickersAnimatedLimit = 1000000
	stickersCollectionTTL = 2 * time.Minute
)

var stickersCmds = map[string]bool{"st": true, "sticker": true, "s": true}

// Inyectables para pruebas.
var (
	stickersExec            = exec.CommandContext
	stickersProbe           = stickersProbeDefault
	stickersMkdirTemp       = os.MkdirTemp
	stickersRemoveAll       = os.RemoveAll
	stickersNow             = time.Now
	stickersRandN           = rand.IntN
	stickersDownloadCurrent = func(ctx *core.Context) ([]byte, string, error) {
		data, err := ctx.DownloadMedia()
		if err != nil {
			return nil, "", err
		}
		return data, stickersMimetypeOf(ctx.Message.Message), nil
	}
	stickersDownloadQuoted = func(ctx *core.Context) ([]byte, string, error) {
		data, err := ctx.DownloadQuoted()
		if err != nil {
			return nil, "", err
		}
		return data, stickersMimetypeOf(ctx.QuotedMessage()), nil
	}
)

type stickersBuffer struct {
	Buffer            []byte
	Mimetype          string
	IsAnimatedSticker bool
}

type stickersSession struct {
	Buffers  []stickersBuffer
	Args     []string
	IsActive bool
	Client   *whatsmeow.Client
	Invoker  *events.Message
	Timer    *time.Timer
}

type stickersArgs struct {
	Crop    bool
	Reverse bool
	Stretch bool
	Shape   string
	Mode    string
}

var (
	stickersMu       sync.Mutex
	stickersSessions = make(map[string]*stickersSession)

	stickersPrefixMu     sync.Mutex
	stickersPrefixCache  []string
	stickersPrefixCached time.Time
)

// Stickers porta addons/stickers.js: .st/.sticker/.s.
type Stickers struct{}

// Commands cumple core.Addon.
func (Stickers) Commands() []string { return []string{"st", "sticker", "s"} }

// Handle replica el handler original, incluido el modo colección ('a').
func (Stickers) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}
	cacheDir := core.DataPath("cache")
	_ = os.MkdirAll(cacheDir, 0o755)

	normalizedArgs := stickersNormalizeArgs(ctx.Args)
	jid := ctx.ChatJID().String()

	if stickersHas(normalizedArgs, "a") {
		argsWithoutA := make([]string, 0, len(normalizedArgs))
		for _, a := range normalizedArgs {
			if a != "a" {
				argsWithoutA = append(argsWithoutA, a)
			}
		}

		stickersMu.Lock()
		if prev, ok := stickersSessions[jid]; ok {
			if prev.Timer != nil {
				prev.Timer.Stop()
			}
			delete(stickersSessions, jid)
		}
		session := &stickersSession{
			Args:     argsWithoutA,
			IsActive: true,
			Client:   ctx.Client,
			Invoker:  ctx.Message,
		}
		stickersSessions[jid] = session
		stickersMu.Unlock()

		session.Timer = time.AfterFunc(stickersCollectionTTL, func() {
			stickersMu.Lock()
			session.IsActive = false
			if stickersSessions[jid] == session {
				delete(stickersSessions, jid)
			}
			stickersMu.Unlock()
			stickersProcessCollection(session, jid)
		})

		if err := ctx.SendQuoted(core.TextMessage("📦 Modo colección activado. Envíame imágenes, vídeos o stickers durante el próximo minuto. Cada archivo se convertirá en un sticker individual con los filtros indicados."), ctx.Message); err != nil {
			return err
		}
		return ctx.React("⏳")
	}

	parsed := stickersParseArgs(normalizedArgs)
	if parsed.Shape == "spherical" {
		parsed.Crop = true
	}

	if stickersIsViewOnce(ctx.Message.Message) {
		return ctx.React("🚫")
	}

	if err := ctx.React("⏳"); err != nil {
		return err
	}

	data, mimetype, isAnimatedSticker, _ := stickersExtract(ctx)
	if len(data) < 100 || mimetype == "" || (!strings.HasPrefix(mimetype, "image/") && !strings.HasPrefix(mimetype, "video/")) {
		return ctx.React("❓")
	}

	isGif := mimetype == "image/gif"
	isVideo := strings.HasPrefix(mimetype, "video/")
	isAnimatedInput := isGif || isVideo || isAnimatedSticker
	uniqueID := stickersUniqueID()

	var final []byte
	var err error
	if !isAnimatedInput {
		ext := "jpg"
		if strings.Contains(mimetype, "webp") {
			ext = "webp"
		}
		final, err = stickersProcessStatic(data, parsed.Crop, parsed.Shape, cacheDir, uniqueID, stickersStaticLimit, ext, parsed.Stretch)
	} else {
		ext := "mp4"
		if isGif {
			ext = "gif"
		} else if isAnimatedSticker {
			ext = "webp"
		}
		final, err = stickersProcessAnimated(data, parsed.Mode, parsed.Crop, parsed.Reverse, parsed.Shape, cacheDir, uniqueID, ext, stickersAnimatedLimit, isGif, parsed.Stretch)
	}
	if err != nil {
		slog.Error("Error en addon de stickers", "error", err)
		return ctx.React("❌")
	}

	limit := stickersStaticLimit
	if isAnimatedInput {
		limit = stickersAnimatedLimit
	}
	if len(final) == 0 || len(final) > limit {
		return ctx.React("❌")
	}

	if err := ctx.SendSticker(final, isAnimatedInput); err != nil {
		return err
	}
	return ctx.React("✅")
}

// ─── Parseo de argumentos y estado ──────────────────────────────────────────

func stickersNormalizeArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		for _, part := range strings.Fields(strings.ToLower(a)) {
			out = append(out, part)
		}
	}
	return out
}

func stickersHas(args []string, v string) bool {
	for _, a := range args {
		if a == v {
			return true
		}
	}
	return false
}

func stickersParseArgs(args []string) stickersArgs {
	out := stickersArgs{Mode: "fluid", Shape: "none"}
	out.Crop = stickersHas(args, "crop") || stickersHas(args, "c")
	out.Reverse = stickersHas(args, "reverse") || stickersHas(args, "r")
	out.Stretch = stickersHas(args, "e") || stickersHas(args, "estirar")
	for _, a := range args {
		if a == "spherical" || a == "s" {
			out.Shape = "spherical"
			break
		}
		if a == "border" || a == "b" {
			out.Shape = "border"
			break
		}
	}
	for _, a := range args {
		if a == "h" || a == "high" {
			out.Mode = "high"
			break
		}
		if a == "f" || a == "fluid" {
			out.Mode = "fluid"
			break
		}
	}
	return out
}

func stickersUniqueID() string {
	return fmt.Sprintf("%d_%05d", stickersNow().UnixNano(), stickersRandN(100000))
}

// ─── Descarga de medios ─────────────────────────────────────────────────────

func stickersMimetypeOf(msg *waE2E.Message) string {
	if msg == nil {
		return ""
	}
	switch {
	case msg.GetImageMessage() != nil:
		return msg.GetImageMessage().GetMimetype()
	case msg.GetVideoMessage() != nil:
		return msg.GetVideoMessage().GetMimetype()
	case msg.GetDocumentMessage() != nil:
		return msg.GetDocumentMessage().GetMimetype()
	case msg.GetStickerMessage() != nil:
		return msg.GetStickerMessage().GetMimetype()
	}
	return ""
}

func stickersExtract(ctx *core.Context) (data []byte, mimetype string, isAnimatedSticker bool, err error) {
	if ctx.Message != nil && ctx.Message.Message != nil {
		m := ctx.Message.Message
		if m.GetImageMessage() != nil || m.GetVideoMessage() != nil || m.GetDocumentMessage() != nil || m.GetStickerMessage() != nil {
			data, mimetype, err = stickersDownloadCurrent(ctx)
			isAnimatedSticker = m.GetStickerMessage().GetIsAnimated()
			return
		}
	}
	quoted := ctx.QuotedMessage()
	if quoted == nil {
		return nil, "", false, nil
	}
	if quoted.GetImageMessage() == nil && quoted.GetVideoMessage() == nil && quoted.GetDocumentMessage() == nil && quoted.GetStickerMessage() == nil {
		return nil, "", false, nil
	}
	data, mimetype, err = stickersDownloadQuoted(ctx)
	if err != nil {
		return nil, "", false, err
	}
	isAnimatedSticker = quoted.GetStickerMessage().GetIsAnimated()
	return data, mimetype, isAnimatedSticker, nil
}

func stickersIsViewOnce(msg *waE2E.Message) bool {
	if msg == nil {
		return false
	}
	if msg.GetImageMessage().GetViewOnce() || msg.GetVideoMessage().GetViewOnce() {
		return true
	}
	return msg.GetViewOnceMessage() != nil || msg.GetViewOnceMessageV2() != nil
}

// stickersAddToSession añade un medio a la colección activa (usado por el listener).
func stickersAddToSession(jid string, data []byte, mimetype string, animated bool) bool {
	stickersMu.Lock()
	defer stickersMu.Unlock()
	session, ok := stickersSessions[jid]
	if !ok || !session.IsActive {
		return false
	}
	session.Buffers = append(session.Buffers, stickersBuffer{Buffer: data, Mimetype: mimetype, IsAnimatedSticker: animated})
	return true
}

// ─── Colección ──────────────────────────────────────────────────────────────

func stickersProcessCollection(session *stickersSession, jid string) {
	stickersMu.Lock()
	buffers := append([]stickersBuffer(nil), session.Buffers...)
	args := append([]string(nil), session.Args...)
	client := session.Client
	invoker := session.Invoker
	stickersMu.Unlock()

	ctx := &core.Context{Client: client, Message: invoker}
	if len(buffers) == 0 {
		_ = ctx.SendText("❌ No se recibieron archivos durante la colección.")
		return
	}

	parsed := stickersParseArgs(args)
	cacheDir := core.DataPath("cache")
	_ = os.MkdirAll(cacheDir, 0o755)

	processed := 0
	for _, item := range buffers {
		isGif := item.Mimetype == "image/gif"
		isVideo := strings.HasPrefix(item.Mimetype, "video/")
		isAnimatedInput := isGif || isVideo || item.IsAnimatedSticker
		uniqueID := stickersUniqueID()

		var final []byte
		var err error
		if !isAnimatedInput {
			ext := "jpg"
			if strings.Contains(item.Mimetype, "webp") {
				ext = "webp"
			}
			final, err = stickersProcessStatic(item.Buffer, parsed.Crop, parsed.Shape, cacheDir, uniqueID, stickersStaticLimit, ext, parsed.Stretch)
		} else {
			ext := "mp4"
			if isGif {
				ext = "gif"
			} else if item.IsAnimatedSticker {
				ext = "webp"
			}
			final, err = stickersProcessAnimated(item.Buffer, parsed.Mode, parsed.Crop, parsed.Reverse, parsed.Shape, cacheDir, uniqueID, ext, stickersAnimatedLimit, isGif, parsed.Stretch)
		}

		limit := stickersStaticLimit
		if isAnimatedInput {
			limit = stickersAnimatedLimit
		}
		if err != nil {
			slog.Error("Error procesando un elemento de la colección", "error", err)
			_ = ctx.SendText("❌ Error al procesar uno de los archivos.")
			continue
		}
		if len(final) == 0 || len(final) > limit {
			_ = ctx.SendText(fmt.Sprintf("⚠️ Un archivo supera el límite de tamaño (%d KB) y no se pudo procesar.", limit/1000))
			continue
		}
		if err := ctx.SendSticker(final, isAnimatedInput); err != nil {
			slog.Error("Error enviando sticker de la colección", "error", err)
			continue
		}
		processed++
	}
	_ = ctx.SendText(fmt.Sprintf("✅ Colección completada. Se generaron %d stickers.", processed))
}

// ─── Filtros y fps ──────────────────────────────────────────────────────────

func stickersFilterChain(isCrop bool, shapeMode string, isStretch bool) string {
	rgb := "r='p(X,Y)':g='p(X,Y)':b='p(X,Y)'"
	shape := ""
	switch shapeMode {
	case "spherical":
		shape = ",geq=" + rgb + ":a='p(X,Y)*lte((X-W/2)*(X-W/2)+(Y-H/2)*(Y-H/2),(min(W,H)/2)*(min(W,H)/2))'"
	case "border":
		r := "(min(W,H)*0.1)"
		dx := "max(0,abs(X-W/2)-(W/2-" + r + "))"
		dy := "max(0,abs(Y-H/2)-(H/2-" + r + "))"
		shape = ",geq=" + rgb + ":a='p(X,Y)*lte(" + dx + "*" + dx + "+" + dy + "*" + dy + "," + r + "*" + r + ")'"
	}

	if isStretch {
		return "scale=512:512,format=rgba" + shape
	}
	if isCrop {
		return "scale='max(512,iw*512/ih)':'max(512,ih*512/iw)',format=rgba,crop=512:512" + shape
	}
	return "scale=512:512:force_original_aspect_ratio=decrease,format=rgba" + shape + ",pad=512:512:(ow-iw)/2:(oh-ih)/2:color=0x00000000"
}

func stickersCalcTargetFps(mode string, duration, sourceFps float64) int {
	fps := 18
	if mode == "high" {
		switch {
		case duration < 1.5:
			fps = int(math.Min(30, sourceFps))
		case duration < 3.0:
			fps = 24
		case duration < 6.0:
			fps = 20
		default:
			fps = 18
		}
		return fps
	}
	switch {
	case duration < 2.0:
		fps = 24
	case duration < 4.0:
		fps = 16
	case duration < 6.0:
		fps = 12
	default:
		fps = 8
	}
	return fps
}

// stickersProbeDefault usa ffprobe con los mismos datos que fluent-ffmpeg.
func stickersProbeDefault(filePath string) (float64, int) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := stickersExec(ctx, "ffprobe", "-v", "error", "-print_format", "json", "-show_format", "-show_streams", filePath).Output()
	if err != nil {
		return 9.9, 24
	}
	var parsed struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			CodecType  string `json:"codec_type"`
			RFrameRate string `json:"r_frame_rate"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return 9.9, 24
	}
	duration := 9.9
	if v, err := strconv.ParseFloat(parsed.Format.Duration, 64); err == nil {
		duration = v
	}
	fps := 24.0
	for _, s := range parsed.Streams {
		if s.CodecType == "video" && s.RFrameRate != "" {
			parts := strings.Split(s.RFrameRate, "/")
			if len(parts) == 2 && parts[1] != "0" {
				a, errA := strconv.ParseFloat(parts[0], 64)
				b, errB := strconv.ParseFloat(parts[1], 64)
				if errA == nil && errB == nil && b != 0 {
					fps = a / b
				}
			}
			break
		}
	}
	return duration, int(math.Round(fps))
}

// ─── Construcción de argumentos ffmpeg ──────────────────────────────────────

func stickersStaticArgs(input, output, filter string, q int) []string {
	return []string{
		"-y", "-err_detect", "ignore_err", "-i", input,
		"-vcodec", "libwebp", "-lossless", "0", "-q:v", strconv.Itoa(q),
		"-compression_level", "6", "-an", "-vf", filter, output,
	}
}

func stickersExtractFramesArgs(input, framesPattern string, fps int, trim float64, filter, ext string) []string {
	args := []string{"-y"}
	if ext == "webp" {
		args = append(args, "-vcodec", "libwebp_anim")
	}
	args = append(args, "-err_detect", "ignore_err", "-i", input, "-t", strconv.FormatFloat(trim, 'f', -1, 64),
		"-c:v", "png", "-pix_fmt", "rgba", "-vf", fmt.Sprintf("fps=%d,%s", fps, filter), framesPattern)
	return args
}

func stickersEncodeArgs(framesPattern, output string, fps, q int) []string {
	return []string{
		"-y", "-framerate", strconv.Itoa(fps), "-i", framesPattern,
		"-c:v", "libwebp", "-lossless", "0", "-q:v", strconv.Itoa(q),
		"-compression_level", "4", "-loop", "0", "-an", output,
	}
}

// ─── Procesamiento estático ─────────────────────────────────────────────────

func stickersProcessStatic(buffer []byte, isCrop bool, shapeMode, diskDir, uniqueID string, sizeLimit int, ext string, isStretch bool) ([]byte, error) {
	inputPath := filepath.Join(diskDir, "in_static_"+uniqueID+"."+ext)
	outputPath := filepath.Join(diskDir, "out_static_"+uniqueID+".webp")
	defer os.Remove(inputPath)
	defer os.Remove(outputPath)

	if err := os.WriteFile(inputPath, buffer, 0o600); err != nil {
		return nil, err
	}

	targetSize := float64(sizeLimit) * 0.90
	filterChain := stickersFilterChain(isCrop, shapeMode, isStretch)
	qMin, qMax := 10, 100
	var best []byte
	bestDiff := math.Inf(1)

	for i := 0; i < 3; i++ {
		q := (qMin + qMax) / 2
		if _, err := stickersExec(context.Background(), "ffmpeg", stickersStaticArgs(inputPath, outputPath, filterChain, q)...).CombinedOutput(); err != nil {
			return nil, err
		}
		current, err := os.ReadFile(outputPath)
		if err != nil {
			return nil, err
		}
		size := float64(len(current))
		diff := math.Abs(targetSize - size)
		if len(current) <= sizeLimit && diff < bestDiff {
			best = current
			bestDiff = diff
		}
		if size > targetSize {
			qMax = q - 1
		} else {
			qMin = q + 1
		}
	}
	if best != nil {
		return best, nil
	}
	return os.ReadFile(outputPath)
}

// ─── Procesamiento animado ──────────────────────────────────────────────────

func stickersProcessAnimated(buffer []byte, mode string, isCrop, isReverse bool, shapeMode, diskDir, uniqueID, ext string, sizeLimit int, isGif, isStretch bool) ([]byte, error) {
	workDir, err := stickersMkdirTemp("", "stk_")
	if err != nil {
		return nil, err
	}
	defer stickersRemoveAll(workDir)

	inputPath := filepath.Join(workDir, "in_"+uniqueID+"."+ext)
	outputPath := filepath.Join(workDir, "out_"+uniqueID+".webp")
	framesDir := filepath.Join(workDir, "frames")
	if err := os.MkdirAll(framesDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(inputPath, buffer, 0o600); err != nil {
		return nil, err
	}

	sourceDuration, sourceFps := stickersProbe(inputPath)
	if sourceDuration <= 0 {
		sourceDuration = 5
	}
	if sourceFps <= 0 {
		sourceFps = 30
	}
	trimDuration := math.Min(sourceDuration, 9.9)
	if isReverse {
		trimDuration = math.Min(sourceDuration, 4.9)
	}
	expectedFinalDuration := trimDuration
	if isReverse {
		expectedFinalDuration = trimDuration * 2
	}
	targetFps := stickersCalcTargetFps(mode, expectedFinalDuration, float64(sourceFps))

	filterChain := stickersFilterChain(isCrop, shapeMode, isStretch)
	framesPattern := filepath.Join(framesDir, "frame_%04d.png")

	if _, err := stickersExec(context.Background(), "ffmpeg", stickersExtractFramesArgs(inputPath, framesPattern, targetFps, trimDuration, filterChain, ext)...).CombinedOutput(); err != nil {
		return nil, err
	}

	framePrefix := "frame_%04d.png"
	if isReverse {
		if err := stickersReverseFrames(framesDir); err != nil {
			return nil, err
		}
		framePrefix = "loop_%04d.png"
	}

	targetSize := float64(sizeLimit) * 0.90
	margin := float64(sizeLimit) * 0.05
	qMin, qMax := 0, 100
	var best []byte
	bestDiff := math.Inf(1)
	frameDelay := int(math.Round(1000 / float64(targetFps)))
	encodePattern := filepath.Join(framesDir, framePrefix)

	for i := 0; i < 4; i++ {
		q := (qMin + qMax) / 2
		if isGif {
			files, _ := filepath.Glob(filepath.Join(framesDir, "*.png"))
			sort.Strings(files)
			if len(files) == 0 {
				return nil, fmt.Errorf("Fallo en img2webp: no hay frames")
			}
			args := []string{"-loop", "0", "-lossy", "-m", "4", "-q", strconv.Itoa(q), "-d", strconv.Itoa(frameDelay)}
			args = append(args, files...)
			args = append(args, "-o", outputPath)
			if _, err := stickersExec(context.Background(), "img2webp", args...).CombinedOutput(); err != nil {
				return nil, fmt.Errorf("Fallo en img2webp: %w", err)
			}
		} else {
			if _, err := stickersExec(context.Background(), "ffmpeg", stickersEncodeArgs(encodePattern, outputPath, targetFps, q)...).CombinedOutput(); err != nil {
				return nil, err
			}
		}

		current, err := os.ReadFile(outputPath)
		if err != nil {
			return nil, err
		}
		size := float64(len(current))
		diff := math.Abs(targetSize - size)
		if len(current) <= sizeLimit && diff < bestDiff {
			best = current
			bestDiff = diff
		}
		if size >= targetSize-margin && size <= targetSize+margin {
			break
		}
		if size > targetSize {
			qMax = q - 1
		} else {
			qMin = q + 1
		}
	}
	if best != nil {
		return best, nil
	}
	return os.ReadFile(outputPath)
}

// stickersReverseFrameSequence replica el mapeo de frames del original.
func stickersReverseFrameSequence(total int) []int {
	if total <= 0 {
		return nil
	}
	mapped := make([]int, 0, total)
	current := 0.0
	for current < float64(total-1) {
		mapped = append(mapped, int(math.Round(current)))
		progress := current / float64(total)
		speed := 1.0
		if progress <= 0.25 {
			t := progress / 0.25
			speed = 0.6 + 0.4*math.Sin(t*math.Pi/2)
		} else if progress >= 0.75 {
			t := (progress - 0.75) / 0.25
			speed = 1.0 + 0.4*(1-math.Cos(t*math.Pi/2))
		}
		current += speed
	}
	if len(mapped) == 0 || mapped[len(mapped)-1] != total-1 {
		mapped = append(mapped, total-1)
	}
	final := append([]int(nil), mapped...)
	for i := len(mapped) - 2; i >= 0; i-- {
		final = append(final, mapped[i])
	}
	return final
}

func stickersReverseFrames(framesDir string) error {
	entries, err := os.ReadDir(framesDir)
	if err != nil {
		return err
	}
	frames := make([]string, 0)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "frame_") {
			frames = append(frames, e.Name())
		}
	}
	sort.Strings(frames)
	seq := stickersReverseFrameSequence(len(frames))
	for i, srcIdx := range seq {
		if srcIdx < 0 || srcIdx >= len(frames) {
			continue
		}
		src := filepath.Join(framesDir, frames[srcIdx])
		dst := filepath.Join(framesDir, fmt.Sprintf("loop_%04d.png", i+1))
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		if err := os.WriteFile(dst, data, 0o600); err != nil {
			return err
		}
	}
	for _, f := range frames {
		_ = os.Remove(filepath.Join(framesDir, f))
	}
	return nil
}

// ─── Prefijos y listener de colección ───────────────────────────────────────

func stickersGetPrefixes() []string {
	stickersPrefixMu.Lock()
	defer stickersPrefixMu.Unlock()
	now := time.Now()
	if stickersPrefixCache != nil && now.Sub(stickersPrefixCached) < 10*time.Second {
		return stickersPrefixCache
	}
	prefixes := []string{",", "!", "/"}
	var cfg struct {
		Prefixes []string `json:"prefixes"`
	}
	if err := core.ReadJSON(core.DataPath("assets", "config.json"), &cfg); err == nil && len(cfg.Prefixes) > 0 {
		prefixes = cfg.Prefixes
	}
	stickersPrefixCache = prefixes
	stickersPrefixCached = now
	return prefixes
}

func stickersIsStickerCommand(text string) bool {
	if text == "" {
		return false
	}
	for _, p := range stickersGetPrefixes() {
		if p == "" || !strings.HasPrefix(text, p) {
			continue
		}
		rest := strings.TrimSpace(text[len(p):])
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		if stickersCmds[strings.ToLower(fields[0])] {
			return true
		}
	}
	return false
}

func stickersInit(client *whatsmeow.Client, st *store.Store) error {
	if client == nil {
		return nil
	}
	client.AddEventHandler(func(evt any) {
		msg, ok := evt.(*events.Message)
		if !ok || msg == nil || msg.Message == nil {
			return
		}
		text := core.ExtractText(msg.Message)
		if stickersIsStickerCommand(text) {
			return
		}
		jid := msg.Info.Chat.String()
		stickersMu.Lock()
		session, ok := stickersSessions[jid]
		active := ok && session.IsActive
		stickersMu.Unlock()
		if !active {
			return
		}

		ctx := &core.Context{Client: client, Message: msg}
		data, mimetype, animated, err := stickersExtract(ctx)
		if err != nil || len(data) <= 100 || mimetype == "" {
			return
		}
		if !strings.HasPrefix(mimetype, "image/") && !strings.HasPrefix(mimetype, "video/") {
			return
		}
		if stickersAddToSession(jid, data, mimetype, animated) {
			_ = ctx.React("📥")
		}
	})
	return nil
}

func stickersResetSessions() {
	stickersMu.Lock()
	for _, s := range stickersSessions {
		if s.Timer != nil {
			s.Timer.Stop()
		}
	}
	stickersSessions = make(map[string]*stickersSession)
	stickersMu.Unlock()
}
