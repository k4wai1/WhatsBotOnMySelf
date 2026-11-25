package core

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"whatsbot-go/internal/store"
)

type fakeAddon struct {
	cmds  []string
	calls int
	got   *Context
	err   error
}

func (f *fakeAddon) Commands() []string { return f.cmds }

func (f *fakeAddon) Handle(ctx *Context) error {
	f.calls++
	f.got = ctx
	return f.err
}

func TestParseCommandTabla(t *testing.T) {
	prefixes := []string{",", "!", "/"}

	casos := []struct {
		nombre  string
		texto   string
		ok      bool
		prefijo string
		comando string
		args    []string
		restart bool
	}{
		{"coma con args", ",dado 20", true, ",", "dado", []string{"20"}, false},
		{"exclamacion", "!moneda", true, "!", "moneda", nil, false},
		{"barra mayúsculas", "/DADO", true, "/", "dado", nil, false},
		{"args múltiples", ",dado 20 extra", true, ",", "dado", []string{"20", "extra"}, false},
		{"espacios internos", ",dado    8", true, ",", "dado", []string{"8"}, false},
		{"restart coma", ",restart", true, ",", "", nil, true},
		{"restart exclamación con espacios", "  !restart  ", true, "!", "", nil, true},
		{"sin prefijo", "dado 20", false, "", "", nil, false},
		{"vacío", "", false, "", "", nil, false},
		{"solo prefijo", ",", false, "", "", nil, false},
		{"espacio antes del prefijo (como index.js)", " ,dado", false, "", "", nil, false},
	}

	for _, caso := range casos {
		t.Run(caso.nombre, func(t *testing.T) {
			got, ok := ParseCommand(caso.texto, prefixes)
			if ok != caso.ok {
				t.Fatalf("ok=%v, esperaba %v (parsed=%#v)", ok, caso.ok, got)
			}
			if !ok {
				return
			}
			if got.Prefix != caso.prefijo || got.Command != caso.comando || got.Restart != caso.restart {
				t.Fatalf("parsed inesperado: %#v", got)
			}
			if len(got.Args) != len(caso.args) {
				t.Fatalf("args=%#v, esperaba %#v", got.Args, caso.args)
			}
			for i := range caso.args {
				if got.Args[i] != caso.args[i] {
					t.Fatalf("args=%#v, esperaba %#v", got.Args, caso.args)
				}
			}
		})
	}
}

func TestParseCommandPrefijosPersonalizados(t *testing.T) {
	got, ok := ParseCommand("#dado", []string{"#"})
	if !ok || got.Prefix != "#" || got.Command != "dado" {
		t.Fatalf("prefijo personalizado no reconocido: %#v ok=%v", got, ok)
	}

	_, ok = ParseCommand(",dado", []string{"#"})
	if ok {
		t.Fatal("un prefijo no configurado no debe enrutar")
	}
}

func TestExtractText(t *testing.T) {
	casos := []struct {
		nombre string
		msg    *waE2E.Message
		want   string
	}{
		{"conversación", &waE2E.Message{Conversation: proto.String(",dado")}, ",dado"},
		{"texto extendido", &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("!moneda")}}, "!moneda"},
		{"caption imagen", &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("/ping")}}, "/ping"},
		{"caption video", &waE2E.Message{VideoMessage: &waE2E.VideoMessage{Caption: proto.String(",ayuda")}}, ",ayuda"},
		{"caption documento", &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{Caption: proto.String(",lista")}}, ",lista"},
		{"sin texto", &waE2E.Message{ImageMessage: &waE2E.ImageMessage{}}, ""},
	}

	for _, caso := range casos {
		t.Run(caso.nombre, func(t *testing.T) {
			if got := ExtractText(caso.msg); got != caso.want {
				t.Fatalf("ExtractText=%q, esperaba %q", got, caso.want)
			}
		})
	}

	if got := ExtractText(nil); got != "" {
		t.Fatalf("ExtractText(nil)=%q", got)
	}
}

