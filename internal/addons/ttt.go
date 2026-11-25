package addons

import (
	"context"
	"log/slog"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"whatsbot-go/internal/core"
	"whatsbot-go/internal/store"
)

type tttJuego struct {
	ChatID      string
	PlayerX     string
	PlayerO     string
	PlayerXName string
	PlayerOName string
	IsBot       bool
	Difficulty  string
	Board       [3][3]string
	Turn        string
	MsgID       string
	Timer       *time.Timer
	Finished    bool
}

type tttMove struct {
	R, C int
}

type tttEspera struct {
	ID   string
	Chat string
	Name string
}

var (
	tttMu          sync.Mutex
	tttGames       = map[string]*tttJuego{}
	tttWaiting     *tttEspera
	tttCoordRe     = regexp.MustCompile(`^(?:([abc])\s*([123])|([123])\s*([abc]))$`)
	tttBotDelay    = time.Second
	tttGameTimeout = 4 * time.Minute
)

// Ttt porta addons/ttt.js: tres en raya.
type Ttt struct{}

func init() {
	core.Register(Ttt{})
	core.RegisterBackground("ttt", tttInit)
}

func (Ttt) Commands() []string { return []string{"ttt"} }

func tttSimbolo(celda string) string {
	switch celda {
	case "X":
		return "❌"
	case "O":
		return "⭕"
	default:
		return "⬛"
	}
}

func (g *tttJuego) toString() string {
	var b strings.Builder
	b.WriteString("Hora de jugar ✨\n\n    🅰️   🅱️   ©️\n")
	for i := 0; i < 3; i++ {
		cells := make([]string, 0, 3)
		for j := 0; j < 3; j++ {
			cells = append(cells, tttSimbolo(g.Board[i][j]))
		}
		b.WriteString(strconv.Itoa(i+1) + "   " + strings.Join(cells, "  ") + "\n")
	}
	b.WriteString("\n❌ " + g.PlayerXName + "\n⭕ ")
	if g.IsBot {
		b.WriteString("Yo (Bot)")
	} else {
		b.WriteString(g.PlayerOName)
	}
	if g.Finished {
		b.WriteString("\n\n🏁 Ya se terminó 🏁")
	} else {
		nombre := g.PlayerXName
		if g.Turn == "O" {
			nombre = g.PlayerOName
		}
		if g.IsBot && g.Turn == "O" {
			nombre = "Yo (Bot)"
		}
		b.WriteString("\n\n🎯 Le toca a: " + nombre + "\n📝 Dime la coordenada (ej: a1, b3)")
	}
	return b.String()
}

func (g *tttJuego) play(row, col int) bool {
	if row < 0 || row > 2 || col < 0 || col > 2 || g.Board[row][col] != "" {
		return false
	}
	g.Board[row][col] = g.Turn
	return true
}

func (g *tttJuego) switchTurn() {
	if g.Turn == "X" {
		g.Turn = "O"
	} else {
		g.Turn = "X"
	}
}

func (g *tttJuego) getWinner() string {
	b := g.Board
	lineas := [][3]string{
		{b[0][0], b[0][1], b[0][2]}, {b[1][0], b[1][1], b[1][2]}, {b[2][0], b[2][1], b[2][2]},
		{b[0][0], b[1][0], b[2][0]}, {b[0][1], b[1][1], b[2][1]}, {b[0][2], b[1][2], b[2][2]},
		{b[0][0], b[1][1], b[2][2]}, {b[0][2], b[1][1], b[2][0]},
	}
	for _, linea := range lineas {
		if linea[0] != "" && linea[0] == linea[1] && linea[1] == linea[2] {
			return linea[0]
		}
	}
	lleno := true
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			if b[i][j] == "" {
				lleno = false
			}
		}
	}
	if lleno {
		return "draw"
	}
	return ""
}

func (g *tttJuego) evaluateBoard() int {
	score := 0
	if g.Board[1][1] == "O" {
		score += 3
	} else if g.Board[1][1] == "X" {
		score -= 3
	}
	for _, p := range [][2]int{{0, 0}, {0, 2}, {2, 0}, {2, 2}} {
		if g.Board[p[0]][p[1]] == "O" {
			score += 2
		} else if g.Board[p[0]][p[1]] == "X" {
			score -= 2
		}
	}
	return score
}

