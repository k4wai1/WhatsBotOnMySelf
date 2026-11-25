// gato.go porta addons/gato.js: tamagotchi por ediciones de mensaje y
// reacciones. El arte vive en gato_sprites.go y los datos en gato_db.go.
package addons

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf16"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"whatsbot-go/internal/core"
	"whatsbot-go/internal/store"
)

const (
	gatoInactividadMS     = 40000
	gatoParpadeoMS        = 7000
	gatoSiestaMS          = 3 * 3600000
	gatoCooldownCariciaMS = 60000
	gatoCooldownBanioMS   = 300000
	gatoEnergiaBanio      = 10
	gatoRazonInactividad  = "/ᐠ - ˕ -マ Ⳋ n/ > Mucha inactividad, me aburrí."
	gatoRazonOtroChat     = "/ᐠ - ˕ -マ Ⳋ n/ > Me llamaste desde otro chat. Cerrando esta pantalla."
	gatoTextoControl      = "> ✨ Reacciona con 👍 ❤️ 😂 😮 😢 🙌 para interactuar"
)

// Temporizadores inyectables en pruebas.
var (
	gatoDelay = time.Sleep
	gatoAfter = func(d time.Duration, fn func()) func() {
		timer := time.AfterFunc(d, fn)
		return func() { timer.Stop() }
	}
	gatoEvery = func(d time.Duration, fn func()) func() {
		done := make(chan struct{})
		var once sync.Once
		go func() {
			ticker := time.NewTicker(d)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					fn()
				case <-done:
					return
				}
			}
		}()
		return func() { once.Do(func() { close(done) }) }
	}
)

type gatoGame struct {
	chatJID   types.JID
	userID    string
	displayID string
	send      func(*waE2E.Message) (string, error)
	edit      func(id, text string) error
	stopLoop  func()
	stopTimer func()

	mu          sync.Mutex
	isAnimating bool
	lastText    string
}

func (g *gatoGame) animating() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.isAnimating
}

func (g *gatoGame) setAnimating(v bool) {
	g.mu.Lock()
	g.isAnimating = v
	g.mu.Unlock()
}

func (g *gatoGame) getLastText() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.lastText
}

func (g *gatoGame) setLastText(v string) {
	g.mu.Lock()
	g.lastText = v
	g.mu.Unlock()
}

var (
	gatoMu    sync.RWMutex
	gatoGames = make(map[string]*gatoGame)
)

// Gato porta el comando .gato y sus subcomandos de renombrado.
type Gato struct{}

// Commands cumple core.Addon.
func (Gato) Commands() []string { return []string{"gato"} }

func init() {
	core.Register(Gato{})
	core.RegisterBackground("gato", gatoInit)
}

// Handle inicia el juego o renombra la mascota.
func (g Gato) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}
	if len(ctx.Args) > 0 {
		return gatoRenombrar(ctx, ctx.Args)
	}
	if err := gatoIniciarJuego(ctx); err != nil {
		fmt.Printf("Error en el gato: %v\n", err)
		return ctx.React("❌")
	}
	return nil
}

func gatoInit(client *whatsmeow.Client, _ *store.Store) error {
	if client == nil {
		return nil
	}
	client.AddEventHandler(func(evt any) {
		msg, ok := evt.(*events.Message)
		if !ok || msg == nil || msg.Message == nil {
			return
		}
		if msg.Message.GetReactionMessage() == nil && msg.Message.GetEncReactionMessage() == nil {
			return
		}
		if err := gatoHandleReaction(client, msg); err != nil {
			fmt.Printf("Error procesando reacción del gato: %v\n", err)
		}
	})
	return nil
}

// ─── Render ────────────────────────────────────────────────────────────────

var gatoUIAcciones = map[string][][2]string{
	"Sala Principal": {
		{"👍", "Estado"}, {"❤️", "Cocina"}, {"😂", "Caricias"},
		{"😮", "Baño"}, {"😢", "Siesta 3h"}, {"🙌", "Nombre"},
	},
	"Cocina": {
		{"👍", "Sala"}, {"❤️", "Alimentar"},
	},
}

func gatoFmtMult(m float64) string {
	return "×" + strconv.FormatFloat(math.Round(m*100)/100, 'f', -1, 64)
}

