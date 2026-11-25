package addons

import (
	"image/color"
	"log/slog"
	"sort"
	"strconv"
	"strings"

	"go.mau.fi/whatsmeow/types"

	"whatsbot-go/internal/core"
)

// Love porta addons/love.js: medidor de amor determinista con imagen.
type Love struct{}

func init() { core.Register(Love{}) }

func (Love) Commands() []string { return []string{"love"} }

func loveNombreStore(ctx *core.Context, jid string) string {
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
	return "~"
}

// lovePorcentaje replica el cálculo determinista (pareja ordenada).
func lovePorcentaje(user1, user2 string) int {
	pareja := []string{user1, user2}
	sort.Strings(pareja)
	return wagoHashPorcentaje(pareja[0] + pareja[1])
}

func loveCaption(percent int) string {
	switch {
	case percent < 20:
		return "💔 *" + strconv.Itoa(percent) + "%* | Esto está más frío que el polo norte. Mejor quédense como amigos."
	case percent < 50:
		return "🥀 *" + strconv.Itoa(percent) + "%* | Hay algo, pero falta chispa. ¡Tienen que trabajar en ello!"
	case percent < 80:
		return "💖 *" + strconv.Itoa(percent) + "%* | ¡Hacen bonita pareja! Tienen bastante potencial juntos."
	case percent < 100:
		return "🔥 *" + strconv.Itoa(percent) + "%* | ¡Amor del bueno! Están hechos el uno para el otro."
	}
	return "✨ *100%* | ¡ALMAS GEMELAS! ¡Cásense ya! ✨"
}

// loveGenerarImagen dibuja el banner 900x300 con avatares y corazón.
func loveGenerarImagen(percent int, nombre1, nombre2 string) []byte {
	img := wagoLienzo(900, 300, color.RGBA{0xff, 0xdd, 0xe1, 0xff})

	wagoDibujarAvatarCirculo(img, wagoAvatar(200, nombre1), 100, 50, 200)
	wagoDibujarAvatarCirculo(img, wagoAvatar(200, nombre2), 600, 50, 200)

	corazon := color.RGBA{0xff, 0x14, 0x93, 0xff}
	cx, cy, w, h := 450, 70, 200, 180
	r := w / 4
	topCurve := int(float64(h) * 0.3)
	wagoCirculo(img, cx-w/4, cy+topCurve, r, corazon)
	wagoCirculo(img, cx+w/4, cy+topCurve, r, corazon)
	wagoPoligono(img, [][2]int{
		{cx - w/2, cy + topCurve},
		{cx + w/2, cy + topCurve},
		{cx, cy + h},
	}, corazon)

	wagoTextoCentrado(img, strconv.Itoa(percent)+"%", cx, cy+110, 50, color.White)
	return wagoPNG(img)
}

func (Love) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}
	sender := ctx.SenderJID().String()
	nombre1 := "~"
	if ctx.Message != nil && ctx.Message.Info.PushName != "" {
		nombre1 = ctx.Message.Info.PushName
	}
	user1 := sender
	user2 := ""
	nombre2 := "~"

	var menciones []string
	if ci := ctx.ContextInfo(); ci != nil {
		menciones = ci.GetMentionedJID()
	}
	switch {
	case len(menciones) >= 2:
		user1, user2 = menciones[0], menciones[1]
		nombre1 = loveNombreStore(ctx, user1)
		nombre2 = loveNombreStore(ctx, user2)
	case len(menciones) == 1:
		user2 = menciones[0]
		nombre2 = loveNombreStore(ctx, user2)
	case ctx.ContextInfo() != nil && ctx.ContextInfo().GetParticipant() != "":
		user2 = ctx.ContextInfo().GetParticipant()
		nombre2 = loveNombreStore(ctx, user2)
	}
	if user2 == "" {
		return ctx.Reply("💕 Menciona a alguien o responde a su mensaje para medir el amor.")
	}

	if err := ctx.React("💖"); err != nil {
		return err
	}
	percent := lovePorcentaje(user1, user2)
	data := loveGenerarImagen(percent, nombre1, nombre2)

	var mencionesJID []types.JID
	for _, jid := range []string{user1, user2} {
		if parsed, err := types.ParseJID(jid); err == nil {
			mencionesJID = append(mencionesJID, parsed)
		}
	}
	if err := wagoEnviarImagenConMenciones(ctx, data, loveCaption(percent), mencionesJID); err != nil {
		slog.Error("❌ [love] Error:", "error", err)
		return ctx.React("⚠️")
	}
	return nil
}

var _ = strings.TrimSpace
