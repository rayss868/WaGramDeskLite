# Privacy Mode (blur) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a built-in privacy mode that blurs message bubbles, contact names, avatars, and chat previews inside the WhatsApp Web and Telegram Web webviews, with three selectable reveal behaviours (hard / hover / click).

**Architecture:** A small JS script injected at document-created adds one `<style>` to the page, every blur rule gated on a `data-wagdl-privacy` attribute we own on `<html>`. Toggling privacy sets that attribute; the reveal mode is a second attribute (`data-wagdl-reveal`). The browser re-evaluates the CSS against new DOM nodes automatically, so React re-renders need no observer. State lives in the global `prefs.json` and reaches the page through two Go bindings.

**Tech Stack:** Go 1.26.4, `github.com/jchv/go-webview2` (WebView2), vanilla JS injected via `w.Init` (`AddScriptToExecuteOnDocumentCreated`), `encoding/json` for prefs.

## Global Constraints

- Windows only: every Go file keeps the `//go:build windows` build tag.
- Module path is `wagramdesklite`; internal packages are imported as `wagramdesklite/internal/...`.
- Dependencies are vendored (`vendor/`); do not add new dependencies.
- `prefs.json` is one **global** file at `getConfigDir()/prefs.json` (`%APPDATA%\WaGramDeskLite\prefs.json`), shared by every account; `profileDir(id)` nests only `UserData` and `window.json`.
- Boolean prefs use `*bool` where `nil` means the default, so older `prefs.json` files stay valid.
- Privacy defaults to **OFF** (unlike `Notifications`/`Lite`, which default on).
- Reveal mode values are exactly `"hard"`, `"hover"`, `"click"`; anything else normalizes to `"hard"`.
- Blur radius is `6px`; the CSS transition is `filter .12s ease`.
- Our own namespace is `wagdl-` for attributes/classes and `#wagdl-privacy` for the style element; the page globals are `window.wagramPrivacyApply`, `window.wagramPrivacySet`, `window.wagramPrivacyRevealSet`.
- Blur is applied to bounded containers (message bubble, chat row, header), never to the message-list scroll container.
- A selector that matches nothing must blur nothing and throw nothing.
- Injected JS must pass `scripts/check-js.sh` (it runs `node --check` on every backtick literal in the Go sources).
- Commit messages follow the repo style: imperative, sentence case, no conventional-commit prefix (e.g. "Add the privacy mode design spec").

**Build/test commands** (run from the repo root):

- Compile everything: `go build ./...`
- Vet: `go vet ./...`
- Run the privacy tests: `go test ./internal/app/ -run TestPrivacy -v`
- Validate injected JS: `./scripts/check-js.sh`
- Full Windows build (only needed for manual GUI checks): `./scripts/build.sh`

---

### Task 1: Privacy fields in prefs (persistence layer)

**Files:**
- Modify: `internal/app/prefs_windows.go`
- Test: `internal/app/prefs_windows_test.go` (new)

**Interfaces:**
- Consumes: nothing (leaf layer).
- Produces:
  - `privacyEnabled(p prefs) bool`
  - `setPrivacyEnabled(p *prefs, on bool)`
  - `privacyReveal(p prefs) string` — returns `"hard"`, `"hover"`, or `"click"`
  - `setPrivacyReveal(p *prefs, mode string)`
  - `prefs.Privacy *bool`, `prefs.PrivacyReveal string`
  - constants `RevealHard`, `RevealHover`, `RevealClick`

- [ ] **Step 1: Write the failing test**

Create `internal/app/prefs_windows_test.go`:

```go
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
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/app/ -run TestPrivacy -v`
Expected: FAIL — compile error, `privacyEnabled` / `privacyReveal` / `RevealHard` / `PrivacyReveal` undefined.

- [ ] **Step 3: Add the two fields to the `prefs` struct**

In `internal/app/prefs_windows.go`, after the `Lite` field (the last field, around line 31), add:

