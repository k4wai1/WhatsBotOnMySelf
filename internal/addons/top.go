package addons

import (
	"fmt"
	"log/slog"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"whatsbot-go/internal/core"
	"whatsbot-go/internal/store"
)

const (
	topHeartbeatMinutos = 5
	topMaxChars         = 4000
)

type topUsuario struct {
	Name     string `json:"name"`
	Msg      int    `json:"msg"`
	Sticker  int    `json:"sticker"`
	Image    int    `json:"image"`
	Video    int    `json:"video"`
	Audio    int    `json:"audio"`
	Reaction int    `json:"reaction"`
	Char     int    `json:"char"`
}

type topStats struct {
	UptimeMinutos int                    `json:"uptime_minutes"`
	Users         map[string]*topUsuario `json:"users"`
}

type topSpam struct {
	LastUser string
	Count    int
}

var (
	topStatsDir = func() string { return core.DataPath("assets", "statistics") }
	topNow      = time.Now
	topInterval = topHeartbeatMinutos * time.Minute
	topMu       sync.Mutex
	topSpamMap  = map[string]*topSpam{}
	topGroupMu  sync.Mutex
)

// Top porta addons/top.js: .ongrup activa la recolección y .top muestra rankings.
type Top struct{}

func init() {
	core.Register(Top{})
	core.RegisterBackground("top", topInit)
}

func (Top) Commands() []string { return []string{"ongrup", "top"} }

func topActivePath() string { return filepath.Join(topStatsDir(), "active_groups.json") }

func topStatsPath(chatID, mes string) string {
	if mes == "" {
		mes = topNow().Format("2006-01")
	}
	return filepath.Join(topStatsDir(), chatID+"_"+mes+".json")
}

func topActiveGroups() []string {
	var grupos []string
	if err := core.ReadJSON(topActivePath(), &grupos); err != nil {
		return []string{}
	}
	if grupos == nil {
		return []string{}
	}
	return grupos
}

func topGuardarActivos(grupos []string) error {
	if grupos == nil {
		grupos = []string{}
	}
	return core.WriteJSON(topActivePath(), grupos)
}

func topCargarStats(chatID, mes string) *topStats {
	var stats topStats
	if err := core.ReadJSON(topStatsPath(chatID, mes), &stats); err != nil {
		return nil
	}
	if stats.Users == nil {
		stats.Users = map[string]*topUsuario{}
	}
	return &stats
}

func topGuardarStats(chatID string, stats *topStats) error {
	return core.WriteJSON(topStatsPath(chatID, ""), stats)
}

func topNombreGenerico(nombre string) bool {
	return nombre == "" || nombre == "Usuario" || nombre == "Anónimo"
}

// topUpdateStats replica updateStats() de top.js.
func topUpdateStats(chatID, userID, tipo, nombre string, amount int) {
	if !wagoContains(topActiveGroups(), chatID) {
		return
	}
	topMu.Lock()
	defer topMu.Unlock()

	stats := topCargarStats(chatID, "")
	if stats == nil {
		stats = &topStats{UptimeMinutos: 0, Users: map[string]*topUsuario{}}
	}
	if stats.Users == nil {
		stats.Users = map[string]*topUsuario{}
	}
	usuario, ok := stats.Users[userID]
	if !ok {
		usuario = &topUsuario{Name: "Usuario"}
		stats.Users[userID] = usuario
	}
	if !topNombreGenerico(nombre) {
		usuario.Name = nombre
	}
	switch tipo {
	case "msg":
		usuario.Msg += amount
	case "sticker":
		usuario.Sticker += amount
	case "image":
		usuario.Image += amount
	case "video":
		usuario.Video += amount
	case "audio":
		usuario.Audio += amount
	case "reaction":
		usuario.Reaction += amount
	case "char":
		usuario.Char += amount
	}
	_ = topGuardarStats(chatID, stats)
}

func topValor(u *topUsuario, categoria string) int {
	switch categoria {
	case "sticker":
		return u.Sticker
	case "image":
		return u.Image
	case "video":
		return u.Video
	case "audio":
		return u.Audio
	case "reaction":
		return u.Reaction
	case "char":
		return u.Char
	default:
		return u.Msg
	}
}

func topCategoria(arg string) (string, bool) {
	switch arg {
	case "mensajes", "msg":
		return "msg", true
	case "stickers", "sticker":
		return "sticker", true
	case "imagenes", "imagen", "fotos":
		return "image", true
	case "videos", "video":
		return "video", true
	case "reacciones", "reaccion":
		return "reaction", true
	case "caracteres", "letras", "char", "texto":
		return "char", true
	case "audios", "audio", "notas", "vn":
		return "audio", true
	}
	return "", false
}

