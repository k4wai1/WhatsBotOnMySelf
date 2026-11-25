package addons

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
)

func TestAlarmCommands(t *testing.T) {
	got := (Alarm{}).Commands()
	if len(got) != 2 || got[0] != "a" || got[1] != "alarma" {
		t.Fatalf("comandos=%#v", got)
	}
}

func TestAlarmParse(t *testing.T) {
	casos := []struct {
		raw     string
		horas   int
		minutos int
		desc    string
		ok      bool
	}{
		{"12", 12, 0, "¡Es hora de tu recordatorio!", true},
		{"13 45", 13, 45, "¡Es hora de tu recordatorio!", true},
		{"17:36 descripción", 17, 36, "descripción", true},
		{"24", 24, 0, "¡Es hora de tu recordatorio!", false},
		{"12 60", 12, 60, "¡Es hora de tu recordatorio!", false},
		{"hola", 0, 0, "", false},
	}
	for _, caso := range casos {
		h, m, d, ok := alarmParse(caso.raw)
		if h != caso.horas || m != caso.minutos || d != caso.desc || ok != caso.ok {
			t.Fatalf("alarmParse(%q)=(%d,%d,%q,%v), esperaba (%d,%d,%q,%v)",
				caso.raw, h, m, d, ok, caso.horas, caso.minutos, caso.desc, caso.ok)
		}
	}
}

func TestAlarmNextTarget(t *testing.T) {
	now := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	got := alarmNextTarget(10, 30, now)
	if got.Hour() != 10 || got.Minute() != 30 || got.Day() != 24 {
		t.Fatalf("mismo día=%v", got)
	}
	got = alarmNextTarget(9, 0, now)
	if got.Day() != 25 {
		t.Fatalf("día siguiente=%v", got)
	}
}

func TestAlarmStoreRoundtrip(t *testing.T) {
	dir := t.TempDir()
	prev := alarmPath
	alarmPath = func() string { return filepath.Join(dir, "recordatorios.json") }
	t.Cleanup(func() { alarmPath = prev })

	lista := []alarmEntry{{ID: "1", ChatID: "1205@g.us", Message: "nota", TargetTime: 123}}
	if err := alarmSave(lista); err != nil {
		t.Fatalf("alarmSave: %v", err)
	}
	got := alarmLoad()
	if len(got) != 1 || got[0].Message != "nota" || got[0].TargetTime != 123 {
		t.Fatalf("roundtrip=%#v", got)
	}
}

func TestAlarmHandleInvalido(t *testing.T) {
	c := &capture{}
	if err := (Alarm{}).Handle(c.newCtx("a", "hola")); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "Formato") {
		t.Fatalf("textos=%#v", c.texts)
	}
}

func TestAlarmHandleGuarda(t *testing.T) {
	dir := t.TempDir()
	prev := alarmPath
	alarmPath = func() string { return filepath.Join(dir, "recordatorios.json") }
	t.Cleanup(func() { alarmPath = prev })

	ctx := wagoCtx("1205@g.us")
	ctx.Args = []string{"18:30", "reunión"}
	var textos []string
	ctx.SendHook = func(m *waE2E.Message) error {
		textos = append(textos, messageText(m))
		return nil
	}
	if err := (Alarm{}).Handle(ctx); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(textos) != 1 || !strings.Contains(textos[0], "18:30") {
		t.Fatalf("textos=%#v", textos)
	}
	lista := alarmLoad()
	if len(lista) != 1 || lista[0].Message != "reunión" || lista[0].ChatID != "1205@g.us" {
		t.Fatalf("guardado=%#v", lista)
	}
}
