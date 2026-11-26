package addons

import (
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"

	"whatsbot-go/internal/core"
)

func TestTttCommands(t *testing.T) {
	if got := (Ttt{}).Commands(); len(got) != 1 || got[0] != "ttt" {
		t.Fatalf("comandos=%#v", got)
	}
}

func TestTttBoardVacio(t *testing.T) {
	g := &tttJuego{PlayerXName: "Ana", PlayerOName: "Luis", Turn: "X"}
	texto := g.toString()
	for _, want := range []string{"Hora de jugar", "🅰️", "❌ Ana", "⭕ Luis", "Le toca a: Ana", "coordenada"} {
		if !strings.Contains(texto, want) {
			t.Fatalf("tablero sin %q:\n%s", want, texto)
		}
	}
}

func TestTttPlayYTurnos(t *testing.T) {
	g := &tttJuego{Turn: "X"}
	if !g.play(0, 0) {
		t.Fatal("jugada válida rechazada")
	}
	if g.play(0, 0) {
		t.Fatal("casilla ocupada aceptada")
	}
	if g.Board[0][0] != "X" {
		t.Fatalf("tablero=%v", g.Board)
	}
	g.switchTurn()
	if g.Turn != "O" {
		t.Fatalf("turno=%q", g.Turn)
	}
}

func TestTttWinner(t *testing.T) {
	g := &tttJuego{Board: [3][3]string{{"X", "X", "X"}, {"", "", ""}, {"", "", ""}}}
	if got := g.getWinner(); got != "X" {
		t.Fatalf("ganador=%q", got)
	}
	g = &tttJuego{Board: [3][3]string{{"O", "", ""}, {"O", "", ""}, {"O", "", ""}}}
	if got := g.getWinner(); got != "O" {
		t.Fatalf("ganador=%q", got)
	}
	g = &tttJuego{Board: [3][3]string{{"X", "", "O"}, {"", "X", ""}, {"O", "", "X"}}}
	if got := g.getWinner(); got != "X" {
		t.Fatalf("diagonal=%q", got)
	}
	g = &tttJuego{Board: [3][3]string{
		{"X", "O", "X"}, {"X", "O", "O"}, {"O", "X", "X"},
	}}
	if got := g.getWinner(); got != "draw" {
		t.Fatalf("empate=%q", got)
	}
}

func TestTttBotMoves(t *testing.T) {
	for _, dificultad := range []string{"facil", "medio", "dificil"} {
		g := &tttJuego{Difficulty: dificultad, Turn: "O"}
		g.makeBotMove()
		movidas := 0
		for r := 0; r < 3; r++ {
			for c := 0; c < 3; c++ {
				if g.Board[r][c] == "O" {
					movidas++
				}
			}
		}
		if movidas != 1 {
			t.Fatalf("dificultad %q produjo %d movimientos", dificultad, movidas)
		}
	}
}

func TestTttCoordRegex(t *testing.T) {
	for _, valida := range []string{"a1", "b3", "1a", "3 c"} {
		if tttCoordRe.FindStringSubmatch(valida) == nil {
			t.Fatalf("%q debía ser válida", valida)
		}
	}
	for _, invalida := range []string{"d1", "a4", "a", ""} {
		if tttCoordRe.FindStringSubmatch(invalida) != nil {
			t.Fatalf("%q debía ser inválida", invalida)
		}
	}
}