func gatoHoraDespertar(cat *gatoCat) string {
	return time.UnixMilli(cat.Siesta.Despierta).Local().Format("15:04")
}

func gatoEstaDurmiendo(cat *gatoCat) bool {
	return cat.Siesta != nil && cat.Siesta.Despierta > gatoNow().UnixMilli()
}

func gatoRenderScreen(cat *gatoCat, asciiArt, nota string) string {
	s := cat.Stats
	dormido := gatoEstaDurmiendo(cat)
	nivel := cat.Economia.Nivel

	var text strings.Builder
	fmt.Fprintf(&text, "🐾 *%s* · Lv.%d · 📍 *%s*\n", cat.Name, nivel, cat.Scene)
	fmt.Fprintf(&text, "🍖 %d%% ⚡ %d%% 💖 %d%%\n",
		int(math.Round(s.Hambre)), int(math.Round(s.Energia)), int(math.Round(s.Felicidad)))

	if dormido {
		fmt.Fprintf(&text, "💤 Duerme hasta %s\n", gatoHoraDespertar(cat))
	} else {
		fmt.Fprintf(&text, "⭐ %d/%d · 🐟 %d · 🔥 %dd %s\n",
			cat.Economia.XP, nivel*100, cat.Economia.Peces, cat.Racha.Dias, gatoFmtMult(gatoMultiplicador(cat)))
	}

	if nota != "" {
		text.WriteString(nota + "\n")
	}

	if !dormido {
		acciones := gatoUIAcciones[cat.Scene]
		if acciones == nil {
			acciones = gatoUIAcciones["Sala Principal"]
		}
		parts := make([]string, 0, len(acciones))
		for _, accion := range acciones {
			parts = append(parts, accion[0]+"("+accion[1]+")")
		}
		text.WriteString("Acciones: " + strings.Join(parts, " | ") + "\n")
	}

	text.WriteString("\n```\n")
	text.WriteString("◈" + strings.Repeat("―", 20) + "◈\n")
	artLines := strings.Split(asciiArt, "\n")
	startRow := 10 - len(artLines)
	for i := 0; i < 10; i++ {
		if i >= startRow && asciiArt != "" {
			text.WriteString("│" + gatoPadEnd(artLines[i-startRow], 20) + "│\n")
		} else {
			text.WriteString("│" + strings.Repeat(" ", 20) + "│\n")
		}
	}
	text.WriteString("◇" + strings.Repeat("―", 20) + "◇\n```")
	return text.String()
}

func gatoPadEnd(s string, width int) string {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	if n >= width {
		return s
	}
	return s + strings.Repeat(" ", width-n)
}

func gatoArteActual(cat *gatoCat) string {
	if gatoEstaDurmiendo(cat) {
		return gatoDormirReposo(0)
	}
	if cat.Scene == "Cocina" {
		return gatoCocinaReposo
	}
	if arte, ok := gatoSalaArte(gatoAnimo(cat)); ok {
		return arte
	}
	return gatoSalaReposo
}

func gatoPantalla(game *gatoGame, cat *gatoCat, arte, nota string) {
	texto := gatoRenderScreen(cat, arte, nota)
	if texto == game.getLastText() {
		return
	}
	if err := game.edit(game.displayID, texto); err == nil {
		game.setLastText(texto)
	}
}

// ─── Juego ─────────────────────────────────────────────────────────────────

func gatoGet(gameID string) *gatoGame {
	gatoMu.RLock()
	defer gatoMu.RUnlock()
	return gatoGames[gameID]
}

func gatoHasSession(userID string) bool {
	gatoMu.RLock()
	defer gatoMu.RUnlock()
	for _, game := range gatoGames {
		if game.userID == userID {
			return true
		}
	}
	return false
}

func gatoResetTimer(gameID string) {
	game := gatoGet(gameID)
	if game == nil {
		return
	}
	if game.stopTimer != nil {
		game.stopTimer()
	}
	game.stopTimer = gatoAfter(gatoInactividadMS*time.Millisecond, func() {
		gatoCloseGame(gameID, gatoRazonInactividad)
	})
}

func gatoCloseGame(gameID, razon string) {
	gatoMu.Lock()
	game := gatoGames[gameID]
	if game != nil {
		delete(gatoGames, gameID)
	}
	gatoMu.Unlock()
	if game == nil {
		return
	}
	if game.stopLoop != nil {
		game.stopLoop()
	}
	if game.stopTimer != nil {
		game.stopTimer()
	}
	if err := game.edit(game.displayID, razon); err != nil {
		_ = err
	}
}

