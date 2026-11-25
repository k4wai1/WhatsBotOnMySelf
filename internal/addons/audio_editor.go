package addons

import (
	"context"
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

	"whatsbot-go/internal/core"
	"whatsbot-go/internal/store"
)

func init() {
	core.Register(AudioEditor{})
	core.RegisterBackground("au", auInit)
}

const (
	auUnirDuracion  = 60 * time.Second
	auUnirMaxAudios = 25
	auUnirMaxCache  = 400
	auMaxBytes      = 15 * 1024 * 1024
)

// Inyectables para pruebas.
var (
	auExecCommand = exec.CommandContext
	auDownload    = auDownloadDefault
	auMkdirTemp   = os.MkdirTemp
	auRemoveAll   = os.RemoveAll
)

type auCachedMedia struct {
	Msg     *events.Message
	Content *waE2E.Message
}

type auSession struct {
	Key          string
	Chat         string
	Owner        string
	Vn           bool
	Invoker      *events.Message
	Client       *whatsmeow.Client
	Items        []auCachedMedia
	Usados       map[string]bool
	Done         bool
	AvisadoLleno bool
	AvisadoViejo bool
	Timer        *time.Timer
}

var (
	auMu         sync.RWMutex
	auMediaCache = make(map[string]auCachedMedia)
	auCacheOrder []string
	auSessions   = make(map[string]*auSession)
)

// AudioEditor porta addons/audio_editor.js: .au y su modo interactivo .au unir.
type AudioEditor struct{}

// Commands cumple core.Addon.
func (AudioEditor) Commands() []string { return []string{"au"} }

// Handle replica el handler de .au.
func (AudioEditor) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}
	chat := ctx.ChatJID().String()
	owner := ctx.SenderJID().String()
	if owner == "" {
		owner = chat
	}

	subCmdPre := ""
	if len(ctx.Args) > 0 {
		subCmdPre = strings.ToLower(ctx.Args[0])
	}

	// ─── .au unir ───────────────────────────────────────────────────────
	if subCmdPre == "unir" {
		asVoiceNote := false
		for _, a := range ctx.Args[1:] {
			if strings.ToLower(a) == "vn" {
				asVoiceNote = true
			}
		}
		if len(ctx.Args) > 1 {
			second := strings.ToLower(ctx.Args[1])
			if second == "cancel" || second == "cancelar" {
				return auCancelar(ctx, chat, owner)
			}
		}
		return auIniciar(ctx, chat, owner, asVoiceNote)
	}

	quoted := ctx.QuotedMessage()
	if quoted == nil || (quoted.GetAudioMessage() == nil && quoted.GetVideoMessage() == nil) {
		if err := ctx.React("🎧"); err != nil {
			return err
		}
		return ctx.SendQuoted(core.TextMessage("❌ Debes responder a un mensaje de audio o video."), ctx.Message)
	}

	mediaType := "audio"
	if quoted.GetVideoMessage() != nil {
		mediaType = "video"
	}

	tmpDir, err := auMkdirTemp("", "au_")
	if err != nil {
		if reactErr := ctx.React("❌"); reactErr != nil {
			return reactErr
		}
		return ctx.SendQuoted(core.TextMessage("❌ Error procesando medio: "+err.Error()), ctx.Message)
	}
	defer auRemoveAll(tmpDir)

	if err := ctx.React("📥"); err != nil {
		return err
	}
	data, err := auDownload(ctx, quoted, mediaType)
	if err != nil {
		return auErrorMedio(ctx, err)
	}
	if len(data) == 0 {
		return auErrorMedio(ctx, fmt.Errorf("El buffer devuelto está vacío. El medio original podría estar expirado o inaccesible."))
	}

	rawPath := filepath.Join(tmpDir, "raw.tmp")
	if err := os.WriteFile(rawPath, data, 0o600); err != nil {
		return auErrorMedio(ctx, err)
	}
	processedPath := filepath.Join(tmpDir, "processed.ogg")

	subCmd := subCmdPre
	switch {
	case subCmd == "vn" || subCmd == "audio":
		asVoiceNote := subCmd == "vn"
		if err := ctx.React("🔄"); err != nil {
			return err
		}
		if err := auNormalize(rawPath, processedPath, false, 0, false, 0); err != nil {
			return auErrorMedio(ctx, err)
		}
		buf, err := os.ReadFile(processedPath)
		if err != nil {
			return auErrorMedio(ctx, err)
		}
		if err := ctx.SendAudio(buf, "audio/ogg; codecs=opus", asVoiceNote, 0); err != nil {
			return err
		}
		return ctx.React("✅")

	case subCmd == "r":
		if len(ctx.Args) < 3 {
			if err := ctx.SendQuoted(core.TextMessage("❌ Uso: .au r <inicio> <fin> [vn]\nEjemplo: .au r 0 5.5 vn"), ctx.Message); err != nil {
				return err
			}
			return ctx.React("⚠️")
		}
		startSec, okStart := auParseTime(ctx.Args[1])
		endSec, okEnd := auParseTime(ctx.Args[2])
		asVoiceNote := len(ctx.Args) > 3 && strings.ToLower(ctx.Args[3]) == "vn"
		if !okStart || !okEnd || startSec < 0 || endSec <= startSec {
			if err := ctx.SendQuoted(core.TextMessage("❌ Tiempos inválidos. Asegúrate de que inicio < fin y sean números positivos."), ctx.Message); err != nil {
				return err
			}
			return ctx.React("⚠️")
		}
		if err := ctx.React("✂️"); err != nil {
			return err
		}
		if err := auNormalize(rawPath, processedPath, true, startSec, true, endSec-startSec); err != nil {
			return auErrorMedio(ctx, err)
		}
		buf, err := os.ReadFile(processedPath)
		if err != nil {
			return auErrorMedio(ctx, err)
		}
		if err := ctx.SendAudio(buf, "audio/ogg; codecs=opus", asVoiceNote, 0); err != nil {
			return err
		}
		return ctx.React("✅")
	}

	if err := ctx.SendQuoted(core.TextMessage(auHelpText), ctx.Message); err != nil {
		return err
	}
	return ctx.React("ℹ️")
}

