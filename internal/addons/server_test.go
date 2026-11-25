package addons

import (
	"bufio"
	"strings"
	"testing"
)

func TestServerCommands(t *testing.T) {
	if got := (Server{}).Commands(); len(got) != 1 || got[0] != "server" {
		t.Fatalf("comandos=%#v", got)
	}
}

func TestServerParseTarget(t *testing.T) {
	t.Setenv("MC_DEFAULT_SERVER", "")
	host, port := serverParseTarget(nil)
	if host != "FocaTostada.aternos.me" || port != 25565 {
		t.Fatalf("default=(%q,%d)", host, port)
	}
	host, port = serverParseTarget([]string{"mc.example.com:25570"})
	if host != "mc.example.com" || port != 25570 {
		t.Fatalf("explícito=(%q,%d)", host, port)
	}
	host, port = serverParseTarget([]string{"mc.example.com:abc"})
	if host != "mc.example.com:abc" || port != 25565 {
		t.Fatalf("puerto inválido=(%q,%d)", host, port)
	}
}

func TestServerVarintRoundtrip(t *testing.T) {
	for _, valor := range []int{0, 1, 127, 128, 255, 300, 767, 65535} {
		raw := serverVarint(valor)
		got, err := serverReadVarint(bufio.NewReader(strings.NewReader(string(raw))))
		if err != nil {
			t.Fatalf("varint %d: %v", valor, err)
		}
		if got != valor {
			t.Fatalf("varint %d → %d", valor, got)
		}
	}
}

func TestServerString(t *testing.T) {
	raw := serverString("hola")
	if len(raw) != 5 || raw[0] != 4 || string(raw[1:]) != "hola" {
		t.Fatalf("serverString=%v", raw)
	}
}
