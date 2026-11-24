package addons

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"whatsbot-go/internal/core"
)

type filesCapture struct {
	texts     []string
	reactions []string
	msgs      []*waE2E.Message
}

func filesNewCtx(c *filesCapture, jid string, fromMe bool, args ...string) *core.Context {
	chat, _ := types.ParseJID(jid)
	msg := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: chat, Sender: chat, IsFromMe: fromMe, IsGroup: strings.HasSuffix(jid, "@g.us")},
			ID:            "F1",
		},
		Message: &waE2E.Message{Conversation: proto.String("f")},
	}
	return &core.Context{
		Command: "f",
		Args:    args,
		Message: msg,
		SendHook: func(m *waE2E.Message) error {
			c.msgs = append(c.msgs, m)
			c.texts = append(c.texts, messageText(m))
			return nil
		},
		ReactHook: func(emoji string) error {
			c.reactions = append(c.reactions, emoji)
			return nil
		},
	}
}

func filesSetup(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	core.SetRoot(root)
	base := filepath.Join(root, "files")
	if err := os.MkdirAll(filepath.Join(base, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "a.txt"), []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "B.txt"), []byte("bb"), 0o644); err != nil {
		t.Fatal(err)
	}
	filesMu.Lock()
	filesSessions = map[string]*filesSession{}
	filesMu.Unlock()
	t.Cleanup(func() {
		filesMu.Lock()
		filesSessions = map[string]*filesSession{}
		filesMu.Unlock()
	})
	return base
}

func TestFilesSoloOwner(t *testing.T) {
	filesSetup(t)
	c := &filesCapture{}
	_ = (Files{}).Handle(filesNewCtx(c, "1@s.whatsapp.net", false))
	if len(c.reactions) != 1 || c.reactions[0] != "🚫" {
		t.Fatalf("esperaba 🚫: %#v", c.reactions)
	}
	if len(c.texts) != 0 {
		t.Fatalf("no debía listar: %#v", c.texts)
	}
}

func TestFilesListado(t *testing.T) {
	filesSetup(t)
	c := &filesCapture{}
	_ = (Files{}).Handle(filesNewCtx(c, "2@s.whatsapp.net", true))
	if len(c.texts) != 1 {
		t.Fatalf("esperaba un listado: %#v", c.texts)
	}
	out := c.texts[0]
	for _, want := range []string{"📁 `files/./`  —  pág 1/1", "[1] 📂 sub/", "[2] 📄 a.txt (10 B)", "[3] 📄 B.txt (2 B)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("listado sin %q:\n%s", want, out)
		}
	}
}

func TestFilesNavegacion(t *testing.T) {
	filesSetup(t)
	c := &filesCapture{}
	_ = (Files{}).Handle(filesNewCtx(c, "3@s.whatsapp.net", true, "1"))
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "📂 Carpeta vacía.") {
		t.Fatalf("navegación inesperada: %#v", c.texts)
	}

	back := &filesCapture{}
	_ = (Files{}).Handle(filesNewCtx(back, "3@s.whatsapp.net", true, "back"))
	if !strings.Contains(back.texts[0], "📁 `files/./`") {
		t.Fatalf("back inesperado: %#v", back.texts)
	}

	root := &filesCapture{}
	_ = (Files{}).Handle(filesNewCtx(root, "3@s.whatsapp.net", true, "root"))
	if !strings.Contains(root.texts[0], "📁 `files/./`") {
		t.Fatalf("root inesperado: %#v", root.texts)
	}
}