```go
	// Privacy blurs message bubbles, contact names, avatars, and chat previews
	// inside the page. Nil means off (unlike Notifications/Lite): a fresh
	// install that blurred everything would look broken.
	Privacy *bool `json:"privacy,omitempty"`
	// PrivacyReveal picks how a blurred element can be peeked at: "hard" (the
	// default; nothing peeks), "hover" (the element sharpens under the mouse),
	// or "click" (a click toggles it sharp). Anything else normalizes to
	// "hard".
	PrivacyReveal string `json:"privacyReveal,omitempty"`
```

- [ ] **Step 4: Add the mode constants and the four helpers**

In `internal/app/prefs_windows.go`, after `setLiteEnabled` (around line 58), add:

```go
// Reveal mode values. Hard is the default and the only one safe to leave on
// while screen sharing.
const (
	RevealHard  = "hard"
	RevealHover = "hover"
	RevealClick = "click"
)

func privacyEnabled(p prefs) bool {
	return p.Privacy != nil && *p.Privacy
}

func setPrivacyEnabled(p *prefs, on bool) {
	v := on
	p.Privacy = &v
}

// privacyReveal returns one of the three modes, never an unknown value.
func privacyReveal(p prefs) string {
	switch p.PrivacyReveal {
	case RevealHover:
		return RevealHover
	case RevealClick:
		return RevealClick
	default:
		return RevealHard
	}
}

func setPrivacyReveal(p *prefs, mode string) {
	switch mode {
	case RevealHover:
		p.PrivacyReveal = RevealHover
	case RevealClick:
		p.PrivacyReveal = RevealClick
	default:
		p.PrivacyReveal = RevealHard
	}
}
```

- [ ] **Step 5: Normalize `PrivacyReveal` in `loadPrefs`**

In `internal/app/prefs_windows.go`, inside `loadPrefs`, after the existing `ViewMode` normalization, add:

```go
	if p.PrivacyReveal != "" && p.PrivacyReveal != RevealHard &&
		p.PrivacyReveal != RevealHover && p.PrivacyReveal != RevealClick {
		p.PrivacyReveal = RevealHard
	}
```

(An empty string is left as-is so `omitempty` keeps the default out of the file.)

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/app/ -run TestPrivacy -v`
Expected: PASS (3 tests).

- [ ] **Step 7: Compile and vet the package**

Run: `go build ./... && go vet ./...`
Expected: no output, exit 0.

- [ ] **Step 8: Commit**

```bash
git add internal/app/prefs_windows.go internal/app/prefs_windows_test.go
git commit -m "Add privacy preferences to the shared prefs"
```

---

### Task 2: The privacy script, bootstrap, and per-service CSS

**Files:**
- Create: `internal/app/privacy_windows.go`
- Test: `internal/app/privacy_windows_test.go` (new)

**Interfaces:**
- Consumes: `privacyEnabled(p prefs) bool`, `privacyReveal(p prefs) string`, `RevealHard` (Task 1).
- Produces:
  - `privacyScript(p prefs) string` — the full JS to inject.
  - `privacyBootstrap(p prefs) string` — a JSON object literal.
  - `privacyCSS() string` — the gated rule block (both services).
  - `privacySelectors() string` — the blurred selector list (both services).

- [ ] **Step 1: Write the failing test**

Create `internal/app/privacy_windows_test.go`:

```go
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
		`#main .message-in`,
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
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/app/ -run TestPrivacy -v`
Expected: FAIL — compile error, `privacyBootstrap` / `privacyCSS` / `privacyScript` undefined.

- [ ] **Step 3: Create the privacy module**

Create `internal/app/privacy_windows.go`:

```go
//go:build windows

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
			apply(next, bootstrap.reveal);
			if (typeof window.wagramPrivacySet === 'function') {
				window.wagramPrivacySet(next);
			}
		}, true);
	}

	if (document.documentElement) { init(); }
	else { document.addEventListener('DOMContentLoaded', init); }
})();`

// WhatsApp anchors. #main and #pane-side are stable element IDs; .message-in /
// .message-out and role="listitem" are long-lived, so WhatsApp's hashed classes
// are not relied upon. Only bounded containers are listed: the same element that
// blurs must be the one that sharpens.
const whatsappPrivacySelectors = `#main .message-in, #main .message-out, #main > header, #pane-side div[role="listitem"]`

