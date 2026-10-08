//go:build linux

package app

import (
	"encoding/json"
	"strings"
)

// privacyBlurScriptTemplate is the JS injected on every document. The two
// tokens are replaced by privacyScript: __WAGDL_BOOTSTRAP__ with the initial
// state as a JS object literal, __WAGDL_CSS__ with the blur rules as a JS
// string literal. Tokens are used instead of fmt verbs because the CSS is data
// and may contain '%' characters that fmt would misread.
//
// The script appends the style to documentElement, applies the gate attributes
// from the baked bootstrap, exposes window.wagramPrivacyApply so the overlay can
// flip the gate without a round trip to Go, and installs the click-reveal and
// shortcut listeners.
const privacyBlurScriptTemplate = `(function () {
	var bootstrap = __WAGDL_BOOTSTRAP__;

	function init() {
		var html = document.documentElement;
		var style = document.createElement('style');
		style.id = 'wagdl-privacy';
		style.textContent = __WAGDL_CSS__;
		html.appendChild(style);

		function apply(on, reveal) {
			if (on) { html.setAttribute('data-wagdl-privacy', 'on'); }
			else { html.removeAttribute('data-wagdl-privacy'); }
			html.setAttribute('data-wagdl-reveal',
				reveal === 'hover' || reveal === 'click' ? reveal : 'hard');
		}

		apply(!!bootstrap.on, bootstrap.reveal);
		window.wagramPrivacyApply = apply;

		document.addEventListener('click', function (ev) {
			if (html.getAttribute('data-wagdl-privacy') !== 'on') { return; }
			if (html.getAttribute('data-wagdl-reveal') !== 'click') { return; }
			var t = ev.target;
			if (!t || !t.closest) { return; }
			var hit = t.closest(bootstrap.sel);
			if (hit) { hit.classList.toggle('wagdl-revealed'); }
		}, true);

		document.addEventListener('keydown', function (ev) {
			if (!ev.ctrlKey || !ev.shiftKey) { return; }
			if (ev.key !== 'B' && ev.key !== 'b') { return; }
			ev.preventDefault();
			ev.stopPropagation();
			var next = html.getAttribute('data-wagdl-privacy') !== 'on';
			var cur = html.getAttribute('data-wagdl-reveal');
			apply(next, cur);
			if (typeof window.wagramPrivacySet === 'function') {
				window.wagramPrivacySet(next);
			}
		}, true);
	}

	if (document.documentElement) { init(); }
	else { document.addEventListener('DOMContentLoaded', init); }
})();`

// WhatsApp anchors, verified against the live web.whatsapp.com DOM. #main and
// #pane-side are stable element IDs; the data-testid values (msg-container,
// conversation-header, cell-frame-container) are WhatsApp's own long-lived
// hooks, so its hashed class names are not relied upon. Only bounded containers
// are listed: the same element that blurs must be the one that sharpens.
const whatsappPrivacySelectors = `#main [data-testid="msg-container"], #main [data-testid="conversation-header"], #pane-side [data-testid="cell-frame-container"]`

// Telegram /a/ anchors, verified against the live web.telegram.org/a/ DOM.
// #MiddleColumn is a stable element ID and .chat-list is the left column's
// stable container; the class names (Message, MiddleHeader, ListItem Chat) are
// Telegram's own, not generated hashes. Only bounded containers are listed.
const telegramPrivacySelectors = `#MiddleColumn .Message, #MiddleColumn .MiddleHeader, .chat-list .ListItem.Chat`

// privacySelectors is the blended list for both services. Both sets ship in
// every document; the set that does not match the loaded page does nothing.
func privacySelectors() string {
	return whatsappPrivacySelectors + ", " + telegramPrivacySelectors
}

// privacyBootstrap renders the initial state baked into the script. The output
// is JSON, which doubles as a JS object literal. sel is the selector list the
// click-reveal handler resolves against.
func privacyBootstrap(p prefs) string {
	b, err := json.Marshal(struct {
		On     bool   `json:"on"`
		Reveal string `json:"reveal"`
		Sel    string `json:"sel"`
	}{privacyEnabled(p), privacyReveal(p), privacySelectors()})
	if err != nil {
		return `{"on":false,"reveal":"hard","sel":""}`
	}
	return string(b)
}

// privacyCSS renders the gated blur rules shared by both services.
func privacyCSS() string {
	sel := privacySelectors()
	return `html[data-wagdl-privacy="on"] :is(` + sel + `) { filter: blur(6px); transition: filter .12s ease; }
html[data-wagdl-privacy="on"][data-wagdl-reveal="hover"] :is(` + sel + `):hover { filter: none; }
html[data-wagdl-privacy="on"][data-wagdl-reveal="click"] :is(` + sel + `).wagdl-revealed { filter: none !important; }`
}

// privacyScript assembles the template, the bootstrap JSON, and the CSS into the
// final JS injected at w.Init time. The CSS is JSON-encoded so it is a valid JS
// string literal regardless of quotes or newlines.
func privacyScript(p prefs) string {
	css, err := json.Marshal(privacyCSS())
	if err != nil {
		css = []byte(`""`)
	}
	return strings.NewReplacer(
		"__WAGDL_BOOTSTRAP__", privacyBootstrap(p),
		"__WAGDL_CSS__", string(css),
	).Replace(privacyBlurScriptTemplate)
}
