// addons/rem.go
// Recordatorios persistentes con múltiples formatos de hora.
// .rem <tiempo> <texto> | .rem list | .rem del N | .rem clear | .rem help
package addons

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	"whatsbot-go/internal/core"
	"whatsbot-go/internal/store"
)

const (
	remMinMS      = int64(5 * 1000)
	remMaxMS      = int64(5 * 365 * 24 * 60 * 60 * 1000)
	remMaxText    = 500
	remMaxPerChat = 50
	remLateGrace  = int64(48 * 60 * 60 * 1000)
	remChunkMS    = int64(1 << 30)
)

// remItem replica un registro de assets/reminders.json.
type remItem struct {
	ID      int64  `json:"id"`
	Chat    string `json:"chat"`
	Text    string `json:"text"`
	At      int64  `json:"at"`
	Created int64  `json:"created"`
}

type remData struct {
	NextID int64     `json:"nextId"`
	Items  []remItem `json:"items"`
}

var (
	remMu         sync.Mutex
	remState      = remData{NextID: 1}
	remTimers     = map[int64]*time.Timer{}
	remClient     *whatsmeow.Client
	remNow        = time.Now
	remScheduleFn func(remItem)
)

func remStorePath() string { return core.DataPath("assets", "reminders.json") }

// remLoadStore replica loadStore(): cualquier archivo corrupto deja el almacén
// vacío y no interrumpe el bot.
func remLoadStore() {
	raw, err := os.ReadFile(remStorePath())
	if err != nil {
		remMu.Lock()
		remState = remData{NextID: 1}
		remMu.Unlock()
		if !os.IsNotExist(err) {
			slog.Error("❌ [rem] store corrupto, iniciando vacío", "error", err)
		}
		return
	}

	var parsed map[string]any
	nextID := int64(1)
	items := []remItem{}
	if err := json.Unmarshal(raw, &parsed); err == nil && parsed != nil {
		if v, ok := parsed["nextId"]; ok {
			if n, ok := remToInt64(v); ok && n != 0 {
				nextID = n
			}
		}
		if arr, ok := parsed["items"].([]any); ok {
			for _, entry := range arr {
				m, ok := entry.(map[string]any)
				if !ok {
					continue
				}
				at, okAt := remToInt64(m["at"])
				id, okID := remToInt64(m["id"])
				text, okText := m["text"].(string)
				chat, okChat := m["chat"].(string)
				if !okAt || !okID || !okText || !okChat {
					continue
				}
				created, _ := remToInt64(m["created"])
				items = append(items, remItem{ID: id, Chat: chat, Text: text, At: at, Created: created})
			}
		}
	} else if err != nil {
		slog.Error("❌ [rem] store corrupto, iniciando vacío", "error", err)
	}

	remMu.Lock()
	remState = remData{NextID: nextID, Items: items}
	remMu.Unlock()
}

func remToInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		if n != n || n > 1e18 || n < -1e18 {
			return 0, false
		}
		return int64(n), true
	case int64:
		return n, true
	case int:
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	case string:
		i, err := strconv.ParseInt(strings.TrimSpace(n), 10, 64)
		return i, err == nil
	}
	return 0, false
}

func remSave() {
	remMu.Lock()
	snapshot := remData{NextID: remState.NextID, Items: append([]remItem(nil), remState.Items...)}
	remMu.Unlock()

	if err := core.WriteJSON(remStorePath(), snapshot); err != nil {
		slog.Error("❌ [rem] no se pudo guardar", "error", err)
	}
}

// ─── Parser de tiempo ───────────────────────────────────────────────────────

var (
	remPrefixRe    = regexp.MustCompile(`(?i)^\s*(?:en\s+)?`)
	remUnitRe      = regexp.MustCompile(`(?i)^(\d{1,6})\s*(d(?:[ií]as?)?|h(?:rs?|oras?)?|m(?:ins?|inutos?)?|s(?:egs?|egundos?)?)`)
	remDayRe       = regexp.MustCompile(`(?i)^\s*(ma[ñn]ana|tmr|hoy|today)\b`)
	remTimeRe      = regexp.MustCompile(`(?i)^\s*(\d{1,2}(?:[:.]\d{1,2})?\s*(?:am|pm)?)(?:\s+|$)`)
	remTimePartsRe = regexp.MustCompile(`(?i)^(\d{1,2})(?:[:.](\d{1,2}))?(?:\s*(am|pm))?$`)
)

