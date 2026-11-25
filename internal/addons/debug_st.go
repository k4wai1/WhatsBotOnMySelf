// debug_st.go — addon de diagnóstico `.debug-st` / `.sdebug`.
//
// Inspecciona un sticker citado (normal, animado, Lottie o con audio), muestra
// todos sus campos de forma segura (longitudes/prefijos, nunca secretos
// completos) y, con el argumento `full`, descarga el WebP y el audio reales,
// extrae el EXIF y pasa ffprobe al audio.
package addons

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
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
	"google.golang.org/protobuf/encoding/protojson"

	"whatsbot-go/internal/core"
	"whatsbot-go/internal/store"
)

func init() {
	core.Register(&debugStAddon{})
	core.RegisterBackground("debug-st", debugStInit)
}

type debugStAddon struct{}

func (*debugStAddon) Commands() []string { return []string{"debug-st", "sdebug"} }

// Inyectables para pruebas (sin red ni binarios reales).
var (
	debugStDownloadFn = func(ctx *core.Context, media whatsmeow.DownloadableMessage) ([]byte, error) {
		if ctx == nil || ctx.Client == nil || media == nil {
			return nil, fmt.Errorf("cliente de WhatsApp no disponible")
		}
		return ctx.Client.Download(context.Background(), media)
	}
	debugStExec    = exec.CommandContext
	debugStProbeFn = debugStProbeAudio
	debugStLog     = func(msg string, args ...any) { slog.Info(msg, args...) }
)

// ─── Caché de stickers originales (sin recortes de la cita) ────────────────

const debugStCacheLimit = 100

type debugStCached struct {
	Message   *waE2E.Message
	Type      string
	MediaType string
	At        time.Time
}

var (
	debugStCacheMu    sync.Mutex
	debugStCacheMap   = map[string]*debugStCached{}
	debugStCacheOrder []string
)

// debugStRemember guarda un mensaje entrante si contiene un sticker.
func debugStRemember(msg *events.Message) bool {
	if msg == nil || msg.Message == nil {
		return false
	}
	if debugStInspectWrappers(msg.Message).Sticker == nil {
		return false
	}
	id := string(msg.Info.ID)
	if id == "" {
		return false
	}
	debugStCacheMu.Lock()
	defer debugStCacheMu.Unlock()
	if _, ok := debugStCacheMap[id]; !ok {
		debugStCacheOrder = append(debugStCacheOrder, id)
	}
	debugStCacheMap[id] = &debugStCached{
		Message:   msg.Message,
		Type:      msg.Info.Type,
		MediaType: msg.Info.MediaType,
		At:        time.Now(),
	}
	for len(debugStCacheOrder) > debugStCacheLimit {
		viejo := debugStCacheOrder[0]
		debugStCacheOrder = debugStCacheOrder[1:]
		delete(debugStCacheMap, viejo)
	}
	return true
}

func debugStCacheGet(id string) (*debugStCached, bool) {
	if id == "" {
		return nil, false
	}
	debugStCacheMu.Lock()
	defer debugStCacheMu.Unlock()
	item, ok := debugStCacheMap[id]
	return item, ok
}

func debugStResetCache() {
	debugStCacheMu.Lock()
	debugStCacheMap = map[string]*debugStCached{}
	debugStCacheOrder = nil
	debugStCacheMu.Unlock()
}

func debugStCacheLen() int {
	debugStCacheMu.Lock()
	defer debugStCacheMu.Unlock()
	return len(debugStCacheMap)
}

// debugStInit escucha stickers para conservar el protobuf original completo.
func debugStInit(client *whatsmeow.Client, _ *store.Store) error {
	if client == nil {
		return nil
	}
	client.AddEventHandler(func(evt any) {
		msg, ok := evt.(*events.Message)
		if !ok || msg == nil || msg.Message == nil {
			return
		}
		debugStRemember(msg)
	})
	return nil
}

// ─── Detección de envoltorios ──────────────────────────────────────────────

