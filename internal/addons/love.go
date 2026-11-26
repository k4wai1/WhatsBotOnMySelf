// love.go porta addons/love.js: medidor de amor determinista con imagen.
package addons

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/disintegration/imaging"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"whatsbot-go/internal/core"
)

const (
	loveWidth       = 900
	loveHeight      = 300
	loveSupersample = 3
)

// Variables inyectables en las pruebas.
var (
	loveHTTP = http.DefaultClient

	loveFetchAvatar = func(ctx *core.Context, jid string) (image.Image, error) {
		return loveFetchProfilePicture(ctx, jid)
	}
)

// Paleta vibrante estilo WhatsApp para los avatares sin foto.
var lovePalette = []color.NRGBA{
	{0xE1, 0x70, 0x55, 0xFF},
	{0x34, 0x9A, 0xE5, 0xFF},
	{0x9C, 0x27, 0xB0, 0xFF},
	{0x00, 0x96, 0x88, 0xFF},
	{0xEF, 0x6C, 0x00, 0xFF},
	{0x43, 0xA0, 0x47, 0xFF},
	{0xF4, 0x50, 0x3E, 0xFF},
	{0x5D, 0x40, 0xCF, 0xFF},
}

// Love porta el comando .love.
type Love struct{}

func init() { core.Register(Love{}) }

func (Love) Commands() []string { return []string{"love"} }