// Telegram /a/ anchors. Best-effort: the /a/ client is React with generated
// class names and no role="listitem" equivalent. These are confirmed against the
// live DOM in Task 6; a stale selector degrades to "no blur", never an error.
const telegramPrivacySelectors = `#column-center .Message, #column-center .ChatInfo, .chatlist .chatlist-chat`

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
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/app/ -run TestPrivacy -v`
Expected: PASS (7 tests total).

- [ ] **Step 5: Compile, vet, and validate the injected JS**

Run: `go build ./... && go vet ./... && ./scripts/check-js.sh`
Expected: build/vet silent (exit 0); check-js reports no errors (the template is syntactically valid JS — the `__WAGDL_*` identifiers are legal).

- [ ] **Step 6: Commit**

```bash
git add internal/app/privacy_windows.go internal/app/privacy_windows_test.go
git commit -m "Add the privacy script, bootstrap, and blur selectors"
```

---

### Task 3: Host bindings to persist the toggles

**Files:**
- Modify: `internal/app/bindings_windows.go` (inside `registerBindings`, after the `wagramLiteSet` binding)

**Interfaces:**
- Consumes: `setPrivacyEnabled` / `setPrivacyReveal` (Task 1), `loadPrefs` / `savePrefs` (existing).
- Produces: page-callable `window.wagramPrivacySet(on)` and `window.wagramPrivacyRevealSet(mode)`, both returning a Promise.

- [ ] **Step 1: Add the two bindings**

In `internal/app/bindings_windows.go`, after the `wagramLiteSet` binding block, add:

```go
	_ = w.Bind("wagramPrivacySet", func(on bool) {
		p := loadPrefs()
		setPrivacyEnabled(&p, on)
		savePrefs(p)
	})
	_ = w.Bind("wagramPrivacyRevealSet", func(mode string) {
		p := loadPrefs()
		setPrivacyReveal(&p, mode)
		savePrefs(p)
	})
```

- [ ] **Step 2: Compile, vet, and validate the injected JS**

Run: `go build ./... && go vet ./... && ./scripts/check-js.sh`
Expected: no output / no errors, exit 0. (Bindings are registered through the WebView2 host and cannot be unit-tested; the compile check is the gate here.)

- [ ] **Step 3: Commit**

```bash
git add internal/app/bindings_windows.go
git commit -m "Add bindings to persist the privacy toggles"
```

---

### Task 4: Wire the script into the webview startup

**Files:**
- Modify: `internal/app/app_windows.go` (the injection block at lines 219-221)

**Interfaces:**
- Consumes: `privacyScript(p prefs) string` (Task 2); `loadPrefs` (existing).
- Produces: nothing new — this is where the feature becomes live.

- [ ] **Step 1: Inject the privacy script**

In `internal/app/app_windows.go`, replace lines 219-221:

```go
	w.Init(initScript)
	w.Init(accountOverlayScript)
	w.Navigate(serviceURL(ensureAccount(profileID).Service))
```

with:

```go
	w.Init(initScript)
	w.Init(accountOverlayScript)
	w.Init(privacyScript(loadPrefs()))
	w.Navigate(serviceURL(ensureAccount(profileID).Service))
