package addons

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"whatsbot-go/internal/core"
)

func unreadSetup(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	core.SetRoot(root)

	unreadMu.Lock()
	unreadHistory = map[string][]unreadEntry{}
	unreadCounter = 0
	unreadMu.Unlock()
	unreadOnce = sync.Once{}
	unreadNow = func() time.Time { return time.UnixMilli(1700000000000) }

	t.Cleanup(func() {
		unreadMu.Lock()
		unreadHistory = map[string][]unreadEntry{}
		unreadCounter = 0
		unreadMu.Unlock()
		unreadOnce = sync.Once{}
		unreadNow = time.Now
	})
	return root
}

func TestUnreadCommands(t *testing.T) {
	if got := (Unread{}).Commands(); len(got) != 1 || got[0] != "unread" {
		t.Fatalf("comandos inesperados: %#v", got)
	}
}

func TestUnreadAppendYRecent(t *testing.T) {
	unreadSetup(t)
	for i := 0; i < 3; i++ {
		unreadAppend("1205@g.us", unreadEntry{Text: "msg", Sender: "u", T: int64(i)})
	}
	recientes := unreadGetRecent("1205@g.us", 2)
	if len(recientes) != 2 {
		t.Fatalf("recientes=%#v", recientes)
	}
	if recientes[1].Text != "msg" {
		t.Fatalf("entrada inesperada: %#v", recientes[1])
	}
	if got := unreadGetRecent("otro@g.us", 5); len(got) != 0 {
		t.Fatalf("chat sin historial: %#v", got)
	}
}

func TestUnreadSaveYLoad(t *testing.T) {
	root := unreadSetup(t)
	unreadAppend("1205@g.us", unreadEntry{Text: "hola", Sender: "584@s.whatsapp.net", T: 1})
	unreadSave()

	path := filepath.Join(root, "assets", "chatHistory.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("no se guardó el historial: %v", err)
	}

	unreadMu.Lock()
	unreadHistory = map[string][]unreadEntry{}
	unreadMu.Unlock()

	unreadLoad()
	got := unreadGetRecent("1205@g.us", 1)
	if len(got) != 1 || got[0].Text != "hola" {
		t.Fatalf("recarga inesperada: %#v", got)
	}
}

func TestUnreadDefaultTypingEsCopia(t *testing.T) {
	unreadSetup(t)
	unreadAppend("a@g.us", unreadEntry{Text: "x"})
	copia := unreadDefaultTyping()
	copia["a@g.us"][0].Text = "mutado"
	if got := unreadGetRecent("a@g.us", 1); got[0].Text != "x" {
		t.Fatal("defaultTyping debe devolver copias")
	}
}

func TestUnreadParseIntPrefix(t *testing.T) {
	casos := map[string]int{
		"42":    42,
		"+7":    7,
		"-3":    -3,
		"12abc": 12,
		"nope":  0,
		"":      0,
	}
	for entrada, want := range casos {
		if got := unreadParseIntPrefix(entrada); got != want {
			t.Fatalf("unreadParseIntPrefix(%q)=%d, esperaba %d", entrada, got, want)
		}
	}
}
