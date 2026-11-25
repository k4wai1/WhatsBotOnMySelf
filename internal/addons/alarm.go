package addons

import (
	"context"
	"fmt"
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

type alarmEntry struct {
	ID         string `json:"id"`
	ChatID     string `json:"chatId"`
	SenderID   string `json:"senderId"`
	IsGroup    bool   `json:"isGroup"`
	TargetTime int64  `json:"targetTime"`
	Message    string `json:"message"`
}

var (
	alarmMu       sync.Mutex
	alarmPath     = func() string { return core.DataPath("assets", "recordatorios.json") }
	alarmNow      = time.Now
	alarmInterval = 5 * time.Minute
	alarmRe       = regexp.MustCompile(`^(\d{1,2})(?:[:\s](\d{1,2}))?(?:\s+(.*))?$`)
)

// Alarm porta addons/alarm.js (assets/recordatorios.json).
type Alarm struct{}

func init() {
	core.Register(Alarm{})
	core.RegisterBackground("alarm", alarmInit)
}

func (Alarm) Commands() []string { return []string{"a", "alarma"} }

func alarmLoad() []alarmEntry {
	var list []alarmEntry
	if err := core.ReadJSON(alarmPath(), &list); err != nil {
		return nil
	}
	return list
}

func alarmSave(list []alarmEntry) error {
	alarmMu.Lock()
	defer alarmMu.Unlock()
	return core.WriteJSON(alarmPath(), list)
}

// alarmParse interpreta ".a 12", ".a 13 45" y ".a 17:36 descripción".
func alarmParse(raw string) (horas, minutos int, descripcion string, ok bool) {
	m := alarmRe.FindStringSubmatch(strings.TrimSpace(raw))
	if m == nil {
		return 0, 0, "", false
	}
	horas, _ = strconv.Atoi(m[1])
	minutos = 0
	if m[2] != "" {
		minutos, _ = strconv.Atoi(m[2])
	}
	descripcion = m[3]
	if descripcion == "" {
		descripcion = "¡Es hora de tu recordatorio!"
	}
	if horas < 0 || horas > 23 || minutos < 0 || minutos > 59 {
		return horas, minutos, descripcion, false
	}
	return horas, minutos, descripcion, true
}

// alarmNextTarget calcula la próxima fecha a esa hora (hoy o mañana).
func alarmNextTarget(horas, minutos int, now time.Time) time.Time {
	target := time.Date(now.Year(), now.Month(), now.Day(), horas, minutos, 0, 0, now.Location())
	if !target.After(now) {
		target = target.AddDate(0, 0, 1)
	}
	return target
}

func (Alarm) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}
	horas, minutos, descripcion, ok := alarmParse(strings.Join(ctx.Args, " "))
	if !ok {
		return ctx.Reply("⚠️ Formato: `.a 12`, `.a 13 45` o `.a 17:36 descripción`")
	}
	if horas < 0 || horas > 23 || minutos < 0 || minutos > 59 {
		return ctx.Reply("❌ Hora o minutos fuera de rango (0-23h / 0-59m).")
	}

	target := alarmNextTarget(horas, minutos, alarmNow())
	entry := alarmEntry{
		ID:         strconv.FormatInt(alarmNow().UnixMilli(), 10),
		ChatID:     ctx.ChatJID().String(),
		SenderID:   ctx.SenderJID().String(),
		IsGroup:    wagoIsGroup(ctx),
		TargetTime: target.UnixMilli(),
		Message:    descripcion,
	}
	list := append(alarmLoad(), entry)
	if err := alarmSave(list); err != nil {
		slog.Error("❌ [alarm] Error:", "error", err)
		return ctx.React("❌")
	}
	return ctx.Reply(fmt.Sprintf("✅ Alarma establecida: *%02d:%02d*\n📝 Nota: %s", horas, minutos, descripcion))
}

func alarmInit(client *whatsmeow.Client, _ *store.Store) error {
	if client == nil {
		return nil
	}
	go func() {
		ticker := time.NewTicker(alarmInterval)
		defer ticker.Stop()
		for range ticker.C {
			alarmTick(client)
		}
	}()
	return nil
}

func alarmTick(client *whatsmeow.Client) {
	list := alarmLoad()
	if len(list) == 0 {
		return
	}
	now := alarmNow().UnixMilli()
	var pending, due []alarmEntry
	for _, entry := range list {
		if now >= entry.TargetTime {
			due = append(due, entry)
		} else {
			pending = append(pending, entry)
		}
	}
	if len(due) == 0 {
		return
	}
	_ = alarmSave(pending)

	for _, entry := range due {
		text := "⏰ *RECORDATORIO*\n\n> " + entry.Message
		var mentions []types.JID
		if entry.IsGroup {
			text += "\n\n🔔 @" + wagoUserOf(entry.SenderID)
			if jid, err := types.ParseJID(entry.SenderID); err == nil {
				mentions = append(mentions, jid)
			}
		}
		msg := core.TextMessage(text)
		if len(mentions) > 0 {
			msg = wagoTextWithMentions(text, mentions)
		}
		jid, err := types.ParseJID(entry.ChatID)
		if err != nil {
			continue
		}
		_, _ = client.SendMessage(context.Background(), jid, msg)
	}
}
