package addons

import (
	"context"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	"whatsbot-go/internal/core"
	"whatsbot-go/internal/store"
)

type banEntry struct {
	ChatID  string `json:"chatId"`
	Number  string `json:"number"`
	FullJID string `json:"fullJid,omitempty"`
	Until   int64  `json:"until"`
}

var (
	banMu       sync.Mutex
	banPath     = func() string { return core.DataPath("assets", "banlist.json") }
	banNow      = time.Now
	banInterval = time.Minute
	banTimeRe   = regexp.MustCompile(`\s(\d+)$`)
)

// Ban porta addons/ban.js.
type Ban struct{}

func init() {
	core.Register(Ban{})
	core.RegisterBackground("ban", banInit)
}

func (Ban) Commands() []string { return []string{"ban"} }

func banRead() []banEntry {
	var list []banEntry
	if err := core.ReadJSON(banPath(), &list); err != nil {
		return nil
	}
	return list
}

func banWrite(list []banEntry) error {
	banMu.Lock()
	defer banMu.Unlock()
	return core.WriteJSON(banPath(), list)
}

// banParseRequest replica la extracción de objetivos y minutos de ban.js.
func banParseRequest(args []string, mentioned []string, quotedParticipant string) ([]string, int) {
	raw := strings.TrimSpace(strings.Join(args, " "))
	minutes := 0

	if len(mentioned) > 0 {
		if len(args) > 0 {
			if n, err := strconv.Atoi(args[len(args)-1]); err == nil {
				minutes = n
			}
		}
		targets := make([]string, 0, len(mentioned))
		for _, m := range mentioned {
			targets = append(targets, wagoJIDNormalize(m))
		}
		return targets, minutes
	}
	if quotedParticipant != "" {
		if len(args) > 0 {
			if n, err := strconv.Atoi(args[0]); err == nil {
				minutes = n
			}
		}
		return []string{wagoJIDNormalize(quotedParticipant)}, minutes
	}
	if raw != "" {
		numberPart := raw
		if m := banTimeRe.FindStringSubmatch(raw); m != nil {
			minutes, _ = strconv.Atoi(m[1])
			numberPart = raw[:len(raw)-len(m[0])]
		}
		clean := wagoSanitizePhone(numberPart)
		if len(clean) >= 10 {
			return []string{clean + "@s.whatsapp.net"}, minutes
		}
	}
	return nil, minutes
}

// wagoJIDNormalize deja solo número@s.whatsapp.net.
func wagoJIDNormalize(jid string) string {
	base := strings.SplitN(jid, "@", 2)[0]
	base = strings.SplitN(base, ":", 2)[0]
	return base + "@s.whatsapp.net"
}

func (Ban) Handle(ctx *core.Context) error {
	if ctx == nil || !wagoIsGroup(ctx) {
		return nil
	}
	if err := ctx.React("⏳"); err != nil {
		return err
	}

	info, err := wagoGroupInfo(ctx)
	if err != nil {
		slog.Error("❌ [ban] Error:", "error", err)
		return ctx.React("❌")
	}
	if !wagoSenderIsAdminOrOwner(ctx, info) {
		return ctx.React("🤡")
	}

	var mentioned []string
	if ci := ctx.ContextInfo(); ci != nil {
		mentioned = ci.GetMentionedJID()
	}
	quoted := ""
	if ci := ctx.ContextInfo(); ci != nil && ci.GetStanzaID() != "" {
		quoted = ci.GetParticipant()
	}
	targets, minutes := banParseRequest(ctx.Args, mentioned, quoted)
	if len(targets) == 0 {
		return ctx.React("❓")
	}
	if !wagoBotIsAdmin(ctx, info) {
		_ = ctx.Reply("❌ Necesito ser administrador del grupo para esto.")
		return ctx.React("❌")
	}
	if ctx.Client == nil {
		return ctx.React("❌")
	}

	if err := ctx.React("🔨"); err != nil {
		return err
	}
	bans := banRead()
	protected := append(wagoOwnerNumbers(), wagoBotNumbers(ctx)...)
	success := 0

	for _, target := range targets {
		number := strings.SplitN(target, "@", 2)[0]
		if wagoContains(protected, number) {
			continue
		}
		targetJID, err := types.ParseJID(target)
		if err != nil {
			continue
		}
		if wagoIsAdminIn(info, targetJID) {
			continue
		}
		if _, err := ctx.Client.UpdateGroupParticipants(context.Background(), ctx.ChatJID(), []types.JID{targetJID}, whatsmeow.ParticipantChangeRemove); err != nil {
			wagoAppendLog("ban.log", "❌ Error al expulsar "+number+": "+err.Error())
			continue
		}
		success++
		if minutes > 0 {
			bans = append(bans, banEntry{
				ChatID:  ctx.ChatJID().String(),
				Number:  number,
				FullJID: target,
				Until:   banNow().Add(time.Duration(minutes) * time.Minute).UnixMilli(),
			})
		}
	}
	if minutes > 0 {
		_ = banWrite(bans)
	}
	if success > 0 {
		return ctx.React("✅")
	}
	return ctx.React("⚠️")
}

func banInit(client *whatsmeow.Client, _ *store.Store) error {
	if client == nil {
		return nil
	}
	go func() {
		ticker := time.NewTicker(banInterval)
		defer ticker.Stop()
		for range ticker.C {
			banReadd(client)
		}
	}()
	return nil
}

func banReadd(client *whatsmeow.Client) {
	bans := banRead()
	if len(bans) == 0 {
		return
	}
	now := banNow().UnixMilli()
	var remaining []banEntry
	for _, ban := range bans {
		if ban.Until > now {
			remaining = append(remaining, ban)
			continue
		}
		chat, err := types.ParseJID(ban.ChatID)
		if err != nil {
			continue
		}
		full := ban.FullJID
		if full == "" {
			full = ban.Number + "@s.whatsapp.net"
		}
		target, err := types.ParseJID(full)
		if err != nil {
			continue
		}
		if _, err := client.UpdateGroupParticipants(context.Background(), chat, []types.JID{target}, whatsmeow.ParticipantChangeAdd); err != nil {
			remaining = append(remaining, ban)
			continue
		}
		wagoAppendLog("ban.log", "♻️ Usuario "+ban.Number+" reincorporado.")
	}
	_ = banWrite(remaining)
}

func wagoContains(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}
