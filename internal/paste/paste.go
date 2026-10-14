//go:build linux

// Package paste bridges the OS clipboard into the page for image pastes.
//
// WebKitGTK strips image payloads out of paste events: DataTransfer arrives
// with an empty types list and zero files (WebKit bug 218519 and the
// CVE-2025-13947 regression from allowsFileAccess()). Text pastes are
// unaffected, and navigator.clipboard.read() is denied outright, so the only
// way to deliver a screenshot is to read the OS clipboard from the host
// process and re-dispatch a paste event carrying the image as a File.
package paste

import (
	"encoding/base64"
	"os"
	"os/exec"
	"strconv"
)

// Script is injected into every document. It leaves healthy paste events
// alone and, when WebKitGTK delivers an empty DataTransfer, reads the image
// through the host binding and dispatches a synthetic paste carrying it.
const Script = `(function () {
  var busy = false;
  document.addEventListener('paste', function (e) {
    if (busy) { return; }
    var dt = e.clipboardData;
    var hasNative = dt && ((dt.files && dt.files.length) || (dt.types && dt.types.length));
    if (hasNative) { return; }
    if (!window.wagramReadClipboardImage) { return; }
    busy = true;
    var target = e.target || document.body;
    Promise.resolve(window.wagramReadClipboardImage()).then(function (b64) {
      busy = false;
      if (!b64) { return; }
      var bin = atob(b64);
      var arr = new Uint8Array(bin.length);
      for (var i = 0; i < bin.length; i++) { arr[i] = bin.charCodeAt(i); }
      var file = new File([arr], 'clipboard.png', { type: 'image/png' });
      var data = new DataTransfer();
      data.items.add(file);
      target.dispatchEvent(new ClipboardEvent('paste', {
        clipboardData: data, bubbles: true, cancelable: true
      }));
    }).catch(function () { busy = false; });
  }, true);
})();`

// clipboardCommands lists the OS clipboard readers in probe order. On Wayland
// wl-paste reads the Wayland clipboard and xclip the X11 one (via XWayland),
// which are different selections, so the active session decides the order.
func clipboardCommands() [][]string {
	xclip := []string{"xclip", "-selection", "clipboard", "-t", "image/png", "-o"}
	wlpaste := []string{"wl-paste", "--type", "image/png", "--no-newline"}
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		return [][]string{wlpaste, xclip}
	}
	return [][]string{xclip, wlpaste}
}

// ClipboardTool reports which clipboard helper ReadImage will use ("xclip" or
// "wl-paste"), or "" when neither is installed. Without one of them the paste
// bridge cannot read images and ReadImage always returns "" — a silent failure
// the caller should surface to the user instead of dropping pastes quietly.
func ClipboardTool() string {
	for _, argv := range clipboardCommands() {
		if _, err := exec.LookPath(argv[0]); err == nil {
			return argv[0]
		}
	}
	return ""
}

// InstallHint names the package that provides the clipboard helper for the
// running session, for use in a user-facing warning.
func InstallHint() string {
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		return "wl-clipboard"
	}
	return "xclip"
}

// WarningText is the user-facing message explaining that image pastes are
// disabled because no clipboard helper is installed. It returns "" when a
// helper exists, so callers can treat "" as "nothing to warn about".
func WarningText() string {
	if ClipboardTool() != "" {
		return ""
	}
	pkg := InstallHint()
	return "Paste gambar nonaktif: paket \"" + pkg + "\" belum terpasang. Jalankan: sudo apt install " + pkg
}

// WarningScript returns a JS snippet that paints a dismissible banner carrying
// WarningText(), or "" when pastes work. Injecting "" is a no-op, so the
// healthy path costs nothing.
func WarningScript() string {
	msg := WarningText()
	if msg == "" {
		return ""
	}
	return `(function () {
  function show() {
    if (document.getElementById('wagram-clip-warn')) { return; }
    var d = document.createElement('div');
    d.id = 'wagram-clip-warn';
    d.style.cssText = 'position:fixed;z-index:2147483647;left:0;right:0;top:0;padding:10px 44px 10px 14px;background:#8a1c1c;color:#fff;font:14px/1.4 system-ui,sans-serif;text-align:center;box-shadow:0 2px 8px rgba(0,0,0,.35)';
    d.textContent = ` + strconv.Quote(msg) + `;
    var x = document.createElement('button');
    x.textContent = '\u00d7';
    x.setAttribute('aria-label', 'Tutup');
    x.style.cssText = 'position:absolute;right:8px;top:50%;transform:translateY(-50%);background:transparent;border:0;color:#fff;font:20px/1 system-ui,sans-serif;cursor:pointer';
    x.onclick = function () { d.remove(); };
    d.appendChild(x);
    (document.body || document.documentElement).appendChild(d);
  }
  if (document.readyState === 'loading') { document.addEventListener('DOMContentLoaded', show); } else { show(); }
})();`
}

// ReadImage returns the OS clipboard image as base64 PNG, or "" when no image
// is available or no clipboard helper is installed.
func ReadImage() string {
	for _, argv := range clipboardCommands() {
		if _, err := exec.LookPath(argv[0]); err != nil {
			continue
		}
		out, err := exec.Command(argv[0], argv[1:]...).Output()
		if err == nil && len(out) > 8 {
			return base64.StdEncoding.EncodeToString(out)
		}
	}
	return ""
}
