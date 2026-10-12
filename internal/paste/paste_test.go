//go:build linux

package paste

import (
	"os"
	"testing"
)

func TestScriptIsInjectable(t *testing.T) {
	// The script runs through WebView Init on every document; it must be a
	// self-contained IIFE that never throws, so the paste path cannot depend
	// on any other injected script.
	if len(Script) < 100 {
		t.Fatalf("Script too short to be a real shim: %d chars", len(Script))
	}
	if !containsAll(Script, "(function () {", "addEventListener('paste'", "ClipboardEvent", "wagramReadClipboardImage") {
		t.Fatal("Script is missing a required part of the fallback path")
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		found := false
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func TestReadImageWithoutDisplayReturnsEmpty(t *testing.T) {
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	if got := ReadImage(); got != "" {
		t.Fatalf("expected empty result with no display, got %d chars of base64", len(got))
	}
}

func TestReadImageReadsXPng(t *testing.T) {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		t.Skip("no display in this environment")
	}
	// Not an assertion: whatever comes back must be plain base64 (no spaces,
	// newlines or URL-encoding), because the shim feeds it straight to atob.
	got := ReadImage()
	for _, r := range got {
		if r == '+' || r == '/' || r == '=' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			continue
		}
		t.Fatalf("unexpected byte %q in base64 output", r)
	}
}