```

- [ ] **Step 2: Compile, vet, and validate the injected JS**

Run: `go build ./... && go vet ./... && ./scripts/check-js.sh`
Expected: no output / no errors, exit 0.

- [ ] **Step 3: Commit**

```bash
git add internal/app/app_windows.go
git commit -m "Inject the privacy script on startup"
```

---

### Task 5: Privacy and Reveal rows in the overlay panel

**Files:**
- Modify: `internal/app/overlay_windows.go` (helpers after `liteOf`; rows after the Lite row's `panel.appendChild(lite);`)

**Interfaces:**
- Consumes: page globals `window.wagramPrivacyApply(on, reveal)` (Task 2), `window.wagramPrivacySet` / `window.wagramPrivacyRevealSet` (Task 3); the existing overlay `el`, `render`, `refresh`, and `state.prefs`.
- Produces: two new `.viewrow` controls that read and write the persisted privacy state.

- [ ] **Step 1: Add the two read helpers**

In `internal/app/overlay_windows.go`, after `liteOf` (around line 92), add:

```js
	function privOf() {
		if (state && state.prefs) {
			if (typeof state.prefs.privacy === 'boolean') { return state.prefs.privacy; }
			if (typeof state.prefs.Privacy === 'boolean') { return state.prefs.Privacy; }
		}
		return false;
	}

	function revealOf() {
		if (state && state.prefs) {
			var v = state.prefs.privacyReveal || state.prefs.PrivacyReveal;
			if (v === 'hover' || v === 'click') { return v; }
		}
		return 'hard';
	}
```

- [ ] **Step 2: Add the two settings rows**

In `internal/app/overlay_windows.go`, after the Lite row's `panel.appendChild(lite);`, add:

```js
		var privOn = privOf();
		var priv = el('div', 'viewrow');
		priv.appendChild(el('span', 'gl', privOn ? '◉' : '○'));
		priv.appendChild(el('span', 'nm', privOn ? 'Privacy: On (blur messages)' : 'Privacy: Off (blur messages)'));
		priv.addEventListener('click', function () {
			var next = !privOn;
			if (typeof window.wagramPrivacyApply === 'function') { window.wagramPrivacyApply(next, revealOf()); }
			if (typeof window.wagramPrivacySet === 'function') {
				window.wagramPrivacySet(next).then(function () { setTimeout(refresh, 80); });
			}
			if (state && state.prefs) {
				if ('privacy' in state.prefs) { state.prefs.privacy = next; }
				state.prefs.Privacy = next;
			}
			privOn = next;
			render();
		});
		panel.appendChild(priv);

		var curReveal = revealOf();
		var reveal = el('div', 'viewrow');
		reveal.appendChild(el('span', 'gl', curReveal === 'hard' ? '▤' : curReveal === 'hover' ? '▦' : '▣'));
		reveal.appendChild(el('span', 'nm',
			curReveal === 'hard' ? 'Reveal: Hard (no peek)'
			: curReveal === 'hover' ? 'Reveal: Hover (peek on hover)'
			: 'Reveal: Click (click to peek)'));
		reveal.addEventListener('click', function () {
			var now = revealOf();
			var next = now === 'hard' ? 'hover' : now === 'hover' ? 'click' : 'hard';
			if (typeof window.wagramPrivacyApply === 'function') { window.wagramPrivacyApply(privOf(), next); }
			if (typeof window.wagramPrivacyRevealSet === 'function') {
				window.wagramPrivacyRevealSet(next).then(function () { setTimeout(refresh, 80); });
			}
			if (state && state.prefs) {
				if ('privacyReveal' in state.prefs) { state.prefs.privacyReveal = next; }
				state.prefs.PrivacyReveal = next;
			}
			render();
		});
		panel.appendChild(reveal);
