package addons

import (
	"image/color"
	"log/slog"
	"math"
	"strconv"
	"time"

	"whatsbot-go/internal/core"
)

var corAhora = time.Now

// Cortisol porta addons/cortisol.js: medidor de cortisol (broma).
type Cortisol struct{}

func init() { core.Register(Cortisol{}) }

func (Cortisol) Commands() []string { return []string{"cor"} }

func cortisolNombreStore(ctx *core.Context, jid string) string {
	if ctx != nil && ctx.Store != nil {
		if contacto, ok := ctx.Store.Get(jid); ok {
			if v, ok := contacto["notify"].(string); ok && v != "" {
				return v
			}
			if v, ok := contacto["name"].(string); ok && v != "" {
				return v
			}
		}
	}
	return "Usuario"
}

// cortisolPorcentaje replica el hash con fecha diaria.
func cortisolPorcentaje(usuario string, ahora time.Time) int {
	return wagoHashPorcentaje(usuario + ahora.Format("2006:01:02"))
}

func cortisolCaption(percent int) string {
	switch {
	case percent < 20:
		return "Modo Zen..."
	case percent < 40:
		return "Todo chill 😌"
	case percent < 60:
		return "Normal 🤔"
	case percent < 80:
		return "Alerta alta 😬"
	}
	return "Al máximo 🔥"
}

// cortisolGenerarImagen dibuja el velocímetro 800x500.
func cortisolGenerarImagen(percent int, nombre string) []byte {
	img := wagoLienzo(800, 500, color.White)
	cx, cy, radio, grosor := 400, 400, 220, 90

	wagoTextoCentrado(img, "CORTISOL LEVEL", cx, 60, 45, color.Black)

	segmentos := []struct {
		c     color.RGBA
		start float64
		end   float64
	}{
		{color.RGBA{0x00, 0xb0, 0x50, 0xff}, 1.0, 1.2},
		{color.RGBA{0x92, 0xd0, 0x50, 0xff}, 1.2, 1.4},
		{color.RGBA{0xff, 0xc0, 0x00, 0xff}, 1.4, 1.6},
		{color.RGBA{0xff, 0x70, 0x00, 0xff}, 1.6, 1.8},
		{color.RGBA{0xff, 0x00, 0x00, 0xff}, 1.8, 2.0},
	}
	for _, seg := range segmentos {
		wagoAnillo(img, cx, cy, radio-grosor/2, radio+grosor/2, seg.start*math.Pi, seg.end*math.Pi, seg.c)
	}
	for i := 1; i <= 4; i++ {
		ang := math.Pi + float64(i)*0.2*math.Pi
		x1 := cx + int(float64(radio-grosor/2)*math.Cos(ang))
		y1 := cy + int(float64(radio-grosor/2)*math.Sin(ang))
		x2 := cx + int(float64(radio+grosor/2+2)*math.Cos(ang))
		y2 := cy + int(float64(radio+grosor/2+2)*math.Sin(ang))
		wagoLinea(img, x1, y1, x2, y2, 10, color.White)
	}

	wagoTextoCentrado(img, "LOW", 170, 280, 30, color.Black)
	wagoTextoCentrado(img, "NORMAL", cx, 120, 30, color.Black)
	wagoTextoCentrado(img, "HIGH", 635, 280, 30, color.Black)

	aguja := math.Pi + float64(percent)/100*math.Pi
	ax := cx + int(float64(radio-30)*math.Cos(aguja))
	ay := cy + int(float64(radio-30)*math.Sin(aguja))
	wagoLinea(img, cx, cy, ax, ay, 24, color.RGBA{0x33, 0x33, 0x33, 0xff})

	wagoCirculo(img, cx, cy, 40, color.RGBA{0x33, 0x33, 0x33, 0xff})
	wagoCirculo(img, cx, cy, 22, color.White)
	wagoCirculo(img, cx, cy, 12, color.RGBA{0x33, 0x33, 0x33, 0xff})

	wagoDibujarAvatarCirculo(img, wagoAvatar(100, nombre), 30, 30, 100)
	return wagoPNG(img)
}

func (Cortisol) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}
	sender := ctx.SenderJID().String()
	targetUser := sender
	targetName := "Usuario"
	if ctx.Message != nil && ctx.Message.Info.PushName != "" {
		targetName = ctx.Message.Info.PushName
	}

	var menciones []string
	if ci := ctx.ContextInfo(); ci != nil {
		menciones = ci.GetMentionedJID()
	}
	if len(menciones) > 0 {
		targetUser = menciones[0]
		targetName = cortisolNombreStore(ctx, targetUser)
	} else if ctx.ContextInfo() != nil && ctx.ContextInfo().GetParticipant() != "" {
		targetUser = ctx.ContextInfo().GetParticipant()
		targetName = cortisolNombreStore(ctx, targetUser)
	}

	if err := ctx.React("🧠"); err != nil {
		return err
	}
	percent := cortisolPorcentaje(targetUser, corAhora())
	data := cortisolGenerarImagen(percent, targetName)
	caption := "*" + targetName + "* : *" + strconv.Itoa(percent) + "%* | " + cortisolCaption(percent)
	if err := ctx.SendImage(data, caption); err != nil {
		slog.Error("❌ [cor] Error:", "error", err)
		return ctx.React("⚠️")
	}
	return nil
}
