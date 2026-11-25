// addons/debug-info.js → debug-info.go
// Reporte de extracción de datos: resolución de LID → PN y nombres.
package addons

import (
	"context"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"whatsbot-go/internal/core"
	"whatsbot-go/internal/store"
)

var debugInfoGroupInfo = func(client *whatsmeow.Client, jid types.JID) (*types.GroupInfo, error) {
	return client.GetGroupInfo(context.Background(), jid)
}

// DebugInfo porta addons/debug-info.js.
type DebugInfo struct{}

func init() { core.Register(DebugInfo{}) }

// Commands cumple core.Addon.
func (DebugInfo) Commands() []string { return []string{"info", "debug"} }

// Handle genera el mismo reporte de texto que el addon original.
func (DebugInfo) Handle(ctx *core.Context) error {
	if ctx == nil || ctx.Message == nil {
		return nil
	}
	jid := ctx.ChatJID().String()

	var reporte strings.Builder
	reporte.WriteString("🔍 *REPORTE DE EXTRACCIÓN DE DATOS*\n\n")

	_ = ctx.React("📡")

	targetID := debugInfoTarget(ctx)
	reporte.WriteString("🎯 *ID Objetivo crudo:* " + targetID + "\n\n")

	pureJid := ""
	tipoID := "Desconocido"
	metodo := "❌ Bloqueo de Privacidad de WA (Comunidad/Anuncio/Ajustes)"

	if strings.HasSuffix(targetID, "@lid") {
		tipoID = "LID (Identidad Oculta)"

		// MÉTODO 1: LID del propio bot.
		if ctx.Client != nil && ctx.Client.Store != nil {
			botLID := ctx.Client.Store.LID.String()
			if botLID != "" && botLID != types.EmptyJID.String() && targetID == botLID {
				if ctx.Client.Store.ID != nil {
					pureJid = store.PurePN(ctx.Client.Store.ID.String())
					if pureJid != "" {
						metodo = "✅ Resuelto (Identidad propia del Bot)"
					}
				}
			}
		}

		// MÉTODO 2: metadatos del grupo.
		if pureJid == "" && strings.HasSuffix(jid, "@g.us") && ctx.Client != nil {
			if groupJID, err := types.ParseJID(jid); err == nil {
				if meta, err := debugInfoGroupInfo(ctx.Client, groupJID); err == nil && meta != nil {
					for _, p := range meta.Participants {
						if p.LID.String() != targetID && p.JID.String() != targetID {
							continue
						}
						cand := p.PhoneNumber
						if cand.IsEmpty() {
							cand = p.JID
						}
						if cand.Server == types.DefaultUserServer {
							pureJid = cand.String()
							metodo = "✅ Resuelto vía Metadatos del Grupo"
							break
						}
					}
				}
			}
		}

		// MÉTODO 3: cruce exhaustivo en el caché de contactos.
		if pureJid == "" && ctx.Store != nil {
			contacts := ctx.Store.Snapshot()
			for key, c := range contacts {
				cid := debugInfoField(c, "id")
				clid := debugInfoField(c, "lid")
				if key != targetID && cid != targetID && clid != targetID {
					continue
				}
				if strings.HasSuffix(key, "@s.whatsapp.net") {
					pureJid = key
					metodo = "✅ Resuelto vía Llave Maestra en Store"
					break
				} else if strings.HasSuffix(cid, "@s.whatsapp.net") {
					pureJid = cid
					metodo = "✅ Resuelto vía Propiedad ID en Store"
					break
				} else if strings.HasSuffix(clid, "@s.whatsapp.net") {
					pureJid = clid
					metodo = "✅ Resuelto vía Cruce Anómalo en Store"
					break
				}
			}
		}
	} else {
		tipoID = "JID Estándar (Número Real)"
		pureJid = store.PurePN(targetID)
		metodo = "✅ No requirió conversión (Es público)"
	}

	finalNumber := "No resuelto"
	if pureJid != "" {
		finalNumber = strings.SplitN(pureJid, "@", 2)[0]
	}

	reporte.WriteString("📱 *MÉTODOS DE NÚMERO:*\n")
	reporte.WriteString("🔹 *Tipo detectado:* " + tipoID + "\n")
	reporte.WriteString("🔹 *Estado:* " + metodo + "\n")
	if pureJid != "" {
		reporte.WriteString("✅")
	} else {
		reporte.WriteString("❌")
	}
	reporte.WriteString(" *Número Real:* " + finalNumber + "\n")
	pureJidText := pureJid
	if pureJidText == "" {
		pureJidText = "No disponible"
	}
	reporte.WriteString("   ↳ _JID Puro final: " + pureJidText + "_\n\n")

	reporte.WriteString("👤 *MÉTODOS DE NOMBRE:*\n")

	participant := debugInfoParticipant(ctx.Message)
	isSelfOrSender := targetID == participant || (participant == "" && targetID == jid)
	if ctx.Message.Info.PushName != "" && isSelfOrSender {
		reporte.WriteString("✅ *msg.pushName:* " + ctx.Message.Info.PushName + "\n")
	} else {
		reporte.WriteString("❌ *msg.pushName:* Indisponible\n")
		reporte.WriteString("   ↳ _Falla: WhatsApp no adjunta el pushName en mensajes citados._\n")
	}

	storeKey := pureJid
	if storeKey == "" {
		storeKey = targetID
	}
	var contact store.Contact
	if ctx.Store != nil {
		if c, ok := ctx.Store.Get(storeKey); ok {
			contact = c
		}
	}
	if contact != nil {
		verifiedName := debugInfoField(contact, "verifiedName")
		name := debugInfoField(contact, "name")
		notify := debugInfoField(contact, "notify")
		bestName := verifiedName
		if bestName == "" {
			bestName = name
		}
		if bestName == "" {
			bestName = notify
		}
		if bestName != "" {
			reporte.WriteString("✅ *Store Caché:* " + bestName + "\n")
			origen := "Notificación"
			if verifiedName != "" {
				origen = "Verificado"
			} else if name != "" {
				origen = "Agenda"
			}
			reporte.WriteString("   ↳ _Origen: " + origen + "_\n")
		} else {
			reporte.WriteString("❌ *Store Caché:* Usuario encontrado en memoria pero sin nombre.\n")
		}
	} else {
		reporte.WriteString("❌ *Store Caché:* Indisponible.\n")
		reporte.WriteString("   ↳ _Falla: No hay registros previos de interacción con esta entidad._\n")
	}

	if err := ctx.SendQuoted(core.TextMessage(reporte.String()), ctx.Message); err != nil {
		fmtPrintDebugInfoError(err)
		return nil
	}
	_ = ctx.React("✅")
	return nil
}

func fmtPrintDebugInfoError(err error) {
	if err != nil {
		println("❌ Error de depuración: " + err.Error())
	}
}

func debugInfoContextInfo(msg *events.Message) *waE2E.ContextInfo {
	if msg == nil || msg.Message == nil {
		return nil
	}
	ext := msg.Message.GetExtendedTextMessage()
	if ext == nil {
		return nil
	}
	return ext.GetContextInfo()
}

func debugInfoParticipant(msg *events.Message) string {
	if msg == nil || !msg.Info.IsGroup {
		return ""
	}
	return msg.Info.Sender.String()
}

func debugInfoTarget(ctx *core.Context) string {
	info := debugInfoContextInfo(ctx.Message)
	if info != nil {
		if p := info.GetParticipant(); p != "" {
			return p
		}
		if mentions := info.GetMentionedJID(); len(mentions) > 0 {
			return mentions[0]
		}
	}
	if p := debugInfoParticipant(ctx.Message); p != "" {
		return p
	}
	return ctx.ChatJID().String()
}

func debugInfoField(c store.Contact, key string) string {
	if c == nil {
		return ""
	}
	if v, ok := c[key].(string); ok {
		return v
	}
	return ""
}