type debugStWrapper struct {
	Chain   []string
	Sticker *waE2E.StickerMessage
	Inner   *waE2E.Message
}

// debugStInspectWrappers recorre los FutureProofMessage conocidos hasta el
// StickerMessage. No imprime nada; solo describe la cadena.
func debugStInspectWrappers(msg *waE2E.Message) debugStWrapper {
	out := debugStWrapper{}
	cur := msg
	for i := 0; i < 8 && cur != nil; i++ {
		switch {
		case cur.GetAudioStickerMessage() != nil:
			out.Chain = append(out.Chain, "AudioStickerMessage (campo 134)")
			cur = cur.GetAudioStickerMessage().GetMessage()
		case cur.GetLottieStickerMessage() != nil:
			out.Chain = append(out.Chain, "LottieStickerMessage (campo 74)")
			cur = cur.GetLottieStickerMessage().GetMessage()
		case cur.GetEphemeralMessage() != nil:
			out.Chain = append(out.Chain, "EphemeralMessage")
			cur = cur.GetEphemeralMessage().GetMessage()
		case cur.GetViewOnceMessage() != nil:
			out.Chain = append(out.Chain, "ViewOnceMessage")
			cur = cur.GetViewOnceMessage().GetMessage()
		case cur.GetViewOnceMessageV2() != nil:
			out.Chain = append(out.Chain, "ViewOnceMessageV2")
			cur = cur.GetViewOnceMessageV2().GetMessage()
		case cur.GetViewOnceMessageV2Extension() != nil:
			out.Chain = append(out.Chain, "ViewOnceMessageV2Extension")
			cur = cur.GetViewOnceMessageV2Extension().GetMessage()
		case cur.GetDocumentWithCaptionMessage() != nil:
			out.Chain = append(out.Chain, "DocumentWithCaptionMessage")
			cur = cur.GetDocumentWithCaptionMessage().GetMessage()
		default:
			if st := cur.GetStickerMessage(); st != nil {
				out.Sticker = st
				out.Inner = cur
			}
			return out
		}
	}
	return out
}

func debugStChainText(chain []string) string {
	if len(chain) == 0 {
		return "directo (StickerMessage)"
	}
	return strings.Join(chain, " → ") + " → StickerMessage"
}

// ─── Formato seguro ────────────────────────────────────────────────────────

func debugStStr(v string) string {
	if v == "" {
		return "—"
	}
	return v
}

