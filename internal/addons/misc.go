package addons

import (
	"context"
	"strings"
	"time"

	"whatsbot-go/internal/core"
)

// miscPingServer mide el round-trip contra los servidores de WhatsApp.
// Es inyectable para que las pruebas no necesiten red.
var miscPingServer = func(ctx *core.Context) (time.Duration, error) {
	if ctx == nil || ctx.Client == nil || ctx.Client.Store == nil || ctx.Client.Store.ID == nil {
		return 0, nil
	}
	id := ctx.Client.Store.ID.String()
	id = strings.SplitN(id, ":", 2)[0]
	if !strings.Contains(id, "@") {
		id += "@s.whatsapp.net"
	}
	start := time.Now()
	_, err := ctx.Client.IsOnWhatsApp(context.Background(), []string{id})
	return time.Since(start), err
}

// Misc porta addons/misc.js: .ping y .pong.
type Misc struct{}

// Commands cumple core.Addon.
func (Misc) Commands() []string { return []string{"ping", "pong"} }

// Handle replica las respuestas y reacciones del addon original.
func (m Misc) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}

	switch ctx.Command {
	case "ping":
		if err := ctx.React("📡"); err != nil {
			return err
		}
		ms, err := miscPingServer(ctx)
		if err != nil {
			return err
		}
		text := strings.Join([]string{
			"🚀 *Ping a WhatsApp*",
			"",
			"⚡ Latencia: *" + miscItoa(ms.Milliseconds()) + "ms*",
		}, "\n")
		if err := ctx.SendQuoted(core.TextMessage(text), ctx.Message); err != nil {
			return err
		}
		return ctx.React("✅")

	case "pong":
		start := time.Now()
		var msgID string
		if ctx.Client != nil && ctx.Message != nil {
			if id, err := wagoEnviarHook(ctx.Client, ctx.ChatJID(), core.TextMessage("🏓")); err == nil {
				msgID = string(id)
			}
		}
		if msgID == "" {
			if err := ctx.SendQuoted(core.TextMessage("🏓"), ctx.Message); err != nil {
				return err
			}
		}
		elapsed := time.Since(start)
		return wagoEditar(ctx, msgID, "🏓 pong: "+miscItoa(elapsed.Milliseconds())+"ms")

	default:
		return ctx.React("❓")
	}
}

// miscItoa evita depender de itoa (ya definido en otro archivo de pruebas).
func miscItoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [24]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func init() {
	core.Register(Misc{})
}
