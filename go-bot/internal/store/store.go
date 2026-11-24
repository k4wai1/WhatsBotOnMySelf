// Package store replica el `store.contacts` de index.js: un mapa de contactos
// por JID que se persiste en baileys_store_cache.json cada 10 segundos.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Contact conserva el esquema libre del caché original ({id, notify, ...}),
// de modo que las claves desconocidas no se pierden al reescribir el archivo.
type Contact map[string]any

// Store es un mapa de contactos protegido con RWMutex.
type Store struct {
	mu       sync.RWMutex
	contacts map[string]Contact
	path     string
	logger   *slog.Logger

	stopOnce sync.Once
	stopCh   chan struct{}
	doneCh   chan struct{}
}

// New crea un store vacío asociado al archivo de caché indicado.
func New(path string) *Store {
	return &Store{
		contacts: make(map[string]Contact),
		path:     path,
		logger:   slog.Default(),
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

// SetLogger permite silenciar o etiquetar los avisos del store.
func (s *Store) SetLogger(l *slog.Logger) {
	if l != nil {
		s.logger = l
	}
}

// Path devuelve la ruta del caché.
func (s *Store) Path() string { return s.path }

// Load lee el caché de disco. Si no existe, deja el store vacío sin error.
func (s *Store) Load() error {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	loaded := make(map[string]Contact)
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, &loaded); err != nil {
		return fmt.Errorf("caché de contactos inválido: %w", err)
	}
	if loaded == nil {
		loaded = make(map[string]Contact)
	}

	s.mu.Lock()
	s.contacts = loaded
	s.mu.Unlock()
	return nil
}

// Save persiste el mapa completo con la misma indentación de 2 espacios del
// bot original.
func (s *Store) Save() error {
	s.mu.RLock()
	snapshot := cloneMap(s.contacts)
	s.mu.RUnlock()

	raw, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return writeAtomic(s.path, raw)
}

// Len devuelve cuántos contactos hay.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.contacts)
}

// Snapshot devuelve una copia profunda del mapa (segura para serializar).
func (s *Store) Snapshot() map[string]Contact {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneMap(s.contacts)
}

// Get devuelve una copia del contacto.
func (s *Store) Get(jid string) (Contact, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.contacts[jid]
	if !ok {
		return nil, false
	}
	return cloneContact(c), true
}

// Upsert fusiona los campos dados con el contacto existente, replicando
// Object.assign(store.contacts[id], update) de index.js.
func (s *Store) Upsert(jid string, fields map[string]any) {
	if jid == "" || len(fields) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	c, ok := s.contacts[jid]
	if !ok || c == nil {
		c = make(Contact)
		s.contacts[jid] = c
	}
	for k, v := range fields {
		c[k] = v
	}
}

// Delete elimina un contacto del mapa (usado en contactos eliminados).
func (s *Store) Delete(jid string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.contacts, jid)
}

// SetNotify guarda {id, notify} como hace el interceptor de mensajes con
// pushName.
func (s *Store) SetNotify(jid, notify string) {
	if jid == "" || notify == "" {
		return
	}
	s.Upsert(jid, map[string]any{"id": jid, "notify": notify})
}

// Notify devuelve el pushName guardado para el JID ("" si no hay).
func (s *Store) Notify(jid string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if c, ok := s.contacts[jid]; ok {
		if v, ok := c["notify"].(string); ok {
			return v
		}
	}
	return ""
}

// Name intenta notify y cae a name/verifiedName, útil para addons que
// resuelven nombres.
func (s *Store) Name(jid string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.contacts[jid]
	if !ok {
		return ""
	}
	for _, key := range []string{"notify", "name", "verifiedName"} {
		if v, ok := c[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// StartAutoSave lanza el guardado periódico (10s por defecto en main) en una
// goroutine, igual que setInterval de index.js.
func (s *Store) StartAutoSave(interval time.Duration) {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	go func() {
		defer close(s.doneCh)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := s.Save(); err != nil {
					s.logger.Warn("no se pudo guardar el caché de contactos", "error", err)
				}
			case <-s.stopCh:
				return
			}
		}
	}()
}

// Close detiene el autoguardado y hace un último Save.
func (s *Store) Close() error {
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	select {
	case <-s.doneCh:
	case <-time.After(2 * time.Second):
	}
	return s.Save()
}

// PurePN replica el cálculo del bot original para el remitente:
// sender.split('@')[0].split(':')[0] + '@s.whatsapp.net'.
func PurePN(jid string) string {
	if jid == "" {
		return ""
	}
	base := strings.SplitN(jid, "@", 2)[0]
	base = strings.SplitN(base, ":", 2)[0]
	if base == "" {
		return ""
	}
	return base + "@s.whatsapp.net"
}

func cloneContact(c Contact) Contact {
	out := make(Contact, len(c))
	for k, v := range c {
		out[k] = v
	}
	return out
}

func cloneMap(m map[string]Contact) map[string]Contact {
	out := make(map[string]Contact, len(m))
	for k, v := range m {
		out[k] = cloneContact(v)
	}
	return out
}

func writeAtomic(path string, data []byte) error {
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
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