func TestFilesDescargar(t *testing.T) {
	filesSetup(t)
	c := &filesCapture{}
	_ = (Files{}).Handle(filesNewCtx(c, "4@s.whatsapp.net", true, "get", "2"))
	if len(c.reactions) == 0 || c.reactions[0] != "⏳" {
		t.Fatalf("esperaba ⏳: %#v", c.reactions)
	}
	found := false
	for _, m := range c.msgs {
		if d := m.GetDocumentMessage(); d != nil {
			if d.GetFileName() != "a.txt" || len(m.GetDocumentMessage().GetFileSHA256()) != 0 {
				if d.GetFileName() != "a.txt" {
					t.Fatalf("documento inesperado: %s", d.GetFileName())
				}
			}
			found = true
		}
	}
	if !found {
		t.Fatal("no se envió el documento")
	}

	bad := &filesCapture{}
	_ = (Files{}).Handle(filesNewCtx(bad, "4@s.whatsapp.net", true, "get", "99"))
	if len(bad.reactions) == 0 || bad.reactions[0] != "❓" {
		t.Fatalf("esperaba ❓: %#v", bad.reactions)
	}
}

func TestFilesSubirSinMedia(t *testing.T) {
	filesSetup(t)
	c := &filesCapture{}
	_ = (Files{}).Handle(filesNewCtx(c, "5@s.whatsapp.net", true, "up"))
	if len(c.reactions) != 1 || c.reactions[0] != "❓" {
		t.Fatalf("esperaba ❓: %#v", c.reactions)
	}
}

func TestFilesEliminarMoverMkdirFind(t *testing.T) {
	base := filesSetup(t)

	rm := &filesCapture{}
	_ = (Files{}).Handle(filesNewCtx(rm, "6@s.whatsapp.net", true, "rm", "2"))
	if len(rm.reactions) == 0 || rm.reactions[0] != "🗑️" {
		t.Fatalf("esperaba 🗑️: %#v", rm.reactions)
	}
	if _, err := os.Stat(filepath.Join(base, "a.txt")); !os.IsNotExist(err) {
		t.Fatal("a.txt debió eliminarse")
	}

	mk := &filesCapture{}
	_ = (Files{}).Handle(filesNewCtx(mk, "6@s.whatsapp.net", true, "mkdir", "nueva"))
	if _, err := os.Stat(filepath.Join(base, "nueva")); err != nil {
		t.Fatalf("no se creó la carpeta: %v", err)
	}

	// El listado actual es: nueva/, sub/, B.txt
	mv := &filesCapture{}
	_ = (Files{}).Handle(filesNewCtx(mv, "6@s.whatsapp.net", true, "mv", "3", "1"))
	if _, err := os.Stat(filepath.Join(base, "nueva", "B.txt")); err != nil {
		t.Fatalf("B.txt no se movió: %v", err)
	}

	find := &filesCapture{}
	_ = (Files{}).Handle(filesNewCtx(find, "6@s.whatsapp.net", true, "find", "zzz"))
	if len(find.texts) != 1 || !strings.Contains(find.texts[0], "🔍 Sin resultados para \"zzz\".") {
		t.Fatalf("find inesperado: %#v", find.texts)
	}
}

func TestFilesHelpYComandos(t *testing.T) {
	filesSetup(t)
	c := &filesCapture{}
	_ = (Files{}).Handle(filesNewCtx(c, "7@s.whatsapp.net", true, "help"))
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "📁 *files — explorador de archivos*") {
		t.Fatalf("help inesperado: %#v", c.texts)
	}
	got := Files{}.Commands()
	if len(got) != 2 || got[0] != "files" || got[1] != "f" {
		t.Fatalf("comandos inesperados: %#v", got)
	}
}

func TestFilesPrettySize(t *testing.T) {
	casos := map[int64]string{
		0:                   "0 B",
		1023:                "1023 B",
		1024:                "1.0 KB",
		1536:                "1.5 KB",
		1024 * 1024:         "1.0 MB",
		3 * 1024 * 1024 / 2: "1.5 MB",
	}
	for n, want := range casos {
		if got := filesPrettySize(n); got != want {
			t.Fatalf("filesPrettySize(%d)=%q esperaba %q", n, got, want)
		}
	}
}
