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

func helpTestEntries() []helpEntry {
	return []helpEntry{
		{
			Command: "sa", Aliases: []string{"sticker", "s"}, Title: "Stickers con sonido",
			Category: "stickers", Description: "Crea stickers que reproducen audio al tocarlos.",
			HowTo:    []string{"Responde a un video corto con {p}sa", "O envía una foto con {p}sa y luego el audio"},
			Options:  []string{"crop — recorta en cuadrado", "circular — forma redonda"},
			Examples: []string{"{p}sa (respondiendo a un video)", "{p}sa crop 0:02 0:05"},
		},
		{
			Command: "yt", Title: "Videos de YouTube", Category: "downloads",
			Description: "Descarga un video para compartirlo.", Examples: []string{"{p}yt <enlace>"},
		},
		{
			Command: "dado", Title: "Tirar dados", Category: "games",
			Description: "Tira dados y muestra el resultado.",
		},
		{
			Command: "debug-st", Title: "Diagnóstico interno", Category: "admin",
			Description: "Revisión interna del bot.", AdminOnly: true,
		},
		{
			Command: "secreto", Title: "Oculto", Category: "utils",
			Description: "No visible.", Hidden: true,
		},
		{
			Command: "st", Aliases: []string{"sticker", "s"}, Title: "Crear stickers",
			Category: "stickers", Description: "Convierte fotos o videos en stickers.",
			HowTo: []string{"Envía una foto con {p}st"}, Examples: []string{"{p}st"},
		},
	}
}

func helpTestData() *helpCacheData {
	entries := helpTestEntries()
	return &helpCacheData{
		Version: helpCacheVersion, GeneratedAt: 1, AddonsHash: "abc",
		Entries: entries, AliasMap: helpBuildAliasMap(entries),
	}
}

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
	data := helpTestData()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	prev := helpCacheAlt
	helpCacheAlt = path
	t.Cleanup(func() { helpCacheAlt = prev })

	loaded := helpLoadCache()
	if loaded == nil || loaded.Version != helpCacheVersion || len(loaded.Entries) != 6 {
		t.Fatalf("caché inesperado: %#v", loaded)
	}
	if loaded.AliasMap["sa"] != 0 || loaded.AliasMap[".sticker"] != 5 {
		t.Fatalf("aliasMap inesperado: %#v", loaded.AliasMap)
	}

	old := `{"generatedAt":1,"addonsHash":"abc","groups":[{"commands":[".dado"],"preview":"dado","technical":"t"}],"aliasMap":{}}`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := helpLoadCache(); got != nil {
		t.Fatalf("una caché anterior a la versión 2 debe regenerarse: %#v", got)
	}
	if err := os.WriteFile(path, []byte("{roto"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := helpLoadCache(); got != nil {
		t.Fatalf("un caché inválido debe devolver nil: %#v", got)
	}
}

func TestHelpParseLLMJSONLimpieza(t *testing.T) {
	contenido := "Aquí tienes:\n```json\n[{\"command\":\".SA\",\"aliases\":[\".s\"],\"title\":\"Prueba\",\"category\":\"inventada\",\"description\":\"Hace algo.\",\"how_to\":[\"Usa {p}sa\"],\"options\":[],\"examples\":[\"{p}sa\"]}]\n```\nFin."
	entries, err := helpParseLLMEntries(contenido)
	if err != nil {
		t.Fatalf("helpParseLLMEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entradas inesperadas: %#v", entries)
	}
	entry := helpNormalizeEntry(entries[0])
	if entry.Command != "sa" || entry.Category != "utils" {
		t.Fatalf("entrada normalizada inesperada: %+v", entry)
	}
	if entry.Aliases[0] != "s" {
		t.Fatalf("alias inesperado: %#v", entry.Aliases)
	}

	if _, err := helpParseLLMEntries("no es json"); err == nil {
		t.Fatal("esperaba error con texto sin JSON")
	}
}

func TestHelpAnalyzeFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		content := "```json\n[{\"command\":\"demo\",\"aliases\":[\"d\"],\"title\":\"Demo\",\"category\":\"utils\",\"description\":\"Hace algo simple.\",\"how_to\":[\"Escribe {p}demo\"],\"options\":[],\"examples\":[\"{p}demo\"]}]\n```"
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": content}}},
		})
	}))
	defer srv.Close()

	prevURL, prevHTTP := helpAPIURL, helpHTTP
	helpAPIURL = srv.URL
	helpHTTP = srv.Client()
	t.Cleanup(func() { helpAPIURL, helpHTTP = prevURL, prevHTTP })

	entries, err := helpAnalyzeFile("package addons // commands: demo, d")
	if err != nil {
		t.Fatalf("helpAnalyzeFile: %v", err)
	}
	if len(entries) != 1 || entries[0].Command != "demo" || entries[0].Title != "Demo" {
		t.Fatalf("entradas inesperadas: %#v", entries)
	}
	if len(entries[0].Aliases) != 1 || entries[0].Aliases[0] != "d" {
		t.Fatalf("alias inesperados: %#v", entries[0].Aliases)
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

func TestHelpLimpiaComandosBorradosAlActualizar(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "prueba")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sa.go"), []byte("package addons"), 0o644); err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(t.TempDir(), "help_cache.json")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		content := `[{"command":"sa","title":"Stickers con sonido","category":"stickers","description":"Crea stickers con audio."},{"command":"borrado","title":"Ya no existe","category":"utils","description":"Este comando se eliminó."}]`
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": content}}},
		})
	}))
	defer srv.Close()

	prevSrc, prevCache, prevHTTP, prevURL, prevLive, prevData := helpSrcAlt, helpCacheAlt, helpHTTP, helpAPIURL, helpLiveCommand, helpData
	helpSrcAlt, helpCacheAlt, helpAPIURL, helpHTTP = dir, cachePath, srv.URL, srv.Client()
	helpLiveCommand = func() []string { return []string{"sa", "other"} }
	helpData = nil
	t.Cleanup(func() {
		helpSrcAlt, helpCacheAlt, helpHTTP, helpAPIURL, helpLiveCommand, helpData = prevSrc, prevCache, prevHTTP, prevURL, prevLive, prevData
	})

	data, err := helpGenerateCache(true)
	if err != nil {
		t.Fatalf("helpGenerateCache: %v", err)
	}
	if len(data.Entries) != 1 || data.Entries[0].Command != "sa" {
		t.Fatalf("comandos borrados quedaron en la caché: %#v", data.Entries)
	}
	if _, ok := data.AliasMap["borrado"]; ok {
		t.Fatal("el comando borrado no debe quedar en aliasMap")
	}
	if data.Version != helpCacheVersion {
		t.Fatalf("versión=%d, esperaba %d", data.Version, helpCacheVersion)
	}
}

