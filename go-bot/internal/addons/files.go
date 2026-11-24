// addons/files.js → files.go
// Explorador de archivos de files/ con navegación, subida, descarga y edición.
package addons

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"

	"whatsbot-go/internal/core"
)

const (
	filesPageSize = 10
	filesTTL      = int64(5 * 60 * 1000)
)

type filesEntry struct {
	Name  string
	Path  string
	IsDir bool
}

type filesSession struct {
	Cwd   string
	T     int64
	Items []filesEntry
	Page  int
	MsgID string
}

var (
	filesMu       sync.Mutex
	filesSessions = map[string]*filesSession{}
	filesNow      = time.Now
)

func filesRoot() string { return core.DataPath("files") }

func filesGetSession(jid string) *filesSession {
	now := filesNow().UnixMilli()
	s := filesSessions[jid]
	if s == nil || now-s.T > filesTTL {
		s = &filesSession{Cwd: filesRoot(), T: now}
		filesSessions[jid] = s
	}
	s.T = now
	return s
}

func filesInsideRoot(p string) bool {
	root, err := filepath.Abs(filesRoot())
	if err != nil {
		root = filepath.Clean(filesRoot())
	}
	clean, err := filepath.Abs(p)
	if err != nil {
		clean = filepath.Clean(p)
	}
	return clean == root || strings.HasPrefix(clean, root+string(os.PathSeparator))
}

func filesLess(a, b string) bool {
	la, lb := strings.ToLower(a), strings.ToLower(b)
	if la != lb {
		return la < lb
	}
	return a < b
}

func filesSortedEntries(dir string) []filesEntry {
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	entries := make([]filesEntry, 0, len(des))
	for _, de := range des {
		entries = append(entries, filesEntry{
			Name:  de.Name(),
			Path:  filepath.Join(dir, de.Name()),
			IsDir: de.IsDir(),
		})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return filesLess(entries[i].Name, entries[j].Name)
	})
	return entries
}

func filesPrettySize(bytes int64) string {
	if bytes < 1024 {
		return strconv.FormatInt(bytes, 10) + " B"
	}
	if bytes < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(bytes)/(1024*1024))
}

func filesFormatListing(s *filesSession) string {
	all := s.Items
	if len(all) == 0 {
		return ""
	}

	total := (len(all) + filesPageSize - 1) / filesPageSize
	if total < 1 {
		total = 1
	}
	if s.Page >= total {
		s.Page = total - 1
	}
	start := s.Page * filesPageSize
	end := start + filesPageSize
	if end > len(all) {
		end = len(all)
	}
	page := all[start:end]

	rel, err := filepath.Rel(filesRoot(), s.Cwd)
	if err != nil || rel == "" {
		rel = "."
	}

	var b strings.Builder
	fmt.Fprintf(&b, "📁 `files/%s/`  —  pág %d/%d\n", rel, s.Page+1, total)
	b.WriteString("─────────────────\n")

	for i, e := range page {
		idx := start + i + 1
		if e.IsDir {
			fmt.Fprintf(&b, "[%d] 📂 %s/\n", idx, e.Name)
			continue
		}
		size := ""
		if info, err := os.Stat(e.Path); err == nil {
			size = " (" + filesPrettySize(info.Size()) + ")"
		}
		fmt.Fprintf(&b, "[%d] 📄 %s%s\n", idx, e.Name, size)
	}

	b.WriteString("\nNavegar: `.f N`  `.f back`  `.f root`")
	if total > 1 {
		b.WriteString("  •  pág: `.f n` / `.f p`")
	}
	b.WriteString("\nArchivos: `.f get N`  `.f up`  `.f rm N`  `.f mv A B`")
	b.WriteString("\nOtros: `.f find q`  `.f mkdir nom`  `.f help`")
	return b.String()
}

