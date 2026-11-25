package addons

import (
	"log/slog"

	"whatsbot-go/internal/core"
)

// Id porta addons/id.js: muestra el JID del chat actual.
type Id struct{}

func init() { core.Register(Id{}) }

func (Id) Commands() []string { return []string{"id"} }

func (Id) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}
	text := "📝 *Info del Chat*\n\n🆔 *ID:* `" + ctx.ChatJID().String() + "`"
	if err := ctx.Reply(text); err != nil {
		slog.Error("❌ [id] Error:", "error", err)
		return ctx.React("❌")
	}
	return nil
}
