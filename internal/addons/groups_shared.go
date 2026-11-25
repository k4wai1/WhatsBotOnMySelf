// groups_shared.go — utilidades de grupos compartidas por los addons de
// moderación. Todos los símbolos van con el prefijo `wago` para no colisionar.
package addons

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	"whatsbot-go/internal/core"
)

var wagoErrNoClient = errors.New("cliente de WhatsApp no disponible")

// wagoOwnerNumbers lee OWNER_NUMBERS=584...,182... del .env.
func wagoOwnerNumbers() []string {
	raw := os.Getenv("OWNER_NUMBERS")
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if n := wagoDigits(part); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// wagoBotNumbers combina la identidad del cliente con BOT_NUMBERS del .env.
func wagoBotNumbers(ctx *core.Context) []string {
	var client *whatsmeow.Client
	if ctx != nil {
		client = ctx.Client
	}
	return wagoBotNumbersForClient(client)
}

// wagoBotNumbersForClient devuelve los números del bot sin necesitar Context.
func wagoBotNumbersForClient(client *whatsmeow.Client) []string {
	set := make(map[string]bool)
	if client != nil && client.Store != nil && client.Store.ID != nil {
		if n := client.Store.ID.User; n != "" {
			set[n] = true
		}
	}
	for _, part := range strings.Split(os.Getenv("BOT_NUMBERS"), ",") {
		if n := wagoDigits(part); n != "" {
			set[n] = true
		}
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	return out
}

func wagoDigits(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// wagoNormalizeNumber replica normalizeNumber() de los addons de rupa:
// 04141234567 → 584141234567 y 4141234567 → 584141234567.
func wagoNormalizeNumber(raw string) string {
	clean := wagoDigits(raw)
	if clean == "" {
		return ""
	}
	if clean[0] == '0' && len(clean) == 11 {
		return "58" + clean[1:]
	}
	if len(clean) == 10 && (clean[0] == '4' || clean[0] == '2') {
		return "58" + clean
	}
	if len(clean) >= 7 {
		return clean
	}
	return ""
}

// wagoSanitizePhone replica sanitizePhoneNumber() de ban.js (sin mínimo).
func wagoSanitizePhone(raw string) string {
	clean := wagoDigits(raw)
	if clean == "" {
		return ""
	}
	if clean[0] == '0' && len(clean) == 11 {
		return "58" + clean[1:]
	}
	if len(clean) == 10 && (clean[0] == '4' || clean[0] == '2') {
		return "58" + clean
	}
	return clean
}

func wagoIsGroup(ctx *core.Context) bool {
	return ctx != nil && ctx.ChatJID().Server == types.GroupServer
}

// wagoGroupInfoFn es inyectable en pruebas.
var wagoGroupInfoFn = func(ctx *core.Context) (*types.GroupInfo, error) {
	if ctx == nil || ctx.Client == nil {
		return nil, wagoErrNoClient
	}
	return ctx.Client.GetGroupInfo(context.Background(), ctx.ChatJID())
}

func wagoGroupInfo(ctx *core.Context) (*types.GroupInfo, error) {
	return wagoGroupInfoFn(ctx)
}

// wagoSenderIsAdminOrOwner comprueba dueño del bot o admin del grupo.
func wagoSenderIsAdminOrOwner(ctx *core.Context, info *types.GroupInfo) bool {
	number := ctx.SenderJID().User
	for _, owner := range wagoOwnerNumbers() {
		if owner == number {
			return true
		}
	}
	if info == nil {
		return false
	}
	return wagoIsAdminIn(info, ctx.SenderJID())
}

func wagoIsAdminIn(info *types.GroupInfo, jid types.JID) bool {
	if info == nil {
		return false
	}
	for _, p := range info.Participants {
		if p.JID.User == jid.User || (p.PhoneNumber.User != "" && p.PhoneNumber.User == jid.User) ||
			(p.LID.User != "" && p.LID.User == jid.User) {
			return p.IsAdmin || p.IsSuperAdmin
		}
	}
	return false
}

func wagoBotIsAdmin(ctx *core.Context, info *types.GroupInfo) bool {
	if info == nil {
		return false
	}
	bots := wagoBotNumbers(ctx)
	for _, p := range info.Participants {
		candidates := []string{p.JID.User, p.PhoneNumber.User, p.LID.User}
		for _, c := range candidates {
			if c == "" {
				continue
			}
			for _, b := range bots {
				if b == c && (p.IsAdmin || p.IsSuperAdmin) {
					return true
				}
			}
		}
	}
	return false
}

func wagoGroupJID(ctx *core.Context) types.JID {
	return ctx.ChatJID()
}

// wagoAppendLog escribe una línea en assets/<name>, como logAction().
func wagoAppendLog(name, line string) {
	path := core.DataPath("assets", name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString("[" + time.Now().UTC().Format(time.RFC3339) + "] " + line + "\n")
}

// wagoVCardNumbers extrae los waid=... de una vCard.
func wagoVCardNumbers(vcard string) []string {
	var out []string
	rest := vcard
	for {
		idx := strings.Index(rest, "waid=")
		if idx < 0 {
			break
		}
		rest = rest[idx+len("waid="):]
		end := 0
		for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
			end++
		}
		if n := wagoNormalizeNumber(rest[:end]); n != "" {
			out = append(out, n)
		}
		if end == 0 {
			rest = rest[1:]
		}
	}
	return out
}