func (g *tttJuego) minimaxLimited(depth int, maximizando bool, limite int) int {
	winner := g.getWinner()
	if winner == "O" {
		return 10 - depth
	}
	if winner == "X" {
		return depth - 10
	}
	if winner == "draw" || depth == limite {
		return g.evaluateBoard()
	}
	mejor := math.MinInt
	if !maximizando {
		mejor = math.MaxInt
	}
	jugador := "X"
	if maximizando {
		jugador = "O"
	}
	for r := 0; r < 3; r++ {
		for c := 0; c < 3; c++ {
			if g.Board[r][c] == "" {
				g.Board[r][c] = jugador
				score := g.minimaxLimited(depth+1, !maximizando, limite)
				g.Board[r][c] = ""
				if maximizando {
					if score > mejor {
						mejor = score
					}
				} else if score < mejor {
					mejor = score
				}
			}
		}
	}
	return mejor
}

func (g *tttJuego) availableMoves() []tttMove {
	var moves []tttMove
	for r := 0; r < 3; r++ {
		for c := 0; c < 3; c++ {
			if g.Board[r][c] == "" {
				moves = append(moves, tttMove{r, c})
			}
		}
	}
	return moves
}

func (g *tttJuego) easyMove() tttMove {
	moves := g.availableMoves()
	return moves[randIntN(len(moves))]
}

func (g *tttJuego) mediumMove() tttMove {
	moves := g.availableMoves()
	if float64(randIntN(100))/100 < 0.7 {
		for _, jugador := range []string{"O", "X"} {
			for _, move := range moves {
				g.Board[move.R][move.C] = jugador
				if g.getWinner() == jugador {
					g.Board[move.R][move.C] = ""
					return move
				}
				g.Board[move.R][move.C] = ""
			}
		}
		mejor := math.MinInt
		mejorMove := moves[0]
		limite := 2
		if randIntN(2) == 0 {
			limite = 3
		}
		for _, move := range moves {
			g.Board[move.R][move.C] = "O"
			score := g.minimaxLimited(0, false, limite)
			g.Board[move.R][move.C] = ""
			if score > mejor {
				mejor = score
				mejorMove = move
			}
		}
		return mejorMove
	}
	var preferidas []tttMove
	for _, m := range moves {
		if (m.R == 1 && m.C == 1) || (m.R == 0 && m.C == 0) || (m.R == 0 && m.C == 2) || (m.R == 2 && m.C == 0) || (m.R == 2 && m.C == 2) {
			preferidas = append(preferidas, m)
		}
	}
	if len(preferidas) > 0 && randIntN(100) < 70 {
		return preferidas[randIntN(len(preferidas))]
	}
	return moves[randIntN(len(moves))]
}

func (g *tttJuego) hardMove() tttMove {
	mejor := math.MinInt
	mejorMove := tttMove{0, 0}
	for r := 0; r < 3; r++ {
		for c := 0; c < 3; c++ {
			if g.Board[r][c] == "" {
				g.Board[r][c] = "O"
				score := g.minimaxLimited(0, false, 9)
				g.Board[r][c] = ""
				if score > mejor {
					mejor = score
					mejorMove = tttMove{r, c}
				}
			}
		}
	}
	return mejorMove
}

func (g *tttJuego) makeBotMove() {
	var move tttMove
	switch g.Difficulty {
	case "facil":
		move = g.easyMove()
	case "dificil":
		move = g.hardMove()
	default:
		move = g.mediumMove()
	}
	g.play(move.R, move.C)
}

func tttEditar(ctx *core.Context, g *tttJuego, texto string) error {
	if ctx.Client != nil && g.MsgID != "" {
		edit := ctx.Client.BuildEdit(ctx.ChatJID(), types.MessageID(g.MsgID), core.TextMessage(texto))
		_, err := ctx.Client.SendMessage(context.Background(), ctx.ChatJID(), edit)
		return err
	}
	return ctx.SendText(texto)
}

