package addons

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"go.mau.fi/whatsmeow/types"

	"whatsbot-go/internal/core"
)

const listMaxGroupsPorMensaje = 50

// List porta addons/list.js.
type List struct{}

func init() { core.Register(List{}) }

func (List) Commands() []string { return []string{"list"} }

// listGroupsFn es inyectable en pruebas.
var listGroupsFn = func(ctx *core.Context) ([]*types.GroupInfo, error) {
	if ctx.Client == nil {
		return nil, wagoErrNoClient
	}
	return ctx.Client.GetJoinedGroups(context.Background())
}

func (List) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}
	if err := ctx.React("⏳"); err != nil {
		return err
	}

	groups, err := listGroupsFn(ctx)
	if err != nil {
		slog.Error("❌ [list] Error:", "error", err)
		return ctx.React("❌")
	}
	if len(groups) == 0 {
		return ctx.Reply("📭 No estás en ningún grupo actualmente.")
	}

	for i := 0; i < len(groups); i += listMaxGroupsPorMensaje {
		end := i + listMaxGroupsPorMensaje
		if end > len(groups) {
			end = len(groups)
		}
		if err := ctx.SendText(listBuildPage(groups[i:end], i/listMaxGroupsPorMensaje+1, i)); err != nil {
			slog.Error("❌ [list] Error:", "error", err)
			return ctx.React("❌")
		}
	}
	return nil
}

// listBuildPage construye cada bloque de 50 grupos (mismo formato que list.js).
func listBuildPage(groups []*types.GroupInfo, pagina, offset int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "📋 *Lista de Grupos* (%d)\n\n", pagina)
	for idx, group := range groups {
		fmt.Fprintf(&b, "%d. *%s*\n   ID: `%s`\n   👥 Participantes: %d\n\n",
			offset+idx+1, group.Name, group.JID.ToNonAD().String(), len(group.Participants))
	}
	b.WriteString("💡 *Tip:* Copia el ID para usar en otros comandos")
	return b.String()
}