```

- [ ] **Step 3: Compile, vet, and validate the injected JS**

Run: `go build ./... && go vet ./... && ./scripts/check-js.sh`
Expected: no output / no errors, exit 0. (The injected JS is a Go string literal; the compile check confirms the literal still parses as Go, and check-js confirms the JS itself.)

- [ ] **Step 4: Commit**

```bash
git add internal/app/overlay_windows.go
git commit -m "Add the Privacy and Reveal rows to the overlay panel"
```

---

### Task 6: Verify the Telegram selectors against the live DOM

**Files:**
- Modify: `internal/app/privacy_windows.go` (the `telegramPrivacySelectors` constant)

**Interfaces:**
- Consumes: the built app and a logged-in Telegram account.
- Produces: a `telegramPrivacySelectors` value that actually matches the running Telegram `/a/` DOM. If a category cannot be matched, drop its selector rather than guessing — a missing category simply stays unblurred.

- [ ] **Step 1: Build and run the app for a Telegram account**

Run: `./scripts/build.sh`, then launch `dist/WaGramDeskLite.exe`, add a Telegram account, and log in.

- [ ] **Step 2: Inspect the live DOM**

Open DevTools, then in the Elements panel locate the actual element for each category and its stable selector:

- message bubble wrapper (the equivalent of WhatsApp's `.message-in` / `.message-out`)
- conversation header (contact name + avatar)
- chat-list row (name + last-message preview + avatar)

Prefer IDs, `role`/`aria` attributes, and structural paths over generated class names.

- [ ] **Step 3: Fill in the selectors**

Edit `telegramPrivacySelectors` in `internal/app/privacy_windows.go` to the confirmed selectors. Keep the list to bounded containers (bubbles, header, chat rows), not the whole scroll area.

- [ ] **Step 4: Rebuild and check visually**

Run: `./scripts/build.sh`, relaunch, toggle Privacy on the Telegram account, and confirm each category blurs and reveals per the selected mode.

- [ ] **Step 5: Run the tests and JS check (must still pass)**

Run: `go test ./internal/app/ -run TestPrivacy -v && ./scripts/check-js.sh`
Expected: PASS / no errors — the selector change only affects string contents.

- [ ] **Step 6: Commit**

```bash
git add internal/app/privacy_windows.go
git commit -m "Confirm the Telegram blur selectors against the live DOM"
```

---

## Manual verification checklist (whole feature)

Run after Task 6, on the built `dist/WaGramDeskLite.exe`:

1. Fresh profile: privacy off, nothing blurred.
2. Toggle Privacy on: message bubbles, names, avatars, and previews blur.
3. In `hover` mode, hover a message: only that element sharpens.
4. In `click` mode, click a message: it stays sharp until clicked again.
5. In `hard` mode, move the mouse around: nothing reveals.
6. Press `Ctrl+Shift+B`: privacy toggles off (everything sharp) and on again.
7. Restart the app with privacy on: content is blurred again from first paint, with no unblurred flash.
8. Repeat 2-6 on a Telegram account with the verified selectors.

## Self-review notes

- **Spec coverage:** prefs fields/helpers (Task 1), script + bootstrap + CSS selector (Task 2), bindings (Task 3), startup wiring (Task 4), overlay rows/helpers/handlers (Task 5), Telegram selector verification (Task 6), unit tests (Tasks 1-2), JS validation (Tasks 2-6), manual checklist (final section). Every spec section maps to a task.
- **Placeholders:** none — the Telegram selectors ship as concrete best-effort values in Task 2 and are corrected by the explicit live-DOM procedure in Task 6.
- **Type consistency:** `privacyEnabled` / `setPrivacyEnabled` / `privacyReveal` / `setPrivacyReveal` / `RevealHard` (Task 1) are the exact names used in Tasks 2 and 5. `privacyScript` / `privacyBootstrap` / `privacyCSS` / `privacySelectors` (Task 2) are the exact names used in Tasks 2 and 4. The page globals `wagramPrivacyApply` (Task 2) and `wagramPrivacySet` / `wagramPrivacyRevealSet` (Task 3) are the exact names called in Task 5. The gate attributes `data-wagdl-privacy` / `data-wagdl-reveal` and the class `wagdl-revealed` are identical across the script, the CSS, and the tests (Task 2).