func gatoCloseUserGames(userID, razon string) {
	gatoMu.RLock()
	ids := make([]string, 0)
	for id, game := range gatoGames {
		if game.userID == userID {
			ids = append(ids, id)
		}
	}
	gatoMu.RUnlock()
	for _, id := range ids {
		gatoCloseGame(id, razon)
	}
}

func gatoUserId(ctx *core.Context) string {
	sender := ctx.SenderJID()
	if sender.IsEmpty() {
		sender = ctx.ChatJID()
	}
	return strings.SplitN(sender.String(), "@", 2)[0]
}

func gatoIniciarJuego(ctx *core.Context) error {
	chatJID := ctx.ChatJID()
	userID := gatoUserId(ctx)
	gatoCloseUserGames(userID, gatoRazonOtroChat)

	db := gatoCargar()
	cat := db[userID]
	if cat == nil {
		cat = gatoCrear(userID)
		db[userID] = cat
	}
	gatoNormalizar(cat)
	gatoAplicarDecay(cat)
	gatoRegistrarRacha(cat)
	cat.LastUpdate = gatoNow().UnixMilli()
	gatoGuardar(db)

	send, edit := gatoContextSender(ctx, chatJID)
	displayText := gatoRenderScreen(cat, gatoArteActual(cat), "")
	displayID, err := send(core.TextMessage(displayText))
	if err != nil {
		return err
	}
	controlID, err := send(core.TextMessage(gatoTextoControl))
	if err != nil {
		return err
	}

	game := &gatoGame{
		chatJID:   chatJID,
		userID:    userID,
		displayID: displayID,
		send:      send,
		edit:      edit,
	}
	gatoMu.Lock()
	gatoGames[controlID] = game
	gatoMu.Unlock()

	game.stopLoop = gatoEvery(gatoParpadeoMS*time.Millisecond, func() { gatoIdleTick(controlID) })
	game.stopTimer = gatoAfter(gatoInactividadMS*time.Millisecond, func() {
		gatoCloseGame(controlID, gatoRazonInactividad)
	})
	return nil
}

func gatoContextSender(ctx *core.Context, chatJID types.JID) (func(*waE2E.Message) (string, error), func(string, string) error) {
	send := func(msg *waE2E.Message) (string, error) {
		if ctx.Client != nil {
			resp, err := ctx.Client.SendMessage(context.Background(), chatJID, msg)
			return resp.ID, err
		}
		if ctx.SendHook != nil {
			return "", ctx.SendHook(msg)
		}
		return "", nil
	}
	edit := func(id, text string) error {
		if ctx.Client == nil || id == "" {
			return nil
		}
		_, err := ctx.Client.SendMessage(context.Background(), chatJID,
			ctx.Client.BuildEdit(chatJID, types.MessageID(id), core.TextMessage(text)))
		return err
	}
	return send, edit
}

func gatoClientSender(client *whatsmeow.Client, chatJID types.JID) (func(*waE2E.Message) (string, error), func(string, string) error) {
	send := func(msg *waE2E.Message) (string, error) {
		if client == nil {
			return "", nil
		}
		resp, err := client.SendMessage(context.Background(), chatJID, msg)
		return resp.ID, err
	}
	edit := func(id, text string) error {
		if client == nil || id == "" {
			return nil
		}
		_, err := client.SendMessage(context.Background(), chatJID,
			client.BuildEdit(chatJID, types.MessageID(id), core.TextMessage(text)))
		return err
	}
	return send, edit
}

