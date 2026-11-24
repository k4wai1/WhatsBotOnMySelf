package addons

import (
	"os"
	"strings"
	"unicode/utf8"

	"whatsbot-go/internal/config"
	"whatsbot-go/internal/core"
)

// Prefix porta addons/prefix.js: .prefix y .setprefix.
type Prefix struct{}

// Commands cumple core.Addon.
func (Prefix) Commands() []string { return []string{"prefix", "setprefix"} }

// prefixConfigPath es la ruta de assets/config.json.
func prefixConfigPath() string { return core.DataPath("assets", "config.json") }

// prefixDefaults replica DEFAULT_PREFIXES: la instancia de rupa (con rupa.js)
// usa '.', el bot principal usa ',' '!' '/'.
func prefixDefaults() []string {
	if _, err := os.Stat(core.DataPath("rupa.js")); err == nil {
		return []string{"."}
	}
	return append([]string(nil), config.DefaultPrefixes...)
}

// prefixLoad lee los prefijos vigentes. Prioriza el Manager en caliente.
func prefixLoad() []string {
	if m := config.Default(); m != nil {
		return m.Prefixes()
	}
	cfg, err := config.Load(prefixConfigPath())
	if err != nil {
		return prefixDefaults()
	}
	return cfg.Prefixes
}

// prefixSave persiste la lista con el mismo esquema { "prefixes": [...] }.
func prefixSave(list []string) error {
	if m := config.Default(); m != nil {
		return m.SetPrefixes(list)
	}
	return config.Save(prefixConfigPath(), &config.Config{Prefixes: append([]string(nil), list...)})
}

// prefixPretty envuelve cada prefijo en backticks con el separador indicado.
func prefixPretty(list []string, sep string) string {
	parts := make([]string, 0, len(list))
	for _, p := range list {
		parts = append(parts, "`"+p+"`")
	}
	return strings.Join(parts, sep)
}

func prefixContains(list []string, p string) bool {
	for _, item := range list {
		if item == p {
			return true
		}
	}
	return false
}

// Handle replica las validaciones y mensajes del addon original.
func (p Prefix) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}

	isOwner := ctx.Message != nil && ctx.Message.Info.IsFromMe
	if !isOwner {
		return ctx.React("🚫")
	}

	prefixes := prefixLoad()
	var sub, arg string
	if len(ctx.Args) > 0 {
		sub = ctx.Args[0]
	}
	if len(ctx.Args) > 1 {
		arg = ctx.Args[1]
	}

	if sub == "" {
		text := "📌 *Prefixes activos:*\n" + prefixPretty(prefixes, "  ") +
			"\n\nTotal: " + miscItoa(int64(len(prefixes)))
		return ctx.SendText(text)
	}

	switch sub {
	case "add":
		if arg == "" || utf8.RuneCountInString(arg) > 3 {
			return ctx.React("❓")
		}
		if prefixContains(prefixes, arg) {
			return ctx.SendText("⚠️ `" + arg + "` ya existe.")
		}
		prefixes = append(prefixes, arg)
		if err := prefixSave(prefixes); err != nil {
			return err
		}
		return ctx.SendText("✅ `" + arg + "` añadido.")

	case "remove", "rm":
		if arg == "" {
			return ctx.React("❓")
		}
		idx := -1
		for i, item := range prefixes {
			if item == arg {
				idx = i
				break
			}
		}
		if idx == -1 {
			return ctx.SendText("❓ `" + arg + "` no está en la lista.")
		}
		if len(prefixes) <= 1 {
			return ctx.SendText("⚠️ Debe haber al menos 1 prefix.")
		}
		prefixes = append(prefixes[:idx], prefixes[idx+1:]...)
		if err := prefixSave(prefixes); err != nil {
			return err
		}
		return ctx.SendText("🗑️ `" + arg + "` eliminado.")

	case "default":
		defaults := prefixDefaults()
		if err := prefixSave(defaults); err != nil {
			return err
		}
		return ctx.SendText("🔄 Prefixes restaurados: " + prefixPretty(defaults, " "))
	}

	help := "📌 *Comandos de prefix*\n" +
		"─────────────\n" +
		"`prefix`              → ver lista actual\n" +
		"`prefix add <p>`      → añadir prefix\n" +
		"`prefix remove <p>`   → eliminar prefix\n" +
		"`prefix rm <p>`       → eliminar prefix (alias)\n" +
		"`prefix default`      → restaurar valores por defecto"
	return ctx.SendText(help)
}

func init() {
	core.Register(Prefix{})
}