func loveNombreStore(ctx *core.Context, jid string) string {
	if ctx != nil && ctx.Store != nil {
		if contacto, ok := ctx.Store.Get(jid); ok {
			for _, clave := range []string{"verifiedName", "notify", "name"} {
				if v, ok := contacto[clave].(string); ok && v != "" {
					return v
				}
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

// loveResolve usa el mismo resolver que cita.go para los JID @lid.
func loveResolve(ctx *core.Context, jid, chatJID string) (string, string) {
	pure, nombre, _ := citaResolveUser(ctx, jid, chatJID)
	if pure == "" {
		pure = jid
	}
	return pure, nombre
}

func loveFetchProfilePicture(ctx *core.Context, jid string) (image.Image, error) {
	if ctx == nil || ctx.Client == nil || jid == "" {
		return nil, fmt.Errorf("sin cliente")
	}
	parsed, err := types.ParseJID(jid)
	if err != nil {
		return nil, err
	}
	reqCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	info, err := ctx.Client.GetProfilePictureInfo(reqCtx, parsed, &whatsmeow.GetProfilePictureParams{Preview: false})
	if err != nil || info == nil || info.URL == "" {
		info, err = ctx.Client.GetProfilePictureInfo(reqCtx, parsed, &whatsmeow.GetProfilePictureParams{Preview: true})
	}
	if err != nil || info == nil || info.URL == "" {
		return nil, fmt.Errorf("sin foto de perfil")
	}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, info.URL, nil)
	if err != nil {
		return nil, err
	}
	res, err := loveHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("foto de perfil: HTTP %d", res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	return img, err
}

func loveFace(size float64) font.Face {
	f, err := opentype.Parse(gobold.TTF)
	if err != nil {
		return nil
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		return nil
	}
	return face
}

// loveAvatarFallback genera un avatar con fondo de color vibrante y la inicial.
func loveAvatarFallback(nombre string, size int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	inicial := "?"
	recortado := strings.TrimSpace(nombre)
	if recortado != "" && recortado != "~" {
		inicial = strings.ToUpper(string([]rune(recortado)[0]))
	}
	colorFondo := lovePalette[wagoHashPorcentaje(recortado+inicial)%len(lovePalette)]

	cx, cy := float64(size)/2, float64(size)/2
	loveCirculoRelleno(img, cx, cy, float64(size)/2, colorFondo)

	face := loveFace(float64(size) * 0.52)
	if face != nil {
		defer face.Close()
		d := &font.Drawer{Dst: img, Src: image.NewUniform(color.NRGBA{255, 255, 255, 255}), Face: face}
		ancho := d.MeasureString(inicial).Round()
		d.Dot = fixed.P(int(cx)-ancho/2, int(cy+float64(size)*0.19))
		d.DrawString(inicial)
	}
	return img
}

func loveCirculoRelleno(dst *image.NRGBA, cx, cy, r float64, c color.NRGBA) {
	if r <= 0 {
		return
	}
	b := dst.Bounds()
	x0, x1 := int(math.Floor(cx-r)), int(math.Ceil(cx+r))
	y0, y1 := int(math.Floor(cy-r)), int(math.Ceil(cy+r))
	r2 := r * r
	for y := y0; y <= y1; y++ {
		if y < b.Min.Y || y >= b.Max.Y {
			continue
		}
		for x := x0; x <= x1; x++ {
			if x < b.Min.X || x >= b.Max.X {
				continue
			}
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
			if dx*dx+dy*dy <= r2 {
				dst.SetNRGBA(x, y, c)
			}
		}
	}
}

// loveDibujarAvatar recorta en círculo con borde blanco y sombra suave.
func loveDibujarAvatar(dst *image.NRGBA, src image.Image, nombre string, x, y, size int) {
	cx, cy := float64(x+size/2), float64(y+size/2)
	r := float64(size) / 2

	loveCirculoRelleno(dst, cx, cy+float64(size)*0.045, r+float64(size)*0.02, color.NRGBA{90, 25, 55, 42})
	loveCirculoRelleno(dst, cx, cy+float64(size)*0.02, r+float64(size)*0.02, color.NRGBA{90, 25, 55, 38})
	loveCirculoRelleno(dst, cx, cy, r+float64(size)*0.025, color.NRGBA{255, 255, 255, 255})

	var avatar image.Image
	if src == nil {
		avatar = loveAvatarFallback(nombre, size)
	} else {
		avatar = imaging.Fill(src, size, size, imaging.Center, imaging.Lanczos)
	}
	b := avatar.Bounds()
	for py := -int(r); py <= int(r); py++ {
		for px := -int(r); px <= int(r); px++ {
			if float64(px*px+py*py) > r*r {
				continue
			}
			sx, sy := px+int(r), py+int(r)
			if sx < 0 || sy < 0 || sx >= b.Dx() || sy >= b.Dy() {
				continue
			}
			dst.Set(int(cx)+px, int(cy)+py, avatar.At(b.Min.X+sx, b.Min.Y+sy))
		}
	}
}

// loveCorazonPoligono muestrea la curva paramétrica clásica del corazón.
func loveCorazonPoligono(cx, cy, w, h float64) [][2]float64 {
	const n = 160
	pts := make([][2]float64, 0, n)
	for i := 0; i < n; i++ {
		t := 2 * math.Pi * float64(i) / n
		st := math.Sin(t)
		hx := 16 * st * st * st
		hy := 13*math.Cos(t) - 5*math.Cos(2*t) - 2*math.Cos(3*t) - math.Cos(4*t)
		pts = append(pts, [2]float64{
			cx + hx*w/32,
			cy + h - (hy+17)*h/29,
		})
	}
	return pts
}

// loveRellenarPoligono rellena por scanline con antialiasing vía supersampling.
func loveRellenarPoligono(dst *image.NRGBA, pts [][2]float64, c color.NRGBA) {
	if len(pts) < 3 {
		return
	}
	b := dst.Bounds()
	minY, maxY := pts[0][1], pts[0][1]
	for _, p := range pts {
		if p[1] < minY {
			minY = p[1]
		}
		if p[1] > maxY {
			maxY = p[1]
		}
	}
	xs := make([]float64, 0, len(pts))
	for y := int(math.Ceil(minY)); y <= int(math.Floor(maxY)); y++ {
		if y < b.Min.Y || y >= b.Max.Y {
			continue
		}
		xs = xs[:0]
		sy := float64(y) + 0.5
		for i := range pts {
			a, z := pts[i], pts[(i+1)%len(pts)]
			if (a[1] <= sy && z[1] > sy) || (z[1] <= sy && a[1] > sy) {
				t := (sy - a[1]) / (z[1] - a[1])
				xs = append(xs, a[0]+t*(z[0]-a[0]))
			}
		}
		if len(xs) < 2 {
			continue
		}
		sort.Float64s(xs)
		for i := 0; i+1 < len(xs); i += 2 {
			xa, xb := int(math.Ceil(xs[i]-0.5)), int(math.Floor(xs[i+1]-0.5))
			if xa < b.Min.X {
				xa = b.Min.X
			}
			if xb >= b.Max.X {
				xb = b.Max.X - 1
			}
			for x := xa; x <= xb; x++ {
				dst.SetNRGBA(x, y, c)
			}
		}
	}
}

func loveDibujarCorazon(dst *image.NRGBA) {
	s := float64(loveSupersample)
	cx, cy := 450*s, 70*s
	w, h := 200*s, 180*s
	loveRellenarPoligono(dst, loveCorazonPoligono(cx, cy+7*s, w, h), color.NRGBA{150, 20, 80, 52})
	loveRellenarPoligono(dst, loveCorazonPoligono(cx, cy+3*s, w, h), color.NRGBA{150, 20, 80, 44})
	loveRellenarPoligono(dst, loveCorazonPoligono(cx, cy-6*s, w+12*s, h+12*s), color.NRGBA{255, 255, 255, 255})
	loveRellenarPoligono(dst, loveCorazonPoligono(cx, cy, w, h), color.NRGBA{255, 20, 147, 255})
}

func loveFondo(dst *image.NRGBA) {
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	for y := 0; y < h; y++ {
		ty := float64(y) / float64(h-1)
		for x := 0; x < w; x++ {
			tx := float64(x) / float64(w-1)
			t := 0.65*ty + 0.35*tx
			dx, dy := tx-0.48, ty-0.5
			d2 := dx*dx + dy*dy
			brillo := 0.0
			if d2 < 0.10 {
				brillo = (1 - d2/0.10) * 7
			}
			g := math.Min(255, 241+(201-241)*t+brillo)
			b := math.Min(255, 244+(217-244)*t+brillo)
			dst.SetNRGBA(x, y, color.NRGBA{255, uint8(g), uint8(b), 255})
		}
	}
}

func loveDestello(dst *image.NRGBA, cx, cy, r float64, c color.NRGBA) {
	pts := make([][2]float64, 0, 8)
	for i := 0; i < 8; i++ {
		a := math.Pi * float64(i) / 4
		rr := r
		if i%2 == 1 {
			rr = r * 0.28
		}
		pts = append(pts, [2]float64{cx + rr*math.Cos(a), cy + rr*math.Sin(a)})
	}
	loveRellenarPoligono(dst, pts, c)
}

func loveDetalles(dst *image.NRGBA) {
	s := float64(loveSupersample)
	corazones := []struct{ x, y, w, h float64 }{
		{40, 38, 16, 14}, {820, 46, 12, 11}, {332, 26, 10, 9},
		{784, 262, 14, 12}, {142, 270, 11, 10},
	}
	for _, c := range corazones {
		loveRellenarPoligono(dst, loveCorazonPoligono(c.x*s, c.y*s, c.w*s, c.h*s), color.NRGBA{255, 255, 255, 72})
	}
	destellos := []struct{ x, y, r float64 }{
		{864, 152, 9}, {34, 172, 7}, {510, 275, 8}, {356, 266, 6},
	}
	for _, d := range destellos {
		loveDestello(dst, d.x*s, d.y*s, d.r*s, color.NRGBA{255, 255, 255, 115})
	}
}

func loveDibujarPorcentaje(dst draw.Image, percent int) {
	face := loveFace(50)
	if face == nil {
		return
	}
	defer face.Close()
	texto := strconv.Itoa(percent) + "%"
	d := &font.Drawer{Dst: dst, Face: face}
	ancho := d.MeasureString(texto).Round()
	x := loveWidth/2 - ancho/2
	const baseline = 178

	d.Src = image.NewUniform(color.NRGBA{120, 0, 65, 130})
	d.Dot = fixed.P(x+2, baseline+2)
	d.DrawString(texto)
	d.Src = image.NewUniform(color.NRGBA{255, 255, 255, 255})
	d.Dot = fixed.P(x, baseline)
	d.DrawString(texto)
}

// loveGenerarImagenConAvatares dibuja el banner 900x300 con avatares reales.
func loveGenerarImagenConAvatares(percent int, nombre1, nombre2 string, av1, av2 image.Image) []byte {
	hi := image.NewNRGBA(image.Rect(0, 0, loveWidth*loveSupersample, loveHeight*loveSupersample))
	loveFondo(hi)
	loveDetalles(hi)

	s := loveSupersample
	loveDibujarAvatar(hi, av1, nombre1, 100*s, 50*s, 200*s)
	loveDibujarAvatar(hi, av2, nombre2, 600*s, 50*s, 200*s)
	loveDibujarCorazon(hi)

	out := imaging.Resize(hi, loveWidth, loveHeight, imaging.Lanczos)
	loveDibujarPorcentaje(out, percent)

	var buf bytes.Buffer
	_ = png.Encode(&buf, out)
	return buf.Bytes()
}

func loveGenerarImagen(percent int, nombre1, nombre2 string) []byte {
	return loveGenerarImagenConAvatares(percent, nombre1, nombre2, nil, nil)
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

	chatJID := ctx.ChatJID().String()
	jid1, nombreRes1 := loveResolve(ctx, user1, chatJID)
	jid2, nombreRes2 := loveResolve(ctx, user2, chatJID)
	if nombre1 == "" || nombre1 == "~" {
		if nombreRes1 != "" && nombreRes1 != "~" {
			nombre1 = nombreRes1
		}
	}
	if nombre2 == "" || nombre2 == "~" {
		if nombreRes2 != "" && nombreRes2 != "~" {
			nombre2 = nombreRes2
		}
	}

	type resultado struct {
		img image.Image
		err error
	}
	canal := make(chan resultado, 2)
	go func() {
		img, err := loveFetchAvatar(ctx, jid1)
		canal <- resultado{img, err}
	}()
	go func() {
		img, err := loveFetchAvatar(ctx, jid2)
		canal <- resultado{img, err}
	}()
	r1, r2 := <-canal, <-canal
	var av1, av2 image.Image
	if r1.err == nil && r1.img != nil {
		av1 = r1.img
	}
	if r2.err == nil && r2.img != nil {
		av2 = r2.img
	}

	data := loveGenerarImagenConAvatares(percent, nombre1, nombre2, av1, av2)

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
