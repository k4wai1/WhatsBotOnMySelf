package addons

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"whatsbot-go/internal/core"
)

func init() {
	core.Register(Voz{})
}

const (
	vozDefaultVoice = "es-VE-SebastianNeural"
	vozMaxText      = 2000
)

var vozVoiceRE = regexp.MustCompile(`^[a-z]{2,3}-[A-Z]{2}-[A-Za-z]+Neural$`)

var vozVoices = [][2]string{
	{"es-VE-SebastianNeural", "🇻🇪 Sebastián (por defecto)"},
	{"es-VE-PaolaNeural", "🇻🇪 Paola"},
	{"es-MX-JorgeNeural", "🇲🇽 Jorge"},
	{"es-MX-DaliaNeural", "🇲🇽 Dalia"},
	{"es-CO-SalomeNeural", "🇨🇴 Salomé"},
	{"es-ES-AlvaroNeural", "🇪🇸 Álvaro"},
	{"es-ES-ElviraNeural", "🇪🇸 Elvira"},
	{"es-US-AlonsoNeural", "🇺🇸 Alonso (neutro)"},
	{"en-US-AriaNeural", "🇺🇸 Aria (inglés)"},
	{"pt-BR-AntonioNeural", "🇧🇷 Antônio (portugués)"},
}

// Inyectables para pruebas (sin binarios reales ni reloj real).
var (
	vozRun         = vozRunDefault
	vozLookPath    = exec.LookPath
	vozStat        = os.Stat
	vozNow         = time.Now
	vozFindEdgeTTS = vozFindEdgeTTSDefault
	vozMkdirTemp   = os.MkdirTemp
	vozRemoveAll   = os.RemoveAll
)

var (
	vozCooldownMu sync.Mutex
	vozCooldowns  = make(map[string]time.Time)
)

// Voz porta addons/voz.js: .voz/.tts texto → nota de voz con edge-tts.
type Voz struct{}

// Commands cumple core.Addon.
func (Voz) Commands() []string { return []string{"voz", "tts"} }

