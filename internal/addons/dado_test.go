package addons

import (
	"errors"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"

	"whatsbot-go/internal/core"
)

type capture struct {
	texts     []string
	reactions []string
}

func (c *capture) newCtx(command string, args ...string) *core.Context {
	return &core.Context{
		Command: command,
		Args:    args,
		SendHook: func(m *waE2E.Message) error {
			c.texts = append(c.texts, messageText(m))
			return nil
		},
		ReactHook: func(emoji string) error {
			c.reactions = append(c.reactions, emoji)
			return nil
		},
	}
}

func messageText(m *waE2E.Message) string {
	if m == nil {
		return ""
	}
	if m.GetConversation() != "" {
		return m.GetConversation()
	}
	if m.GetExtendedTextMessage() != nil {
		return m.GetExtendedTextMessage().GetText()
	}
	return ""
}

// sinAleatorio fija randIntN y lo restaura al terminar la prueba.
func sinAleatorio(t *testing.T, fn func(n int) int) {
	t.Helper()
	prev := randIntN
	randIntN = fn
	t.Cleanup(func() { randIntN = prev })
}

func TestMonedaCaraYCruz(t *testing.T) {
	d := Dado{}

	sinAleatorio(t, func(int) int { return 0 })
	cara := &capture{}
	if err := d.Handle(cara.newCtx("moneda")); err != nil {
		t.Fatalf("Handle(moneda) devolvió error: %v", err)
	}
	if len(cara.texts) != 1 || !strings.Contains(cara.texts[0], "CARA") {
		t.Fatalf("esperaba CARA, obtuve %#v", cara.texts)
	}

	sinAleatorio(t, func(int) int { return 1 })
	cruz := &capture{}
	if err := d.Handle(cruz.newCtx("moneda")); err != nil {
		t.Fatalf("Handle(moneda) devolvió error: %v", err)
	}
	if len(cruz.texts) != 1 || !strings.Contains(cruz.texts[0], "CRUZ") {
		t.Fatalf("esperaba CRUZ, obtuve %#v", cruz.texts)
	}
}

func TestDadoPorDefectoSeisCaras(t *testing.T) {
	d := Dado{}
	var carasPedidas int
	sinAleatorio(t, func(n int) int {
		carasPedidas = n
		return n - 1
	})

	c := &capture{}
	if err := d.Handle(c.newCtx("dado")); err != nil {
		t.Fatalf("Handle(dado) devolvió error: %v", err)
	}
	if carasPedidas != 6 {
		t.Fatalf("esperaba 6 caras, se pidieron %d", carasPedidas)
	}
	if len(c.texts) != 1 || c.texts[0] != "🎲 6 (d6)" {
		t.Fatalf("respuesta inesperada: %#v", c.texts)
	}
	if len(c.reactions) != 0 {
		t.Fatalf("no esperaba reacciones, obtuve %#v", c.reactions)
	}
}

func TestDadoCarasPersonalizadas(t *testing.T) {
	d := Dado{}
	var carasPedidas int
	sinAleatorio(t, func(n int) int {
		carasPedidas = n
		return 0
	})

	c := &capture{}
	if err := d.Handle(c.newCtx("dado", "20")); err != nil {
		t.Fatalf("Handle(dado 20) devolvió error: %v", err)
	}
	if carasPedidas != 20 {
		t.Fatalf("esperaba 20 caras, se pidieron %d", carasPedidas)
	}
	if len(c.texts) != 1 || c.texts[0] != "🎲 1 (d20)" {
		t.Fatalf("respuesta inesperada: %#v", c.texts)
	}
}

func TestDadoRangoValido(t *testing.T) {
	d := Dado{}
	for _, n := range []int{2, 1000} {
		sinAleatorio(t, func(int) int { return 0 })
		c := &capture{}
		if err := d.Handle(c.newCtx("dado", itoa(n))); err != nil {
			t.Fatalf("Handle(dado %d) devolvió error: %v", n, err)
		}
		if len(c.texts) != 1 || !strings.HasSuffix(c.texts[0], "(d"+itoa(n)+")") {
			t.Fatalf("respuesta inesperada para %d: %#v", n, c.texts)
		}
	}
}

func TestDadoArgumentosInvalidos(t *testing.T) {
	d := Dado{}
	for _, arg := range []string{"0", "1", "1001", "abc", "-5", "2.5"} {
		c := &capture{}
		if err := d.Handle(c.newCtx("dado", arg)); err != nil {
			t.Fatalf("Handle(dado %q) devolvió error: %v", arg, err)
		}
		if len(c.reactions) != 1 || c.reactions[0] != "⚠️" {
			t.Fatalf("esperaba reacción ⚠️ para %q, obtuve %#v", arg, c.reactions)
		}
		if len(c.texts) != 1 || !strings.Contains(c.texts[0], "entre 2 y 1000") {
			t.Fatalf("esperaba aviso de rango para %q, obtuve %#v", arg, c.texts)
		}
	}
}

func TestDadoPropagaErrorDeEnvio(t *testing.T) {
	d := Dado{}
	sinAleatorio(t, func(int) int { return 0 })

	boom := errors.New("sin conexión")
	ctx := &core.Context{
		Command: "dado",
		SendHook: func(*waE2E.Message) error {
			return boom
		},
	}
	if err := d.Handle(ctx); !errors.Is(err, boom) {
		t.Fatalf("esperaba el error del envío, obtuve %v", err)
	}
}

func TestDadoCommands(t *testing.T) {
	got := Dado{}.Commands()
	if len(got) != 2 || got[0] != "dado" || got[1] != "moneda" {
		t.Fatalf("comandos inesperados: %#v", got)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
