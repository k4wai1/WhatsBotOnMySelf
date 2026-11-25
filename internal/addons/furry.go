package addons

import (
	"log/slog"
	"os"

	"whatsbot-go/internal/core"
)

// Furry porta addons/furry.js: imágenes furry aleatorias.
type Furry struct{}

func init() { core.Register(Furry{}) }

func (Furry) Commands() []string { return []string{"furry"} }

var furryLimite = wagoNuevoLimite(wagoImgLimit, wagoImgVentana)

func (Furry) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}
	if !furryLimite.permitir(ctx.SenderJID().String()) {
		return ctx.React("❌")
	}
	ruta, ok := wagoImagenAleatoria(core.DataPath("assets", "furry"))
	if !ok {
		return ctx.Reply("No hay imágenes")
	}
	data, err := os.ReadFile(ruta)
	if err != nil {
		slog.Error("❌ [furry] Error:", "error", err)
		return ctx.React("❌")
	}
	if err := ctx.SendImage(data, "Que tal unos furros?"); err != nil {
		slog.Error("❌ [furry] Error:", "error", err)
		return ctx.React("❌")
	}
	return nil
}
