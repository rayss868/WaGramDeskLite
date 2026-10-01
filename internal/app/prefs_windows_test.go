//go:build windows

package app

import (
	"encoding/json"
	"testing"
)

func TestPrivacyDefaultsToOff(t *testing.T) {
	if privacyEnabled(prefs{}) {
		t.Fatal("empty prefs must have privacy off")
	}
}

func TestSetPrivacyEnabledRoundTrips(t *testing.T) {
	var p prefs
	setPrivacyEnabled(&p, true)
	if !privacyEnabled(p) {
		t.Fatal("privacy must be on after setPrivacyEnabled(true)")
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back prefs
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !privacyEnabled(back) {
		t.Fatal("privacy must survive a JSON round-trip")
	}
	setPrivacyEnabled(&p, false)
	if privacyEnabled(p) {
		t.Fatal("privacy must be off after setPrivacyEnabled(false)")
	}
}

func TestPrivacyRevealNormalizes(t *testing.T) {
	cases := map[string]string{
		"":         RevealHard,
		RevealHard: RevealHard,
		RevealHover: RevealHover,
		RevealClick: RevealClick,
		"garbage":  RevealHard,
	}
	for in, want := range cases {
		p := prefs{PrivacyReveal: in}
		if got := privacyReveal(p); got != want {
			t.Errorf("privacyReveal(%q) = %q, want %q", in, got, want)
		}
	}
}
