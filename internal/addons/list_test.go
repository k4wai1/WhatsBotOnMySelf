package addons

import (
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/types"

	"whatsbot-go/internal/core"
)

func TestListCommands(t *testing.T) {
	if got := (List{}).Commands(); len(got) != 1 || got[0] != "list" {
		t.Fatalf("comandos=%#v", got)
	}
}

func TestListBuildPage(t *testing.T) {
	groups := []*types.GroupInfo{
		{GroupName: types.GroupName{Name: "Grupo A"}, JID: types.NewJID("1205", types.GroupServer), Participants: []types.GroupParticipant{{}, {}}},
		{GroupName: types.GroupName{Name: "Grupo B"}, JID: types.NewJID("1206", types.GroupServer)},
	}
	out := listBuildPage(groups, 1, 0)
	for _, want := range []string{"Lista de Grupos* (1)", "1. *Grupo A*", "Participantes: 2", "2. *Grupo B*", "1206@g.us"} {
		if !strings.Contains(out, want) {
			t.Fatalf("listado sin %q:\n%s", want, out)
		}
	}
}

func TestListSinGrupos(t *testing.T) {
	prev := listGroupsFn
	listGroupsFn = func(*core.Context) ([]*types.GroupInfo, error) { return nil, nil }
	t.Cleanup(func() { listGroupsFn = prev })

	c := &capture{}
	if err := (List{}).Handle(c.newCtx("list")); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "No estás en ningún grupo") {
		t.Fatalf("textos=%#v", c.texts)
	}
}

func TestListVariosGrupos(t *testing.T) {
	prev := listGroupsFn
	listGroupsFn = func(*core.Context) ([]*types.GroupInfo, error) {
		return []*types.GroupInfo{
			{GroupName: types.GroupName{Name: "Uno"}, JID: types.NewJID("1", types.GroupServer)},
			{GroupName: types.GroupName{Name: "Dos"}, JID: types.NewJID("2", types.GroupServer)},
		}, nil
	}
	t.Cleanup(func() { listGroupsFn = prev })

	c := &capture{}
	if err := (List{}).Handle(c.newCtx("list")); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.texts) != 1 || !strings.Contains(c.texts[0], "Uno") || !strings.Contains(c.texts[0], "Dos") {
		t.Fatalf("textos=%#v", c.texts)
	}
}
