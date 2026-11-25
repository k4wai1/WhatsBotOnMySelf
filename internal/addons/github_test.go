package addons

import (
	"strings"
	"testing"
)

func TestGithubCommands(t *testing.T) {
	got := Github{}.Commands()
	if len(got) != 3 || got[0] != "git" || got[1] != "github" || got[2] != "repo" {
		t.Fatalf("comandos inesperados: %#v", got)
	}
}

func TestGithubHandle(t *testing.T) {
	c := &capture{}
	if err := (Github{}).Handle(c.newCtx("github")); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(c.reactions) != 1 || c.reactions[0] != "🚀" {
		t.Fatalf("reacciones=%#v", c.reactions)
	}
	if len(c.texts) != 1 {
		t.Fatalf("textos=%#v", c.texts)
	}
	if !strings.Contains(c.texts[0], "WhatsBotOnMySelf") || !strings.Contains(c.texts[0], githubRepoURL) {
		t.Fatalf("texto inesperado: %q", c.texts[0])
	}
}

func TestGithubNilContext(t *testing.T) {
	if err := (Github{}).Handle(nil); err != nil {
		t.Fatalf("debía tolerar ctx nil: %v", err)
	}
}