type remWhen struct {
	At       int64
	Text     string
	NeedTime bool
}

func remUnitMS(unit string) (int64, bool) {
	switch unit[0] {
	case 'd', 'D':
		return 24 * 60 * 60 * 1000, true
	case 'h', 'H':
		return 60 * 60 * 1000, true
	case 'm', 'M':
		return 60 * 1000, true
	case 's', 'S':
		return 1000, true
	}
	return 0, false
}

// remParseWhen replica parseWhen() de rem.js.
func remParseWhen(rawArgs string) (remWhen, bool) {
	raw := strings.TrimSpace(rawArgs)
	if raw == "" {
		return remWhen{}, false
	}

	// 1) Relativo
	if loc := remPrefixRe.FindStringIndex(raw); loc != nil {
		pos := loc[1]
		total := int64(0)
		count := 0
		boundary := false
		for pos < len(raw) {
			m := remUnitRe.FindStringSubmatchIndex(raw[pos:])
			if m == nil || m[0] != 0 {
				break
			}
			numStr := raw[pos+m[2] : pos+m[3]]
			unit := raw[pos+m[4] : pos+m[5]]
			n, err := strconv.ParseInt(numStr, 10, 64)
			if err != nil || n <= 0 {
				return remWhen{}, false
			}
			factor, ok := remUnitMS(unit)
			if !ok {
				return remWhen{}, false
			}
			total += n * factor
			pos += m[1]
			count++

			spaces := 0
			for pos < len(raw) && (raw[pos] == ' ' || raw[pos] == '\t' || raw[pos] == '\n' || raw[pos] == '\r') {
				pos++
				spaces++
			}
			if spaces > 0 {
				boundary = true
			}
		}
		if count > 0 && (pos == len(raw) || boundary) {
			if total <= 0 {
				return remWhen{}, false
			}
			return remWhen{At: remNow().UnixMilli() + total, Text: strings.TrimSpace(raw[pos:])}, true
		}
	}

	// 2) Absoluto
	rest := raw
	dayOffset := 0
	dayMatch := remDayRe.FindStringSubmatchIndex(rest)
	if dayMatch != nil {
		w := strings.ToLower(rest[dayMatch[2]:dayMatch[3]])
		if w == "hoy" || w == "today" {
			dayOffset = 0
		} else {
			dayOffset = 1
		}
		rest = rest[dayMatch[1]:]
	}

	timeLoc := remTimeRe.FindStringSubmatchIndex(rest)
	if timeLoc == nil {
		if dayMatch != nil {
			return remWhen{NeedTime: true, Text: strings.TrimSpace(rest)}, true
		}
		return remWhen{}, false
	}
	timeExpr := rest[timeLoc[2]:timeLoc[3]]
	after := timeLoc[1]

	parts := remTimePartsRe.FindStringSubmatch(strings.TrimSpace(timeExpr))
	if parts == nil {
		return remWhen{}, false
	}

	hh, _ := strconv.Atoi(parts[1])
	mm := 0
	if parts[2] != "" {
		mm, _ = strconv.Atoi(parts[2])
	}
	mer := strings.ToLower(parts[3])

	if mer != "" {
		if hh < 1 || hh > 12 {
			return remWhen{}, false
		}
		if hh == 12 {
			hh = 0
		}
		if mer == "pm" {
			hh += 12
		}
	} else if hh > 23 {
		return remWhen{}, false
	}
	if mm > 59 {
		return remWhen{}, false
	}

	now := remNow()
	d := time.Date(now.Year(), now.Month(), now.Day(), hh, mm, 0, 0, now.Location())
	d = d.AddDate(0, 0, dayOffset)
	if dayOffset == 0 && d.UnixMilli() <= now.UnixMilli() {
		d = d.AddDate(0, 0, 1)
	}

	return remWhen{At: d.UnixMilli(), Text: strings.TrimSpace(rest[after:])}, true
}