func tttFinish(ctx *core.Context, g *tttJuego, resultado string) {
	g.Finished = true
	if g.Timer != nil {
		g.Timer.Stop()
	}
	texto := g.toString()
	switch {
	case resultado == "draw":
		texto += "\n\n🤝 Quedamos iguales... empate ✨"
	case resultado == "O" && g.IsBot:
		texto += "\n\n🤖 Gané...! fue muy divertido"
	case resultado == "X" && g.IsBot:
		texto += "\n\n🎉 ¡Me ganaste!... Eres super listo 💕"
	default:
		ganador := g.PlayerXName
		if resultado == "O" {
			ganador = g.PlayerOName
		}
		texto += "\n\n🎉 Ganó " + ganador + ", felicidades"
	}
	_ = tttEditar(ctx, g, texto)
	tttMu.Lock()
	delete(tttGames, g.ChatID)
	tttMu.Unlock()
}

// tttHandleMove replica handleMove().
func tttHandleMove(ctx *core.Context, g *tttJuego, match []string) error {
	sender := ctx.SenderJID().String()
	jugadorActual := g.PlayerX
	if g.Turn == "O" {
		jugadorActual = g.PlayerO
	}
	if jugadorActual != sender {
		if g.IsBot && g.Turn == "O" {
			return ctx.React("😖")
		}
		_ = ctx.React("❌")
		return ctx.Reply("Nu, todavía no te toca... estoy pensando")
	}

	letra := match[1]
	numero := match[2]
	if letra == "" {
		letra = match[4]
	}
	if numero == "" {
		numero = match[3]
	}
	colMap := map[string]int{"a": 0, "b": 1, "c": 2}
	rowMap := map[string]int{"1": 0, "2": 1, "3": 2}
	if !g.play(rowMap[numero], colMap[letra]) {
		_ = ctx.React("🚫")
		return ctx.Reply("Ahí no se puede, ya está ocupado...")
	}

	if winner := g.getWinner(); winner != "" {
		_ = ctx.React("✅")
		tttFinish(ctx, g, winner)
		return nil
	}

	g.switchTurn()
	if err := tttEditar(ctx, g, g.toString()); err != nil {
		return err
	}
	if err := ctx.React("✅"); err != nil {
		return err
	}

	if g.IsBot && !g.Finished && g.Turn == "O" {
		time.AfterFunc(tttBotDelay, func() {
			tttMu.Lock()
			actual := tttGames[g.ChatID]
			tttMu.Unlock()
			if actual == nil || actual != g || actual.Turn != "O" || actual.Finished {
				return
			}
			actual.makeBotMove()
			if winner := actual.getWinner(); winner != "" {
				tttFinish(ctx, actual, winner)
				return
			}
			actual.switchTurn()
			_ = tttEditar(ctx, actual, actual.toString())
		})
	}
	return nil
}

func tttPrefijos() []string {
	return topPrefijos()
}

func tttInit(client *whatsmeow.Client, st *store.Store) error {
	if client == nil {
		return nil
	}
	client.AddEventHandler(func(evt any) {
		msg, ok := evt.(*events.Message)
		if !ok || msg.Message == nil || msg.Info.IsFromMe {
			return
		}
		chatID := msg.Info.Chat.String()
		tttMu.Lock()
		game := tttGames[chatID]
		tttMu.Unlock()
		if game == nil {
			return
		}
		texto := msg.Message.GetConversation()
		if texto == "" && msg.Message.GetExtendedTextMessage() != nil {
			texto = msg.Message.GetExtendedTextMessage().GetText()
		}
		cuerpo := strings.ToLower(strings.TrimSpace(texto))
		if cuerpo == "" {
			return
		}
		for _, p := range tttPrefijos() {
			if strings.HasPrefix(cuerpo, strings.ToLower(p)) {
				return
			}
		}
		match := tttCoordRe.FindStringSubmatch(cuerpo)
		if match == nil {
			return
		}
		ctx := &core.Context{Client: client, Message: msg, Store: st}
		if err := tttHandleMove(ctx, game, match); err != nil {
			slog.Error("❌ [ttt] Error:", "error", err)
		}
	})
	return nil
}

