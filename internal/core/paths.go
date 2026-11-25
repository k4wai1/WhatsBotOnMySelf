package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

var (
	rootOnce sync.Once
	rootPath string
)

// Root devuelve la raíz del proyecto original (donde viven index.js,
// assets/, cache/, statistics/...). Respeta BOT_ROOT y, si no, sube desde el
// directorio actual hasta encontrar la raíz del bot.
func Root() string {
	rootOnce.Do(func() {
		rootPath = resolveRoot()
	})
	return rootPath
}

// SetRoot fuerza la raíz (útil en tests).
func SetRoot(path string) {
	rootOnce.Do(func() {})
	rootPath = path
}

// DataPath une la raíz de datos con las partes indicadas.
func DataPath(parts ...string) string {
	return filepath.Join(append([]string{Root()}, parts...)...)
}

// ReadJSON lee un JSON de disco.
func ReadJSON(path string, v any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}

// WriteJSON escribe JSON con la misma indentación de 2 espacios que
// JSON.stringify(x, null, 2) y de forma atómica.
func WriteJSON(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')

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

	if _, err := tmp.Write(raw); err != nil {
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

func resolveRoot() string {
	if env := os.Getenv("BOT_ROOT"); env != "" {
		if abs, err := filepath.Abs(env); err == nil {
			return abs
		}
	}

	// 1) Directorio del ejecutable: permite lanzar `./whatsbot` desde
	// cualquier carpeta (systemd, cron, otra ruta del VPS).
	if exe, err := os.Executable(); err == nil {
		if dir := searchBotRoot(filepath.Dir(exe)); dir != "" {
			return dir
		}
	}

	// 2) Directorio actual y sus padres.
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	if dir := searchBotRoot(wd); dir != "" {
		return dir
	}
	return wd
}

// searchBotRoot sube como máximo 6 niveles buscando la raíz del bot.
func searchBotRoot(start string) string {
	dir := start
	for i := 0; i < 6; i++ {
		if isBotRoot(dir) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func isBotRoot(dir string) bool {
	// Raíz de datos/documentación del repositorio actual.
	if _, err := os.Stat(filepath.Join(dir, "assets", "config.json")); err == nil {
		return true
	}
	// Estructura del bot Go en la raíz del repositorio.
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
		if info, err := os.Stat(filepath.Join(dir, "cmd", "bot")); err == nil && info.IsDir() {
			return true
		}
	}
	// Compatibilidad con el bot Node.js (tag v1-nodejs-legacy).
	if _, err := os.Stat(filepath.Join(dir, "index.js")); err == nil {
		return true
	}
	return false
}