// topEsComandoStats replica isStatsCommand() con los prefijos configurados.
func topPrefijos() []string {
	prefijos := []string{",", "!", "/"}
	var cfg struct {
		Prefixes []string `json:"prefixes"`
	}
	if err := core.ReadJSON(core.DataPath("assets", "config.json"), &cfg); err == nil && len(cfg.Prefixes) > 0 {
		prefijos = cfg.Prefixes
	}
	return prefijos
}

func topEsComandoStats(texto string) bool {
	prefijos := topPrefijos()
	t := strings.ToLower(strings.TrimSpace(texto))
	for _, p := range prefijos {
		p = strings.ToLower(p)
		if p == "" || !strings.HasPrefix(t, p) {
			continue
		}
		resto := strings.Fields(strings.TrimSpace(t[len(p):]))
		if len(resto) == 0 {
			continue
		}
		if resto[0] == "ongrup" || resto[0] == "top" {
			return true
		}
	}
	return false
}

func topUptimeTexto(stats *topStats, targetMonth string, ahora time.Time) string {
	if stats.UptimeMinutos == 0 {
		return ""
	}
	posibles := 1
	if targetMonth != "" {
		if t, err := time.Parse("2006-01", targetMonth); err == nil {
			posibles = time.Date(t.Year(), t.Month()+1, 0, 0, 0, 0, 0, ahora.Location()).Day() * 24 * 60
		}
	} else {
		inicio := time.Date(ahora.Year(), ahora.Month(), 1, 0, 0, 0, 0, ahora.Location())
		posibles = int(ahora.Sub(inicio).Minutes())
	}
	if posibles < 1 {
		posibles = 1
	}
	porcentaje := float64(stats.UptimeMinutos) / float64(posibles) * 100
	if porcentaje > 100 {
		porcentaje = 100
	}
	texto := strconv.FormatFloat(porcentaje, 'f', 2, 64)
	llenos := int(math.Round(porcentaje / 10))
	if llenos > 10 {
		llenos = 10
	}
	if llenos < 0 {
		llenos = 0
	}
	barra := strings.Repeat("🟩", llenos) + strings.Repeat("⬛", 10-llenos)
	return fmt.Sprintf("\n⚡ *Uptime Bot:* %s%%\n%s (%d min activos)\n", texto, barra, stats.UptimeMinutos)
}

func topFormatoMiles(n int) string {
	s := strconv.Itoa(n)
	neg := false
	if strings.HasPrefix(s, "-") {
		neg = true
		s = s[1:]
	}
	var partes []string
	for len(s) > 3 {
		partes = append([]string{s[len(s)-3:]}, partes...)
		s = s[:len(s)-3]
	}
	partes = append([]string{s}, partes...)
	out := strings.Join(partes, ".")
	if neg {
		out = "-" + out
	}
	return out
}

func topLimite(ctx *core.Context) int {
	info, err := wagoGroupInfo(ctx)
	if err != nil || info == nil || len(info.Participants) == 0 {
		return 10
	}
	return int(math.Ceil(float64(len(info.Participants)) / 2))
}

func (Top) Handle(ctx *core.Context) error {
	if ctx == nil || !wagoIsGroup(ctx) {
		return nil
	}
	chatID := ctx.ChatJID().String()

	if ctx.Command == "ongrup" {
		activos := topActiveGroups()
		if wagoContains(activos, chatID) {
			return ctx.Reply("ℹ️ Las estadísticas ya están activas.")
		}
		if err := topGuardarActivos(append(activos, chatID)); err != nil {
			slog.Error("❌ [top] Error:", "error", err)
			return ctx.React("❌")
		}
		return ctx.Reply("✅ *Estadísticas activadas.*")
	}

	targetMonth := ""
	categoria := "msg"
	for _, arg := range ctx.Args {
		a := strings.ToLower(arg)
		if len(a) == 7 && a[4] == '-' {
			if _, err := time.Parse("2006-01", a); err == nil {
				targetMonth = a
				continue
			}
		}
		if cat, ok := topCategoria(a); ok {
			categoria = cat
		}
	}

	stats := topCargarStats(chatID, targetMonth)
	if stats == nil {
		mes := targetMonth
		if mes == "" {
			mes = "este mes"
		}
		return ctx.Reply("📂 No hay datos para " + mes + ".")
	}

	uptime := topUptimeTexto(stats, targetMonth, topNow())
	limite := topLimite(ctx)

	titulos := map[string]string{
		"msg":      "💬 Top Mensajes",
		"sticker":  "👾 Top Stickers",
		"image":    "📸 Top Imágenes",
		"video":    "🎥 Top Videos",
		"audio":    "🎤 Top Audios",
		"reaction": "❤️ Top Reacciones",
		"char":     "📝 Top Caracteres",
	}
	titulo := titulos[categoria]
	if titulo == "" {
		titulo = "Top"
	}
	mes := targetMonth
	if mes == "" {
		mes = "Mes Actual"
	}

	type entrada struct {
		id    string
		user  *topUsuario
		valor int
	}
	entradas := make([]entrada, 0, len(stats.Users))
	for id, user := range stats.Users {
		entradas = append(entradas, entrada{id: id, user: user, valor: topValor(user, categoria)})
	}
	sort.SliceStable(entradas, func(i, j int) bool {
		if entradas[i].valor == entradas[j].valor {
			return entradas[i].id < entradas[j].id
		}
		return entradas[i].valor > entradas[j].valor
	})
	if len(entradas) > limite {
		entradas = entradas[:limite]
	}

	var b strings.Builder
	fmt.Fprintf(&b, "📊 *%s*\n👥 Mostrando top %d (50%% del grupo)\n📅 Fecha: %s%s\n\n", titulo, limite, mes, uptime)
	pos := 1
	for _, e := range entradas {
		if e.valor <= 0 {
			continue
		}
		medalla := strconv.Itoa(pos) + "."
		switch pos {
		case 1:
			medalla = "🥇"
		case 2:
			medalla = "🥈"
		case 3:
			medalla = "🥉"
		}
		fmt.Fprintf(&b, "%s %s: %s\n", medalla, e.user.Name, topFormatoMiles(e.valor))
		pos++
	}
	return ctx.Reply(b.String())
}

