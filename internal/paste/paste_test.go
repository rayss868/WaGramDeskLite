//go:build linux

package paste

import (
	"os"
	"path/filepath"
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

// TestClipboardToolMissingIsSilentButDetectable is the regression guard for the
// user-visible bug: with no clipboard helper installed the paste bridge used to
// fail completely silently. ClipboardTool must report "" (so the caller can
// warn) and ReadImage must return "" without panicking.
func TestClipboardToolMissingIsSilentButDetectable(t *testing.T) {
	t.Setenv("DISPLAY", ":99")
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("PATH", "/nonexistent")
	if got := ClipboardTool(); got != "" {
		t.Fatalf("expected no clipboard tool, got %q", got)
	}
	if got := ReadImage(); got != "" {
		t.Fatalf("expected empty image without a clipboard tool, got %d bytes", len(got))
	}
}

// TestClipboardToolDetected checks the helper is found when present, and that
// the reported name matches the session (Wayland prefers wl-paste).
func TestClipboardToolDetected(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"xclip", "wl-paste"} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)

	t.Setenv("WAYLAND_DISPLAY", "")
	if got := ClipboardTool(); got != "xclip" {
		t.Fatalf("X11 session: want xclip, got %q", got)
	}
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	if got := ClipboardTool(); got != "wl-paste" {
		t.Fatalf("Wayland session: want wl-paste, got %q", got)
	}
}

func TestInstallHint(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "")
	if got := InstallHint(); got != "xclip" {
		t.Fatalf("X11: want xclip, got %q", got)
	}
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	if got := InstallHint(); got != "wl-clipboard" {
		t.Fatalf("Wayland: want wl-clipboard, got %q", got)
	}
}

// TestWarningWhenHelperMissing covers the fix for the silent-failure bug: with
// no helper the user must get a visible message naming the package to install,
// and the injected script must carry it.
func TestWarningWhenHelperMissing(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("PATH", "/nonexistent")

	msg := WarningText()
	if msg == "" {
		t.Fatal("expected a warning when no clipboard helper is installed")
	}
	if !containsAll(msg, "xclip", "apt install") {
		t.Fatalf("warning must name xclip and the install command, got %q", msg)
	}
	script := WarningScript()
	if !containsAll(script, "wagram-clip-warn", "xclip") {
		t.Fatalf("warning script missing banner or package name: %q", script)
	}
}

// TestNoWarningWhenHelperPresent guards the healthy path: nothing is injected.
func TestNoWarningWhenHelperPresent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "xclip"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("PATH", dir)

	if got := WarningText(); got != "" {
		t.Fatalf("expected no warning when xclip is present, got %q", got)
	}
	if got := WarningScript(); got != "" {
		t.Fatalf("expected no injected script when xclip is present, got %q", got)
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
