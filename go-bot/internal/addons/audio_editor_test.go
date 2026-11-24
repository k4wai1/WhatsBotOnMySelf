package addons

import (
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"whatsbot-go/internal/core"
)

func TestAudioEditorCommands(t *testing.T) {
	got := AudioEditor{}.Commands()
	if len(got) != 1 || got[0] != "au" {
		t.Fatalf("comandos inesperados: %#v", got)
	}
}

func TestAuParseTime(t *testing.T) {
	casos := []struct {
		entrada string
		want    float64
		ok      bool
	}{
		{"2.5", 2.5, true},
		{"2,5", 2.5, true},
		{"8.3s", 8.3, true},
		{"12", 12, true},
		{"-3", -3, true},
		{"abc", 0, false},
		{"", 0, false},
	}
	for _, caso := range casos {
		got, ok := auParseTime(caso.entrada)
		if ok != caso.ok || got != caso.want {
			t.Fatalf("auParseTime(%q)=(%v,%v), esperaba (%v,%v)", caso.entrada, got, ok, caso.want, caso.ok)
		}
	}
}

func TestAuNormalizeArgs(t *testing.T) {
	args := auNormalizeArgs("in.ogg", "out.ogg", true, 2.5, true, 8)
	unido := strings.Join(args, " ")
	for _, want := range []string{"-i in.ogg", "-ss 2.5", "-t 8", "-ar 48000", "libopus", "-f ogg"} {
		if !strings.Contains(unido, want) {
			t.Fatalf("args sin %q: %v", want, args)
		}
	}
	if args[len(args)-1] != "out.ogg" {
		t.Fatalf("la salida debe ir al final: %v", args)
	}

	sinCortes := auNormalizeArgs("a", "b", false, 0, false, 0)
	if strings.Contains(strings.Join(sinCortes, " "), "-ss") || strings.Contains(strings.Join(sinCortes, " "), "-t ") {
		t.Fatalf("no debía recortar: %v", sinCortes)
	}
}

func TestAuMergeArgs(t *testing.T) {
	args := auMergeArgs([]string{"a.ogg", "b.ogg"}, "out.ogg")
	unido := strings.Join(args, " ")
	for _, want := range []string{"-i a.ogg", "-i b.ogg", "-filter_complex", "concat=n=2:v=0:a=1[out]", "-map [out]"} {
		if !strings.Contains(unido, want) {
			t.Fatalf("args sin %q: %v", want, args)
		}
	}
	if args[len(args)-1] != "out.ogg" {
		t.Fatalf("la salida debe ir al final: %v", args)
	}
}

func TestAuSinMediaCitado(t *testing.T) {
	var textos []string
	var reacciones []string
	ctx := &core.Context{
		Command: "au",
		Args:    []string{"r", "1", "2"},
		Message: &events.Message{
			Info:    types.MessageInfo{MessageSource: types.MessageSource{Chat: types.NewJID("1", types.DefaultUserServer)}},
			Message: &waE2E.Message{Conversation: proto.String("au r 1 2")},
		},
		SendHook: func(m *waE2E.Message) error {
			if m.GetConversation() != "" {
				textos = append(textos, m.GetConversation())
			}
			if m.GetExtendedTextMessage() != nil {
				textos = append(textos, m.GetExtendedTextMessage().GetText())
			}
			return nil
		},
		ReactHook: func(e string) error { reacciones = append(reacciones, e); return nil },
	}
	if err := (AudioEditor{}).Handle(ctx); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(reacciones) != 1 || reacciones[0] != "🎧" {
		t.Fatalf("reacciones=%#v", reacciones)
	}
	if len(textos) != 1 || !strings.Contains(textos[0], "Debes responder a un mensaje de audio o video") {
		t.Fatalf("textos=%#v", textos)
	}
}