var auHelpText = "🎵 *Editor de audio AU*\nComandos:\n- `.au vn` → convierte el audio/video a nota de voz.\n- `.au audio` → convierte a audio normal.\n- `.au r <inicio> <fin> [vn]` → recorta del segundo A al B. Agrega \"vn\" al final para que sea nota de voz.\n- `.au unir [vn]` → junta varios audios en uno solo. Manda (o reacciona a) los audios durante 1 minuto; al final los envía unidos. Con \"vn\" el resultado va como nota de voz.\n- `.au unir cancel` → cancela la unión en curso.\nEjemplo: `.au r 2,5 8.3 vn`\n\n💡 Funciona con audios y videos (extrae el audio del video automáticamente)."

func auErrorMedio(ctx *core.Context, err error) error {
	if reactErr := ctx.React("❌"); reactErr != nil {
		return reactErr
	}
	return ctx.SendQuoted(core.TextMessage("❌ Error procesando medio: "+err.Error()), ctx.Message)
}

// ─── Modo unir ──────────────────────────────────────────────────────────────

func auIniciar(ctx *core.Context, chat, owner string, vn bool) error {
	key := chat + "|" + owner
	var hadPrev bool

	auMu.Lock()
	if prev, ok := auSessions[key]; ok {
		hadPrev = true
		if prev.Timer != nil {
			prev.Timer.Stop()
		}
	}
	session := &auSession{
		Key:     key,
		Chat:    chat,
		Owner:   owner,
		Vn:      vn,
		Invoker: ctx.Message,
		Client:  ctx.Client,
		Usados:  make(map[string]bool),
	}
	auSessions[key] = session
	auMu.Unlock()

	session.Timer = time.AfterFunc(auUnirDuracion, func() {
		auFinalizar(session)
	})

	if err := ctx.React("⏱️"); err != nil {
		return err
	}

	text := "🔗 *Modo unir activado* (1 minuto)\n\n" +
		"Manda los audios que quieras unir, *o reacciona a audios/videos ya enviados en este chat* " +
		"(la reacción solo funciona con medios que el bot haya visto desde que está encendido; " +
		"si uno viejo no lo reconoce, reenvíalo).\n\n" +
		"Cada medio recogido se marca con ✓. Al minuto los envío unidos en *un solo audio*. "
	if vn {
		text += "El resultado irá como *nota de voz* 🎙️.\n"
	}
	text += "Progreso: "
	if hadPrev {
		text += "(reinicié una unión en curso) "
	}
	text += "Cancelar con `.au unir cancel`"
	return ctx.SendQuoted(core.TextMessage(text), ctx.Message)
}

