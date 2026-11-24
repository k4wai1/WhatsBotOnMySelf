package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestLoadCreaArchivoConDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "assets", "config.json")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load devolvió error: %v", err)
	}
	if !reflect.DeepEqual(cfg.Prefixes, DefaultPrefixes) {
		t.Fatalf("prefijos=%#v, esperaba %#v", cfg.Prefixes, DefaultPrefixes)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("el archivo no se creó: %v", err)
	}
	var enDisco Config
	if err := json.Unmarshal(raw, &enDisco); err != nil {
		t.Fatalf("JSON inválido en disco: %v", err)
	}
	if !reflect.DeepEqual(enDisco.Prefixes, DefaultPrefixes) {
		t.Fatalf("en disco=%#v", enDisco.Prefixes)
	}
}

func TestLoadLeeConfigExistente(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"prefixes":["#","."]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load devolvió error: %v", err)
	}
	if !reflect.DeepEqual(cfg.Prefixes, []string{"#", "."}) {
		t.Fatalf("prefijos=%#v", cfg.Prefixes)
	}
}

func TestLoadJSONInvalidoUsaDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{esto no es json`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err == nil {
		t.Fatal("esperaba error de parseo")
	}
	if !reflect.DeepEqual(cfg.Prefixes, DefaultPrefixes) {
		t.Fatalf("debe caer a los defaults, obtuve %#v", cfg.Prefixes)
	}
}

func TestLoadListaVaciaUsaDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"prefixes":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if !reflect.DeepEqual(cfg.Prefixes, DefaultPrefixes) {
		t.Fatalf("prefijos=%#v", cfg.Prefixes)
	}
}

func TestSaveFormatoDosEspacios(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Save(path, &Config{Prefixes: []string{"!", "/"}}); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	texto := string(raw)
	if !strings.Contains(texto, "\n  \"prefixes\"") {
		t.Fatalf("se esperaba indentación de 2 espacios, archivo:\n%s", texto)
	}
	if !strings.HasSuffix(texto, "\n") {
		t.Fatal("el archivo debe terminar en salto de línea")
	}
}

func TestSaveNilUsaDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Save(path, nil); err != nil {
		t.Fatalf("Save error: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if !reflect.DeepEqual(cfg.Prefixes, DefaultPrefixes) {
		t.Fatalf("prefijos=%#v", cfg.Prefixes)
	}
}

func TestEnsureCreaYReutiliza(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg, err := Ensure(path)
	if err != nil {
		t.Fatalf("Ensure error: %v", err)
	}
	if !reflect.DeepEqual(cfg.Prefixes, DefaultPrefixes) {
		t.Fatalf("prefijos=%#v", cfg.Prefixes)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Ensure no creó el archivo: %v", err)
	}
}

func TestManagerSetPrefixesPersiste(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	m, err := NewManager(path)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	if err := m.SetPrefixes([]string{"#"}); err != nil {
		t.Fatalf("SetPrefixes error: %v", err)
	}
	if got := m.Prefixes(); !reflect.DeepEqual(got, []string{"#"}) {
		t.Fatalf("Prefixes()=%#v", got)
	}

	otro, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if !reflect.DeepEqual(otro.Prefixes, []string{"#"}) {
		t.Fatalf("no se persistió: %#v", otro.Prefixes)
	}

	if err := m.SetPrefixes(nil); err != nil {
		t.Fatalf("SetPrefixes(nil) error: %v", err)
	}
	if got := m.Prefixes(); !reflect.DeepEqual(got, DefaultPrefixes) {
		t.Fatalf("una lista vacía debe restaurar defaults, obtuve %#v", got)
	}
}

func TestManagerLecturaConcurrente(t *testing.T) {
	m, err := NewManager(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = m.Prefixes()
			}
		}()
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				_ = m.SetPrefixes([]string{"!", "/"})
				_ = m.Config()
			}
		}(i)
	}
	wg.Wait()

	cfg := m.Config()
	if len(cfg.Prefixes) != 2 {
		t.Fatalf("prefijos finales inesperados: %#v", cfg.Prefixes)
	}
	cfg.Prefixes[0] = "MUTADO"
	if m.Config().Prefixes[0] == "MUTADO" {
		t.Fatal("Config() debe devolver una copia, no la lista interna")
	}
}