func (Ttt) Handle(ctx *core.Context) error {
	if ctx == nil || ctx.Message == nil {
		return nil
	}
	chatID := ctx.ChatJID().String()
	sender := ctx.SenderJID().String()
	senderName := ctx.Message.Info.PushName
	if senderName == "" {
		senderName = wagoUserOf(sender)
	}

	tttMu.Lock()
	existente := tttGames[chatID]
	tttMu.Unlock()
	if existente != nil {
		asCoord := strings.ToLower(strings.TrimSpace(strings.Join(ctx.Args, " ")))
		if match := tttCoordRe.FindStringSubmatch(asCoord); match != nil {
			return tttHandleMove(ctx, existente, match)
		}
		return ctx.Reply("⚠️ Ya estamos jugando... dime una coordenada (ej: a1) 💕")
	}

	p1 := sender
	p2 := ""
	isBot := false
	difficulty := "medio"
	playerXName := senderName
	playerOName := ""

	parts := make([]string, 0, len(ctx.Args))
	for _, a := range ctx.Args {
		parts = append(parts, strings.ToLower(a))
	}
	diffKw := map[string]string{
		"facil": "facil", "1": "facil", "easy": "facil",
		"medio": "medio", "2": "medio",
		"dificil": "dificil", "3": "dificil", "hard": "dificil",
	}

	var mencionados []string
	if ci := ctx.ContextInfo(); ci != nil {
		mencionados = ci.GetMentionedJID()
	}
	botNums := wagoBotNumbers(ctx)
	botMencionado := false
	for _, jid := range mencionados {
		if wagoContains(botNums, strings.SplitN(jid, "@", 2)[0]) {
			botMencionado = true
			break
		}
	}
	if !botMencionado {
		cuerpo := strings.ToLower(ctx.RawText)
		for _, num := range botNums {
			if strings.Contains(cuerpo, "@"+num) {
				botMencionado = true
				break
			}
		}
	}

	switch {
	case botMencionado:
		p2 = "BOT"
		isBot = true
		playerOName = "Bot"
		for _, part := range parts {
			if v, ok := diffKw[part]; ok {
				difficulty = v
				break
			}
		}
	case len(mencionados) > 0:
		p2 = mencionados[0]
		if p2 == p1 {
			return ctx.Reply("Eh? no puedes jugar solito 😿")
		}
		playerOName = strings.SplitN(p2, "@", 2)[0]
	default:
		tttMu.Lock()
		if tttWaiting != nil && tttWaiting.Chat == chatID && tttWaiting.ID != p1 {
			p2 = p1
			p1 = tttWaiting.ID
			playerOName = strings.SplitN(p2, "@", 2)[0]
			tttWaiting = nil
			tttMu.Unlock()
		} else {
			tttWaiting = &tttEspera{ID: p1, Chat: chatID, Name: playerXName}
			tttMu.Unlock()
			return ctx.Reply("Buscando a alguien que quiera jugar... 🐢\n\nO si quieres juego yo: .ttt @bot [facil(1)|medio(2)|dificil(3)]")
		}
	}

	game := &tttJuego{
		ChatID: chatID, PlayerX: p1, PlayerO: p2,
		PlayerXName: playerXName, PlayerOName: playerOName,
		IsBot: isBot, Difficulty: difficulty, Turn: "X",
	}
	game.Timer = time.AfterFunc(tttGameTimeout, func() {
		tttMu.Lock()
		actual := tttGames[chatID]
		tttMu.Unlock()
		if actual == game {
			tttMu.Lock()
			delete(tttGames, chatID)
			tttMu.Unlock()
			_ = tttEditar(ctx, game, "Se acabó el tiempo, perdón 😿 se cancela.")
		}
	})

	var msgID string
	if ctx.SendHook != nil {
		_ = ctx.SendHook(core.TextMessage(game.toString()))
	} else if ctx.Client != nil {
		resp, err := ctx.Client.SendMessage(context.Background(), ctx.ChatJID(), core.TextMessage(game.toString()))
		if err == nil {
			msgID = string(resp.ID)
		}
	}
	game.MsgID = msgID

	tttMu.Lock()
	tttGames[chatID] = game
	tttMu.Unlock()

	if isBot {
		return ctx.Reply("Sii, jugaré contigo 🫡\nDificultad: " + difficulty + "\n\n¡Haré mi mejor esfuerzo!")
	}
	return nil
}