func gatoIdleTick(gameID string) {
	game := gatoGet(gameID)
	if game == nil || game.animating() {
		return
	}

	db := gatoCargar()
	cat := db[game.userID]
	if cat == nil {
		return
	}

	if gatoEstaDurmiendo(cat) {
		if gatoNow().UnixMilli() >= cat.Siesta.Despierta {
			_ = gatoDespertar(game, db, cat)
			return
		}
		for i := 0; i < 3; i++ {
			g2 := gatoGet(gameID)
			if g2 == nil || g2.animating() {
				return
			}
			db2 := gatoCargar()
			c2 := db2[g2.userID]
			if c2 == nil || !gatoEstaDurmiendo(c2) {
				return
			}
			gatoPantalla(g2, c2, gatoDormirReposo(i), "")
			gatoDelay(1800 * time.Millisecond)
		}
		return
	}

	animo := gatoAnimo(cat)
	escena := gatoSalaReposo
	parpadeo := gatoSalaParpadeo
	if cat.Scene == "Cocina" {
		escena = gatoCocinaReposo
		parpadeo = gatoCocinaParpadeo
	}
	_ = escena
	if animo != "" {
		return
	}

	gatoPantalla(game, cat, parpadeo, "")
	gatoDelay(500 * time.Millisecond)

	g2 := gatoGet(gameID)
	if g2 == nil || g2.animating() {
		return
	}
	db2 := gatoCargar()
	c2 := db2[g2.userID]
	if c2 == nil {
		return
	}
	gatoNormalizar(c2)
	gatoPantalla(g2, c2, gatoArteActual(c2), "")
}

// ─── Acciones ──────────────────────────────────────────────────────────────

type gatoAccion func(*gatoGame, map[string]*gatoCat, *gatoCat) error

var gatoMenus = map[string]map[string]gatoAccion{
	"Sala Principal": {
		"👍":  gatoRefrescar,
		"❤️": gatoIrACocina,
		"😂":  gatoAcariciar,
		"😮":  gatoBaniar,
		"😢":  gatoDormir,
		"🙌":  gatoPedirRenombre,
	},
	"Cocina": {
		"👍":  gatoIrASala,
		"❤️": gatoAlimentar,
	},
}

func gatoRefrescar(game *gatoGame, _ map[string]*gatoCat, cat *gatoCat) error {
	gatoPantalla(game, cat, gatoArteActual(cat), "")
	return nil
}

func gatoIrACocina(game *gatoGame, db map[string]*gatoCat, cat *gatoCat) error {
	if cat.Scene == "Cocina" {
		return nil
	}
	cat.Scene = "Cocina"
	gatoGuardar(db)
	gatoPantalla(game, cat, gatoCocinaReposo, "")
	return nil
}

func gatoIrASala(game *gatoGame, db map[string]*gatoCat, cat *gatoCat) error {
	if cat.Scene != "Cocina" {
		return nil
	}
	cat.Scene = "Sala Principal"
	gatoGuardar(db)
	gatoPantalla(game, cat, gatoArteActual(cat), "")
	return nil
}

func gatoAlimentar(game *gatoGame, db map[string]*gatoCat, cat *gatoCat) error {
	game.setAnimating(true)
	defer game.setAnimating(false)

	cat.Scene = "Comiendo..."
	food := gatoComidas[randIntN(len(gatoComidas))]

	gatoPantalla(game, cat, gatoCocinaComer1(food), "")
	gatoDelay(time.Second)
	gatoPantalla(game, cat, gatoCocinaComer2(food), "")
	gatoDelay(time.Second)

	if food == "🍋" {
		cat.Stats.Hambre = gatoClamp(cat.Stats.Hambre + 1)
		bocas := []string{"༝", "△", "ᯅ", "Д", "‸"}
		for i := 0; i < 6; i++ {
			boca := bocas[randIntN(len(bocas))]
			cola := "ノ"
			if i%2 == 0 {
				cola = "/"
			}
			gatoPantalla(game, cat, gatoCocinaLimon(boca, cola), "")
			gatoDelay(500 * time.Millisecond)
		}
	} else {
		cat.Stats.Hambre = gatoClamp(cat.Stats.Hambre + 20)
		bocas := []string{"⤙", "~", "-"}
		for i := 0; i < 10; i++ {
			gatoPantalla(game, cat, gatoCocinaMasticar(bocas[i%3]), "")
			gatoDelay(500 * time.Millisecond)
		}
	}

	cat.Scene = "Cocina"
	cat.LastUpdate = gatoNow().UnixMilli()
	var ganado *gatoGanado
	if gatoMarcarDiario(cat, "comida") {
		g := gatoOtorgar(cat, 10, 2)
		ganado = &g
	}
	gatoGuardar(db)
	gatoCelebrarSiSube(game, cat, ganado)
	gatoPantalla(game, cat, gatoCocinaReposo, "")
	return nil
}