func auCancelar(ctx *core.Context, chat, owner string) error {
	key := chat + "|" + owner
	auMu.Lock()
	session, ok := auSessions[key]
	if !ok || session.Done {
		auMu.Unlock()
		if err := ctx.SendQuoted(core.TextMessage("ℹ️ No hay ninguna unión de audios activa en este chat."), ctx.Message); err != nil {
			return err
		}
		return ctx.React("🤷")
	}
	session.Done = true
	if session.Timer != nil {
		session.Timer.Stop()
	}
	delete(auSessions, key)
	auMu.Unlock()

	if err := ctx.React("🛑"); err != nil {
		return err
	}
	return ctx.SendQuoted(core.TextMessage(fmt.Sprintf("🛑 Unión cancelada. Los %d audios recogidos no se enviarán.", len(session.Items))), ctx.Message)
}

// auUnwrapMedia desenrolla view-once/efímeros para hallar el audio/video real.
func auUnwrapMedia(msg *waE2E.Message) *waE2E.Message {
	m := msg
	for i := 0; i < 5 && m != nil; i++ {
		if m.GetAudioMessage() != nil || m.GetVideoMessage() != nil {
			return m
		}
		switch {
		case m.GetViewOnceMessage() != nil:
			m = m.GetViewOnceMessage().GetMessage()
		case m.GetViewOnceMessageV2() != nil:
			m = m.GetViewOnceMessageV2().GetMessage()
		case m.GetViewOnceMessageV2Extension() != nil:
			m = m.GetViewOnceMessageV2Extension().GetMessage()
		case m.GetEphemeralMessage() != nil:
			m = m.GetEphemeralMessage().GetMessage()
		default:
			return nil
		}
	}
	return nil
}

func auIsUnirMedia(content *waE2E.Message) bool {
	return content != nil && (content.GetAudioMessage() != nil || content.GetVideoMessage() != nil)
}

func auMediaType(content *waE2E.Message) string {
	if content.GetAudioMessage() != nil {
		return "audio"
	}
	return "video"
}

func auEsDescargable(content *waE2E.Message, mediaType string) bool {
	if content == nil {
		return false
	}
	if mediaType == "video" {
		node := content.GetVideoMessage()
		return node != nil && (node.GetURL() != "" || node.GetDirectPath() != "")
	}
	node := content.GetAudioMessage()
	return node != nil && (node.GetURL() != "" || node.GetDirectPath() != "")
}

func auCacheKey(chat, id string) string { return chat + ":" + id }

func auRecordMediaCache(msg *events.Message, content *waE2E.Message) {
	if !auIsUnirMedia(content) || msg == nil {
		return
	}
	chat := msg.Info.Chat.String()
	id := msg.Info.ID
	if chat == "" || id == "" {
		return
	}
	key := auCacheKey(chat, id)
	auMu.Lock()
	defer auMu.Unlock()
	if _, exists := auMediaCache[key]; !exists {
		if len(auCacheOrder) >= auUnirMaxCache {
			oldest := auCacheOrder[0]
			auCacheOrder = auCacheOrder[1:]
			delete(auMediaCache, oldest)
		}
		auCacheOrder = append(auCacheOrder, key)
	}
	auMediaCache[key] = auCachedMedia{Msg: msg, Content: content}
}

