package main

import "testing"

func TestPairPhoneFromArgs(t *testing.T) {
	casos := []struct {
		args  []string
		phone string
	}{
		{[]string{"--pair", "584121234567"}, "584121234567"},
		{[]string{"--pair=+58 412-123-4567"}, "584121234567"},
		{[]string{"-pair", "584121234567"}, "584121234567"},
		{[]string{"-pair=584121234567"}, "584121234567"},
		{[]string{"--pair"}, ""},
		{[]string{"--otro", "584121234567"}, ""},
		{nil, ""},
	}
	for _, caso := range casos {
		if got := pairPhoneFromArgs(caso.args); got != caso.phone {
			t.Fatalf("pairPhoneFromArgs(%q)=%q, esperaba %q", caso.args, got, caso.phone)
		}
	}
}

func TestSanitizePairPhone(t *testing.T) {
	casos := map[string]string{
		"+58 412-123-4567": "584121234567",
		"584121234567":     "584121234567",
		"abc":              "",
		"":                 "",
	}
	for entrada, want := range casos {
		if got := sanitizePairPhone(entrada); got != want {
			t.Fatalf("sanitizePairPhone(%q)=%q, esperaba %q", entrada, got, want)
		}
	}
}

func TestMaskPairPhone(t *testing.T) {
	if got := maskPairPhone("584121234567"); got != "********4567" {
		t.Fatalf("maskPairPhone=%q", got)
	}
	if got := maskPairPhone("12"); got != "****" {
		t.Fatalf("maskPairPhone corto=%q", got)
	}
}
