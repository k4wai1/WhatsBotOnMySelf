package addons

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"whatsbot-go/internal/core"
)

func TestHelpCommands(t *testing.T) {
	got := Help{}.Commands()
	if len(got) != 2 || got[0] != "help" || got[1] != "ayuda" {
		t.Fatalf("comandos inesperados: %#v", got)
	}
}

func TestHelpComputeHashCambiaConElContenido(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "uno.go"), []byte("package addons"), 0o644); err != nil {
		t.Fatal(err)
	}
	prevSrc := helpSrcAlt
	helpSrcAlt = dir
	t.Cleanup(func() { helpSrcAlt = prevSrc })

	h1, err := helpComputeHash()
	if err != nil {
		t.Fatalf("helpComputeHash: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "uno.go"), []byte("package addons // más contenido"), 0o644); err != nil {
		t.Fatal(err)
	}
	h2, err := helpComputeHash()
	if err != nil {
		t.Fatalf("helpComputeHash: %v", err)
	}
	if h1 == h2 {
		t.Fatal("el hash debe cambiar cuando cambia el código")
	}

	// Los tests y el propio help.go se ignoran.
	if err := os.WriteFile(filepath.Join(dir, "help.go"), []byte("package addons // distinto"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x_test.go"), []byte("package addons // test"), 0o644); err != nil {
		t.Fatal(err)
	}
	h3, _ := helpComputeHash()
	if h3 != h2 {
		t.Fatal("help.go y *_test.go no deben afectar al hash")
	}
}

func TestHelpLoadCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "help_cache.json")
	if err := os.WriteFile(path, []byte(`{"generatedAt":1,"addonsHash":"abc","groups":[{"commands":[".dado"],"preview":"dado","technical":"t"}],"aliasMap":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	prev := helpCacheAlt
	helpCacheAlt = path
	t.Cleanup(func() { helpCacheAlt = prev })

	data := helpLoadCache()
	if data == nil || len(data.Groups) != 1 || data.Groups[0].Commands[0] != ".dado" {
		t.Fatalf("caché inesperado: %#v", data)
	}
	if data.AliasMap == nil {
		t.Fatal("aliasMap debía inicializarse")
	}

	if err := os.WriteFile(path, []byte("{roto"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := helpLoadCache(); got != nil {
		t.Fatalf("un caché inválido debe devolver nil: %#v", got)
	}
}

func TestHelpAnalyzeFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		content := `[{"commands":[".demo",".d"],"preview":"Demo","technical":"Hace algo"}]`
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": content}}},
		})
	}))
	defer srv.Close()

	prevURL, prevHTTP := helpAPIURL, helpHTTP
	helpAPIURL = srv.URL
	helpHTTP = srv.Client()
	t.Cleanup(func() { helpAPIURL, helpHTTP = prevURL, prevHTTP })

	groups, err := helpAnalyzeFile("module.exports = { commands: ['demo', 'd'] }")
	if err != nil {
		t.Fatalf("helpAnalyzeFile: %v", err)
	}
	if len(groups) != 1 || groups[0].Preview != "Demo" {
		t.Fatalf("grupos inesperados: %#v", groups)
	}
	if len(groups[0].Commands) != 2 || groups[0].Commands[0] != ".demo" {
		t.Fatalf("comandos inesperados: %#v", groups[0].Commands)
	}
}

func TestHelpAnalyzeFileErrorHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	prevURL, prevHTTP := helpAPIURL, helpHTTP
	helpAPIURL = srv.URL
	helpHTTP = srv.Client()
	t.Cleanup(func() { helpAPIURL, helpHTTP = prevURL, prevHTTP })

	if _, err := helpAnalyzeFile("code"); err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("esperaba error 500, obtuve %v", err)
	}
}

var _ = core.DataPath