func debugStBool(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func debugStUint(v uint32) string { return strconv.FormatUint(uint64(v), 10) }

// debugStSecret describe un secreto sin exponerlo: longitud + prefijo corto.
func debugStSecret(raw []byte) string {
	if len(raw) == 0 {
		return "no"
	}
	prefijo := raw
	if len(prefijo) > 4 {
		prefijo = prefijo[:4]
	}
	return fmt.Sprintf("%d bytes (prefijo %s)", len(raw), hex.EncodeToString(prefijo))
}

// debugStPath recorta el DirectPath a su parte relativa sin query sensible.
func debugStPath(raw string) string {
	if raw == "" {
		return "no"
	}
	if idx := strings.IndexByte(raw, '?'); idx >= 0 {
		raw = raw[:idx]
	}
	if len(raw) > 100 {
		raw = raw[:100] + "…"
	}
	return raw
}

func debugStLen(b []byte) string { return fmt.Sprintf("%d bytes", len(b)) }

// ─── Secciones del reporte ─────────────────────────────────────────────────

func debugStStanzaSection(ctx *core.Context, original *debugStCached) string {
	var b strings.Builder
	b.WriteString("*Stanza (mensaje actual)*\n")
	if ctx == nil || ctx.Message == nil {
		b.WriteString("• sin Info disponible\n")
		return b.String()
	}
	info := ctx.Message.Info
	if original != nil {
		fmt.Fprintf(&b, "• Tipo: %s (original cacheado)\n", debugStStr(original.Type))
		fmt.Fprintf(&b, "• MediaType: %s (original cacheado)\n", debugStStr(original.MediaType))
	} else {
		fmt.Fprintf(&b, "• Tipo: %s\n", debugStStr(info.Type))
		fmt.Fprintf(&b, "• MediaType: %s\n", debugStStr(info.MediaType))
	}
	fmt.Fprintf(&b, "• Chat: %s\n", debugStStr(info.Chat.String()))
	fmt.Fprintf(&b, "• De: %s\n", debugStStr(info.Sender.String()))
	fmt.Fprintf(&b, "• ID: %s\n", debugStStr(string(info.ID)))
	if info.PushName != "" {
		fmt.Fprintf(&b, "• PushName: %s\n", info.PushName)
	}
	if !info.Timestamp.IsZero() {
		fmt.Fprintf(&b, "• Timestamp: %s\n", info.Timestamp.Format("2006-01-02 15:04:05 -07:00"))
	}
	if ci := ctx.ContextInfo(); ci != nil {
		fmt.Fprintf(&b, "• Cita stanzaID: %s\n", debugStStr(ci.GetStanzaID()))
		fmt.Fprintf(&b, "• Cita participante: %s\n", debugStStr(ci.GetParticipant()))
	}
	return b.String()
}

func debugStStickerSection(st *waE2E.StickerMessage) string {
	var b strings.Builder
	b.WriteString("*StickerMessage*\n")
	fmt.Fprintf(&b, "• Mimetype: %s\n", debugStStr(st.GetMimetype()))
	fmt.Fprintf(&b, "• Tamaño: %sx%s\n", debugStUint(st.GetWidth()), debugStUint(st.GetHeight()))
	fmt.Fprintf(&b, "• FileLength: %d\n", st.GetFileLength())
	fmt.Fprintf(&b, "• IsAnimated: %s\n", debugStBool(st.GetIsAnimated()))
	fmt.Fprintf(&b, "• IsLottie: %s\n", debugStBool(st.GetIsLottie()))
	fmt.Fprintf(&b, "• IsAvatar: %s\n", debugStBool(st.GetIsAvatar()))
	fmt.Fprintf(&b, "• IsAiSticker: %s\n", debugStBool(st.GetIsAiSticker()))
	fmt.Fprintf(&b, "• Emojis: %s\n", debugStStr(st.GetEmojis()))
	fmt.Fprintf(&b, "• AccessibilityLabel: %s\n", debugStStr(st.GetAccessibilityLabel()))
	fmt.Fprintf(&b, "• PngThumbnail: %s\n", debugStLen(st.GetPngThumbnail()))
	fmt.Fprintf(&b, "• MediaKey: %s\n", debugStSecret(st.GetMediaKey()))
	fmt.Fprintf(&b, "• DirectPath: %s\n", debugStPath(st.GetDirectPath()))
	fmt.Fprintf(&b, "• FileSHA256: %s\n", debugStSecret(st.GetFileSHA256()))
	fmt.Fprintf(&b, "• FileEncSHA256: %s\n", debugStSecret(st.GetFileEncSHA256()))
	return b.String()
}

func debugStAudioSection(a *waE2E.AudioMessage) string {
	if a == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n*AudioMessage anidado (campo 26)*\n")
	fmt.Fprintf(&b, "• Mimetype: %s\n", debugStStr(a.GetMimetype()))
	fmt.Fprintf(&b, "• PTT: %s\n", debugStBool(a.GetPTT()))
	fmt.Fprintf(&b, "• Seconds: %d\n", a.GetSeconds())
	fmt.Fprintf(&b, "• FileLength: %d\n", a.GetFileLength())
	fmt.Fprintf(&b, "• Waveform: %s\n", debugStLen(a.GetWaveform()))
	fmt.Fprintf(&b, "• StreamingSidecar: %s\n", debugStLen(a.GetStreamingSidecar()))
	fmt.Fprintf(&b, "• ViewOnce: %s\n", debugStBool(a.GetViewOnce()))
	fmt.Fprintf(&b, "• DirectPath: %s\n", debugStPath(a.GetDirectPath()))
	fmt.Fprintf(&b, "• MediaKey: %s\n", debugStSecret(a.GetMediaKey()))
	fmt.Fprintf(&b, "• FileSHA256: %s\n", debugStSecret(a.GetFileSHA256()))
	fmt.Fprintf(&b, "• FileEncSHA256: %s\n", debugStSecret(a.GetFileEncSHA256()))
	return b.String()
}

// ─── EXIF del WebP ─────────────────────────────────────────────────────────

// debugStWebPChunk extrae el payload de un chunk RIFF concreto.
func debugStWebPChunk(data []byte, fourcc string) ([]byte, bool) {
	if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return nil, false
	}
	pos := 12
	for pos+8 <= len(data) {
		size := int(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
		start := pos + 8
		if size < 0 || start+size > len(data) {
			break
		}
		if string(data[pos:pos+4]) == fourcc {
			return data[start : start+size], true
		}
		pos = start + size
		if pos%2 == 1 {
			pos++
		}
	}
	return nil, false
}

func debugStRead16(b []byte, little bool) uint16 {
	if len(b) < 2 {
		return 0
	}
	if little {
		return binary.LittleEndian.Uint16(b)
	}
	return binary.BigEndian.Uint16(b)
}

func debugStRead32(b []byte, little bool) uint32 {
	if len(b) < 4 {
		return 0
	}
	if little {
		return binary.LittleEndian.Uint32(b)
	}
	return binary.BigEndian.Uint32(b)
}

// debugStEXIFUserComment localiza el tag UserComment (0x9286) en el TIFF.
func debugStEXIFUserComment(payload []byte) ([]byte, bool) {
	if len(payload) > 6 && string(payload[:6]) == "Exif\x00\x00" {
		payload = payload[6:]
	}
	if len(payload) < 8 {
		return nil, false
	}
	var little bool
	switch {
	case payload[0] == 'I' && payload[1] == 'I':
		little = true
	case payload[0] == 'M' && payload[1] == 'M':
		little = false
	default:
		return nil, false
	}
	if debugStRead16(payload[2:], little) != 42 {
		return nil, false
	}
	ifd := int(debugStRead32(payload[4:], little))
	if ifd < 0 || ifd+2 > len(payload) {
		return nil, false
	}
	n := int(debugStRead16(payload[ifd:], little))
	for i := 0; i < n; i++ {
		off := ifd + 2 + i*12
		if off+12 > len(payload) {
			break
		}
		tag := debugStRead16(payload[off:], little)
		if tag != 0x9286 {
			continue
		}
		count := int(debugStRead32(payload[off+4:], little))
		valueOff := int(debugStRead32(payload[off+8:], little))
		if count <= 0 {
			return nil, false
		}
		if count <= 4 {
			if off+8+count > len(payload) {
				return nil, false
			}
			return payload[off+8 : off+8+count], true
		}
		if valueOff < 0 || valueOff+count > len(payload) {
			return nil, false
		}
		return payload[valueOff : valueOff+count], true
	}
	return nil, false
}

func debugStTrimPadding(raw []byte) []byte {
	raw = bytes.TrimRight(raw, "\x00")
	raw = bytes.TrimSpace(raw)
	if len(raw) >= 8 {
		prefijo := string(raw[:8])
		if prefijo == "ASCII\x00\x00\x00" || prefijo == "UNICODE\x00" {
			raw = bytes.TrimSpace(bytes.TrimRight(raw[8:], "\x00"))
		}
	}
	return raw
}

// debugStEXIFJSON devuelve el JSON del EXIF si existe.
func debugStEXIFJSON(webp []byte) (string, int, bool) {
	payload, ok := debugStWebPChunk(webp, "EXIF")
	if !ok {
		return "", 0, false
	}
	if comment, ok := debugStEXIFUserComment(payload); ok {
		if limpio := debugStTrimPadding(comment); len(limpio) > 0 {
			if json.Valid(limpio) {
				return string(limpio), len(payload), true
			}
			if s := debugStJSONEn(payload); s != "" {
				return s, len(payload), true
			}
			return string(limpio), len(payload), true
		}
	}
	return debugStJSONEn(payload), len(payload), true
}

// debugStJSONEn busca el primer objeto JSON válido dentro de un bloque binario.
func debugStJSONEn(raw []byte) string {
	inicio := bytes.IndexByte(raw, '{')
	if inicio < 0 {
		return ""
	}
	for fin := len(raw); fin > inicio; fin-- {
		if raw[fin-1] != '}' {
			continue
		}
		candidato := raw[inicio:fin]
		if json.Valid(candidato) {
			return string(candidato)
		}
	}
	return ""
}

// ─── ffprobe del audio ─────────────────────────────────────────────────────

type debugStProbeResult struct {
	Codec      string
	Channels   int
	SampleRate string
	Duration   float64
}

func debugStParseProbe(raw []byte) (debugStProbeResult, error) {
	var parsed struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			CodecType  string `json:"codec_type"`
			CodecName  string `json:"codec_name"`
			Channels   int    `json:"channels"`
			SampleRate string `json:"sample_rate"`
			Duration   string `json:"duration"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return debugStProbeResult{}, err
	}
	out := debugStProbeResult{}
	encontrado := false
	for _, s := range parsed.Streams {
		if s.CodecType != "audio" {
			continue
		}
		encontrado = true
		out.Codec = s.CodecName
		out.Channels = s.Channels
		out.SampleRate = s.SampleRate
		if v, err := strconv.ParseFloat(s.Duration, 64); err == nil {
			out.Duration = v
		}
		break
	}
	if !encontrado {
		return out, fmt.Errorf("sin pista de audio en ffprobe")
	}
	if out.Duration == 0 {
		if v, err := strconv.ParseFloat(parsed.Format.Duration, 64); err == nil {
			out.Duration = v
		}
	}
	return out, nil
}

func debugStProbeAudio(data []byte) (debugStProbeResult, error) {
	dir, err := os.MkdirTemp("", "debugst_")
	if err != nil {
		return debugStProbeResult{}, err
	}
	defer os.RemoveAll(dir)

	ext := "bin"
	if bytes.HasPrefix(data, []byte("OggS")) {
		ext = "ogg"
	}
	path := filepath.Join(dir, "audio."+ext)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return debugStProbeResult{}, err
	}
	defer os.Remove(path)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	raw, err := debugStExec(ctx, "ffprobe", "-v", "error", "-print_format", "json",
		"-show_format", "-show_streams", path).Output()
	if err != nil {
		return debugStProbeResult{}, err
	}
	return debugStParseProbe(raw)
}

func debugStProbeText(r debugStProbeResult) string {
	sr := r.SampleRate
	if sr == "" {
		sr = "?"
	}
	return fmt.Sprintf("codec=%s, canales=%d, sample_rate=%s Hz, duración=%.2f s",
		debugStStr(r.Codec), r.Channels, sr, r.Duration)
}

// ─── Descarga real (modo full) ─────────────────────────────────────────────

func debugStFullSection(ctx *core.Context, st *waE2E.StickerMessage, audio *waE2E.AudioMessage) string {
	var b strings.Builder
	b.WriteString("\n*Descarga real + EXIF + ffprobe*\n")

	webp, err := debugStDownloadFn(ctx, st)
	if err != nil {
		fmt.Fprintf(&b, "• WebP: ❌ error al descargar: %v\n", err)
	} else {
		fmt.Fprintf(&b, "• WebP: ✅ descifrado OK (%d bytes)\n", len(webp))
		exifJSON, exifLen, ok := debugStEXIFJSON(webp)
		if !ok {
			b.WriteString("• EXIF: no encontrado en el WebP\n")
		} else {
			fmt.Fprintf(&b, "• EXIF: encontrado (%d bytes)\n", exifLen)
			if exifJSON != "" {
				const maxWhatsApp = 1500
				if len(exifJSON) > maxWhatsApp {
					fmt.Fprintf(&b, "```json\n%s\n```\n_(recortado; JSON completo en consola)_\n", exifJSON[:maxWhatsApp])
				} else {
					fmt.Fprintf(&b, "```json\n%s\n```\n", exifJSON)
				}
				debugStLog("🔬 [debug-st] EXIF JSON", "json", exifJSON)
			} else {
				b.WriteString("• EXIF: sin JSON legible\n")
			}
		}
	}

	if audio == nil {
		b.WriteString("• Audio: no tiene AudioMessage anidado\n")
		return b.String()
	}
	audioData, err := debugStDownloadFn(ctx, audio)
	if err != nil {
		fmt.Fprintf(&b, "• Audio: ❌ error al descargar: %v\n", err)
		return b.String()
	}
	fmt.Fprintf(&b, "• Audio: ✅ descifrado OK (%d bytes)\n", len(audioData))
	probe, err := debugStProbeFn(audioData)
	if err != nil {
		fmt.Fprintf(&b, "• ffprobe: ⚠️ %v\n", err)
		return b.String()
	}
	fmt.Fprintf(&b, "• ffprobe: %s\n", debugStProbeText(probe))
	return b.String()
}

// ─── Handler ───────────────────────────────────────────────────────────────

func debugStWantsFull(args []string) bool {
	for _, a := range args {
		if strings.EqualFold(strings.TrimSpace(a), "full") {
			return true
		}
	}
	return false
}

func (*debugStAddon) Handle(ctx *core.Context) error {
	if ctx == nil || ctx.Message == nil {
		return nil
	}
	quoted := ctx.QuotedMessage()
	stanzaID := ""
	if ci := ctx.ContextInfo(); ci != nil {
		stanzaID = ci.GetStanzaID()
	}

	// Prioridad: protobuf original capturado en vivo (la cita puede recortar
	// el AudioStickerMessage 134 y su AudioMessage 26).
	sourceMsg := quoted
	fuente := "fallback QuotedMessage (puede venir recortado; reenvía el sticker primero)"
	var original *debugStCached
	if cached, ok := debugStCacheGet(stanzaID); ok && cached.Message != nil {
		sourceMsg = cached.Message
		original = cached
		fuente = "caché original en vivo"
	}
	if sourceMsg == nil {
		return ctx.Reply("⚠️ Cita un sticker con `.debug-st` para inspeccionarlo.")
	}

	w := debugStInspectWrappers(sourceMsg)
	if w.Sticker == nil {
		return ctx.Reply("⚠️ El mensaje citado no contiene un StickerMessage.")
	}

	// Volcado completo del protobuf en consola/log.
	if raw, err := (protojson.MarshalOptions{Multiline: true, Indent: "  "}).Marshal(sourceMsg); err == nil {
		debugStLog("🔬 [debug-st] Protobuf JSON", "json", string(raw))
	} else {
		slog.Warn("🔬 [debug-st] no se pudo serializar el protobuf", "error", err)
	}

	var b strings.Builder
	b.WriteString("🔬 *DEBUG STICKER*\n\n")
	fmt.Fprintf(&b, "*Fuente:* %s\n", fuente)
	fmt.Fprintf(&b, "*Envoltorio:* %s\n\n", debugStChainText(w.Chain))
	b.WriteString(debugStStanzaSection(ctx, original))
	b.WriteString("\n")
	b.WriteString(debugStStickerSection(w.Sticker))
	audio := w.Sticker.GetAudioMessage()
	b.WriteString(debugStAudioSection(audio))
	if debugStWantsFull(ctx.Args) {
		b.WriteString(debugStFullSection(ctx, w.Sticker, audio))
	} else {
		b.WriteString("\n💡 Usa `.debug-st full` para descargar, leer EXIF y correr ffprobe.")
	}

	if err := ctx.Reply(b.String()); err != nil {
		return err
	}
	return ctx.React("✅")
}
