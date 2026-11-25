package addons

import (
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"whatsbot-go/internal/core"
)

type remCapture struct {
	texts     []string
	reactions []string
}

func remNewCtx(c *remCapture, jid, cmd string, args ...string) *core.Context {
	chat, _ := types.ParseJID(jid)
	msg := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: chat, Sender: chat},
			ID:            "REM1",
		},
		Message: &waE2E.Message{Conversation: proto.String(cmd)},
	}
	return &core.Context{
		Command: cmd,
		Args:    args,
		Message: msg,
		SendHook: func(m *waE2E.Message) error {
			c.texts = append(c.texts, messageText(m))
			return nil
		},
		ReactHook: func(emoji string) error {
			c.reactions = append(c.reactions, emoji)
			return nil
		},
	}
}

func remSetup(t *testing.T) {
	t.Helper()
	core.SetRoot(t.TempDir())
	prevNow := remNow
	prevSchedule := remScheduleFn
	remNow = func() time.Time {
		return time.Date(2026, 6, 15, 12, 0, 0, 0, time.Local)
	}
	remScheduleFn = func(remItem) {}
	t.Cleanup(func() {
		remNow = prevNow
		remScheduleFn = prevSchedule
		remMu.Lock()
		remState = remData{NextID: 1}
		for id, tm := range remTimers {
			if tm != nil {
				tm.Stop()
			}
			delete(remTimers, id)
		}
		remMu.Unlock()
	})
}

func TestRemParseWhen(t *testing.T) {
	remSetup(t)
	base := remNow().UnixMilli()

	casos := []struct {
		in       string
		wantText string
		wantMs   int64
	}{
		{"30m llamar a mamá", "llamar a mamá", 30 * 60 * 1000},
		{"1h45m sacar la comida", "sacar la comida", 105 * 60 * 1000},
		{"2 dias 4 horas revisar", "revisar", (2*24 + 4) * 3600 * 1000},
		{"en 15 min café", "café", 15 * 60 * 1000},
		{"10s prueba", "prueba", 10 * 1000},
	}
	for _, c := range casos {
		got, ok := remParseWhen(c.in)
		if !ok {
			t.Fatalf("remParseWhen(%q) no parseó", c.in)
		}
		if got.At != base+c.wantMs {
			t.Fatalf("remParseWhen(%q).At=%d, esperaba %d", c.in, got.At, base+c.wantMs)
		}
		if got.Text != c.wantText {
			t.Fatalf("remParseWhen(%q).Text=%q, esperaba %q", c.in, got.Text, c.wantText)
		}
	}
}

func TestRemParseWhenAbsoluto(t *testing.T) {
	remSetup(t)
	loc := time.Local

	got, ok := remParseWhen("17:30 reunión")
	if !ok || got.Text != "reunión" {
		t.Fatalf("17:30 no parseado: %#v ok=%v", got, ok)
	}
	if want := time.Date(2026, 6, 15, 17, 30, 0, 0, loc).UnixMilli(); got.At != want {
		t.Fatalf("17:30 At=%d esperaba %d", got.At, want)
	}

	got, ok = remParseWhen("9pm serie nueva")
	if !ok || got.Text != "serie nueva" {
		t.Fatalf("9pm no parseado: %#v ok=%v", got, ok)
	}
	if want := time.Date(2026, 6, 15, 21, 0, 0, 0, loc).UnixMilli(); got.At != want {
		t.Fatalf("9pm At=%d esperaba %d", got.At, want)
	}

	got, ok = remParseWhen("11:00 ya pasó")
	if !ok {
		t.Fatal("11:00 no parseado")
	}
	if want := time.Date(2026, 6, 16, 11, 0, 0, 0, loc).UnixMilli(); got.At != want {
		t.Fatalf("11:00 pasado At=%d esperaba %d", got.At, want)
	}

	got, ok = remParseWhen("mañana 8am gym")
	if !ok || got.Text != "gym" {
		t.Fatalf("mañana 8am no parseado: %#v ok=%v", got, ok)
	}
	if want := time.Date(2026, 6, 16, 8, 0, 0, 0, loc).UnixMilli(); got.At != want {
		t.Fatalf("mañana 8am At=%d esperaba %d", got.At, want)
	}

	got, ok = remParseWhen("mañana gym")
	if !ok || !got.NeedTime || got.Text != "gym" {
		t.Fatalf("mañana sin hora: %#v ok=%v", got, ok)
	}
}

func TestRemParseWhenInvalido(t *testing.T) {
	remSetup(t)
	for _, in := range []string{"", "abc", "0s cero", "25:00 tarde", "13pm raro", "1h:30m"} {
		if got, ok := remParseWhen(in); ok {
			t.Fatalf("remParseWhen(%q) debió fallar, obtuve %#v", in, got)
		}
	}
}

func TestRemFmtDurYFecha(t *testing.T) {
	remSetup(t)
	casos := map[int64]string{
		0:                       "0s",
		30 * 1000:               "30s",
		90 * 1000:               "1m 30s",
		3600 * 1000:             "1h",
		(24*3600 + 3600) * 1000: "1d 1h",
		(72 * 3600) * 1000:      "3d",
	}
	for ms, want := range casos {
		if got := remFmtDur(ms); got != want {
			t.Fatalf("remFmtDur(%d)=%q esperaba %q", ms, got, want)
		}
	}

	hoy := remNow()
	if got := remFmtDate(hoy.UnixMilli()); !strings.HasPrefix(got, "hoy a las 12:00") {
		t.Fatalf("remFmtDate(hoy)=%q", got)
	}
	if got := remFmtDate(hoy.AddDate(0, 0, 1).UnixMilli()); !strings.HasPrefix(got, "mañana a las 12:00") {
		t.Fatalf("remFmtDate(mañana)=%q", got)
	}
	if got := remFmtDate(time.Date(2026, 6, 20, 9, 5, 0, 0, time.Local).UnixMilli()); !strings.Contains(got, "sáb 20 jun") {
		t.Fatalf("remFmtDate(20 jun)=%q", got)
	}
}

