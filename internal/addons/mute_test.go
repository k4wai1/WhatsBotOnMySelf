package addons

import (
	"path/filepath"
	"testing"

	"whatsbot-go/internal/core"
)

func TestMuteCommands(t *testing.T) {
	got := (Mute{}).Commands()
	if len(got) != 2 || got[0] != "mute" || got[1] != "unmute" {
		t.Fatalf("comandos=%#v", got)
	}
}

func TestMuteParseMinutes(t *testing.T) {
	casos := []struct {
		args []string
		want int
		ok   bool
	}{
		{nil, 0, true},
		{[]string{""}, 0, true},
		{[]string{"15"}, 15, true},
		{[]string{"2.6"}, 3, true},
		{[]string{"0"}, 0, false},
		{[]string{"-3"}, 0, false},
		{[]string{"abc"}, 0, false},
	}
	for _, caso := range casos {
		got, ok := muteParseMinutes(caso.args)
		if got != caso.want || ok != caso.ok {
			t.Fatalf("muteParseMinutes(%v)=(%d,%v), esperaba (%d,%v)", caso.args, got, ok, caso.want, caso.ok)
		}
	}
}

func TestMuteStoreRoundtrip(t *testing.T) {
	dir := t.TempDir()
	prev := mutePath
	mutePath = func() string { return filepath.Join(dir, "mutes.json") }
	t.Cleanup(func() { mutePath = prev })

	until := int64(1700000000000)
	data := muteData{Groups: map[string]*muteInfo{
		"1205@g.us": {Until: &until, By: "584141234567", ChatName: "Grupo"},
	}}
	if err := muteSave(data); err != nil {
		t.Fatalf("muteSave: %v", err)
	}

	cargado := muteLoad()
	info, ok := cargado.Groups["1205@g.us"]
	if !ok || info.Until == nil || *info.Until != until || info.ChatName != "Grupo" {
		t.Fatalf("roundtrip inesperado: %#v", cargado)
	}

	if len(muteLoad().Groups) != 1 {
		t.Fatal("debía cargar un grupo")
	}
}

func TestMuteExpired(t *testing.T) {
	vencido := int64(1000)
	futuro := int64(5000)
	sinFecha := (*int64)(nil)
	data := muteData{Groups: map[string]*muteInfo{
		"a@g.us": {Until: &vencido},
		"b@g.us": {Until: &futuro},
		"c@g.us": {Until: sinFecha},
	}}
	got := muteExpired(data, 2000)
	if len(got) != 1 || got[0] != "a@g.us" {
		t.Fatalf("expired=%#v", got)
	}
}

func TestMuteFueraDeGrupo(t *testing.T) {
	if err := (Mute{}).Handle(wagoCtx("58412345678")); err != nil {
		t.Fatalf("fuera de grupo debe ser silencioso: %v", err)
	}
}

var _ = core.DataPath