func auAvisoTope(session *auSession) {
	auMu.Lock()
	if session.AvisadoLleno {
		auMu.Unlock()
		return
	}
	session.AvisadoLleno = true
	auMu.Unlock()
	ctx := &core.Context{Client: session.Client, Message: session.Invoker}
	_ = ctx.SendQuoted(core.TextMessage(fmt.Sprintf("⚠️ Tope de %d audios para la unión; lo que llegue de más se ignorará.", auUnirMaxAudios)), session.Invoker)
}

func auHandleMedia(client *whatsmeow.Client, msg *events.Message) {
	content := auUnwrapMedia(msg.Message)
	if content == nil {
		return
	}
	auRecordMediaCache(msg, content)

	chat := msg.Info.Chat.String()
	owner := msg.Info.Sender.String()
	if owner == "" {
		owner = chat
	}
	key := chat + "|" + owner

	auMu.Lock()
	session := auSessions[key]
	auMu.Unlock()
	if session == nil || session.Done {
		return
	}

	id := msg.Info.ID
	mediaType := auMediaType(content)

	auMu.Lock()
	if session.Usados[id] || session.Done {
		auMu.Unlock()
		return
	}
	auMu.Unlock()

	if !auEsDescargable(content, mediaType) {
		ctx := &core.Context{Client: client, Message: msg}
		_ = ctx.React("🙅")
		_ = ctx.SendQuoted(core.TextMessage("🚫 Ese audio lo envié yo y WhatsApp no me deja volver a descargarlo. Reenvíalo tú (o desde otro chat) y lo agrego igual."), msg)
		return
	}

	auMu.Lock()
	if len(session.Items) >= auUnirMaxAudios {
		auMu.Unlock()
		auAvisoTope(session)
		return
	}
	if session.Usados[id] || session.Done {
		auMu.Unlock()
		return
	}
	session.Usados[id] = true
	session.Items = append(session.Items, auCachedMedia{Msg: msg, Content: content})
	auMu.Unlock()

	ctx := &core.Context{Client: client, Message: msg}
	_ = ctx.React("✅")
}

func auHandleReaction(client *whatsmeow.Client, msg *events.Message, reaction *waE2E.ReactionMessage) {
	chat := msg.Info.Chat.String()
	reactor := msg.Info.Sender.String()
	if reactor == "" {
		reactor = chat
	}
	key := chat + "|" + reactor

	auMu.Lock()
	session := auSessions[key]
	auMu.Unlock()
	if session == nil || session.Done {
		return
	}

	targetID := reaction.GetKey().GetID()
	if targetID == "" {
		return
	}
	targetChat := reaction.GetKey().GetRemoteJID()
	if targetChat == "" {
		targetChat = chat
	}

	auMu.RLock()
	cached, ok := auMediaCache[auCacheKey(targetChat, targetID)]
	if !ok {
		cached, ok = auMediaCache[auCacheKey(chat, targetID)]
	}
	auMu.RUnlock()

	if !ok || !auIsUnirMedia(cached.Content) {
		auMu.Lock()
		warn := !session.AvisadoViejo
		session.AvisadoViejo = true
		auMu.Unlock()
		if warn {
			ctx := &core.Context{Client: client, Message: msg}
			_ = ctx.SendQuoted(core.TextMessage("ℹ️ No tengo en caché ese audio reaccionado (muy antiguo o el bot se reinició). Reenvíalo durante el minuto y lo agrego igual."), msg)
		}
		return
	}

	mediaType := auMediaType(cached.Content)
	if !auEsDescargable(cached.Content, mediaType) {
		ctx := &core.Context{Client: client, Message: msg}
		_ = ctx.React("🙅")
		_ = ctx.SendQuoted(core.TextMessage("🚫 Ese audio lo envié yo y WhatsApp no me deja volver a descargarlo. Reenvíalo tú (o desde otro chat) durante el minuto y lo agrego igual."), msg)
		return
	}

	id := cached.Msg.Info.ID
	auMu.Lock()
	if session.Usados[id] || session.Done {
		auMu.Unlock()
		return
	}
	if len(session.Items) >= auUnirMaxAudios {
		auMu.Unlock()
		auAvisoTope(session)
		return
	}
	session.Usados[id] = true
	session.Items = append(session.Items, cached)
	auMu.Unlock()

	ctx := &core.Context{Client: client, Message: cached.Msg}
	_ = ctx.React("✅")
}

