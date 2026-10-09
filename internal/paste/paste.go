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

// ReadImage returns the OS clipboard image as base64 PNG, or "" when no image
// is available. Wayland is probed first when that session is active because
// wl-clipboard and xclip read different clipboards there.
func ReadImage() string {
	cmds := [][]string{
		{"xclip", "-selection", "clipboard", "-t", "image/png", "-o"},
		{"wl-paste", "--type", "image/png", "--no-newline"},
	}
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		cmds[0], cmds[1] = cmds[1], cmds[0]
	}
	for _, argv := range cmds {
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
