//go:build windows

package app

import (
	"net/url"
	"unsafe"

	"github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"
)

func openExternalLink(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return false
	}
	uri, err := windows.UTF16PtrFromString(raw)
	if err != nil {
		return false
	}
	r, _, _ := procShellExecuteW.Call(0, strPtr("open"), uintptr(unsafe.Pointer(uri)), 0, 0, swShowNormal)
	return r > 32
}

func initExternalLinks(w webview2.WebView) {
	_ = w.Bind("wagramOpenExternal", openExternalLink)
	w.Init(externalLinksScript)
}

// Keep messenger navigation in the page, but hand external web links to Windows.
const externalLinksScript = `
(function () {
	function externalURL(value) {
		try {
			var u = new URL(value, location.href);
			if ((u.protocol === 'https:' || u.protocol === 'http:') && u.origin !== location.origin) {
				return u.href;
			}
		} catch (e) {}
		return '';
	}
	function openLink(ev) {
		if (ev.defaultPrevented || (ev.type === 'click' ? ev.button !== 0 : ev.button !== 1)) { return; }
		var target = ev.target;
		var link = target && target.closest ? target.closest('a[href]') : null;
		if (!link) { return; }
		var uri = externalURL(link.href);
		if (!uri) { return; }
		ev.preventDefault();
		window.wagramOpenExternal(uri);
	}
	window.addEventListener('click', openLink);
	window.addEventListener('auxclick', openLink);
	var originalOpen = window.open;
	window.open = function (value) {
		var uri = externalURL(value);
		if (uri) {
			window.wagramOpenExternal(uri);
			return null;
		}
		return originalOpen.apply(window, arguments);
	};
})();
`
