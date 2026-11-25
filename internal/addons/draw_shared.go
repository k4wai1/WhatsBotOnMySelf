// draw_shared.go — primitivas de dibujo usadas por cortisol y love.
package addons

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

func wagoFuente(size float64) font.Face {
	f, err := opentype.Parse(gobold.TTF)
	if err != nil {
		return nil
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil
	}
	return face
}

func wagoLienzo(w, h int, fondo color.Color) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: fondo}, image.Point{}, draw.Src)
	return img
}

func wagoTexto(img *image.RGBA, texto string, x, y int, size float64, c color.Color) {
	face := wagoFuente(size)
	if face == nil {
		return
	}
	defer face.Close()
	d := &font.Drawer{Dst: img, Src: image.NewUniform(c), Face: face, Dot: fixed.P(x, y)}
	d.DrawString(texto)
}

func wagoTextoCentrado(img *image.RGBA, texto string, cx, y int, size float64, c color.Color) {
	face := wagoFuente(size)
	if face == nil {
		return
	}
	defer face.Close()
	d := &font.Drawer{Dst: img, Src: image.NewUniform(c), Face: face}
	ancho := d.MeasureString(texto)
	d.Dot = fixed.P(cx-int(ancho/2), y)
	d.DrawString(texto)
}

func wagoCirculo(img *image.RGBA, cx, cy, r int, c color.Color) {
	if r <= 0 {
		return
	}
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			if x < 0 || y < 0 || x >= img.Bounds().Dx() || y >= img.Bounds().Dy() {
				continue
			}
			dx, dy := float64(x-cx), float64(y-cy)
			if dx*dx+dy*dy <= float64(r*r) {
				img.Set(x, y, c)
			}
		}
	}
}

// wagoAnillo pinta una franja de anillo entre ángulos (radianes).
func wagoAnillo(img *image.RGBA, cx, cy, rIn, rOut int, angIni, angFin float64, c color.Color) {
	for y := cy - rOut; y <= cy+rOut; y++ {
		for x := cx - rOut; x <= cx+rOut; x++ {
			if x < 0 || y < 0 || x >= img.Bounds().Dx() || y >= img.Bounds().Dy() {
				continue
			}
			dx, dy := float64(x-cx), float64(y-cy)
			dist := math.Hypot(dx, dy)
			if dist < float64(rIn) || dist > float64(rOut) {
				continue
			}
			ang := math.Atan2(dy, dx)
			for ang < angIni {
				ang += 2 * math.Pi
			}
			if ang >= angIni && ang <= angFin {
				img.Set(x, y, c)
			}
		}
	}
}

func wagoLinea(img *image.RGBA, x1, y1, x2, y2, grosor int, c color.Color) {
	pasos := int(math.Hypot(float64(x2-x1), float64(y2-y1)))
	if pasos < 1 {
		pasos = 1
	}
	for i := 0; i <= pasos; i++ {
		t := float64(i) / float64(pasos)
		x := int(float64(x1) + (float64(x2)-float64(x1))*t)
		y := int(float64(y1) + (float64(y2)-float64(y1))*t)
		wagoCirculo(img, x, y, grosor/2, c)
	}
}

// wagoPoligono rellena un polígono por escaneo con point-in-polygon.
func wagoPoligono(img *image.RGBA, puntos [][2]int, c color.Color) {
	if len(puntos) < 3 {
		return
	}
	minX, minY := puntos[0][0], puntos[0][1]
	maxX, maxY := minX, minY
	for _, p := range puntos {
		if p[0] < minX {
			minX = p[0]
		}
		if p[1] < minY {
			minY = p[1]
		}
		if p[0] > maxX {
			maxX = p[0]
		}
		if p[1] > maxY {
			maxY = p[1]
		}
	}
	dentro := func(px, py int) bool {
		inside := false
		j := len(puntos) - 1
		for i := 0; i < len(puntos); i++ {
			xi, yi := puntos[i][0], puntos[i][1]
			xj, yj := puntos[j][0], puntos[j][1]
			if (yi > py) != (yj > py) && float64(px) < float64(xj-xi)*float64(py-yi)/float64(yj-yi)+float64(xi) {
				inside = !inside
			}
			j = i
		}
		return inside
	}
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			if x < 0 || y < 0 || x >= img.Bounds().Dx() || y >= img.Bounds().Dy() {
				continue
			}
			if dentro(x, y) {
				img.Set(x, y, c)
			}
		}
	}
}

// wagoAvatar dibuja un avatar gris circular con la inicial.
func wagoAvatar(size int, nombre string) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	inicial := "~"
	recortado := strings.TrimSpace(nombre)
	if recortado != "" && recortado != "~" {
		inicial = strings.ToUpper(string([]rune(recortado)[0]))
	}
	wagoCirculo(img, size/2, size/2, size/2, color.RGBA{0x88, 0x88, 0x88, 0xff})
	face := wagoFuente(float64(size) * 0.5)
	if face != nil {
		defer face.Close()
		d := &font.Drawer{Dst: img, Src: image.NewUniform(color.White), Face: face}
		ancho := d.MeasureString(inicial)
		d.Dot = fixed.P(size/2-int(ancho/2), int(float64(size)*0.68))
		d.DrawString(inicial)
	}
	return img
}

// wagoDibujarAvatarCirculo recorta el avatar en un círculo y lo pega.
func wagoDibujarAvatarCirculo(dst *image.RGBA, avatar *image.RGBA, x, y, size int) {
	cx, cy, r := x+size/2, y+size/2, size/2
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			dx, dy := float64(px-r), float64(py-r)
			if dx*dx+dy*dy > float64(r*r) {
				continue
			}
			sx := px * avatar.Bounds().Dx() / size
			sy := py * avatar.Bounds().Dy() / size
			dst.Set(x+px, y+py, avatar.At(sx, sy))
		}
	}
	wagoAnillo(dst, cx, cy, r-2, r, 0, 2*math.Pi, color.White)
}

func wagoPNG(img *image.RGBA) []byte {
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
