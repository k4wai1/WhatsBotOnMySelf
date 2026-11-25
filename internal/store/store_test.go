package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestLoadArchivoInexistente(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "no-existe.json"))
	if err := s.Load(); err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if s.Len() != 0 {
		t.Fatalf("esperaba store vacío, tiene %d", s.Len())
	}
}

func TestLoadCargaEsquemaExistente(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baileys_store_cache.json")
	datos := map[string]any{
		"13172681498808@lid": map[string]any{"id": "13172681498808@lid", "notify": "Carlos"},
		"58412345678@s.whatsapp.net": map[string]any{
			"id":     "58412345678@s.whatsapp.net",
			"notify": "Luis",
			"name":   "Luis Pérez",
		},
	}
	raw, _ := json.Marshal(datos)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	s := New(path)
	if err := s.Load(); err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if s.Len() != 2 {
		t.Fatalf("Len=%d, esperaba 2", s.Len())
	}
	if got := s.Notify("13172681498808@lid"); got != "Carlos" {
		t.Fatalf("notify=%q", got)
	}
	if got := s.Name("58412345678@s.whatsapp.net"); got != "Luis" {
		t.Fatalf("Name=%q", got)
	}
}

func TestLoadJSONInvalido(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	if err := os.WriteFile(path, []byte("{no-json"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(path)
	if err := s.Load(); err == nil {
		t.Fatal("esperaba error de parseo")
	}
	if s.Len() != 0 {
		t.Fatal("un caché inválido no debe cargar contactos")
	}
}

func TestUpsertGetNameDelete(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "cache.json"))

	s.Upsert("a@s.whatsapp.net", map[string]any{"id": "a@s.whatsapp.net", "notify": "Ana"})
	s.Upsert("a@s.whatsapp.net", map[string]any{"name": "Ana María"})

	c, ok := s.Get("a@s.whatsapp.net")
	if !ok {
		t.Fatal("el contacto debe existir")
	}
	if c["notify"] != "Ana" || c["name"] != "Ana María" {
		t.Fatalf("merge incorrecto: %#v", c)
	}

	// Get devuelve copia: mutar no afecta al store.
	c["notify"] = "Hackeado"
	if got := s.Notify("a@s.whatsapp.net"); got != "Ana" {
		t.Fatalf("Get debe devolver copia, notify=%q", got)
	}

	s.SetNotify("b@s.whatsapp.net", "Beto")
	if got := s.Name("b@s.whatsapp.net"); got != "Beto" {
		t.Fatalf("Name debe caer a notify, obtuve %q", got)
	}

	s.Delete("a@s.whatsapp.net")
	if _, ok := s.Get("a@s.whatsapp.net"); ok {
		t.Fatal("el contacto debía borrarse")
	}
	if s.Len() != 1 {
		t.Fatalf("Len=%d, esperaba 1", s.Len())
	}
}

func TestUpsertIgnoraVacios(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "cache.json"))
	s.Upsert("", map[string]any{"notify": "x"})
	s.Upsert("a@s.whatsapp.net", nil)
	s.SetNotify("b@s.whatsapp.net", "")
	if s.Len() != 0 {
		t.Fatalf("no debió crear contactos, Len=%d", s.Len())
	}
}

func TestSaveYRecargaConservaEsquema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baileys_store_cache.json")
	s := New(path)
	s.Upsert("58412345678@s.whatsapp.net", map[string]any{
		"id":          "58412345678@s.whatsapp.net",
		"notify":      "Luis",
		"customField": "se-conserva",
	})

	if err := s.Save(); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("JSON inválido: %v", err)
	}
	contacto := parsed["58412345678@s.whatsapp.net"]
	if contacto["id"] != "58412345678@s.whatsapp.net" || contacto["notify"] != "Luis" {
		t.Fatalf("esquema inesperado: %#v", contacto)
	}
	if contacto["customField"] != "se-conserva" {
		t.Fatal("los campos desconocidos deben conservarse")
	}

	otro := New(path)
	if err := otro.Load(); err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if otro.Len() != 1 || otro.Notify("58412345678@s.whatsapp.net") != "Luis" {
		t.Fatalf("recarga incorrecta: %#v", otro.Snapshot())
	}
}

func TestPurePN(t *testing.T) {
	casos := map[string]string{
		"58412345678@s.whatsapp.net":    "58412345678@s.whatsapp.net",
		"58412345678:12@s.whatsapp.net": "58412345678@s.whatsapp.net",
		"13172681498808@lid":            "13172681498808@s.whatsapp.net",
		"120363000000000000@g.us":       "120363000000000000@s.whatsapp.net",
		"":                              "",
	}
	for entrada, esperado := range casos {
		if got := PurePN(entrada); got != esperado {
			t.Errorf("PurePN(%q)=%q, esperaba %q", entrada, got, esperado)
		}
	}
}

func TestConcurrencia(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "cache.json"))

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				s.Upsert("a@s.whatsapp.net", map[string]any{"notify": "x"})
				_ = s.Notify("a@s.whatsapp.net")
				_ = s.Name("a@s.whatsapp.net")
				_ = s.Snapshot()
				_ = s.Len()
			}
		}()
	}
	wg.Wait()

	if s.Len() != 1 {
		t.Fatalf("Len=%d, esperaba 1", s.Len())
	}
}

func TestAutoSaveYClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	s := New(path)
	s.StartAutoSave(10 * time.Millisecond)
	s.Upsert("a@s.whatsapp.net", map[string]any{"id": "a@s.whatsapp.net", "notify": "Ana"})

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("el autoguardado no creó el archivo")
		}
		time.Sleep(5 * time.Millisecond)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	recargado := New(path)
	if err := recargado.Load(); err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if recargado.Notify("a@s.whatsapp.net") != "Ana" {
		t.Fatal("el contacto no se persistió")
	}

	// Close es idempotente.
	if err := s.Close(); err != nil {
		t.Fatalf("segundo Close error: %v", err)
	}
}

func TestSaveCreaDirectorios(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "dir", "cache.json")
	s := New(path)
	s.Upsert("a@s.whatsapp.net", map[string]any{"id": "a@s.whatsapp.net"})
	if err := s.Save(); err != nil {
		t.Fatalf("Save error: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("el archivo no existe: %v", err)
	}
}
