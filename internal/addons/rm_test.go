package addons

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestRmCommands(t *testing.T) {
	if got := (Rm{}).Commands(); len(got) != 1 || got[0] != "rm" {
		t.Fatalf("comandos=%#v", got)
	}
}

func TestRmNormalize(t *testing.T) {
	casos := map[string]string{
		"584141234567@s.whatsapp.net":    "584141234567@s.whatsapp.net",
		"584141234567:12@s.whatsapp.net": "584141234567@s.whatsapp.net",
		"584141234567@lid":               "584141234567@lid",
		"":                               "",
	}
	for entrada, want := range casos {
		if got := rmNormalize(entrada); got != want {
			t.Fatalf("rmNormalize(%q)=%q, esperaba %q", entrada, got, want)
		}
	}
}

func TestRmParseArgs(t *testing.T) {
	count, formato, objetivo := rmParseArgs([]string{"5", "audio"}, nil)
	if count != 5 || formato != "audio" || objetivo != "" {
		t.Fatalf("(%d,%q,%q)", count, formato, objetivo)
	}
	count, formato, objetivo = rmParseArgs([]string{"img"}, []string{"584141234567@s.whatsapp.net"})
	if count != 1 || formato != "img" || objetivo != "584141234567@s.whatsapp.net" {
		t.Fatalf("(%d,%q,%q)", count, formato, objetivo)
	}
	count, _, objetivo = rmParseArgs([]string{"@584141234567"}, nil)
	if count != 1 || objetivo != "584141234567@s.whatsapp.net" {
		t.Fatalf("(%d,%q)", count, objetivo)
	}
	if count, _, _ := rmParseArgs([]string{"9999"}, nil); count != rmMaxCount {
		t.Fatalf("tope count=%d", count)
	}
	if count, _, _ := rmParseArgs([]string{"0"}, nil); count != 1 {
		t.Fatalf("mínimo count=%d", count)
	}
}

func TestRmDetailedType(t *testing.T) {
	casos := []struct {
		msg  *waE2E.Message
		want string
	}{
		{&waE2E.Message{ImageMessage: &waE2E.ImageMessage{}}, "img"},
		{&waE2E.Message{VideoMessage: &waE2E.VideoMessage{}}, "video"},
		{&waE2E.Message{VideoMessage: &waE2E.VideoMessage{GifPlayback: proto.Bool(true)}}, "gif"},
		{&waE2E.Message{AudioMessage: &waE2E.AudioMessage{}}, "audio"},
		{&waE2E.Message{AudioMessage: &waE2E.AudioMessage{PTT: proto.Bool(true)}}, "ptt"},
		{&waE2E.Message{StickerMessage: &waE2E.StickerMessage{}}, "sticker"},
		{&waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{}}, "doc"},
		{&waE2E.Message{ContactMessage: &waE2E.ContactMessage{}}, "contact"},
		{&waE2E.Message{LocationMessage: &waE2E.LocationMessage{}}, "location"},
		{&waE2E.Message{Conversation: proto.String("hola")}, "text"},
		{nil, ""},
	}
	for _, caso := range casos {
		if got := rmDetailedType(caso.msg); got != caso.want {
			t.Fatalf("rmDetailedType=%q, esperaba %q", got, caso.want)
		}
	}

	envuelto := &waE2E.Message{EphemeralMessage: &waE2E.FutureProofMessage{
		Message: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{}},
	}}
	if got := rmDetailedType(envuelto); got != "img" {
		t.Fatalf("ephemeral=%q", got)
	}
}

func TestRmTipoCoincide(t *testing.T) {
	if !rmTipoCoincide("audio", "ptt") || !rmTipoCoincide("media", "sticker") || !rmTipoCoincide("all", "text") {
		t.Fatal("coincidencias esperadas")
	}
	if rmTipoCoincide("img", "video") || rmTipoCoincide("doc", "text") {
		t.Fatal("no debían coincidir")
	}
}

func TestRmBufferTope(t *testing.T) {
	prev := rmBuffers
	rmBuffers = map[string][]rmStored{}
	t.Cleanup(func() { rmBuffers = prev })

	for i := 0; i < rmMaxBuffer+10; i++ {
		rmBufferPush("1205@g.us", rmStored{})
	}
	if got := len(rmBufferGet("1205@g.us")); got != rmMaxBuffer {
		t.Fatalf("buffer=%d, esperaba %d", got, rmMaxBuffer)
	}
}

func TestRmFueraDeGrupo(t *testing.T) {
	if err := (Rm{}).Handle(wagoCtx("58412345678")); err != nil {
		t.Fatalf("fuera de grupo debe ser silencioso: %v", err)
	}
}