// ─── Formato legible ────────────────────────────────────────────────────────

var (
	remDias  = []string{"dom", "lun", "mar", "mié", "jue", "vie", "sáb"}
	remMeses = []string{"ene", "feb", "mar", "abr", "may", "jun", "jul", "ago", "sep", "oct", "nov", "dic"}
)

func remFmtDate(ms int64) string {
	d := time.UnixMilli(ms)
	hh := d.Hour()
	mm := d.Minute()
	now := remNow()
	hoy := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	manana := hoy.AddDate(0, 0, 1)
	day := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, d.Location())

	var prefijo string
	switch {
	case day.Equal(hoy):
		prefijo = "hoy"
	case day.Equal(manana):
		prefijo = "mañana"
	default:
		prefijo = remDias[int(d.Weekday())] + " " + strconv.Itoa(d.Day()) + " " + remMeses[int(d.Month())-1]
	}
	return prefijo + " a las " + remPad2(hh) + ":" + remPad2(mm)
}

func remPad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

func remFmtDur(ms int64) string {
	s := ms / 1000
	d := s / 86400
	h := (s % 86400) / 3600
	m := (s % 3600) / 60
	seg := s % 60

	parts := []string{}
	if d != 0 {
		parts = append(parts, strconv.FormatInt(d, 10)+"d")
	}
	if h != 0 {
		parts = append(parts, strconv.FormatInt(h, 10)+"h")
	}
	if m != 0 {
		parts = append(parts, strconv.FormatInt(m, 10)+"m")
	}
	if seg != 0 && d == 0 && h == 0 {
		parts = append(parts, strconv.FormatInt(seg, 10)+"s")
	}
	if len(parts) == 0 {
		return "0s"
	}
	return strings.Join(parts, " ")
}

const remHelp = `⏰ *Recordatorios*

*Crear:*
• ` + "`.rem 30m llamar a mamá`" + `
• ` + "`.rem 2h tomar agua`" + `
• ` + "`.rem 1h45m sacar la comida`" + `
• ` + "`.rem 17:30 reunión`" + ` (si ya pasó → mañana)
• ` + "`.rem 9pm serie nueva`" + `
• ` + "`.rem mañana 8am gym`" + `

*Gestionar:*
• ` + "`.rem list`" + ` — ver pendientes
• ` + "`.rem del N`" + ` — borrar el #N
• ` + "`.rem clear`" + ` — borrar todos`

// ─── Programación y disparo ────────────────────────────────────────────────

func remSchedule(item remItem) {
	if remScheduleFn != nil {
		remScheduleFn(item)
		return
	}

	remMu.Lock()
	if t, ok := remTimers[item.ID]; ok && t != nil {
		t.Stop()
	}
	delay := item.At - remNow().UnixMilli()
	if delay <= 0 {
		delete(remTimers, item.ID)
		remMu.Unlock()
		remFire(item, false)
		return
	}
	effective := delay
	if effective > remChunkMS {
		effective = remChunkMS
	}
	remTimers[item.ID] = time.AfterFunc(time.Duration(effective)*time.Millisecond, func() {
		remMu.Lock()
		delete(remTimers, item.ID)
		remMu.Unlock()
		if delay > remChunkMS {
			remSchedule(item)
			return
		}
		remFire(item, false)
	})
	remMu.Unlock()
}

func remFire(item remItem, late bool) {
	remMu.Lock()
	kept := make([]remItem, 0, len(remState.Items))
	for _, it := range remState.Items {
		if it.ID != item.ID {
			kept = append(kept, it)
		}
	}
	remState.Items = kept
	if t, ok := remTimers[item.ID]; ok && t != nil {
		t.Stop()
	}
	delete(remTimers, item.ID)
	client := remClient
	remMu.Unlock()

	remSave()
	if client == nil {
		return
	}

	text := "⏰ *Recordatorio:* " + item.Text
	if late {
		text += "\n_(atrasado " + remFmtDur(remNow().UnixMilli()-item.At) + ")_"
	}
	if err := remSend(client, item.Chat, text); err != nil {
		slog.Error("❌ [rem] no se pudo entregar", "id", item.ID, "error", err)
	}
}

