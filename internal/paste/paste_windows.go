//go:build windows

// Package paste bridges the OS clipboard into the page for image pastes.
//
// The Windows build needs no bridge: WebView2 delivers paste events with a
// populated DataTransfer (files included) and navigator.clipboard.read() is
// permitted, so the WebKitGTK workaround in paste.go is Linux-only. This
// stub exists so the package compiles on every platform and shared code can
// import it without build-tag gymnastics.
package paste

// Script is injected into every document. It is empty on Windows because
// native paste events already carry their payloads.
const Script = ""

// ReadImage returns the OS clipboard image as base64 PNG, or "" when no
// image is available. Windows reads the clipboard through the native paste
// event instead, so this always returns "".
func ReadImage() string {
	return ""
}

// ClipboardTool reports the clipboard helper ReadImage would use. Windows
// needs none, so it reports "" like the Linux build does when no helper is
// installed; callers gate the "install xclip/wl-clipboard" warning on Linux.
func ClipboardTool() string { return "" }

// InstallHint names the package that provides a clipboard helper. Windows
// needs none.
func InstallHint() string { return "" }
