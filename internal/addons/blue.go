package addons

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"whatsbot-go/internal/core"
)

const wagoImgLimit = 4

var wagoImgVentana = 10 * time.Minute

// wagoImagenes lista archivos de imagen de un directorio.
func wagoImagenes(dir string) ([]string, error) {
	entradas, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var salida []string
	for _, e := range entradas {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		switch ext {
		case ".jpg", ".jpeg", ".png", ".gif":
			salida = append(salida, e.Name())
		}
	}
	return salida, nil
}

// wagoImagenAleatoria devuelve la ruta de una imagen aleatoria.
func wagoImagenAleatoria(dir string) (string, bool) {
	archivos, err := wagoImagenes(dir)
	if err != nil || len(archivos) == 0 {
		return "", false
	}
	return filepath.Join(dir, archivos[randIntN(len(archivos))]), true
}

// Blue porta addons/blue.js: ejercicios de integrales aleatorios.
type Blue struct{}

func init() { core.Register(Blue{}) }

func (Blue) Commands() []string { return []string{"blue"} }

var blueLimite = wagoNuevoLimite(wagoImgLimit, wagoImgVentana)

func (Blue) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}
	userID := ctx.SenderJID().String()
	if !blueLimite.permitir(userID) {
		return ctx.React("❌")
	}
	ruta, ok := wagoImagenAleatoria(core.DataPath("assets", "blue"))
	if !ok {
		return ctx.Reply("No hay ejercicios")
	}
	data, err := os.ReadFile(ruta)
	if err != nil {
		slog.Error("❌ [blue] Error:", "error", err)
		return ctx.React("❌")
	}
	if err := ctx.SendImage(data, "Unas integrales?"); err != nil {
		slog.Error("❌ [blue] Error:", "error", err)
		return ctx.React("❌")
	}
	return nil
}
