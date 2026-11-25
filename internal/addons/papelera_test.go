package addons

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"google.golang.org/protobuf/proto"
)

func TestPapeleraSinComandos(t *testing.T) {
	if got := (Papelera{}).Commands(); len(got) != 0 {
		t.Fatalf("comandos=%#v", got)
	}
}

func TestPapeleraEsBasura(t *testing.T) {
	casos := map[string]bool{
		"🗑":   true,
		"🗑️":  true,
		"🚮":   true,
		"🚮️":  true,
		"👍":   false,
		"":    false,
		"🗑️x": false,
	}
	for entrada, want := range casos {
		if got := papeleraEsBasura(entrada); got != want {
			t.Fatalf("papeleraEsBasura(%q)=%v, esperaba %v", entrada, got, want)
		}
	}
}

func TestPapeleraEsDelBot(t *testing.T) {
	bots := []string{"58412345678"}
	key := func(fromMe bool, participant, remote string) *waCommon.MessageKey {
		k := &waCommon.MessageKey{ID: proto.String("ABC"), FromMe: proto.Bool(fromMe)}
		if participant != "" {
			k.Participant = proto.String(participant)
		}
		if remote != "" {
			k.RemoteJID = proto.String(remote)
		}
		return k
	}

	if !papeleraEsDelBot(key(true, "", "1205@g.us"), bots) {
		t.Fatal("fromMe sin participant debe ser del bot")
	}
	if !papeleraEsDelBot(key(true, "58412345678@s.whatsapp.net", "1205@g.us"), bots) {
		t.Fatal("fromMe con participant del bot debe ser del bot")
	}
	if papeleraEsDelBot(key(true, "58499999999@s.whatsapp.net", "1205@g.us"), bots) {
		t.Fatal("fromMe con participant ajeno no debe ser del bot")
	}
	if !papeleraEsDelBot(key(false, "58412345678@s.whatsapp.net", "1205@g.us"), bots) {
		t.Fatal("participant del bot debe ser del bot")
	}
	if papeleraEsDelBot(key(false, "", "58499999999@s.whatsapp.net"), bots) {
		t.Fatal("remitente ajeno no debe ser del bot")
	}
	if papeleraEsDelBot(&waCommon.MessageKey{}, bots) {
		t.Fatal("key sin ID no debe ser del bot")
	}
}

func TestPapeleraProcesarSinCliente(t *testing.T) {
	papeleraProcesar(nil, &waCommon.MessageKey{ID: proto.String("1")}, "🗑")
}