func TestRemComandoHelp(t *testing.T) {
	remSetup(t)
	c := &remCapture{}
	if err := (Rem{}).Handle(remNewCtx(c, "111@s.whatsapp.net", "rem")); err != nil {
		t.Fatal(err)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "⏰ *Recordatorios*") {
		t.Fatalf("help inesperado: %#v", c.texts)
	}
}

func TestRemComandoCrearYListar(t *testing.T) {
	remSetup(t)
	c := &remCapture{}
	ctx := remNewCtx(c, "222@s.whatsapp.net", "rem", "30m", "llamar", "a", "mamá")
	if err := (Rem{}).Handle(ctx); err != nil {
		t.Fatal(err)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "⏰ Anotado para hoy a las 12:30") || !strings.Contains(c.texts[0], "*(en 30m)*") {
		t.Fatalf("creación inesperada: %#v", c.texts)
	}

	list := &remCapture{}
	if err := (Rem{}).Handle(remNewCtx(list, "222@s.whatsapp.net", "rem", "list")); err != nil {
		t.Fatal(err)
	}
	if len(list.texts) != 1 || !strings.Contains(list.texts[0], "⏰ *Pendientes (1):*") || !strings.Contains(list.texts[0], "llamar a mamá") {
		t.Fatalf("listado inesperado: %#v", list.texts)
	}
}

func TestRemComandoDelYClear(t *testing.T) {
	remSetup(t)
	jid := "333@s.whatsapp.net"
	for _, args := range [][]string{{"30m", "uno"}, {"1h", "dos"}} {
		if err := (Rem{}).Handle(remNewCtx(&remCapture{}, jid, "rem", args...)); err != nil {
			t.Fatal(err)
		}
	}

	bad := &remCapture{}
	_ = (Rem{}).Handle(remNewCtx(bad, jid, "rem", "del", "x"))
	if len(bad.reactions) != 1 || bad.reactions[0] != "⚠️" {
		t.Fatalf("del inválido reacciones: %#v", bad.reactions)
	}

	del := &remCapture{}
	_ = (Rem{}).Handle(remNewCtx(del, jid, "rem", "del", "1"))
	if len(del.reactions) == 0 || del.reactions[0] != "🗑️" {
		t.Fatalf("del reacciones: %#v", del.reactions)
	}
	if !strings.Contains(del.texts[0], "🗑️ Eliminado: \"uno\"") {
		t.Fatalf("del texto: %#v", del.texts)
	}

	clear := &remCapture{}
	_ = (Rem{}).Handle(remNewCtx(clear, jid, "rem", "clear"))
	if !strings.Contains(clear.texts[0], "🗑️ 1 recordatorio(s) eliminados.") {
		t.Fatalf("clear texto: %#v", clear.texts)
	}
}

func TestRemComandoErrores(t *testing.T) {
	remSetup(t)
	c := &remCapture{}
	_ = (Rem{}).Handle(remNewCtx(c, "444@s.whatsapp.net", "rem", "cuando", "sea"))
	if len(c.reactions) != 1 || c.reactions[0] != "⚠️" {
		t.Fatalf("esperaba ⚠️: %#v", c.reactions)
	}
	if !strings.Contains(c.texts[0], "No entendí el tiempo") {
		t.Fatalf("texto inesperado: %#v", c.texts)
	}

	noText := &remCapture{}
	_ = (Rem{}).Handle(remNewCtx(noText, "444@s.whatsapp.net", "rem", "30m"))
	if !strings.Contains(noText.texts[0], "Falta el texto del recordatorio") {
		t.Fatalf("texto sin texto: %#v", noText.texts)
	}
}

func TestRemPersistenciaEsquema(t *testing.T) {
	remSetup(t)
	jid := "555@s.whatsapp.net"
	_ = (Rem{}).Handle(remNewCtx(&remCapture{}, jid, "rem", "2h", "persistir"))

	var raw map[string]any
	if err := core.ReadJSON(remStorePath(), &raw); err != nil {
		t.Fatalf("no se pudo leer reminders.json: %v", err)
	}
	if _, ok := raw["nextId"]; !ok {
		t.Fatal("falta nextId")
	}
	items, ok := raw["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("items inesperados: %#v", raw["items"])
	}
	first := items[0].(map[string]any)
	for _, key := range []string{"id", "chat", "text", "at", "created"} {
		if _, ok := first[key]; !ok {
			t.Fatalf("falta la clave %q en %#v", key, first)
		}
	}

	// Al recargar desde disco, el recordatorio sigue disponible.
	remLoadStore()
	list := &remCapture{}
	_ = (Rem{}).Handle(remNewCtx(list, jid, "rem", "list"))
	if !strings.Contains(list.texts[0], "persistir") {
		t.Fatalf("no recargó: %#v", list.texts)
	}
}

func TestRemCommands(t *testing.T) {
	got := Rem{}.Commands()
	if len(got) != 2 || got[0] != "rem" || got[1] != "recordar" {
		t.Fatalf("comandos inesperados: %#v", got)
	}
}