func TestRegisterYLookup(t *testing.T) {
	r := NewRouter(nil)
	addon := &fakeAddon{cmds: []string{"Dado", " moneda "}}
	r.Register(addon)

	if got := r.Commands(); len(got) != 2 {
		t.Fatalf("comandos registrados inesperados: %#v", got)
	}
	if _, ok := r.Lookup("dado"); !ok {
		t.Fatal("dado debe estar registrado en minúsculas")
	}
	if _, ok := r.Lookup("moneda"); !ok {
		t.Fatal("moneda debe estar registrada sin espacios")
	}
	if _, ok := r.Lookup("otro"); ok {
		t.Fatal("otro no debería existir")
	}
}

func TestRegistroDuplicadoGanaElUltimo(t *testing.T) {
	r := NewRouter(nil)
	primero := &fakeAddon{cmds: []string{"eco"}}
	segundo := &fakeAddon{cmds: []string{"eco"}}
	r.Register(primero)
	r.Register(segundo)

	if _, err := r.Lookup("eco"); !err {
		t.Fatal("eco debe existir")
	}
	if err := r.Dispatch(&Context{Command: "eco"}); err != nil {
		t.Fatalf("Dispatch devolvió error: %v", err)
	}
	if primero.calls != 0 || segundo.calls != 1 {
		t.Fatalf("ganó el addon equivocado: primero=%d segundo=%d", primero.calls, segundo.calls)
	}
}

func TestDispatchDesconocido(t *testing.T) {
	r := NewRouter(nil)
	err := r.Dispatch(&Context{Command: "nada"})
	if !errors.Is(err, ErrUnknownCommand) {
		t.Fatalf("esperaba ErrUnknownCommand, obtuve %v", err)
	}
}

func TestDispatchPropagaError(t *testing.T) {
	r := NewRouter(nil)
	boom := errors.New("boom")
	r.Register(&fakeAddon{cmds: []string{"falla"}, err: boom})

	if err := r.Dispatch(&Context{Command: "falla"}); !errors.Is(err, boom) {
		t.Fatalf("esperaba boom, obtuve %v", err)
	}
}

func TestProcessMessageEnrutaYAlimentaStore(t *testing.T) {
	dir := t.TempDir()
	st := store.New(filepath.Join(dir, "baileys_store_cache.json"))
	prefixes := []string{","}
	r := NewRouter(func() []string { return prefixes })
	r.Store = st

	addon := &fakeAddon{cmds: []string{"dado"}}
	r.Register(addon)

	msg := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:   types.NewJID("120363000000000000", types.GroupServer),
				Sender: types.NewJID("58412345678", types.DefaultUserServer),
			},
			PushName: "Luis",
			ID:       "ABC123",
		},
		Message: &waE2E.Message{Conversation: proto.String(",dado 20")},
	}
	r.ProcessMessage(msg)

	if addon.calls != 1 {
		t.Fatalf("el addon debió ejecutarse una vez, se ejecutó %d", addon.calls)
	}
	if addon.got.Command != "dado" || addon.got.Prefix != "," || len(addon.got.Args) != 1 || addon.got.Args[0] != "20" {
		t.Fatalf("contexto inesperado: %#v", addon.got)
	}
	if addon.got.RawText != ",dado 20" {
		t.Fatalf("RawText inesperado: %q", addon.got.RawText)
	}
	if got := st.Notify("58412345678@s.whatsapp.net"); got != "Luis" {
		t.Fatalf("pushName no guardado, notify=%q", got)
	}
}

func TestProcessMessageIgnoraSinTexto(t *testing.T) {
	r := NewRouter(nil)
	addon := &fakeAddon{cmds: []string{"dado"}}
	r.Register(addon)

	r.ProcessMessage(&events.Message{Message: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{}}})
	r.ProcessMessage(nil)
	r.ProcessMessage(&events.Message{})
	if addon.calls != 0 {
		t.Fatalf("no debió ejecutarse, se ejecutó %d veces", addon.calls)
	}
}

