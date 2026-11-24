// addons/debug-pp.js → debug-pp.go
// Reporte de foto de perfil con cascada de reintentos.
package addons

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	"whatsbot-go/internal/core"
)

var (
	debugPpProfileURL = func(client *whatsmeow.Client, jid types.JID, preview bool) (string, error) {
		info, err := client.GetProfilePictureInfo(context.Background(), jid, &whatsmeow.GetProfilePictureParams{Preview: preview})
		if err != nil {
			return "", err
		}
		if info == nil || info.URL == "" {
			return "", fmt.Errorf("sin foto")
		}
		return info.URL, nil
	}
	debugPpFetch = func(url string) ([]byte, error) {
		client := &http.Client{Timeout: 30 * time.Second}
		res, err := client.Get(url)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			return nil, fmt.Errorf("HTTP %d", res.StatusCode)
		}
		return io.ReadAll(io.LimitReader(res.Body, 32<<20))
	}
)

// DebugPp porta addons/debug-pp.js.
type DebugPp struct{}

func init() { core.Register(DebugPp{}) }

// Commands cumple core.Addon.
func (DebugPp) Commands() []string { return []string{"getpp", "debugpp"} }

// Handle replica los tres intentos y el reporte del original.
func (DebugPp) Handle(ctx *core.Context) error {
	if ctx == nil || ctx.Message == nil {
		return nil
	}
	var reporte strings.Builder
	reporte.WriteString("🖼️ *REPORTE DE FOTO DE PERFIL*\n\n")

	_ = ctx.React("📸")

	targetID := debugPpTarget(ctx)
	pureJid := debugPpPureJid(targetID)

	reporte.WriteString("🎯 *Objetivo:* " + pureJid + "\n\n")

	finalURL := ""
	successMethod := ""

	// Método 1: JID crudo, alta resolución.
	if url, err := debugPpProfileURL(ctx.Client, debugPpParseJID(targetID), false); err == nil && url != "" {
		finalURL = url
		successMethod = "JID Crudo (Alta Res)"
		reporte.WriteString("✅ *Método 1 (Crudo/Alta):* ¡Éxito!\n")
	} else {
		reporte.WriteString("❌ *Método 1 (Crudo/Alta):* " + debugPpErrText(err) + "\n")
		reporte.WriteString("   ↳ _Falla común si targetId tiene un ':15' al final._\n")
	}

	// Método 2: JID puro, alta resolución.
	if finalURL == "" {
		if url, err := debugPpProfileURL(ctx.Client, debugPpParseJID(pureJid), false); err == nil && url != "" {
			finalURL = url
			successMethod = "JID Puro (Alta Res)"
			reporte.WriteString("✅ *Método 2 (Puro/Alta):* ¡Éxito!\n")
		} else {
			reporte.WriteString("❌ *Método 2 (Puro/Alta):* " + debugPpErrText(err) + "\n")
		}
	}

	// Método 3: JID puro, vista previa.
	if finalURL == "" {
		if url, err := debugPpProfileURL(ctx.Client, debugPpParseJID(pureJid), true); err == nil && url != "" {
			finalURL = url
			successMethod = "JID Puro (Thumbnail)"
			reporte.WriteString("✅ *Método 3 (Puro/Baja):* ¡Éxito!\n")
		} else {
			reporte.WriteString("❌ *Método 3 (Puro/Baja):* Privacidad estricta o sin foto.\n")
		}
	}
	_ = successMethod

	if finalURL != "" {
		reporte.WriteString("\n🔗 *URL Obtenida:* " + finalURL + "\n")
		data, err := debugPpFetch(finalURL)
		if err != nil {
			_ = ctx.React("❌")
			return nil
		}
		if err := ctx.SendImage(data, reporte.String()); err != nil {
			_ = ctx.React("❌")
			return nil
		}
	} else {
		reporte.WriteString("\n💀 *Conclusión:* Es imposible obtener la foto. El usuario la tiene restringida a \"Mis Contactos\" o no tiene foto.")
		_ = ctx.SendQuoted(core.TextMessage(reporte.String()), ctx.Message)
	}

	_ = ctx.React("✅")
	return nil
}

func debugPpTarget(ctx *core.Context) string {
	info := debugInfoContextInfo(ctx.Message)
	if info != nil {
		if p := info.GetParticipant(); p != "" {
			return p
		}
		if mentions := info.GetMentionedJID(); len(mentions) > 0 {
			return mentions[0]
		}
	}
	if p := debugInfoParticipant(ctx.Message); p != "" {
		return p
	}
	return ctx.ChatJID().String()
}

func debugPpPureJid(raw string) string {
	base := strings.SplitN(raw, "@", 2)[0]
	base = strings.SplitN(base, ":", 2)[0]
	return base + "@s.whatsapp.net"
}

func debugPpParseJID(raw string) types.JID {
	jid, err := types.ParseJID(raw)
	if err != nil {
		return types.EmptyJID
	}
	return jid
}

func debugPpErrText(err error) string {
	if err == nil {
		return "401/404 Unauthorized"
	}
	return err.Error()
}
