package addons

import (
	"strings"
	"testing"
	"time"
)

func TestSysinfoCommands(t *testing.T) {
	got := (Sysinfo{}).Commands()
	if len(got) != 2 || got[0] != "sysinfo" || got[1] != "sistema" {
		t.Fatalf("comandos=%#v", got)
	}
}

func TestSysinfoHandle(t *testing.T) {
	prevStart, prevNow := sysinfoStart, sysinfoNow
	sysinfoStart = time.Now().Add(-2 * time.Hour)
	sysinfoNow = func() time.Time { return sysinfoStart.Add(2 * time.Hour) }
	t.Cleanup(func() { sysinfoStart, sysinfoNow = prevStart, prevNow })

	c := &capture{}
	if err := (Sysinfo{}).Handle(c.newCtx("sysinfo")); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.texts) != 1 {
		t.Fatalf("textos=%#v", c.texts)
	}
	for _, want := range []string{"Bot Status", "Go:", "RSS:", "Uptime"} {
		if !strings.Contains(c.texts[0], want) {
			t.Fatalf("texto sin %q: %s", want, c.texts[0])
		}
	}
}
