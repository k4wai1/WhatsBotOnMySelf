package addons

import (
	"context"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	"whatsbot-go/internal/core"
	"whatsbot-go/internal/store"
)

type muteInfo struct {
	Until    *int64 `json:"until"`
	By       string `json:"by,omitempty"`
	MutedAt  int64  `json:"mutedAt,omitempty"`
	ChatName string `json:"chatName,omitempty"`
}

type muteData struct {
	Groups map[string]*muteInfo `json:"groups"`
}

var (
	muteMu       sync.Mutex
	mutePath     = func() string { return core.DataPath("assets", "mutes.json") }
	muteNow      = time.Now
	muteInterval = time.Minute
)

// Mute porta addons/mute.js.
type Mute struct{}

func init() {
	core.Register(Mute{})
	core.RegisterBackground("mute", muteInit)
}

func (Mute) Commands() []string { return []string{"mute", "unmute"} }

func muteLoad() muteData {
	data := muteData{Groups: map[string]*muteInfo{}}
	if err := core.ReadJSON(mutePath(), &data); err != nil {
		return muteData{Groups: map[string]*muteInfo{}}
	}
	if data.Groups == nil {
		data.Groups = map[string]*muteInfo{}
	}
	return data
}

func muteSave(data muteData) error {
	muteMu.Lock()
	defer muteMu.Unlock()
	return core.WriteJSON(mutePath(), data)
}

// muteParseMinutes replica parseFloat+round de mute.js. Devuelve ok=false si
// el argumento no es un número positivo.
func muteParseMinutes(args []string) (int, bool) {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return 0, true
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(args[0]), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
		return 0, false
	}
	return int(math.Round(v)), true
}

func (Mute) Handle(ctx *core.Context) error {
	if ctx == nil || !wagoIsGroup(ctx) {
		return nil
	}
	info, err := wagoGroupInfo(ctx)
	if err != nil {
		slog.Error("❌ [mute] Error:", "error", err)
		return ctx.React("❌")
	}
	if !wagoSenderIsAdminOrOwner(ctx, info) {
		return ctx.React("⛔")
	}
	if !wagoBotIsAdmin(ctx, info) {
		return ctx.Reply("❌ El bot no es Admin aquí.")
	}

	data := muteLoad()
	chatID := ctx.ChatJID().String()

	if ctx.Command == "mute" {
		minutes, ok := muteParseMinutes(ctx.Args)
		if !ok {
			return ctx.React("❓")
		}
		var until *int64
		if len(ctx.Args) > 0 && strings.TrimSpace(ctx.Args[0]) != "" {
			u := muteNow().Add(time.Duration(minutes) * time.Minute).UnixMilli()
			until = &u
		}
		if ctx.Client != nil {
			if err := ctx.Client.SetGroupAnnounce(context.Background(), ctx.ChatJID(), true); err != nil {
				slog.Error("❌ [mute] Error:", "error", err)
				return ctx.React("❌")
			}
		}
		data.Groups[chatID] = &muteInfo{Until: until, By: ctx.SenderJID().User, MutedAt: muteNow().UnixMilli(), ChatName: info.Name}
		_ = muteSave(data)
		wagoAppendLog("mute.log", "Mute en "+chatID)
		if until == nil {
			return ctx.React("🔇")
		}
		if err := ctx.React("⏱️"); err != nil {
			return err
		}
		return ctx.Reply("🔒 Chat cerrado por " + strconv.Itoa(minutes) + " minutos.")
	}

	if ctx.Command == "unmute" {
		if ctx.Client != nil {
			if err := ctx.Client.SetGroupAnnounce(context.Background(), ctx.ChatJID(), false); err != nil {
				slog.Error("❌ [mute] Error:", "error", err)
				return ctx.React("❌")
			}
		}
		delete(data.Groups, chatID)
		_ = muteSave(data)
		wagoAppendLog("mute.log", "Unmute en "+chatID)
		return ctx.React("🔊")
	}
	return nil
}

func muteInit(client *whatsmeow.Client, _ *store.Store) error {
	if client == nil {
		return nil
	}
	go func() {
		ticker := time.NewTicker(muteInterval)
		defer ticker.Stop()
		for range ticker.C {
			muteProcessExpired(client)
		}
	}()
	return nil
}

// muteExpired devuelve los grupos cuyo mute temporal ya venció (para pruebas).
func muteExpired(data muteData, now int64) []string {
	var out []string
	for groupID, info := range data.Groups {
		if info != nil && info.Until != nil && now >= *info.Until {
			out = append(out, groupID)
		}
	}
	return out
}

func muteProcessExpired(client *whatsmeow.Client) {
	data := muteLoad()
	for _, groupID := range muteExpired(data, muteNow().UnixMilli()) {
		jid, err := types.ParseJID(groupID)
		if err == nil {
			if err := client.SetGroupAnnounce(context.Background(), jid, false); err == nil {
				_, _ = client.SendMessage(context.Background(), jid, core.TextMessage("🔓 Tiempo cumplido. Chat abierto."))
				wagoAppendLog("mute.log", "🔓 Auto-Unmute en "+groupID)
			}
		}
		delete(data.Groups, groupID)
	}
	_ = muteSave(data)
}