func filesRender(ctx *core.Context, s *filesSession) error {
	s.Items = filesSortedEntries(s.Cwd)
	out := filesFormatListing(s)
	text := out
	if text == "" {
		text = "📂 Carpeta vacía."
	}

	if ctx.Client != nil && s.MsgID != "" {
		edit := ctx.Client.BuildEdit(ctx.ChatJID(), types.MessageID(s.MsgID), core.TextMessage(text))
		if _, err := ctx.Client.SendMessage(context.Background(), ctx.ChatJID(), edit); err == nil {
			return nil
		}
		s.MsgID = ""
	}

	if ctx.Client != nil {
		resp, err := ctx.Client.SendMessage(context.Background(), ctx.ChatJID(), core.TextMessage(text))
		if err == nil {
			s.MsgID = string(resp.ID)
			return nil
		}
	}
	return ctx.SendMessage(core.TextMessage(text))
}

// Files porta addons/files.js.
type Files struct{}

func init() { core.Register(Files{}) }

// Commands cumple core.Addon.
func (Files) Commands() []string { return []string{"files", "f"} }

// Handle replica el explorador de archivos original.
func (Files) Handle(ctx *core.Context) error {
	if ctx == nil || ctx.Message == nil {
		return nil
	}

	filesMu.Lock()
	defer filesMu.Unlock()

	jid := ctx.ChatJID().String()

	if !ctx.Message.Info.IsFromMe {
		_ = ctx.React("🚫")
		return nil
	}

	ses := filesGetSession(jid)
	// El listado siempre refleja el contenido actual del directorio.
	ses.Items = filesSortedEntries(ses.Cwd)
	args := ctx.Args

	if len(args) == 0 {
		return filesRender(ctx, ses)
	}

	sub := args[0]

	// ── Navegación: <N> / cd <N> ─────────────────────────────────────────
	if sub == "cd" || filesIsDigits(sub) {
		raw := sub
		if sub == "cd" {
			if len(args) > 1 {
				raw = args[1]
			} else {
				raw = ""
			}
		}
		idx := filesParseIntPrefix(raw) - 1
		if idx < 0 || idx >= len(ses.Items) || !ses.Items[idx].IsDir {
			_ = ctx.React("❓")
			return nil
		}
		target := ses.Items[idx]
		if !filesInsideRoot(target.Path) {
			_ = ctx.React("🚫")
			return nil
		}
		ses.Cwd = target.Path
		ses.Page = 0
		return filesRender(ctx, ses)
	}

	if sub == "back" {
		parent := filepath.Dir(ses.Cwd)
		if !filesInsideRoot(parent) || parent == ses.Cwd {
			return filesRender(ctx, ses)
		}
		ses.Cwd = parent
		ses.Page = 0
		return filesRender(ctx, ses)
	}

	if sub == "root" {
		ses.Cwd = filesRoot()
		ses.Page = 0
		return filesRender(ctx, ses)
	}

	if sub == "n" || sub == "next" {
		total := (len(ses.Items) + filesPageSize - 1) / filesPageSize
		if total < 1 {
			total = 1
		}
		if ses.Page+1 < total {
			ses.Page++
		}
		return filesRender(ctx, ses)
	}

	if sub == "p" || sub == "prev" {
		if ses.Page > 0 {
			ses.Page--
		}
		return filesRender(ctx, ses)
	}

	// ── Descargar: get <idx1> [idx2...] ──────────────────────────────────
	if sub == "get" {
		for _, raw := range args[1:] {
			idx := filesParseIntPrefix(raw) - 1
			if idx < 0 || idx >= len(ses.Items) || ses.Items[idx].IsDir {
				_ = ctx.React("❓")
				continue
			}
			target := ses.Items[idx]
			_ = ctx.React("⏳")
			data, err := os.ReadFile(target.Path)
			if err != nil {
				fmt.Printf("❌ [files] Error enviando %s: %v\n", target.Name, err)
				_ = ctx.React("❌")
				continue
			}
			if err := ctx.SendDocument(data, target.Name, ""); err != nil {
				fmt.Printf("❌ [files] Error enviando %s: %v\n", target.Name, err)
				_ = ctx.React("❌")
			}
		}
		return nil
	}

	// ── Subir archivo: up (respondiendo a un multimedia) ─────────────────
	if sub == "up" {
		target := ctx.QuotedMessage()
		if target == nil && ctx.Message != nil {
			target = ctx.Message.Message
		}
		mediaKey, _, name, mime := filesMediaOf(target)
		if mediaKey == "" {
			_ = ctx.React("❓")
			return nil
		}
		_ = ctx.React("⏳")

		buf, err := ctx.Download(target)
		if err != nil {
			fmt.Printf("❌ [files] Error subiendo: %v\n", err)
			_ = ctx.React("❌")
			return nil
		}

		fname := name
		if fname == "" {
			fname = "file_" + strconv.FormatInt(filesNow().UnixMilli(), 10)
		}
		if filepath.Ext(fname) == "" {
			ext := "bin"
			if mime != "" {
				if parts := strings.SplitN(mime, "/", 2); len(parts) == 2 {
					ext = strings.SplitN(parts[1], ";", 2)[0]
				}
			}
			fname += "." + ext
		}

		if err := os.MkdirAll(ses.Cwd, 0o755); err != nil {
			_ = ctx.React("❌")
			return nil
		}
		dest := filepath.Join(ses.Cwd, fname)
		if _, err := os.Stat(dest); err == nil {
			_ = ctx.React("⚠️")
			return nil
		}
		if err := os.WriteFile(dest, buf, 0o644); err != nil {
			fmt.Printf("❌ [files] Error subiendo: %v\n", err)
			_ = ctx.React("❌")
			return nil
		}
		return filesRender(ctx, ses)
	}

	// ── Eliminar: rm <idx1> [idx2...] ────────────────────────────────────
	if sub == "rm" {
		deleted := []string{}
		for _, raw := range args[1:] {
			idx := filesParseIntPrefix(raw) - 1
			if idx < 0 || idx >= len(ses.Items) {
				continue
			}
			target := ses.Items[idx]
			var err error
			if target.IsDir {
				err = os.RemoveAll(target.Path)
			} else {
				err = os.Remove(target.Path)
			}
			if err != nil {
				fmt.Printf("❌ [files] Error eliminando %s: %v\n", target.Name, err)
				continue
			}
			deleted = append(deleted, target.Name)
		}
		if len(deleted) == 0 {
			_ = ctx.React("❓")
			return nil
		}
		_ = ctx.React("🗑️")
		return filesRender(ctx, ses)
	}

	// ── Mover: mv <idx> <destIdx> ────────────────────────────────────────
	if sub == "mv" {
		srcIdx := -1
		destIdx := -1
		if len(args) > 1 {
			srcIdx = filesParseIntPrefix(args[1]) - 1
		}
		if len(args) > 2 {
			destIdx = filesParseIntPrefix(args[2]) - 1
		}
		if srcIdx < 0 || srcIdx >= len(ses.Items) || destIdx < 0 || destIdx >= len(ses.Items) || !ses.Items[destIdx].IsDir {
			_ = ctx.React("❓")
			return nil
		}
		src := ses.Items[srcIdx]
		dest := ses.Items[destIdx]
		newPath := filepath.Join(dest.Path, src.Name)
		if _, err := os.Stat(newPath); err == nil {
			_ = ctx.React("⚠️")
			return nil
		}
		if err := os.Rename(src.Path, newPath); err != nil {
			fmt.Printf("❌ [files] Error moviendo: %v\n", err)
			_ = ctx.React("❌")
			return nil
		}
		return filesRender(ctx, ses)
	}

	// ── Buscar: find <palabra> ───────────────────────────────────────────
	if sub == "find" {
		q := strings.ToLower(strings.Join(args[1:], " "))
		if q == "" {
			_ = ctx.React("❓")
			return nil
		}
		all := filesSortedEntries(ses.Cwd)
		filtered := make([]filesEntry, 0, len(all))
		for _, e := range all {
			if strings.Contains(strings.ToLower(e.Name), q) {
				filtered = append(filtered, e)
			}
		}
		if len(filtered) == 0 {
			return ctx.SendMessage(core.TextMessage("🔍 Sin resultados para \"" + q + "\"."))
		}
		ses.Items = filtered
		ses.Page = 0
		return filesRender(ctx, ses)
	}

	// ── Crear carpeta: mkdir <nombre> ────────────────────────────────────
	if sub == "mkdir" {
		name := strings.Join(args[1:], " ")
		if name == "" {
			_ = ctx.React("❓")
			return nil
		}
		dirPath := filepath.Join(ses.Cwd, name)
		if _, err := os.Stat(dirPath); err == nil {
			_ = ctx.React("⚠️")
			return nil
		}
		if err := os.MkdirAll(dirPath, 0o755); err != nil {
			fmt.Printf("❌ [files] Error creando carpeta: %v\n", err)
			_ = ctx.React("❌")
			return nil
		}
		return filesRender(ctx, ses)
	}

	// ── Ayuda ────────────────────────────────────────────────────────────
	if sub == "help" || sub == "h" || sub == "?" {
		return ctx.SendMessage(core.TextMessage(filesHelpText))
	}

	return filesRender(ctx, ses)
}

