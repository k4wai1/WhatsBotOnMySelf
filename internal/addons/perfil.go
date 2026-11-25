// addons/perfil.js → perfil.go
// Envía la foto de perfil del objetivo resolviendo LID → PN con el store.
package addons

import (
	"context"
	"errors"
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
	perfilProfileURL = func(client *whatsmeow.Client, jid types.JID, preview bool) (string, error) {
		info, err := client.GetProfilePictureInfo(context.Background(), jid, &whatsmeow.GetProfilePictureParams{Preview: preview})
		if err != nil {
			return "", err
		}
		if info == nil || info.URL == "" {
			return "", errors.New("sin foto")
		}
		return info.URL, nil
	}
	perfilFetch = func(url string) ([]byte, error) {
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

// Perfil porta addons/perfil.js.
type Perfil struct{}

func init() { core.Register(Perfil{}) }

// Commands cumple core.Addon.
func (Perfil) Commands() []string { return []string{"pf", "perfil"} }

// Handle replica la resolución de ID y la cascada de la foto.
func (Perfil) Handle(ctx *core.Context) error {
	if ctx == nil || ctx.Message == nil {
		return nil
	}
	jid := ctx.ChatJID().String()

	_ = ctx.React("⏳")

	info := debugInfoContextInfo(ctx.Message)
	isGroup := strings.HasSuffix(jid, "@g.us")

	targetID := ""
	switch {
	case info != nil && len(info.GetMentionedJID()) > 0:
		targetID = info.GetMentionedJID()[0]
	case info != nil && info.GetQuotedMessage() != nil:
		targetID = info.GetParticipant()
		if targetID == "" {
			if isGroup {
				targetID = debugInfoParticipant(ctx.Message)
			} else {
				targetID = jid
			}
		}
	default:
		targetID = debugInfoParticipant(ctx.Message)
		if targetID == "" {
			if ctx.Message.Info.IsFromMe {
				targetID = perfilBotID(ctx)
			} else {
				targetID = jid
			}
		}
	}

	// Traductor de @lid a JID si existe en memoria.
	if strings.Contains(targetID, "@lid") && ctx.Store != nil {
		if c, ok := ctx.Store.Get(targetID); ok {
			if pn, ok := c["pn"].(string); ok && pn != "" {
				if strings.Contains(pn, "@") {
					targetID = pn
				} else {
					targetID = pn + "@s.whatsapp.net"
				}
			}
		}
	}

	pureJid := perfilPureJid(targetID)

	var ppURL string
	if url, err := perfilProfileURL(ctx.Client, perfilParseJID(targetID), false); err == nil && url != "" {
		ppURL = url
	} else if url, err := perfilProfileURL(ctx.Client, perfilParseJID(pureJid), false); err == nil && url != "" {
		ppURL = url
	} else if url, err := perfilProfileURL(ctx.Client, perfilParseJID(pureJid), true); err == nil && url != "" {
		ppURL = url
	}

	if ppURL == "" {
		_ = ctx.React("🤷‍♂️")
		return ctx.SendQuoted(core.TextMessage("❌ El bot no tiene acceso a la foto de perfil (Oculta por privacidad o sin foto configurada)."), ctx.Message)
	}

	data, err := perfilFetch(ppURL)
	if err != nil {
		_ = ctx.React("❌")
		return nil
	}
	if err := ctx.SendImage(data, "📸"); err != nil {
		_ = ctx.React("❌")
		return nil
	}
	_ = ctx.React("✅")
	return nil
}

func perfilBotID(ctx *core.Context) string {
	if ctx.Client != nil && ctx.Client.Store != nil && ctx.Client.Store.ID != nil {
		return ctx.Client.Store.ID.String()
	}
	return ctx.ChatJID().String()
}

func perfilPureJid(raw string) string {
	base := strings.SplitN(raw, "@", 2)[0]
	base = strings.SplitN(base, ":", 2)[0]
	return base + "@s.whatsapp.net"
}

func perfilParseJID(raw string) types.JID {
	jid, err := types.ParseJID(raw)
	if err != nil {
		return types.EmptyJID
	}
	return jid
}