func topInit(client *whatsmeow.Client, _ *store.Store) error {
	go func() {
		ticker := time.NewTicker(topInterval)
		defer ticker.Stop()
		for range ticker.C {
			topHeartbeat()
		}
	}()
	if client == nil {
		return nil
	}
	client.AddEventHandler(func(evt any) {
		msg, ok := evt.(*events.Message)
		if ok && msg.Message != nil {
			topProcesarMensaje(client, msg)
		}
	})
	return nil
}

func topHeartbeat() {
	grupos := topActiveGroups()
	if len(grupos) == 0 {
		return
	}
	topMu.Lock()
	defer topMu.Unlock()
	for _, chatID := range grupos {
		stats := topCargarStats(chatID, "")
		if stats == nil {
			stats = &topStats{UptimeMinutos: 0, Users: map[string]*topUsuario{}}
		}
		stats.UptimeMinutos += topHeartbeatMinutos
		_ = topGuardarStats(chatID, stats)
	}
}

func topProcesarMensaje(client *whatsmeow.Client, msg *events.Message) {
	chatID := msg.Info.Chat.String()
	if msg.Info.Chat.Server != types.GroupServer || !wagoContains(topActiveGroups(), chatID) {
		return
	}
	userID := msg.Info.Sender.ToNonAD().String()
	nombre := msg.Info.PushName
	if msg.Info.IsFromMe {
		userID = wagoUserOf(clientOwnID(client)) + "@s.whatsapp.net"
		nombre = "Bot"
	} else if nombre == "" {
		nombre = "Usuario"
	}

	if msg.Message.GetReactionMessage() != nil {
		topUpdateStats(chatID, userID, "reaction", nombre, 1)
		return
	}

	texto := msg.Message.GetConversation()
	if texto == "" && msg.Message.GetExtendedTextMessage() != nil {
		texto = msg.Message.GetExtendedTextMessage().GetText()
	}
	if topEsComandoStats(texto) {
		return
	}

	tipo := "msg"
	switch {
	case msg.Message.GetStickerMessage() != nil:
		tipo = "sticker"
	case msg.Message.GetImageMessage() != nil:
		tipo = "image"
	case msg.Message.GetVideoMessage() != nil:
		tipo = "video"
	case msg.Message.GetAudioMessage() != nil:
		tipo = "audio"
	}
	if len([]rune(texto)) > topMaxChars {
		return
	}

	topMu.Lock()
	tracker := topSpamMap[chatID]
	if tracker == nil {
		tracker = &topSpam{}
		topSpamMap[chatID] = tracker
	}
	if tracker.LastUser == userID {
		tracker.Count++
		if tracker.Count > 5 {
			topMu.Unlock()
			return
		}
	} else {
		tracker.LastUser = userID
		tracker.Count = 1
	}
	topMu.Unlock()

	topUpdateStats(chatID, userID, tipo, nombre, 1)
	if tipo == "msg" && texto != "" {
		topUpdateStats(chatID, userID, "char", nombre, len(utf16.Encode([]rune(texto))))
	}
}

func clientOwnID(client *whatsmeow.Client) string {
	if client != nil && client.Store != nil && client.Store.ID != nil {
		return client.Store.ID.String()
	}
	return ""
}