func gatoAcariciar(game *gatoGame, db map[string]*gatoCat, cat *gatoCat) error {
	ahora := gatoNow().UnixMilli()
	if ahora-cat.UltimaCaricia < gatoCooldownCariciaMS {
		return gatoAviso(game, cat, gatoSalaFeliz, "🫶 Aún está lleno de mimos...")
	}
	cat.UltimaCaricia = ahora
	game.setAnimating(true)
	defer game.setAnimating(false)

	gatoPantalla(game, cat, gatoSalaCaricia(0), "")
	gatoDelay(800 * time.Millisecond)
	gatoPantalla(game, cat, gatoSalaCaricia(1), "")
	gatoDelay(800 * time.Millisecond)

	var ganado *gatoGanado
	if cat.Stats.Felicidad >= 98 || citaRand() < 0.10 {
		cat.Stats.Felicidad = gatoClamp(cat.Stats.Felicidad - 3)
		gatoGuardar(db)
		gatoPantalla(game, cat, gatoSalaEnfadado, "💢 ¡Te arañó! Ya tenía suficientes mimos.")
		gatoDelay(1800 * time.Millisecond)
	} else {
		cat.Stats.Felicidad = gatoClamp(cat.Stats.Felicidad + 4)
		if gatoMarcarDiario(cat, "caricia") {
			g := gatoOtorgar(cat, 6, 1)
			ganado = &g
		}
		gatoGuardar(db)
	}
	gatoCelebrarSiSube(game, cat, ganado)
	return nil
}

func gatoBaniar(game *gatoGame, db map[string]*gatoCat, cat *gatoCat) error {
	if cat.Stats.Energia < gatoEnergiaBanio {
		return gatoAviso(game, cat, gatoSalaTriste,
			fmt.Sprintf("⚡ Demasiado cansado para bañarse (necesita %d%% de energía).", gatoEnergiaBanio))
	}
	ahora := gatoNow().UnixMilli()
	if ahora-cat.UltimaBanio < gatoCooldownBanioMS {
		return gatoAviso(game, cat, gatoSalaEnfadado, "🛁 Recién bañado, déjalo un rato.")
	}
	cat.UltimaBanio = ahora
	game.setAnimating(true)
	defer game.setAnimating(false)

	for i := 0; i < 5; i++ {
		gatoPantalla(game, cat, gatoBanioBurbujas(i), "")
		gatoDelay(650 * time.Millisecond)
	}
	cat.Stats.Energia = gatoClamp(cat.Stats.Energia - gatoEnergiaBanio)
	cat.Stats.Felicidad = gatoClamp(cat.Stats.Felicidad + 5)
	cat.LastUpdate = gatoNow().UnixMilli()
	var ganado *gatoGanado
	if gatoMarcarDiario(cat, "baño") {
		g := gatoOtorgar(cat, 8, 2)
		ganado = &g
	}
	gatoGuardar(db)
	gatoCelebrarSiSube(game, cat, ganado)
	return nil
}

func gatoDormir(game *gatoGame, db map[string]*gatoCat, cat *gatoCat) error {
	if gatoEstaDurmiendo(cat) {
		return nil
	}
	ahora := gatoNow().UnixMilli()
	cat.Siesta = &gatoSiesta{Inicio: ahora, Despierta: ahora + gatoSiestaMS}
	gatoGuardar(db)
	gatoPantalla(game, cat, gatoDormirReposo(0), "")
	return nil
}

func gatoDespertar(game *gatoGame, db map[string]*gatoCat, cat *gatoCat) error {
	if cat.Siesta == nil {
		return nil
	}
	inicio := cat.Siesta.Inicio
	if inicio == 0 {
		inicio = cat.Siesta.Despierta - gatoSiestaMS
	}
	fraccion := float64(gatoNow().UnixMilli()-inicio) / float64(gatoSiestaMS)
	if fraccion > 1 {
		fraccion = 1
	}
	if fraccion < 0 {
		fraccion = 0
	}
	cat.Stats.Energia = gatoClamp(cat.Stats.Energia + fraccion*100)
	cat.Siesta = nil
	cat.LastUpdate = gatoNow().UnixMilli()
	gatoGuardar(db)
	gatoPantalla(game, cat, gatoArteActual(cat), "")
	return nil
}

