package addons

import (
	"log/slog"
	"os"

	"whatsbot-go/internal/core"
)

// Felix porta addons/felix.js: audio divertido de Felix.
type Felix struct{}

func init() { core.Register(Felix{}) }

func (Felix) Commands() []string { return []string{"felix"} }

var felixLimite = wagoNuevoLimite(wagoImgLimit, wagoImgVentana)

func (Felix) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}
	if !felixLimite.permitir(ctx.SenderJID().String()) {
		return ctx.React("❌")
	}
	ruta := core.DataPath("assets", "audios", "felix.mp3")
	data, err := os.ReadFile(ruta)
	if err != nil {
		return ctx.Reply("El audio de Felix no está disponible.")
	}
	if err := ctx.SendAudio(data, "audio/mp4", false, 0); err != nil {
		slog.Error("❌ [felix] Error:", "error", err)
		return ctx.React("❌")
	}
	return nil
}
