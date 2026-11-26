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
	if client != nil && client.Store != nil {
		if n := client.Store.LID.User; n != "" {
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

// wagoSameUser compara dos JID ignorando el número de dispositivo (:AD).
func wagoSameUser(a, b types.JID) bool {
	if a.IsEmpty() || b.IsEmpty() {
		return false
	}
	return a.ToNonAD() == b.ToNonAD()
}

// wagoResolveParticipant resuelve un JID (que puede venir en @lid o con
// dispositivo :AD) contra los participantes del grupo, devolviendo el JID
// real que debe usarse en las APIs de whatsmeow y su número limpio.
// NUNCA convierte @lid a @s.whatsapp.net inventando un número.
func wagoResolveParticipant(info *types.GroupInfo, rawJID string) (types.JID, string) {
	jid, err := types.ParseJID(strings.TrimSpace(rawJID))
	if err != nil {
		return types.EmptyJID, ""
	}
	jid = jid.ToNonAD()
	if jid.IsEmpty() {
		return types.EmptyJID, ""
	}
	if info != nil {
		for _, p := range info.Participants {
			if !wagoSameUser(p.JID, jid) && !wagoSameUser(p.LID, jid) && !wagoSameUser(p.PhoneNumber, jid) {
				continue
			}
			real := p.JID.ToNonAD()
			if real.IsEmpty() {
				real = p.LID.ToNonAD()
			}
			if real.IsEmpty() {
				real = p.PhoneNumber.ToNonAD()
			}
			if real.IsEmpty() {
				real = jid
			}
			return real, wagoDigits(real.User)
		}
	}
	return jid, wagoDigits(jid.User)
}

// wagoSenderJIDs devuelve el remitente y su JID alternativo (@lid / número).
func wagoSenderJIDs(ctx *core.Context) []types.JID {
	if ctx == nil || ctx.Message == nil {
		return nil
	}
	sender := ctx.SenderJID()
	alt := ctx.Message.Info.SenderAlt
	if alt.IsEmpty() || wagoSameUser(sender, alt) {
		return []types.JID{sender}
	}
	return []types.JID{sender, alt}
}

// wagoSenderIsAdminOrOwner comprueba dueño del bot o admin del grupo.
func wagoSenderIsAdminOrOwner(ctx *core.Context, info *types.GroupInfo) bool {
	if ctx == nil {
		return false
	}
	senders := wagoSenderJIDs(ctx)
	for _, sender := range senders {
		number := sender.User
		for _, owner := range wagoOwnerNumbers() {
			if owner == number {
				return true
			}
		}
		if ctx.Client != nil && ctx.Client.Store != nil {
			if id := ctx.Client.Store.ID; id != nil && wagoSameUser(*id, sender) {
				return true
			}
			if wagoSameUser(ctx.Client.Store.LID, sender) {
				return true
			}
		}
	}
	if info == nil {
		return false
	}
	for _, sender := range senders {
		if wagoIsAdminIn(info, sender) {
			return true
		}
	}
	return false
}

func wagoIsAdminIn(info *types.GroupInfo, jid types.JID) bool {
	if info == nil || jid.IsEmpty() {
		return false
	}
	for _, p := range info.Participants {
		if wagoSameUser(p.JID, jid) || wagoSameUser(p.LID, jid) || wagoSameUser(p.PhoneNumber, jid) {
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
		if !p.IsAdmin && !p.IsSuperAdmin {
			continue
		}
		for _, candidato := range []types.JID{p.JID, p.LID, p.PhoneNumber} {
			if candidato.IsEmpty() {
				continue
			}
			if wagoContains(bots, candidato.User) {
				return true
			}
		}
		if ctx != nil && ctx.Client != nil && ctx.Client.Store != nil {
			if id := ctx.Client.Store.ID; id != nil &&
				(wagoSameUser(*id, p.JID) || wagoSameUser(*id, p.LID) || wagoSameUser(*id, p.PhoneNumber)) {
				return true
			}
			if wagoSameUser(ctx.Client.Store.LID, p.JID) || wagoSameUser(ctx.Client.Store.LID, p.LID) {
				return true
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

// wagoVCardNumbers extrae números de waid=... y de las líneas TEL de una vCard.
func wagoVCardNumbers(vcard string) []string {
	var out []string
	seen := make(map[string]bool)
	add := func(raw string) {
		if n := wagoNormalizeNumber(raw); n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}

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
		if end > 0 {
			add(rest[:end])
		}
		if end == 0 {
			rest = rest[1:]
		}
	}

	for _, line := range strings.Split(vcard, "\n") {
		line = strings.TrimSpace(line)
		if len(line) < 4 || !strings.EqualFold(line[:3], "TEL") {
			continue
		}
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		add(line[colon+1:])
	}
	return out
}
