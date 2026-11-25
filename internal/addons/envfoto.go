package addons

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"whatsbot-go/internal/core"
)

var (
	envfotoHTTP   = &http.Client{Timeout: 30 * time.Second}
	envfotoNow    = time.Now
	envfotoSendFn = envfotoEnviarCanal
)

// Envfoto porta addons/envfoto.js.
type Envfoto struct{}

func init() { core.Register(Envfoto{}) }

func (Envfoto) Commands() []string { return []string{"envfoto"} }

func envfotoCanal() string { return os.Getenv("NEWS_CHANNEL_ID") }

// envfotoGenerarImagen replica el canvas 800x400 del modo 1.
func envfotoGenerarImagen(ahora time.Time) ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, 800, 400))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)

	dibujar := func(texto string, x, y int) {
		d := &font.Drawer{
			Dst:  img,
			Src:  image.NewUniform(color.Black),
			Face: basicfont.Face7x13,
			Dot:  fixed.P(x, y),
		}
		d.DrawString(texto)
	}
	dibujar("Prueba de imagen a canal", 50, 200)
	dibujar("Enviado: "+ahora.Format("2/1/2006, 15:04:05"), 50, 300)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// envfotoDescargar descarga una imagen con headers de navegador.
func envfotoDescargar(rawURL string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	req.Header.Set("Accept-Language", "es-ES,es;q=0.9")
	resp, err := envfotoHTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("no se pudo descargar la imagen: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("no se pudo descargar la imagen: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func envfotoEnviarCanal(ctx *core.Context, jid types.JID, data []byte, caption string) error {
	if ctx.Client == nil {
		return fmt.Errorf("cliente no disponible")
	}
	resp, err := ctx.Client.Upload(context.Background(), data, whatsmeow.MediaImage)
	if err != nil {
		return err
	}
	msg := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
		URL:           proto.String(resp.URL),
		Mimetype:      proto.String("image/png"),
		Caption:       proto.String(caption),
		FileSHA256:    resp.FileSHA256,
		FileEncSHA256: resp.FileEncSHA256,
		FileLength:    proto.Uint64(resp.FileLength),
		MediaKey:      resp.MediaKey,
		DirectPath:    proto.String(resp.DirectPath),
	}}
	_, err = ctx.Client.SendMessage(context.Background(), jid, msg)
	return err
}

func (Envfoto) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}
	canal := envfotoCanal()
	if canal == "" {
		return ctx.Reply("❌ Configura NEWS_CHANNEL_ID en el .env para usar este comando.")
	}
	modo := "1"
	if len(ctx.Args) > 0 && ctx.Args[0] != "" {
		modo = ctx.Args[0]
	}
	if err := ctx.React("⏳"); err != nil {
		return err
	}

	var buffer []byte
	var caption string
	var err error

	switch modo {
	case "1":
		buffer, err = envfotoGenerarImagen(envfotoNow())
		caption = "🎨 Imagen generada con Canvas (modo 1)"
	case "2":
		if len(ctx.Args) < 2 || ctx.Args[1] == "" {
			err = fmt.Errorf("falta la URL. Uso: envfoto 2 <url>")
			break
		}
		buffer, err = envfotoDescargar(ctx.Args[1])
		_, nombre := filepath.Split(ctx.Args[1])
		if nombre == "" {
			nombre = "web"
		}
		caption = "🌐 Imagen descargada: " + nombre
	case "3":
		ruta := ""
		if len(ctx.Args) > 1 && ctx.Args[1] != "" {
			ruta = ctx.Args[1]
		} else {
			ruta = core.DataPath("assets", "statistics", "rates_chart.png")
		}
		if _, statErr := os.Stat(ruta); statErr != nil {
			err = fmt.Errorf("archivo no encontrado: %s", ruta)
			break
		}
		buffer, err = os.ReadFile(ruta)
		caption = "📁 Imagen local: " + filepath.Base(ruta)
	default:
		err = fmt.Errorf("modo inválido. Usa envfoto [1|2|3] [url|ruta]")
	}

	if err == nil {
		jid, parseErr := types.ParseJID(canal)
		if parseErr != nil {
			err = parseErr
		} else if sendErr := envfotoSendFn(ctx, jid, buffer, caption); sendErr != nil {
			err = sendErr
		}
	}

	if err != nil {
		_ = ctx.Reply("❌ Error: " + err.Error())
	} else if replyErr := ctx.Reply("✅ Imagen enviada al canal (modo " + modo + ")"); replyErr != nil {
		return replyErr
	}
	// envfoto.js reacciona ✅ también en el finally.
	return ctx.React("✅")
}