func TestProcessMessageRestart(t *testing.T) {
	r := NewRouter(func() []string { return []string{",", "!", "/"} })
	addon := &fakeAddon{cmds: []string{"dado"}}
	r.Register(addon)

	var salida int
	r.Exit = func(code int) { salida = code }

	r.ProcessMessage(&events.Message{
		Info:    types.MessageInfo{MessageSource: types.MessageSource{Chat: types.NewJID("1", types.DefaultUserServer)}},
		Message: &waE2E.Message{Conversation: proto.String("  !restart ")},
	})

	if salida != 1 {
		t.Fatalf("esperaba Exit(1), obtuve %d", salida)
	}
	if addon.calls != 0 {
		t.Fatal("restart no debe llegar al addon")
	}
}

func TestPrefijosDinamicos(t *testing.T) {
	actuales := []string{","}
	r := NewRouter(func() []string { return actuales })
	addon := &fakeAddon{cmds: []string{"dado"}}
	r.Register(addon)

	nuevo := &events.Message{Message: &waE2E.Message{Conversation: proto.String("#dado")}}
	r.ProcessMessage(nuevo)
	if addon.calls != 0 {
		t.Fatal("# no está configurado todavía")
	}

	actuales = []string{"#"}
	r.ProcessMessage(nuevo)
	if addon.calls != 1 {
		t.Fatal("tras cambiar los prefijos, #dado debe enrutar")
	}
}

func TestRegisterBackgroundSeEjecuta(t *testing.T) {
	r := NewRouter(nil)
	var ejecutado bool
	r.RegisterBackground("prueba", func(_ *whatsmeow.Client, _ *store.Store) error {
		ejecutado = true
		return nil
	})
	if err := r.StartBackgrounds(nil, store.New(filepath.Join(t.TempDir(), "c.json"))); err != nil {
		t.Fatalf("StartBackgrounds error: %v", err)
	}
	if !ejecutado {
		t.Fatal("el proceso en segundo plano no se ejecutó")
	}
}

func TestRegisterBackgroundAceptaAddonConInit(t *testing.T) {
	r := NewRouter(nil)
	addon := &initAddon{}
	r.Register(addon)

	if err := r.StartBackgrounds(nil, store.New(filepath.Join(t.TempDir(), "c.json"))); err != nil {
		t.Fatalf("StartBackgrounds error: %v", err)
	}
	if !addon.iniciado {
		t.Fatal("Init del BackgroundAddon no se ejecutó")
	}

	// Registrar de nuevo no debe repetir el Init.
	r.Register(addon)
	if err := r.StartBackgrounds(nil, nil); err != nil {
		t.Fatalf("StartBackgrounds error: %v", err)
	}
	if addon.veces != 1 {
		t.Fatalf("Init se ejecutó %d veces, esperaba 1", addon.veces)
	}
}

type initAddon struct {
	iniciado bool
	veces    int
}

func (i *initAddon) Commands() []string    { return []string{"initdemo"} }
func (i *initAddon) Handle(*Context) error { return nil }
func (i *initAddon) Init(*whatsmeow.Client, *store.Store) error {
	i.iniciado = true
	i.veces++
	return nil
}

func TestNormalizeCommand(t *testing.T) {
	if got := NormalizeCommand("  DADO "); got != "dado" {
		t.Fatalf("NormalizeCommand=%q", got)
	}
	if got := NormalizeCommand(""); got != "" {
		t.Fatalf("NormalizeCommand vacío=%q", got)
	}
}

func TestTextMessageHelper(t *testing.T) {
	msg := TextMessage("hola")
	if msg.GetConversation() != "hola" {
		t.Fatalf("TextMessage inesperado: %v", msg)
	}
	if !strings.Contains(msg.String(), "hola") {
		t.Fatal("el mensaje debe contener el texto")
	}
}
