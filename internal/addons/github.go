package addons

import (
	"log/slog"

	"whatsbot-go/internal/core"
)

const githubRepoURL = "https://github.com/k4wai1/WhatsBotOnMySelf"

// Github porta addons/github.js.
type Github struct{}

// Commands replica los alias del addon original.
func (Github) Commands() []string { return []string{"git", "github", "repo"} }

// Handle envía el enlace del repositorio.
func (Github) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}

	if err := ctx.React("🚀"); err != nil {
		slog.Error("Error en github.js:", "error", err)
		_ = ctx.React("❌")
		return nil
	}

	text := "🤖 *WhatsBotOnMySelf*\n\n🔗 " + githubRepoURL
	if err := ctx.SendQuoted(core.TextMessage(text), ctx.Message); err != nil {
		slog.Error("Error en github.js:", "error", err)
		_ = ctx.React("❌")
	}
	return nil
}

func init() { core.Register(Github{}) }