func TestTttHandleCreaPartida(t *testing.T) {
	tttMu.Lock()
	tttGames = map[string]*tttJuego{}
	tttWaiting = nil
	tttMu.Unlock()
	prevDelay := tttBotDelay
	tttBotDelay = time.Hour
	t.Cleanup(func() { tttBotDelay = prevDelay })

	t.Setenv("BOT_NUMBERS", "584999999999")
	var tablero string
	ctx := wagoCtx("1205@g.us")
	ctx.Command = "ttt"
	ctx.Args = []string{"@584999999999", "facil"}
	ctx.Message.Message = &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text:        protoString("ttt @584999999999 facil"),
		ContextInfo: &waE2E.ContextInfo{MentionedJID: []string{"584999999999@s.whatsapp.net"}},
	}}
	ctx.SendHook = func(m *waE2E.Message) error {
		if texto := messageText(m); strings.Contains(texto, "Hora de jugar") {
			tablero = texto
		}
		return nil
	}
	ctx.ReactHook = func(string) error { return nil }
	if err := (Ttt{}).Handle(ctx); err != nil {
		t.Fatalf("Handle error: %v", err)
	}

	tttMu.Lock()
	game := tttGames["1205@g.us"]
	tttMu.Unlock()
	if game == nil {
		t.Fatalf("no se creó la partida; tablero=%q", tablero)
	}
	if !game.IsBot || game.Difficulty != "facil" {
		t.Fatalf("juego=%#v", game)
	}
	if !strings.Contains(tablero, "Hora de jugar") {
		t.Fatalf("tablero=%q", tablero)
	}
}

func TestTttHandleMovimiento(t *testing.T) {
	tttMu.Lock()
	game := &tttJuego{
		ChatID: "1205@g.us", PlayerX: "58412345678@s.whatsapp.net", PlayerO: "584999999999@s.whatsapp.net",
		PlayerXName: "Ana", PlayerOName: "Luis", Turn: "X",
	}
	tttGames = map[string]*tttJuego{"1205@g.us": game}
	tttWaiting = nil
	tttMu.Unlock()

	ctx := wagoCtx("1205@g.us")
	ctx.Command = "ttt"
	ctx.Args = []string{"a1"}
	ctx.SendHook = func(*waE2E.Message) error { return nil }
	ctx.ReactHook = func(string) error { return nil }
	if err := (Ttt{}).Handle(ctx); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if game.Board[0][0] != "X" {
		t.Fatalf("tablero=%v", game.Board)
	}
	if game.Turn != "O" {
		t.Fatalf("turno=%q", game.Turn)
	}

	tttMu.Lock()
	delete(tttGames, "1205@g.us")
	tttMu.Unlock()
}

func TestTttMencionaBotLiteral(t *testing.T) {
	casos := []struct {
		parts []string
		raw   string
		want  bool
	}{
		{[]string{"@bot"}, "!ttt @bot", true},
		{[]string{"@BOT", "facil"}, "!ttt @BOT facil", true},
		{[]string{"@bot,"}, "!ttt @bot, dificil", true},
		{[]string{"ana"}, "!ttt ana", false},
	}
	for _, c := range casos {
		if got := tttMencionaBot(c.parts, c.raw, nil, nil); got != c.want {
			t.Fatalf("tttMencionaBot(%#v, %q)=%v, esperaba %v", c.parts, c.raw, got, c.want)
		}
	}
}

func TestTttHandleBotLiteral(t *testing.T) {
	tttMu.Lock()
	tttGames = map[string]*tttJuego{}
	tttWaiting = nil
	tttMu.Unlock()
	prevDelay := tttBotDelay
	tttBotDelay = time.Hour
	t.Cleanup(func() { tttBotDelay = prevDelay })

	t.Setenv("BOT_NUMBERS", "584000000000")
	ctx := wagoCtx("1205@g.us")
	ctx.Command = "ttt"
	ctx.Args = []string{"@bot", "dificil"}
	ctx.RawText = "!ttt @bot dificil"
	ctx.Message.Message = &waE2E.Message{Conversation: protoString("!ttt @bot dificil")}
	ctx.SendHook = func(*waE2E.Message) error { return nil }
	ctx.ReactHook = func(string) error { return nil }
	if err := (Ttt{}).Handle(ctx); err != nil {
		t.Fatalf("Handle error: %v", err)
	}

	tttMu.Lock()
	game := tttGames["1205@g.us"]
	tttMu.Unlock()
	if game == nil {
		t.Fatal("no se creó la partida con @bot literal")
	}
	if !game.IsBot || game.Difficulty != "dificil" {
		t.Fatalf("juego=%#v", game)
	}
}

var _ = core.DataPath