func TestHelpApplyPrefix(t *testing.T) {
	for _, prefix := range []string{"!", ".", "/"} {
		got := helpApplyPrefix("{p}sa y {p}help", prefix)
		want := prefix + "sa y " + prefix + "help"
		if got != want {
			t.Fatalf("prefijo %q: %q, esperaba %q", prefix, got, want)
		}
	}
	if got := helpApplyPrefix("{p}sa", ""); got != ".sa" {
		t.Fatalf("prefijo vacío: %q", got)
	}
}

func TestHelpRenderMenuOcultaAdmin(t *testing.T) {
	menu := helpRenderMenu(helpTestData(), "!")
	for _, want := range []string{
		"📖 Comandos del bot", "🎨 Stickers y Multimedia", "!sa", "!yt", "!dado",
		"💡 Escribe !help <comando>", "📂 También puedes ver una categoría: !help stickers",
	} {
		if !strings.Contains(menu, want) {
			t.Fatalf("falta %q en el menú:\n%s", want, menu)
		}
	}
	for _, hidden := range []string{"!debug-st", "!secreto", "⚙️ Administración"} {
		if strings.Contains(menu, hidden) {
			t.Fatalf("el menú no debe contener %q:\n%s", hidden, menu)
		}
	}
}

func TestHelpRenderCategory(t *testing.T) {
	data := helpTestData()
	cat, ok := helpResolveCategory("juegos")
	if !ok || cat.Key != "games" {
		t.Fatalf("categoría juegos=%+v ok=%v", cat, ok)
	}
	got := helpRenderCategory(data, "!", cat)
	if !strings.Contains(got, "🎮 Juegos y Diversión") || !strings.Contains(got, "!dado — Tirar dados") {
		t.Fatalf("categoría inesperada:\n%s", got)
	}

	cat, ok = helpResolveCategory("descargas")
	if !ok || cat.Key != "downloads" {
		t.Fatalf("categoría descargas=%+v ok=%v", cat, ok)
	}
	if got := helpRenderCategory(data, ".", cat); !strings.Contains(got, ".yt — Videos de YouTube") {
		t.Fatalf("categoría descargas inesperada:\n%s", got)
	}

	cat, ok = helpResolveCategory("admin")
	if !ok || !cat.Admin {
		t.Fatalf("categoría admin=%+v ok=%v", cat, ok)
	}
	got = helpRenderCategory(data, "!", cat)
	if !strings.Contains(got, "!debug-st — Diagnóstico interno") || strings.Contains(got, "!sa") {
		t.Fatalf("categoría admin inesperada:\n%s", got)
	}
}

func TestHelpBusquedaPorComandoAliasYCategoria(t *testing.T) {
	data := helpTestData()
	for query, want := range map[string]string{"sa": "sa", "sticker": "st", "s": "st", "st": "st"} {
		idx, ok := data.AliasMap[helpNormalize(query)]
		if !ok || data.Entries[idx].Command != want {
			t.Fatalf("búsqueda %q: idx=%d ok=%v, esperaba %s", query, idx, ok, want)
		}
	}
	for _, query := range []string{"stickers", "STICKERS Y MULTIMEDIA", "juegos", "descargas", "admin"} {
		if _, ok := helpResolveCategory(query); !ok {
			t.Fatalf("no se resolvió la categoría %q", query)
		}
	}
	if _, ok := helpResolveCategory("noexiste"); ok {
		t.Fatal("una categoría inexistente no debe resolverse")
	}
}

