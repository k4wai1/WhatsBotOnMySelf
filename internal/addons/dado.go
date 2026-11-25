// Package addons contiene los comandos portados desde addons/*.js.
package addons

import (
	"math/rand/v2"
	"strconv"

	"whatsbot-go/internal/core"
)

// randIntN es inyectable para pruebas deterministas.
var randIntN = rand.IntN

// Dado porta addons/dado.js: .dado [N] y .moneda.
type Dado struct{}

// Commands cumple core.Addon.
func (Dado) Commands() []string { return []string{"dado", "moneda"} }

// Handle replica las validaciones y respuestas del addon original.
func (d Dado) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}

	if ctx.Command == "moneda" {
		esCara := randIntN(2) == 0
		resultado := "CRUZ"
		if esCara {
			resultado = "CARA"
		}
		return ctx.SendQuoted(core.TextMessage("🪙 *"+resultado+"*"), ctx.Message)
	}

	caras := 6
	if len(ctx.Args) > 0 && ctx.Args[0] != "" {
		n, err := strconv.Atoi(ctx.Args[0])
		if err != nil || n < 2 || n > 1000 {
			if reactErr := ctx.React("⚠️"); reactErr != nil {
				return reactErr
			}
			return ctx.Reply("🎲 Indica caras entre 2 y 1000. Ej: `.dado 20`")
		}
		caras = n
	}

	resultado := 1 + randIntN(caras)
	return ctx.Reply("🎲 " + strconv.Itoa(resultado) + " (d" + strconv.Itoa(caras) + ")")
}