const filesHelpText = `📁 *files — explorador de archivos*
Un solo mensaje se auto-actualiza al navegar/modificar.

*Navegación*
` + "`.f`" + `          → listar directorio actual
` + "`.f <N>`" + `      → entrar a carpeta [N]
` + "`.f back`" + `     → subir un nivel
` + "`.f root`" + `     → ir a la raíz ` + "`files/`" + `
` + "`.f n`" + ` / ` + "`.f p`" + ` → paginar (next/prev)

*Archivos*
` + "`.f get <N...>`" + `   → descargar archivo(s)
` + "`.f up`" + `           → subir archivo (responde a un documento)
` + "`.f rm <N...>`" + `    → eliminar archivo(s)/carpeta(s)
` + "`.f mv <A> <B>`" + `   → mover ítem [A] a carpeta [B]
` + "`.f find <q>`" + `     → buscar por nombre
` + "`.f mkdir <nom>`" + `  → crear carpeta`

// filesMediaOf devuelve el primer tipo multimedia presente, igual que el
// arreglo mediaTypes del addon original.
func filesMediaOf(msg *waE2E.Message) (key string, target *waE2E.Message, name, mime string) {
	if msg == nil {
		return "", nil, "", ""
	}
	switch {
	case msg.GetDocumentMessage() != nil:
		d := msg.GetDocumentMessage()
		return "documentMessage", msg, d.GetFileName(), d.GetMimetype()
	case msg.GetImageMessage() != nil:
		return "imageMessage", msg, "", msg.GetImageMessage().GetMimetype()
	case msg.GetVideoMessage() != nil:
		return "videoMessage", msg, "", msg.GetVideoMessage().GetMimetype()
	case msg.GetAudioMessage() != nil:
		return "audioMessage", msg, "", msg.GetAudioMessage().GetMimetype()
	case msg.GetStickerMessage() != nil:
		return "stickerMessage", msg, "", msg.GetStickerMessage().GetMimetype()
	}
	return "", nil, "", ""
}

func filesIsDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func filesParseIntPrefix(s string) int {
	s = strings.TrimSpace(s)
	i := 0
	neg := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	start := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == start {
		return 0
	}
	n, err := strconv.Atoi(s[start:i])
	if err != nil {
		return 0
	}
	if neg {
		return -n
	}
	return n
}