// ─── Finalización ───────────────────────────────────────────────────────────

func auFinalizar(session *auSession) {
	auMu.Lock()
	if session.Done {
		auMu.Unlock()
		return
	}
	session.Done = true
	if session.Timer != nil {
		session.Timer.Stop()
	}
	delete(auSessions, session.Key)
	items := append([]auCachedMedia(nil), session.Items...)
	auMu.Unlock()

	ctx := &core.Context{Client: session.Client, Message: session.Invoker}

	if len(items) == 0 {
		_ = ctx.SendQuoted(core.TextMessage("🫥 No se recogió ningún audio; el modo unir termina sin resultado."), session.Invoker)
		_ = ctx.React("🫥")
		return
	}

	tmpDir, err := auMkdirTemp("", "au_")
	if err != nil {
		auFinalizarError(ctx, session, err)
		return
	}
	defer auRemoveAll(tmpDir)

	inputs := make([]string, 0, len(items))
	omitidos := 0
	for i, item := range items {
		mediaType := auMediaType(item.Content)
		data, err := auDownload(ctx, item.Content, mediaType)
		if err != nil || len(data) == 0 {
			slog.Warn("⚠️ .au unir: medio omitido", "indice", i, "error", err)
			omitidos++
			continue
		}
		ext := "ogg"
		if mediaType == "video" {
			ext = "mp4"
		}
		rawPath := filepath.Join(tmpDir, fmt.Sprintf("src_%d.%s", len(inputs), ext))
		if err := os.WriteFile(rawPath, data, 0o600); err != nil {
			omitidos++
			continue
		}
		inputs = append(inputs, rawPath)
	}

	if len(inputs) == 0 {
		causa := "todos los audios recogidos están expirados o no descargables"
		if len(items) == 1 && !auEsDescargable(items[0].Content, auMediaType(items[0].Content)) {
			causa = "el audio no es re-descargable (fue enviado por el bot: reenvíalo desde otro chat y reacciona a esa copia)"
		}
		auFinalizarError(ctx, session, fmt.Errorf("%s", causa))
		return
	}

	_ = ctx.React("🔗")

	mergedPath := filepath.Join(tmpDir, "merged.ogg")
	if err := auMerge(inputs, mergedPath); err != nil {
		auFinalizarError(ctx, session, err)
		return
	}
	merged, err := os.ReadFile(mergedPath)
	if err != nil {
		auFinalizarError(ctx, session, err)
		return
	}
	if len(merged) == 0 {
		auFinalizarError(ctx, session, fmt.Errorf("la fusión salió vacía"))
		return
	}
	if len(merged) > auMaxBytes {
		auFinalizarError(ctx, session, fmt.Errorf("el audio unido pesa %.1f MB y WhatsApp lo rechaza; une menos o más cortos", float64(len(merged))/(1024*1024)))
		return
	}

	if err := ctx.SendAudio(merged, "audio/ogg; codecs=opus", session.Vn, 0); err != nil {
		auFinalizarError(ctx, session, err)
		return
	}
	_ = ctx.React("✅")
	text := fmt.Sprintf("🔗 Audios unidos: %d", len(inputs))
	if omitidos > 0 {
		text += fmt.Sprintf(" (%d omitidos: expirados o no descargables)", omitidos)
	}
	_ = ctx.SendQuoted(core.TextMessage(text), session.Invoker)
}