func gatoPedirRenombre(game *gatoGame, _ map[string]*gatoCat, cat *gatoCat) error {
	game.setAnimating(true)
	defer game.setAnimating(false)
	gatoPantalla(game, cat, gatoArteActual(cat), "✏️ Escribe ,gato <nombre> para renombrarlo (máx 10 caracteres).")
	gatoDelay(6 * time.Second)
	gatoPantalla(game, cat, gatoArteActual(cat), "")
	return nil
}

func gatoAviso(game *gatoGame, cat *gatoCat, arte, texto string) error {
	game.setAnimating(true)
	defer game.setAnimating(false)
	gatoPantalla(game, cat, arte, texto)
	gatoDelay(2200 * time.Millisecond)
	gatoPantalla(game, cat, gatoArteActual(cat), "")
	return nil
}

func gatoCelebrarSiSube(game *gatoGame, cat *gatoCat, ganado *gatoGanado) {
	if ganado != nil && ganado.Subio {
		gatoPantalla(game, cat, gatoArteActual(cat),
			fmt.Sprintf("🎉 ¡%s subió a Lv.%d!", cat.Name, cat.Economia.Nivel))
		gatoDelay(2500 * time.Millisecond)
	}
	gatoPantalla(game, cat, gatoArteActual(cat), "")
}

// ─── Reacciones y renombrado ───────────────────────────────────────────────

func gatoHandleReaction(client *whatsmeow.Client, msg *events.Message) error {
	if msg == nil || msg.Message == nil {
		return nil
	}
	reaction := msg.Message.GetReactionMessage()
	if reaction == nil {
		if msg.Message.GetEncReactionMessage() != nil && client != nil {
			decrypted, err := client.DecryptReaction(context.Background(), msg)
			if err != nil {
				return nil
			}
			reaction = decrypted
		}
	}
	if reaction == nil {
		return nil
	}

	targetID := reaction.GetKey().GetID()
	if targetID == "" {
		return nil
	}
	game := gatoGet(targetID)
	if game == nil {
		return nil
	}

	reactor := msg.Info.Sender
	if reactor.IsEmpty() {
		reactor = msg.Info.Chat
	}
	reactorID := strings.SplitN(reactor.String(), "@", 2)[0]
	if reactorID != game.userID || game.animating() {
		return nil
	}

	emoji := reaction.GetText()
	if emoji == "" {
		return nil
	}

	gatoResetTimer(targetID)
	db := gatoCargar()
	cat := db[game.userID]
	if cat == nil {
		gatoCloseGame(targetID, gatoRazonInactividad)
		return nil
	}

	gatoAplicarDecay(cat)
	gatoGuardar(db)

	if gatoEstaDurmiendo(cat) {
		if emoji != "👍" {
			return nil
		}
		return gatoDespertar(game, db, cat)
	}

	accion, ok := gatoMenus[cat.Scene][emoji]
	if !ok {
		return nil
	}
	if err := accion(game, db, cat); err != nil {
		fmt.Printf("Error en acción del gato: %v\n", err)
		game.setAnimating(false)
	}
	return nil
}

func gatoSanitizarNombre(texto string) string {
	var b strings.Builder
	for _, r := range texto {
		switch r {
		case '\\', '*', '_', '`', '~', '[', ']', '\n':
			continue
		}
		if unicode.IsLetter(r) || unicode.IsNumber(r) || r == ' ' {
			b.WriteRune(r)
		}
	}
	limpio := strings.Join(strings.Fields(b.String()), " ")
	return qrTruncate(limpio, 10)
}

func gatoRenombrar(ctx *core.Context, args []string) error {
	userID := gatoUserId(ctx)
	nombre := gatoSanitizarNombre(strings.Join(args, " "))
	if nombre == "" {
		return ctx.React("⚠️")
	}

	db := gatoCargar()
	cat := db[userID]
	if cat == nil {
		cat = gatoCrear(userID)
		db[userID] = cat
	}
	gatoNormalizar(cat)
	cat.Name = nombre
	gatoGuardar(db)

	if gatoHasSession(userID) {
		return ctx.React("✅")
	}
	return ctx.Reply("🐾 Tu michi ahora se llama *" + nombre + "*.")
}

// gatoSendForClient se conserva para que el sender de cliente sea reutilizable
// al construir juegos desde reacciones de otro chat.
var _ = gatoClientSender

var _ = proto.String