func remSend(client *whatsmeow.Client, chat, text string) error {
	jid, err := types.ParseJID(chat)
	if err != nil {
		return err
	}
	_, err = client.SendMessage(context.Background(), jid, core.TextMessage(text))
	return err
}

// remInit replica init(sock) de rem.js.
func remInit(client *whatsmeow.Client, _ *store.Store) error {
	remMu.Lock()
	remClient = client
	remMu.Unlock()

	remLoadStore()

	now := remNow().UnixMilli()
	vivos := []remItem{}
	late := []remItem{}

	remMu.Lock()
	for _, item := range remState.Items {
		if item.At > now {
			vivos = append(vivos, item)
		} else if now-item.At < remLateGrace {
			late = append(late, item)
		}
	}
	remState.Items = vivos
	n := len(remState.Items)
	remMu.Unlock()

	remSave()
	for _, item := range vivos {
		remSchedule(item)
	}
	for _, item := range late {
		remFire(item, true)
	}
	slog.Info("⚙️ [rem] recordatorio(s) activo(s)", "total", n)
	return nil
}

// ─── Comando .rem ───────────────────────────────────────────────────────────

func init() {
	core.Register(Rem{})
	core.RegisterBackground("rem", remInit)
}

// Rem porta addons/rem.js.
type Rem struct{}

// Commands cumple core.Addon.
func (Rem) Commands() []string { return []string{"rem", "recordar"} }

// Handle replica el handler de rem.js.
func (Rem) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}
	if err := remHandle(ctx); err != nil {
		slog.Error("❌ [rem]", "error", err)
		_ = ctx.React("❌")
	}
	return nil
}

func remHandle(ctx *core.Context) error {
	jid := ctx.ChatJID().String()
	args := ctx.Args

	if len(args) == 0 || strings.ToLower(args[0]) == "help" || args[0] == "?" {
		return ctx.SendQuoted(core.TextMessage(remHelp), ctx.Message)
	}

	sub := strings.ToLower(args[0])
	remLoadStore()

	switch {
	case sub == "list" || sub == "ls":
		return remList(ctx, jid)
	case sub == "del" || sub == "rm" || sub == "delete":
		return remDel(ctx, jid, args)
	case sub == "clear":
		return remClear(ctx, jid)
	}

	return remCreate(ctx, jid, args)
}

func remMine(jid string) []remItem {
	remMu.Lock()
	defer remMu.Unlock()
	mine := []remItem{}
	for _, it := range remState.Items {
		if it.Chat == jid {
			mine = append(mine, it)
		}
	}
	sort.SliceStable(mine, func(i, j int) bool { return mine[i].At < mine[j].At })
	return mine
}

func remList(ctx *core.Context, jid string) error {
	mine := remMine(jid)
	if len(mine) == 0 {
		return ctx.SendQuoted(core.TextMessage("📭 No tienes recordatorios pendientes."), ctx.Message)
	}
	now := remNow().UnixMilli()
	lines := make([]string, 0, len(mine))
	for i, it := range mine {
		lines = append(lines, "*"+strconv.Itoa(i+1)+".* "+remFmtDate(it.At)+" · en "+remFmtDur(it.At-now)+"\n   "+remCut(it.Text, 120))
	}
	text := "⏰ *Pendientes (" + strconv.Itoa(len(mine)) + "):*\n\n" + strings.Join(lines, "\n\n")
	return ctx.SendQuoted(core.TextMessage(text), ctx.Message)
}