func TestHelpRenderEntryPlantilla(t *testing.T) {
	got := helpRenderEntry(helpTestEntries()[0], "!")
	for _, want := range []string{
		"🎨 !sa (también: !sticker, !s) — Stickers con sonido",
		"Crea stickers que reproducen audio al tocarlos.",
		"Cómo usarlo:", "• Responde a un video corto con !sa",
		"Opciones útiles:", "• crop — recorta en cuadrado",
		"Ejemplos:", "• !sa crop 0:02 0:05",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("falta %q en la ficha:\n%s", want, got)
		}
	}
	if strings.Contains(got, "{p}") {
		t.Fatalf("quedó un marcador sin reemplazar:\n%s", got)
	}
}

func TestHelpRenderUnknown(t *testing.T) {
	got := helpRenderUnknown("nope", "/")
	if !strings.Contains(got, "❓ No encontré ayuda para \"nope\".") || !strings.Contains(got, "/help") {
		t.Fatalf("respuesta inesperada:\n%s", got)
	}
}

var _ = core.DataPath

func TestHelpSourcesEmbebidas(t *testing.T) {
	prev := helpSrcAlt
	helpSrcAlt = ""
	t.Cleanup(func() { helpSrcAlt = prev })

	sources, err := helpSources()
	if err != nil {
		t.Fatalf("helpSources embebido: %v", err)
	}
	if len(sources) == 0 {
		t.Fatal("no se embebió ninguna fuente de addon")
	}
	var sawSA bool
	for _, src := range sources {
		if src.Name == "help.go" || strings.HasSuffix(src.Name, "_test.go") {
			t.Fatalf("no debía incluir %q", src.Name)
		}
		if !strings.Contains(string(src.Code), "package addons") {
			t.Fatalf("fuente embebida inválida: %q", src.Name)
		}
		if src.Name == "sa.go" {
			sawSA = true
		}
	}
	if !sawSA {
		t.Fatal("faltó sa.go en las fuentes embebidas")
	}
	if _, err := helpComputeHash(); err != nil {
		t.Fatalf("hash de fuentes embebidas: %v", err)
	}
}

func TestHelpPrioridadComandoSobreCategoria(t *testing.T) {
	data := helpTestData()

	res := helpResolveQuery(data, "st")
	if res.Category != nil || res.EntryIndex < 0 || data.Entries[res.EntryIndex].Command != "st" {
		t.Fatalf("!help st debe resolver la ficha del comando st: %+v", res)
	}
	ficha := helpRenderEntry(data.Entries[res.EntryIndex], "!")
	if !strings.Contains(ficha, "!st (también: !sticker, !s) — Crear stickers") {
		t.Fatalf("la ficha no muestra los alias:\n%s", ficha)
	}
	if strings.Contains(ficha, "🎨 Stickers y Multimedia\n") {
		t.Fatalf("!help st no debe mostrar la categoría:\n%s", ficha)
	}

	catRes := helpRenderQuerySim(data, "stickers", "!")
	if !strings.Contains(catRes, "🎨 Stickers y Multimedia") {
		t.Fatalf("!help stickers debe mostrar la categoría:\n%s", catRes)
	}
	if strings.Contains(catRes, "(también:") {
		t.Fatalf("!help stickers no debe mostrar una ficha:\n%s", catRes)
	}
}

func helpRenderQuerySim(data *helpCacheData, query, prefix string) string {
	res := helpResolveQuery(data, query)
	if res.EntryIndex >= 0 {
		return helpRenderEntry(data.Entries[res.EntryIndex], prefix)
	}
	if res.Category != nil {
		return helpRenderCategory(data, prefix, *res.Category)
	}
	return helpRenderUnknown(query, prefix)
}

func TestHelpOpcionesLargasYCortas(t *testing.T) {
	opciones := []string{
		"crop / c — recorta en cuadrado",
		"estirar / e — estira la imagen al cuadrado",
		"spherical / s — forma redonda",
		"border / b — borde redondeado",
		"reverse / r — reproduce al revés",
		"high / h — más calidad",
		"auto / wave / onda — genera una onda visual desde un audio",
		"vertical / v — formato vertical",
		"horizontal / h — formato horizontal",
		"color / c — mantiene la foto a color",
		"audio — saca el audio de un sticker",
		"invertir — invierte los colores",
	}
	e := helpNormalizeEntry(helpEntry{Command: "st", Options: opciones})
	if len(e.Options) != len(opciones) {
		t.Fatalf("opciones recortadas: %d de %d", len(e.Options), len(opciones))
	}
	ficha := helpRenderEntry(e, "!")
	if !strings.Contains(ficha, "• crop / c — recorta en cuadrado") || !strings.Contains(ficha, "• auto / wave / onda — genera una onda visual desde un audio") {
		t.Fatalf("faltan versiones largas/cortas:\n%s", ficha)
	}
	if !strings.Contains(helpSystemPrompt, "crop / c — recorta en cuadrado") || strings.Contains(helpSystemPrompt, "máximo 6") {
		t.Fatal("el prompt debe pedir todas las opciones con versiones largas/cortas juntas")
	}
}
