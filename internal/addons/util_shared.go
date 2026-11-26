// util_shared.go — pequeñas utilidades compartidas por los lotes B/C/D.
package addons

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"whatsbot-go/internal/core"
)

// wagoEnviarHook envía un mensaje y devuelve su ID. Inyectable para pruebas.
var wagoEnviarHook = func(client *whatsmeow.Client, jid types.JID, msg *waE2E.Message) (types.MessageID, error) {
	if client == nil {
		return "", fmt.Errorf("sin cliente")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := client.SendMessage(ctx, jid, msg)
	if err != nil {
		return "", err
	}
	return resp.ID, nil
}

// wagoEditarHook edita un mensaje propio. Inyectable para pruebas sin red.
var wagoEditarHook = func(client *whatsmeow.Client, jid types.JID, msgID, texto string) error {
	if client == nil || msgID == "" {
		return fmt.Errorf("sin cliente o identificador de mensaje")
	}
	reqCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := client.SendMessage(reqCtx, jid,
		client.BuildEdit(jid, types.MessageID(msgID), core.TextMessage(texto)))
	return err
}

// wagoEditar mantiene el comportamiento de Node.js: edita el mensaje ya
// enviado; si no es posible (sin cliente), envía el texto como respuesta nueva.
func wagoEditar(ctx *core.Context, msgID, texto string) error {
	if ctx != nil && ctx.Client != nil && ctx.Message != nil && msgID != "" {
		if err := wagoEditarHook(ctx.Client, ctx.ChatJID(), msgID, texto); err == nil {
			return nil
		}
	}
	if ctx != nil {
		return ctx.Reply(texto)
	}
	return nil
}

func wagoGetenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// wagoTextWithMentions construye un texto con menciones (equivalente a
// { text, mentions } de Baileys).
func wagoTextWithMentions(text string, mentions []types.JID) *waE2E.Message {
	if len(mentions) == 0 {
		return core.TextMessage(text)
	}
	jids := make([]string, 0, len(mentions))
	for _, jid := range mentions {
		jids = append(jids, jid.String())
	}
	return &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text:        proto.String(text),
			ContextInfo: &waE2E.ContextInfo{MentionedJID: jids},
		},
	}
}

func wagoUserOf(jid string) string {
	for i := 0; i < len(jid); i++ {
		if jid[i] == '@' || jid[i] == ':' {
			return jid[:i]
		}
	}
	return jid
}

// wagoLimite implementa el patrón "N usos por ventana" de blue/cat/felix/
// furry/sushi, sin depender de temporizadores por petición.
type wagoLimite struct {
	mu      sync.Mutex
	usos    map[string][]int64
	max     int
	ventana time.Duration
	ahora   func() time.Time
}

func wagoNuevoLimite(max int, ventana time.Duration) *wagoLimite {
	return &wagoLimite{usos: map[string][]int64{}, max: max, ventana: ventana, ahora: time.Now}
}

func (l *wagoLimite) permitir(usuario string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	ahora := l.ahora().UnixMilli()
	historial := l.usos[usuario]
	filtrado := historial[:0]
	for _, ts := range historial {
		if ahora-ts < l.ventana.Milliseconds() {
			filtrado = append(filtrado, ts)
		}
	}
	if len(filtrado) >= l.max {
		l.usos[usuario] = filtrado
		return false
	}
	l.usos[usuario] = append(filtrado, ahora)
	return true
}

// wagoHashPorcentaje replica el hash int32 de love.js/cortisol.js.
func wagoHashPorcentaje(seed string) int {
	var hash int32
	for _, r := range seed {
		hash = int32(r) + ((hash << 5) - hash)
	}
	v := int64(hash)
	if v < 0 {
		v = -v
	}
	return int(v % 101)
}

// wagoEnviarImagenConMenciones envía una imagen con menciones opcionales.
func wagoEnviarImagenConMenciones(ctx *core.Context, data []byte, caption string, menciones []types.JID) error {
	if ctx == nil {
		return nil
	}
	if ctx.Client == nil {
		imgMsg := &waE2E.ImageMessage{Caption: proto.String(caption)}
		if len(menciones) > 0 {
			jids := make([]string, 0, len(menciones))
			for _, jid := range menciones {
				jids = append(jids, jid.String())
			}
			imgMsg.ContextInfo = &waE2E.ContextInfo{MentionedJID: jids}
		}
		return ctx.SendMessage(&waE2E.Message{ImageMessage: imgMsg})
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
	if len(menciones) > 0 {
		jids := make([]string, 0, len(menciones))
		for _, jid := range menciones {
			jids = append(jids, jid.String())
		}
		msg.ImageMessage.ContextInfo = &waE2E.ContextInfo{MentionedJID: jids}
	}
	return ctx.SendMessage(msg)
}