// Handle replica el flujo del addon original.
func (Voz) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}
	jid := ctx.ChatJID().String()

	now := vozNow()
	vozCooldownMu.Lock()
	last := vozCooldowns[jid]
	if now.Sub(last) < 3*time.Second {
		vozCooldownMu.Unlock()
		return ctx.React("⏳")
	}
	vozCooldowns[jid] = now
	vozCooldownMu.Unlock()

	voice := vozDefaultVoice
	rest := append([]string(nil), ctx.Args...)

	if len(rest) > 0 && strings.HasPrefix(rest[0], "@") {
		v := strings.TrimPrefix(rest[0], "@")
		if !vozVoiceRE.MatchString(v) {
			if err := ctx.React("⚠️"); err != nil {
				return err
			}
			return ctx.SendQuoted(core.TextMessage("❌ Voz inválida. Formato: `@es-MX-DaliaNeural`. Mira `.voz voces`."), ctx.Message)
		}
		voice = v
		rest = rest[1:]
	}

	if len(rest) > 0 && strings.ToLower(rest[0]) == "voces" {
		return ctx.SendQuoted(core.TextMessage(vozListaVoces()), ctx.Message)
	}

	text := strings.TrimSpace(strings.Join(rest, " "))
	if text == "" {
		if err := ctx.React("⚠️"); err != nil {
			return err
		}
		ayuda := "🎙️ Convierte texto a nota de voz.\n\n• `.voz hola, esto es una prueba`\n• `.voz @es-MX-DaliaNeural hola`\n• `.voz voces` para ver voces"
		return ctx.SendQuoted(core.TextMessage(ayuda), ctx.Message)
	}

	bin := vozFindEdgeTTS()
	if bin == "" {
		if err := ctx.React("❌"); err != nil {
			return err
		}
		return ctx.SendQuoted(core.TextMessage("❌ edge-tts no está instalado en el servidor.\nInstálalo con: `pip install edge-tts`"), ctx.Message)
	}

	if len(text) > vozMaxText {
		text = text[:vozMaxText]
	}

	tmpDir, err := vozMkdirTemp("", "voz_")
	if err != nil {
		if reactErr := ctx.React("❌"); reactErr != nil {
			return reactErr
		}
		return ctx.SendQuoted(core.TextMessage("❌ No pude generar la voz: "+err.Error()), ctx.Message)
	}
	defer vozRemoveAll(tmpDir)

	mp3Path := filepath.Join(tmpDir, "out.mp3")
	oggPath := filepath.Join(tmpDir, "out.ogg")

	if err := ctx.React("🎙️"); err != nil {
		return err
	}

	// 1) Sintetizar MP3.
	synthCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if out, err := vozRun(synthCtx, bin, "--voice", voice, "--text", text, "--write-media", mp3Path); err != nil {
		suffix := ""
		if voice != vozDefaultVoice {
			suffix = " (¿voz existente?)"
		}
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		msg = vozRecortar(msg, 120)
		if reactErr := ctx.React("❌"); reactErr != nil {
			return reactErr
		}
		return ctx.SendQuoted(core.TextMessage("❌ No pude generar la voz: edge-tts falló"+suffix+": "+msg), ctx.Message)
	}

	if info, err := vozStat(mp3Path); err != nil || info.Size() < 100 {
		if reactErr := ctx.React("❌"); reactErr != nil {
			return reactErr
		}
		return ctx.SendQuoted(core.TextMessage("❌ No pude generar la voz: audio vacío generado"), ctx.Message)
	}

	// 2) Normalizar a OGG/Opus 48 kHz.
	if err := ctx.React("⏳"); err != nil {
		return err
	}
	if out, err := vozRun(context.Background(), "ffmpeg", vozFFmpegArgs(mp3Path, oggPath)...); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		if reactErr := ctx.React("❌"); reactErr != nil {
			return reactErr
		}
		return ctx.SendQuoted(core.TextMessage("❌ No pude generar la voz: "+vozRecortar(msg, 120)), ctx.Message)
	}

	buf, err := os.ReadFile(oggPath)
	if err != nil {
		if reactErr := ctx.React("❌"); reactErr != nil {
			return reactErr
		}
		return ctx.SendQuoted(core.TextMessage("❌ No pude generar la voz: "+err.Error()), ctx.Message)
	}

	// 3) Enviar como nota de voz.
	if err := ctx.SendAudio(buf, "audio/ogg; codecs=opus", true, 0); err != nil {
		return err
	}
	return ctx.React("✅")
}

func vozListaVoces() string {
	var b strings.Builder
	b.WriteString("🎙️ *Voces recomendadas:*\n\n")
	for i, v := range vozVoices {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("• `.voz @")
		b.WriteString(v[0])
		b.WriteString(" texto`\n   ")
		b.WriteString(v[1])
	}
	b.WriteString("\n\nTambién sirven otras de la lista oficial de Edge (formato xx-XX-NombreNeural).")
	return b.String()
}

// vozFFmpegArgs replica .noVideo().audioFrequency(48000).toFormat('ogg')
// .audioCodec('libopus').
func vozFFmpegArgs(input, output string) []string {
	return []string{"-y", "-i", input, "-vn", "-ar", "48000", "-c:a", "libopus", "-f", "ogg", output}
}

// vozRunDefault ejecuta un binario con timeout y devuelve stdout+stderr.
func vozRunDefault(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// vozFindEdgeTTSDefault replica findEdgeTts() del original.
func vozFindEdgeTTSDefault() string {
	home, _ := os.UserHomeDir()
	candidates := []string{
		os.Getenv("EDGE_TTS_BIN"),
		"edge-tts",
		filepath.Join(home, ".local/bin/edge-tts"),
		filepath.Join(home, ".pyenv/versions/3.10.14/bin/edge-tts"),
		"/usr/local/bin/edge-tts",
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if !strings.Contains(c, "/") {
			if _, err := vozLookPath(c); err == nil {
				return c
			}
			continue
		}
		if _, err := vozStat(c); err == nil {
			return c
		}
	}
	return ""
}

func vozRecortar(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// vozResetCooldowns se usa en pruebas para aislar el estado global.
func vozResetCooldowns() {
	vozCooldownMu.Lock()
	vozCooldowns = make(map[string]time.Time)
	vozCooldownMu.Unlock()
}
