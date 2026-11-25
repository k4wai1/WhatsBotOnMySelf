package addons

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"whatsbot-go/internal/core"
)

const (
	unzipMaxBytes        = 300 * 1024 * 1024
	unzipMaxUncompressed = 2 * 1024 * 1024 * 1024
	unzipPrivateLimit    = 7
	unzipMaxFiles        = 40
)

// Unzip porta addons/unzip.js.
type Unzip struct{}

func init() { core.Register(Unzip{}) }

func (Unzip) Commands() []string { return []string{"unzip"} }

// unzipSendFn inyectable en pruebas.
var unzipSendFn = unzipSendDocument

type unzipExtraido struct {
	Nombre string
	Datos  []byte
}

func unzipDocumento(msg *waE2E.Message) *waE2E.DocumentMessage {
	if msg == nil {
		return nil
	}
	return msg.GetDocumentMessage()
}

func unzipEsZip(doc *waE2E.DocumentMessage) bool {
	if doc == nil {
		return false
	}
	nombre := strings.ToLower(doc.GetFileName())
	mime := strings.ToLower(doc.GetMimetype())
	return strings.HasSuffix(nombre, ".zip") || strings.Contains(mime, "zip")
}

func unzipRemitente(chat types.JID, sender types.JID) types.JID {
	base := sender
	if base.User == "" {
		base = chat
	}
	return types.NewJID(base.User, types.DefaultUserServer)
}

// unzipExtraerRaiz descomprime en memoria solo los archivos sueltos de la raíz.
func unzipExtraerRaiz(buffer []byte) ([]unzipExtraido, error) {
	reader, err := zip.NewReader(bytes.NewReader(buffer), int64(len(buffer)))
	if err != nil {
		return nil, err
	}
	var total uint64
	for _, f := range reader.File {
		total += f.UncompressedSize64
		if total > unzipMaxUncompressed {
			return nil, fmt.Errorf("el contenido descomprimido excede el límite de seguridad del servidor")
		}
	}
	var salida []unzipExtraido
	for _, f := range reader.File {
		if f.FileInfo().IsDir() {
			continue
		}
		nombre := f.Name
		if strings.Contains(nombre, "/") || strings.Contains(nombre, "\\") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(io.LimitReader(rc, int64(unzipMaxUncompressed)))
		rc.Close()
		if err != nil {
			return nil, err
		}
		salida = append(salida, unzipExtraido{Nombre: filepath.Base(nombre), Datos: data})
	}
	sort.Slice(salida, func(i, j int) bool { return salida[i].Nombre < salida[j].Nombre })
	return salida, nil
}

func unzipSendDocument(ctx *core.Context, jid types.JID, filename string, data []byte, quoted *events.Message) error {
	if ctx == nil {
		return fmt.Errorf("contexto nulo")
	}
	if ctx.Client == nil {
		return fmt.Errorf("cliente no disponible")
	}
	resp, err := ctx.Client.Upload(context.Background(), data, whatsmeow.MediaDocument)
	if err != nil {
		return err
	}
	msg := &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
		URL:           proto.String(resp.URL),
		Mimetype:      proto.String("application/octet-stream"),
		Title:         proto.String(filename),
		FileName:      proto.String(filename),
		FileSHA256:    resp.FileSHA256,
		FileEncSHA256: resp.FileEncSHA256,
		FileLength:    proto.Uint64(resp.FileLength),
		MediaKey:      resp.MediaKey,
		DirectPath:    proto.String(resp.DirectPath),
	}}
	if quoted != nil && jid == ctx.ChatJID() {
		return ctx.SendQuoted(msg, quoted)
	}
	return ctx.SendTo(jid, msg)
}

func (Unzip) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}
	var doc *waE2E.DocumentMessage
	fromQuoted := false
	if ctx.Message != nil {
		doc = unzipDocumento(ctx.Message.Message)
	}
	if doc == nil {
		doc = unzipDocumento(ctx.QuotedMessage())
		fromQuoted = doc != nil
	}
	if doc == nil {
		if err := ctx.React("❓"); err != nil {
			return err
		}
		return ctx.Reply("❌ Envía el comando .unzip como *caption de un .zip* o responde a un mensaje con un .zip citado.")
	}
	if !unzipEsZip(doc) {
		if err := ctx.React("❌"); err != nil {
			return err
		}
		return ctx.Reply("❌ Solo acepto archivadores .zip.")
	}
	declarado := int64(doc.GetFileLength())
	if declarado > unzipMaxBytes {
		if err := ctx.React("🚫"); err != nil {
			return err
		}
		return ctx.Reply(fmt.Sprintf("🚫 El archivo pesa %.1f MB. El límite es 300 MB.", float64(declarado)/(1024*1024)))
	}

	if err := ctx.React("⏳"); err != nil {
		return err
	}

	var buffer []byte
	var err error
	if fromQuoted {
		buffer, err = ctx.DownloadQuoted()
	} else {
		buffer, err = ctx.Download(ctx.Message.Message)
	}
	if err != nil {
		return unzipError(ctx, err)
	}
	if len(buffer) > unzipMaxBytes {
		return unzipError(ctx, fmt.Errorf("el archivo pesa %.1f MB. El límite es 300 MB", float64(len(buffer))/(1024*1024)))
	}

	archivos, err := unzipExtraerRaiz(buffer)
	if err != nil {
		return unzipError(ctx, err)
	}
	if len(archivos) == 0 {
		return unzipError(ctx, fmt.Errorf("no hay archivos sueltos en la raíz del .zip (solo carpetas)"))
	}

	overflow := len(archivos) - unzipMaxFiles
	aEnviar := archivos
	if overflow > 0 {
		aEnviar = archivos[:unzipMaxFiles]
	}
	privado := len(aEnviar) > unzipPrivateLimit
	target := ctx.ChatJID()
	if privado {
		target = unzipRemitente(ctx.ChatJID(), ctx.SenderJID())
	}

	for _, archivo := range aEnviar {
		destino := target
		var cita *events.Message
		if !privado {
			destino = ctx.ChatJID()
			cita = ctx.Message
		}
		if err := unzipSendFn(ctx, destino, archivo.Nombre, archivo.Datos, cita); err != nil {
			return unzipError(ctx, err)
		}
	}

	if privado {
		nombre := doc.GetFileName()
		if nombre == "" {
			nombre = "el .zip"
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, "📦 *%d archivos* descomprimidos de %s.", len(aEnviar), nombre)
		if overflow > 0 {
			fmt.Fprintf(&sb, "\n⚠️ %d archivo(s) adicional(es) quedaron fuera del tope de %d.", overflow, unzipMaxFiles)
		}
		sb.WriteString("\n📨 Enviados a tu chat privado por ser más de 7 archivos.")
		if err := ctx.Reply(sb.String()); err != nil {
			return err
		}
	}
	return ctx.React("✅")
}

func unzipError(ctx *core.Context, err error) error {
	_ = ctx.React("❌")
	_ = ctx.Reply("❌ No pude completar la extracción: " + err.Error())
	return nil
}
