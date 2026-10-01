//go:build windows

package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPrivacyBootstrapDefaultsOff(t *testing.T) {
	var got struct {
		On     bool   `json:"on"`
		Reveal string `json:"reveal"`
		Sel    string `json:"sel"`
	}
	if err := json.Unmarshal([]byte(privacyBootstrap(prefs{})), &got); err != nil {
		t.Fatalf("bootstrap is not valid JSON: %v", err)
	}
	if got.On {
		t.Fatal("bootstrap must default to off")
	}
	if got.Reveal != RevealHard {
		t.Fatalf("bootstrap reveal = %q, want %q", got.Reveal, RevealHard)
	}
	if got.Sel == "" {
		t.Fatal("bootstrap must carry the click selector list")
	}
}

func TestPrivacyBootstrapOnWithClick(t *testing.T) {
	p := prefs{}
	setPrivacyEnabled(&p, true)
	setPrivacyReveal(&p, RevealClick)
	var got struct {
		On     bool   `json:"on"`
		Reveal string `json:"reveal"`
	}
	if err := json.Unmarshal([]byte(privacyBootstrap(p)), &got); err != nil {
		t.Fatalf("bootstrap is not valid JSON: %v", err)
	}
	if !got.On || got.Reveal != RevealClick {
		t.Fatalf("bootstrap = %+v, want on/click", got)
	}
}

func TestPrivacyCSSIsGatedAndHasThreeModes(t *testing.T) {
	css := privacyCSS()
	for _, want := range []string{
		`html[data-wagdl-privacy="on"]`,
		`#main [data-testid="msg-container"]`,
		`#pane-side`,
		`data-wagdl-reveal="hover"`,
		`data-wagdl-reveal="click"`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("CSS must contain %q", want)
		}
	}
}

func TestPrivacyScriptSubstitutesTokens(t *testing.T) {
	s := privacyScript(prefs{})
	if strings.Contains(s, "__WAGDL_BOOTSTRAP__") || strings.Contains(s, "__WAGDL_CSS__") {
		t.Fatal("tokens must all be substituted")
	}
	if !strings.Contains(s, `"on":false`) {
		t.Fatal("script must carry the bootstrap JSON")
	}
	if !strings.Contains(s, "data-wagdl-privacy") {
		t.Fatal("script must contain the CSS")
	}
}

func TestPrivacyScriptKeepsLiveRevealOnShortcut(t *testing.T) {
	s := privacyScript(prefs{})
	if strings.Contains(s, "apply(next, bootstrap.reveal)") {
		t.Fatal("the shortcut must not reapply the baked reveal; it must keep the live one")
	}
	if !strings.Contains(s, "html.getAttribute('data-wagdl-reveal')") {
		t.Fatal("the shortcut must read the current reveal from the attribute")
	}
}
