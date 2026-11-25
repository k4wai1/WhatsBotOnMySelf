package addons

import (
	"log/slog"
	"os"

	"whatsbot-go/internal/core"
)

// Sushi porta addons/sushi.js: fotos aleatorias de Sushi el gato.
type Sushi struct{}

func init() { core.Register(Sushi{}) }

func (Sushi) Commands() []string { return []string{"sushi"} }

var sushiLimite = wagoNuevoLimite(wagoImgLimit, wagoImgVentana)

func (Sushi) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}
	if !sushiLimite.permitir(ctx.SenderJID().String()) {
		return ctx.React("❌")
	}
	ruta, ok := wagoImagenAleatoria(core.DataPath("assets", "img_sushi"))
	if !ok {
		return ctx.Reply("No hay imágenes de Sushi")
	}
	data, err := os.ReadFile(ruta)
	if err != nil {
		slog.Error("❌ [sushi] Error:", "error", err)
		return ctx.Reply("Ocurrió un error cargando las imágenes 😿")
	}
	if err := ctx.SendImage(data, "Miau!"); err != nil {
		slog.Error("❌ [sushi] Error:", "error", err)
		return ctx.Reply("Ocurrió un error cargando las imágenes 😿")
	}
	return nil
}
