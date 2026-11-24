package addons

import (
	"path/filepath"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"whatsbot-go/internal/config"
	"whatsbot-go/internal/core"
)

func prefixSetupManager(t *testing.T) (*config.Manager, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	m, err := config.NewManager(path)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	config.SetDefault(m)
	t.Cleanup(func() { config.SetDefault(nil) })
	return m, path
}

func prefixCtx(c *capture, cmd string, args ...string) *core.Context {
	ctx := c.newCtx(cmd, args...)
	ctx.Message = &events.Message{Info: types.MessageInfo{MessageSource: types.MessageSource{IsFromMe: true}}}
	return ctx
}

func prefixReadPrefixes(t *testing.T, path string) []string {
	t.Helper()
	var stored struct {
		Prefixes []string `json:"prefixes"`
	}
	if err := core.ReadJSON(path, &stored); err != nil {
		t.Fatalf("leyendo config: %v", err)
	}
	return stored.Prefixes
}

func TestPrefixCommands(t *testing.T) {
	got := Prefix{}.Commands()
	if len(got) != 2 || got[0] != "prefix" || got[1] != "setprefix" {
		t.Fatalf("comandos inesperados: %#v", got)
	}
}

func TestPrefixSoloHost(t *testing.T) {
	prefixSetupManager(t)

	c := &capture{}
	// Sin mensaje (o con fromMe=false) no es el host.
	if err := (Prefix{}).Handle(c.newCtx("prefix")); err != nil {
		t.Fatalf("Handle devolvió error: %v", err)
	}
	if len(c.reactions) != 1 || c.reactions[0] != "🚫" {
		t.Fatalf("esperaba 🚫, obtuve %#v", c.reactions)
	}
	if len(c.texts) != 0 {
		t.Fatalf("no esperaba texto, obtuve %#v", c.texts)
	}
}

func TestPrefixLista(t *testing.T) {
	prefixSetupManager(t)

	c := &capture{}
	if err := (Prefix{}).Handle(prefixCtx(c, "prefix")); err != nil {
		t.Fatalf("Handle devolvió error: %v", err)
	}
	if len(c.texts) != 1 {
		t.Fatalf("esperaba 1 texto, obtuve %#v", c.texts)
	}
	for _, fragment := range []string{"📌 *Prefixes activos:*", "`,`  `!`  `/`", "Total: 3"} {
		if !strings.Contains(c.texts[0], fragment) {
			t.Fatalf("falta %q en %q", fragment, c.texts[0])
		}
	}
}

func TestPrefixAdd(t *testing.T) {
	_, path := prefixSetupManager(t)

	c := &capture{}
	if err := (Prefix{}).Handle(prefixCtx(c, "prefix", "add", "$")); err != nil {
		t.Fatalf("Handle(add) devolvió error: %v", err)
	}
	if len(c.texts) != 1 || c.texts[0] != "✅ `$` añadido." {
		t.Fatalf("texto inesperado: %#v", c.texts)
	}
	got := prefixReadPrefixes(t, path)
	if len(got) != 4 || got[3] != "$" {
		t.Fatalf("prefijos guardados inesperados: %#v", got)
	}

	dup := &capture{}
	if err := (Prefix{}).Handle(prefixCtx(dup, "prefix", "add", ",")); err != nil {
		t.Fatalf("Handle(add duplicado) devolvió error: %v", err)
	}
	if len(dup.texts) != 1 || dup.texts[0] != "⚠️ `,` ya existe." {
		t.Fatalf("texto inesperado: %#v", dup.texts)
	}

	long := &capture{}
	if err := (Prefix{}).Handle(prefixCtx(long, "prefix", "add", "abcd")); err != nil {
		t.Fatalf("Handle(add largo) devolvió error: %v", err)
	}
	if len(long.reactions) != 1 || long.reactions[0] != "❓" {
		t.Fatalf("esperaba ❓, obtuve %#v", long.reactions)
	}
}

func TestPrefixRemove(t *testing.T) {
	_, path := prefixSetupManager(t)

	c := &capture{}
	if err := (Prefix{}).Handle(prefixCtx(c, "prefix", "rm", "!")); err != nil {
		t.Fatalf("Handle(rm) devolvió error: %v", err)
	}
	if len(c.texts) != 1 || c.texts[0] != "🗑️ `!` eliminado." {
		t.Fatalf("texto inesperado: %#v", c.texts)
	}
	if got := prefixReadPrefixes(t, path); len(got) != 2 || got[0] != "," || got[1] != "/" {
		t.Fatalf("prefijos guardados inesperados: %#v", got)
	}

	missing := &capture{}
	if err := (Prefix{}).Handle(prefixCtx(missing, "prefix", "remove", "?")); err != nil {
		t.Fatalf("Handle(remove missing) devolvió error: %v", err)
	}
	if len(missing.texts) != 1 || missing.texts[0] != "❓ `?` no está en la lista." {
		t.Fatalf("texto inesperado: %#v", missing.texts)
	}

	if err := (Prefix{}).Handle(prefixCtx(&capture{}, "prefix", "remove", ",")); err != nil {
		t.Fatalf("Handle(remove) devolvió error: %v", err)
	}
	last := &capture{}
	if err := (Prefix{}).Handle(prefixCtx(last, "prefix", "remove", "/")); err != nil {
		t.Fatalf("Handle(remove último) devolvió error: %v", err)
	}
	if len(last.texts) != 1 || last.texts[0] != "⚠️ Debe haber al menos 1 prefix." {
		t.Fatalf("texto inesperado: %#v", last.texts)
	}
}

func TestPrefixDefault(t *testing.T) {
	m, path := prefixSetupManager(t)
	if err := m.SetPrefixes([]string{"x"}); err != nil {
		t.Fatalf("SetPrefixes: %v", err)
	}

	c := &capture{}
	if err := (Prefix{}).Handle(prefixCtx(c, "prefix", "default")); err != nil {
		t.Fatalf("Handle(default) devolvió error: %v", err)
	}
	if len(c.texts) != 1 || c.texts[0] != "🔄 Prefixes restaurados: `,` `!` `/`" {
		t.Fatalf("texto inesperado: %#v", c.texts)
	}
	if got := prefixReadPrefixes(t, path); len(got) != 3 {
		t.Fatalf("prefijos guardados inesperados: %#v", got)
	}
}

func TestPrefixAyuda(t *testing.T) {
	prefixSetupManager(t)

	c := &capture{}
	if err := (Prefix{}).Handle(prefixCtx(c, "prefix", "otra-cosa")); err != nil {
		t.Fatalf("Handle(ayuda) devolvió error: %v", err)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "📌 *Comandos de prefix*") {
		t.Fatalf("texto inesperado: %#v", c.texts)
	}
}