func auFinalizarError(ctx *core.Context, session *auSession, err error) {
	slog.Error("Error en AU/unir (fusión)", "error", err)
	_ = ctx.React("❌")
	_ = ctx.SendQuoted(core.TextMessage("❌ Error al unir audios: "+err.Error()), session.Invoker)
}

// ─── ffmpeg ─────────────────────────────────────────────────────────────────

func auNormalizeArgs(input, output string, hasStart bool, start float64, hasDur bool, duration float64) []string {
	args := []string{"-y", "-i", input, "-vn", "-ar", "48000"}
	if hasStart {
		args = append(args, "-ss", strconv.FormatFloat(start, 'f', -1, 64))
	}
	if hasDur {
		args = append(args, "-t", strconv.FormatFloat(duration, 'f', -1, 64))
	}
	args = append(args, "-c:a", "libopus", "-f", "ogg", output)
	return args
}

func auNormalize(input, output string, hasStart bool, start float64, hasDur bool, duration float64) error {
	_, err := auExecCommand(context.Background(), "ffmpeg", auNormalizeArgs(input, output, hasStart, start, hasDur, duration)...).CombinedOutput()
	return err
}

// auMergeArgs replica el complexFilter de mergeAudios().
func auMergeArgs(inputs []string, output string) []string {
	n := len(inputs)
	chains := make([]string, 0, n)
	labels := make([]string, 0, n)
	for i := range inputs {
		chains = append(chains, fmt.Sprintf("[%d:a]aformat=sample_rates=48000:channel_layouts=mono[a%d]", i, i))
		labels = append(labels, fmt.Sprintf("[a%d]", i))
	}
	graph := strings.Join(chains, ";") + ";" + strings.Join(labels, "") + fmt.Sprintf("concat=n=%d:v=0:a=1[out]", n)

	args := []string{"-y"}
	for _, in := range inputs {
		args = append(args, "-i", in)
	}
	args = append(args,
		"-filter_complex", graph,
		"-map", "[out]",
		"-vn",
		"-c:a", "libopus",
		"-ar", "48000",
		"-b:a", "64k",
		"-f", "ogg",
		output,
	)
	return args
}

func auMerge(inputs []string, output string) error {
	if len(inputs) == 0 {
		return nil
	}
	out, err := auExecCommand(context.Background(), "ffmpeg", auMergeArgs(inputs, output)...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("%s", msg)
		}
	}
	return err
}

func auDownloadDefault(ctx *core.Context, content *waE2E.Message, mediaType string) ([]byte, error) {
	return ctx.Download(content)
}

func auParseTime(s string) (float64, bool) {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", ".")
	end := 0
	seenDot := false
	for end < len(s) {
		c := s[end]
		if c >= '0' && c <= '9' {
			end++
			continue
		}
		if c == '.' && !seenDot {
			seenDot = true
			end++
			continue
		}
		if (c == '+' || c == '-') && end == 0 {
			end++
			continue
		}
		break
	}
	if end == 0 {
		return 0, false
	}
	v, err := strconv.ParseFloat(s[:end], 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// ─── Listener en segundo plano ──────────────────────────────────────────────

func auInit(client *whatsmeow.Client, st *store.Store) error {
	auMu.Lock()
	for _, s := range auSessions {
		if s.Timer != nil {
			s.Timer.Stop()
		}
	}
	auSessions = make(map[string]*auSession)
	auMu.Unlock()

	if client == nil {
		return nil
	}
	client.AddEventHandler(func(evt any) {
		msg, ok := evt.(*events.Message)
		if !ok || msg == nil || msg.Message == nil {
			return
		}
		if reaction := msg.Message.GetReactionMessage(); reaction != nil {
			auHandleReaction(client, msg, reaction)
			return
		}
		auHandleMedia(client, msg)
	})
	return nil
}

func auResetState() {
	auMu.Lock()
	for _, s := range auSessions {
		if s.Timer != nil {
			s.Timer.Stop()
		}
	}
	auSessions = make(map[string]*auSession)
	auMediaCache = make(map[string]auCachedMedia)
	auCacheOrder = nil
	auMu.Unlock()
}
