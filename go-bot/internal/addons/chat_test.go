package addons

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestChatCommands(t *testing.T) {
	got := Chat{}.Commands()
	if len(got) != 2 || got[0] != "chat" || got[1] != "c" {
		t.Fatalf("comandos inesperados: %#v", got)
	}
}

func TestChatTruncateUserText(t *testing.T) {
	corto := strings.Repeat("a", chatMaxUserChars)
	if got := chatTruncateUserText(corto); got != corto {
		t.Fatalf("no debía truncar: %d chars", len(got))
	}

	largo := strings.Repeat("b", chatMaxUserChars+500)
	got := chatTruncateUserText(largo)
	if !strings.HasPrefix(got, strings.Repeat("b", chatMaxUserChars)) {
		t.Fatal("debía conservar los primeros 2000 caracteres")
	}
	if !strings.Contains(got, "Mensaje cortado a 2000 caracteres") {
		t.Fatalf("falta el aviso de corte: %q", got[len(got)-120:])
	}
}

func TestChatEscXML(t *testing.T) {
	got := chatEscXML(`a & b <c> "d"`)
	want := "a &amp; b &lt;c&gt; &quot;d&quot;"
	if got != want {
		t.Fatalf("chatEscXML=%q, esperaba %q", got, want)
	}
}

func TestChatTimeAgo(t *testing.T) {
	now := time.Now().UnixMilli()
	casos := []struct {
		ts   int64
		want string
	}{
		{now, "hace 1 min"},
		{now - 120_000, "hace 2 min"},
		{now - 2*3600_000, "hace 2 h"},
		{now - 48*3600_000, "hace 2 d"},
	}
	for _, caso := range casos {
		if got := chatTimeAgo(caso.ts); got != caso.want {
			t.Fatalf("chatTimeAgo(%d)=%q, esperaba %q", caso.ts, got, caso.want)
		}
	}
}

func TestChatParseDsmlToolCalls(t *testing.T) {
	contenido := `<|DSML|invoke name="buscar_web">` +
		`<|DSML|parameter name="query">clima en Caracas</|DSML|parameter>` +
		`</|DSML|invoke>`
	calls := chatParseDsmlToolCalls(contenido)
	if len(calls) != 1 {
		t.Fatalf("esperaba 1 tool-call: %#v", calls)
	}
	fn, ok := calls[0]["function"].(map[string]any)
	if !ok || fn["name"] != "buscar_web" {
		t.Fatalf("función inesperada: %#v", calls[0])
	}
	if args, _ := fn["arguments"].(string); !strings.Contains(args, "clima en Caracas") {
		t.Fatalf("argumentos inesperados: %q", args)
	}
}

func TestChatSanitizeDsmlText(t *testing.T) {
	got := chatSanitizeDsmlText("hola <|DSML|algo>mundo</|DSML|algo>")
	if strings.Contains(got, "DSML") {
		t.Fatalf("quedaron etiquetas DSML: %q", got)
	}
	if !strings.Contains(got, "hola") || !strings.Contains(got, "mundo") {
		t.Fatalf("texto inesperado: %q", got)
	}
}

func TestChatJSNumberYClamp(t *testing.T) {
	casos := map[float64]string{1: "1", 1.5: "1.5", 1000: "1000", 0: "0"}
	for entrada, want := range casos {
		if got := chatJSNumber(entrada); got != want {
			t.Fatalf("chatJSNumber(%v)=%q, esperaba %q", entrada, got, want)
		}
	}
	if got := chatClampInt(500, 1, 30); got != 30 {
		t.Fatalf("clamp alto=%d", got)
	}
	if got := chatClampInt(-5, 1, 30); got != 1 {
		t.Fatalf("clamp bajo=%d", got)
	}
}

func TestChatGetRecent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chatHistory.json")
	var b strings.Builder
	b.WriteString(`{"1205@g.us":[`)
	for i := 0; i < 5; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"text":"m%d","sender":"u","t":%d}`, i, i)
	}
	b.WriteString(`]}`)
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	prev := chatHistoryPathAlt
	chatHistoryPathAlt = path
	chatHistMu.Lock()
	chatHistCache = nil
	chatHistModTime = time.Time{}
	chatHistSize = 0
	chatHistMu.Unlock()
	t.Cleanup(func() { chatHistoryPathAlt = prev })

	recientes := chatGetRecent("1205@g.us", 2)
	if len(recientes) != 2 || recientes[0].Text != "m3" || recientes[1].Text != "m4" {
		t.Fatalf("historial inesperado: %#v", recientes)
	}
	if got := chatGetRecent("otro@g.us", 2); len(got) != 0 {
		t.Fatalf("chat sin historial: %#v", got)
	}
}