func remDel(ctx *core.Context, jid string, args []string) error {
	num := 0
	if len(args) > 1 {
		num = remParseIntPrefix(args[1])
	}
	if num < 1 {
		_ = ctx.React("⚠️")
		return ctx.SendQuoted(core.TextMessage("Uso: `.rem del <número>` (ver la lista con `.rem list`)"), ctx.Message)
	}

	mine := remMine(jid)
	if num > len(mine) {
		_ = ctx.React("❌")
		return ctx.SendQuoted(core.TextMessage("No existe el recordatorio #"+strconv.Itoa(num)+". Mira la lista con `.rem list`."), ctx.Message)
	}
	target := mine[num-1]

	remMu.Lock()
	kept := make([]remItem, 0, len(remState.Items))
	for _, it := range remState.Items {
		if it.ID != target.ID {
			kept = append(kept, it)
		}
	}
	remState.Items = kept
	if t, ok := remTimers[target.ID]; ok && t != nil {
		t.Stop()
	}
	delete(remTimers, target.ID)
	remMu.Unlock()
	remSave()

	_ = ctx.React("🗑️")
	return ctx.SendQuoted(core.TextMessage("🗑️ Eliminado: \""+remCut(target.Text, 80)+"\""), ctx.Message)
}

func remClear(ctx *core.Context, jid string) error {
	remMu.Lock()
	before := len(remState.Items)
	for _, it := range remState.Items {
		if it.Chat == jid {
			if t, ok := remTimers[it.ID]; ok && t != nil {
				t.Stop()
			}
			delete(remTimers, it.ID)
		}
	}
	kept := make([]remItem, 0, len(remState.Items))
	for _, it := range remState.Items {
		if it.Chat != jid {
			kept = append(kept, it)
		}
	}
	remState.Items = kept
	removed := before - len(remState.Items)
	remMu.Unlock()
	remSave()

	text := "No tenías recordatorios."
	if removed > 0 {
		text = "🗑️ " + strconv.Itoa(removed) + " recordatorio(s) eliminados."
	}
	return ctx.SendQuoted(core.TextMessage(text), ctx.Message)
}

func remCreate(ctx *core.Context, jid string, args []string) error {
	parsed, ok := remParseWhen(strings.Join(args, " "))
	if !ok || parsed.NeedTime {
		_ = ctx.React("⚠️")
		motivo := "No entendí el tiempo"
		if ok && parsed.NeedTime {
			motivo = "Falta la hora (ej: `.rem mañana 9am texto`)"
		}
		return ctx.SendQuoted(core.TextMessage("❌ "+motivo+".\n\n"+remHelp), ctx.Message)
	}

	text := remCut(parsed.Text, remMaxText)
	if text == "" {
		_ = ctx.React("⚠️")
		return ctx.SendQuoted(core.TextMessage("❌ Falta el texto del recordatorio.\nEjemplo: `.rem 30m llamar a mamá`\n\n"+remHelp), ctx.Message)
	}

	delta := parsed.At - remNow().UnixMilli()
	if delta < remMinMS || delta > remMaxMS {
		_ = ctx.React("⚠️")
		return ctx.SendQuoted(core.TextMessage("❌ El tiempo debe estar entre 5 segundos y 5 años."), ctx.Message)
	}

	remMu.Lock()
	enEsteChat := 0
	for _, it := range remState.Items {
		if it.Chat == jid {
			enEsteChat++
		}
	}
	if enEsteChat >= remMaxPerChat {
		remMu.Unlock()
		_ = ctx.React("🚫")
		return ctx.SendQuoted(core.TextMessage("Límite alcanzado ("+strconv.Itoa(remMaxPerChat)+" por chat). Borra algunos con `.rem del N`."), ctx.Message)
	}

	item := remItem{
		ID:      remState.NextID,
		Chat:    jid,
		Text:    text,
		At:      parsed.At,
		Created: remNow().UnixMilli(),
	}
	remState.NextID++
	remState.Items = append(remState.Items, item)
	remMu.Unlock()
	remSave()
	remSchedule(item)

	return ctx.SendQuoted(core.TextMessage("⏰ Anotado para "+remFmtDate(item.At)+" *(en "+remFmtDur(delta)+")*:\n\""+text+"\""), ctx.Message)
}

// remParseIntPrefix imita parseInt(): acepta el prefijo numérico y descarta el resto.
func remParseIntPrefix(s string) int {
	s = strings.TrimSpace(s)
	i := 0
	neg := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	start := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == start {
		return 0
	}
	n, err := strconv.Atoi(s[start:i])
	if err != nil {
		return 0
	}
	if neg {
		return -n
	}
	return n
}

func remCut(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
