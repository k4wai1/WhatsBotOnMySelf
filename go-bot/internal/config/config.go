// Package config lee y escribe assets/config.json manteniendo el mismo
// esquema que el bot original en Node.js: { "prefixes": [",", "!", "/"] }.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// DefaultPrefixes replica loadPrefixes() de index.js cuando no hay config
// válida en disco.
var DefaultPrefixes = []string{",", "!", "/"}

// Config es el contenido de assets/config.json.
type Config struct {
	Prefixes []string `json:"prefixes"`
}

// normalize garantiza que siempre haya prefijos utilizables.
func (c *Config) normalize() {
	if len(c.Prefixes) == 0 {
		c.Prefixes = append([]string(nil), DefaultPrefixes...)
	}
}

// New devuelve una configuración con los prefijos por defecto ya aplicados.
func New() *Config {
	return &Config{Prefixes: append([]string(nil), DefaultPrefixes...)}
}

// Load lee el archivo indicado. Si no existe lo crea con los valores por
// defecto (igual que el arranque de index.js). Un JSON inválido no impide
// arrancar: se devuelven los prefijos por defecto y el error de parseo.
func Load(path string) (*Config, error) {
	cfg := New()

	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if saveErr := Save(path, cfg); saveErr != nil {
				return cfg, saveErr
			}
			return cfg, nil
		}
		return cfg, err
	}

	var parsed Config
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return cfg, fmt.Errorf("config.json inválido: %w", err)
	}
	parsed.normalize()
	return &parsed, nil
}

// Save escribe la configuración con la misma indentación de 2 espacios que
// usaba JSON.stringify(cfg, null, 2).
func Save(path string, cfg *Config) error {
	if cfg == nil {
		cfg = New()
	}
	cfg.normalize()

	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return writeAtomic(path, raw, 0o644)
}

// Ensure crea el archivo si no existe y devuelve la configuración vigente.
func Ensure(path string) (*Config, error) {
	if _, err := os.Stat(path); err == nil {
		return Load(path)
	}
	return Load(path)
}

// writeAtomic escribe en un temporal y renombra para no dejar el archivo a
// medias si el proceso muere.
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// Manager envuelve Config con acceso concurrente, útil para el router (que
// consulta los prefijos en cada mensaje) y para el addon .prefix.
type Manager struct {
	mu   sync.RWMutex
	path string
	cfg  *Config
}

// NewManager carga (o crea) la configuración del path indicado.
func NewManager(path string) (*Manager, error) {
	cfg, err := Load(path)
	m := &Manager{path: path, cfg: cfg}
	if err != nil {
		return m, err
	}
	return m, nil
}

// Path devuelve la ruta del archivo gestionado.
func (m *Manager) Path() string { return m.path }

// Prefixes es una copia segura de la lista actual.
func (m *Manager) Prefixes() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]string(nil), m.cfg.Prefixes...)
}

// SetPrefixes reemplaza la lista, la normaliza y la persiste.
func (m *Manager) SetPrefixes(list []string) error {
	cfg := &Config{Prefixes: append([]string(nil), list...)}
	cfg.normalize()

	m.mu.Lock()
	m.cfg = cfg
	m.mu.Unlock()

	return Save(m.path, cfg)
}

// Config devuelve una copia de la configuración vigente.
func (m *Manager) Config() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return Config{Prefixes: append([]string(nil), m.cfg.Prefixes...)}
}

var (
	defaultMu sync.RWMutex
	defaultM  *Manager
)

// SetDefault registra el Manager global que usan los addons (p. ej. .prefix)
// para que los prefijos cambien en caliente sin reiniciar.
func SetDefault(m *Manager) {
	defaultMu.Lock()
	defaultM = m
	defaultMu.Unlock()
}

// Default devuelve el Manager global o nil si aún no se registró.
func Default() *Manager {
	defaultMu.RLock()
	defer defaultMu.RUnlock()
	return defaultM
}
