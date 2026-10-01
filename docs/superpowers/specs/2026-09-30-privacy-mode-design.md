# Privacy Mode (blur) — Design Spec

Date: 2026-09-30
Branch: `feat/privacy-mode` (cut from `upstream/main` at v1.2.1)
Status: approved for implementation planning

## Problem

WaGram Desk Lite wraps WhatsApp Web and Telegram Web in a WebView2 window. In a
shared or public space — a café, an open-plan office, a screen share — message
text, contact names, and media are visible to anyone looking at the screen. The
existing third-party solution is a browser extension, which cannot be installed
inside a WebView2 wrapper.

The goal is a **built-in** privacy mode, shipped with the app, that blurs
sensitive content inside the page and reveals it on demand, with no extension
required. It must work for both services the app already supports.

## Decisions (from brainstorming)

- **Built-in, not an extension.** The app already injects arbitrary JS/CSS into
  the page via `w.Init`, so blur is a first-class feature.
- **Scope = all sensitive content.** Message text, contact names, avatars, chat
  list previews, and media. Not just one category.
- **Reveal = advanced setting with three modes.** The user asked for all three,
  because different situations call for different behaviour:
  - `hard` — nothing reveals while privacy is on; only the master toggle does.
  - `hover` — an element sharpens while the mouse is over it.
  - `click` — clicking an element toggles it sharp until clicked again.
- **Toggle = panel row + keyboard shortcut.** A row in the existing overlay
  panel, plus a shortcut used as a panic toggle.
- **Approach = CSS gated on a root attribute.** Chosen over JS text-tagging and
  over a mask overlay. See "Chosen approach".

## Background: how the app already works

- Go + WebView2 (`github.com/jchv/go-webview2`), module `wagramdesklite`.
- `Run()` in `internal/app/app_windows.go` injects scripts with `w.Init(...)`
  (which maps to `AddScriptToExecuteOnDocumentCreated`, so a script runs on
  every new document, including hard navigations) and then navigates:
  - `app_windows.go:219` `w.Init(initScript)` — User-Agent spoof + notification
    polyfill.
  - `app_windows.go:220` `w.Init(accountOverlayScript)` — the in-page account
    switcher, living in a shadow root on `documentElement`.
  - `app_windows.go:221` `w.Navigate(serviceURL(ensureAccount(profileID).Service))`.
- `internal/app/bindings_windows.go` `registerBindings(w, hwnd)` exposes host
  functions as `window.<name>()` returning a Promise. The existing settings
  bindings (`wagramNotificationsSet`, `wagramLiteSet`) each do
  load-prefs → mutate → save-prefs.
- `internal/app/prefs_windows.go` holds the `prefs` struct, persisted to
  `prefs.json`. Boolean settings use `*bool` where `nil` means the default, so
  files written before a setting existed stay valid. **`prefsPath()` is
  `filepath.Join(getConfigDir(), "prefs.json")`, and `getConfigDir()` is shared
  across accounts — so `prefs.json` is one global file, not per account.**
  (`profileDir(id)` nests only `UserData` and `window.json`; prefs is not nested.)
- `internal/app/overlay_windows.go` renders the settings rows as `.viewrow`
  elements (glyph span + label span), with JS helpers `prefsOf()`, `notifOf()`,
  `liteOf()` at lines 70-92. The View/Notifications/Lite toggles at lines
  267-317 are the template the new rows follow. The overlay host is a direct
  child of `documentElement` with `z-index: 2147483000`, so it sits outside any
  page subtree the blur rules touch.
- `internal/app/service_windows.go` defines `Service` (`ServiceWhatsApp`,
  `ServiceTelegram`), `serviceURL`, `serviceLabel`, `serviceBadge`. The `account`
  struct carries `Service`.
- `internal/app/app_windows.go` launches WebView2 with
  `--disable-gpu --disable-gpu-compositing`, so CSS `filter` is software-rendered.
- `scripts/check-js.sh` extracts every backtick JS literal from the Go sources
  and runs `node --check` on it; injected JS must pass.
- There are currently **no tests** in the repository.

## Chosen approach: CSS gate on `<html>`

A single injected script adds one `<style>` to the page document containing all
blur rules, every rule gated under an attribute on `<html>`:
`html[data-wagdl-privacy="on"]`. Turning privacy on sets that attribute;
turning it off removes it. The reveal mode is a second attribute,
`data-wagdl-reveal="hard"|"hover"|"click"`.

Why this is the most robust option:

- The browser re-evaluates CSS selectors against new DOM nodes automatically.
  React re-renders need no observer, no re-tagging, no polling.
- Selectors live in one readable block, as data. When WhatsApp or Telegram
  changes its DOM and a selector goes stale, the fix is one line.
- The initial state is baked into the injected script at `w.Init()` time, so
  there is no flash of unblurred content on startup when privacy is on.
- With no observer running while privacy is off, the idle CPU cost is zero.
- A selector that matches nothing blurs nothing and crashes nothing — the
  failure mode is degradation, not breakage.

Rejected alternatives: JS tagging with a `MutationObserver` (more code, CPU per
mutation, risk of missed nodes, and the marking is what breaks first on
re-render); and a full-area mask (not a real blur, and cannot work per element).

## Architecture

### New file: `internal/app/privacy_windows.go`

Holds the whole privacy concern.

- `privacyBlurScriptTemplate` — a JS const with two placeholder tokens rather
  than `fmt` verbs, because the CSS block is data and may contain `%`
  characters. Substitution is done by `strings.ReplaceAll` on
  `__WAGDL_BOOTSTRAP__` and `__WAGDL_CSS__`.
- `privacyBootstrap(p prefs) string` — returns a JSON literal
  `{"on":bool,"reveal":"hard"|"hover"|"click","sel":string}` baked into the
  script at startup; `sel` is the selector list the click-reveal handler uses.
- `privacyCSS() string` — returns the CSS rule block, containing the selectors
  for **both** services. Both sets ship in every document; the set that does not
  match the loaded page simply does nothing. This avoids threading the account's
  service into the script and keeps the template a pure constant.
- `privacyScript(p prefs) string` — assembles template + bootstrap + CSS into
  the final JS to inject.

Script behaviour (runs at document-created on every document):

1. Create `<style id="wagdl-privacy">` and append it to `document.documentElement`
   (set via `appendChild`; no `<head>` dependency), filling it with the baked CSS.
2. Read the baked bootstrap JSON and set `data-wagdl-privacy` plus
   `data-wagdl-reveal` on `document.documentElement`.
3. Expose `window.wagramPrivacyApply(on, reveal)` so the overlay and the shortcut
   can apply a change immediately, before the Go binding persists it.
4. Install one capturing `keydown` listener that implements the shortcut.

CSS shape (illustrative; the `<sel>` list is filled per service):

```css
html[data-wagdl-privacy="on"] :is(<sel>) {
  filter: blur(6px);
  transition: filter .12s ease;
}
html[data-wagdl-privacy="on"][data-wagdl-reveal="hover"] :is(<sel>):hover {
  filter: none;
}
html[data-wagdl-privacy="on"][data-wagdl-reveal="click"] :is(<sel>).wagdl-revealed {
  filter: none !important;
}
```

Blur radius is a constant in the block, easy to tune. Leaf-level selectors keep
the blurred area small (see Performance).

### `internal/app/app_windows.go`

After `w.Init(accountOverlayScript)` (line 220) add one injection:

```go
w.Init(privacyScript(loadPrefs()))
```

`privacyScript` assembles the template, the bootstrap JSON, and the CSS, so the
call site stays one line. It sits after the overlay script and before the
navigate on line 221.

### `internal/app/prefs_windows.go`

Add to the `prefs` struct:

- `Privacy *bool` with `json:"privacy,omitempty"` — **nil means OFF** (the
  inverse of `Notifications`/`Lite`). A first-run install that blurs every
  message would look broken and alarming to a new user, so privacy is opt-in.
- `PrivacyReveal string` with `json:"privacyReveal,omitempty"` — `""` or
  `"hard"` means hard blur (the default); `"hover"` and `"click"` are the other
  two modes. Any unknown value normalizes to `"hard"`.

New helpers, following the existing naming:

- `privacyEnabled(p prefs) bool` — `p.Privacy != nil && *p.Privacy`.
- `setPrivacyEnabled(p *prefs, on bool)`.
- `privacyReveal(p prefs) string` — returns one of the three modes, normalizing.
- `setPrivacyReveal(p *prefs, mode string)`.

`loadPrefs` normalizes `PrivacyReveal` the same way it already normalizes
`ViewMode`.

### `internal/app/bindings_windows.go`

Two bindings in `registerBindings`, each load-prefs → mutate → save-prefs, like
`wagramLiteSet`:

- `wagramPrivacySet(on bool)`.
- `wagramPrivacyRevealSet(mode string)`.

`accountsView.Prefs` already carries the whole `prefs` struct, so the overlay
receives the new fields with no change to the view shape.

### `internal/app/overlay_windows.go`

New rows appended after the Lite row (line 317), following the existing glyph +
label + click-handler pattern. Both rows always render, each reflecting the
current persisted value — the Reveal row is not hidden when privacy is off,
matching how View/Notifications/Lite behave.

- Privacy: label `Privacy: On (blur messages)` / `Privacy: Off (blur messages)`.
- Reveal: label `Reveal: Hard (no peek)` / `Reveal: Hover (peek on hover)` /
  `Reveal: Click (click to peek)`. Clicking cycles through the three modes.

Two helpers mirror `notifOf`/`liteOf` (lines 78-92), reading both camelCase and
PascalCase for compatibility:

- `privOf()` — reads `state.prefs.privacy` / `Privacy`, default false.
- `revealOf()` — reads `state.prefs.privacyReveal` / `PrivacyReveal`, default
  `"hard"`.

Click handlers: toggle the local value, call `window.wagramPrivacyApply(...)` for
the instant effect, call the Go binding to persist, update `state.prefs`
optimistically, re-render.

## Reveal modes

- **Master toggle** (panel row and shortcut) turns privacy on or off. Off means
  everything is sharp, regardless of reveal mode.
- **While privacy is on**, the reveal mode governs per-element peeking:
  - `hard` — nothing peeks; only the master toggle reveals. The safe default for
    screen sharing.
  - `hover` — hovering an element sharpens it; it blurs again when the mouse
    leaves. CSS-only.
  - `click` — clicking an element toggles the `wagdl-revealed` class. Needs one
    delegated capturing click listener in the injected script. The overlay panel
    and the app's own controls are outside the blurred selectors, so their clicks
    are unaffected.

## Shortcut

One capturing `keydown` listener on the document. Default `Ctrl+Shift+B`:

```js
if (ev.ctrlKey && ev.shiftKey && (ev.key === 'B' || ev.key === 'b')) {
  ev.preventDefault(); ev.stopPropagation();
  // flip privacy, persist via window.wagramPrivacySet
}
```

It fires only while the webview is focused, which is the intended use as a panic
toggle during a presentation. `preventDefault`/`stopPropagation` keep it from
reaching WhatsApp/Telegram shortcuts.

## Selectors per service

### WhatsApp (verified against the live DOM)

Blur is applied to **bounded containers**, not to every text node inside them:
the same element that blurs must be the one that sharpens, so hover and click
reveal stay consistent (a blurred child inside a sharpened parent, or vice
versa, reads as a glitch). Gated under `html[data-wagdl-privacy="on"]`:

- `#main [data-testid="msg-container"]` — message bubbles (text and any
  media inside them).
- `#main [data-testid="conversation-header"]` — conversation header (name +
  avatar).
- `#pane-side [data-testid="cell-frame-container"]` — chat list rows (avatar +
  name + preview).

Stable anchors: `#main` and `#pane-side` are stable element IDs; the
`data-testid` values (`msg-container`, `conversation-header`,
`cell-frame-container`) are WhatsApp's own long-lived hooks, so its hashed class
names are not relied upon. The full-screen media viewer (opened by clicking an
image) lives outside these containers and is a known gap for v1.

### Telegram `/a/` (verified against the live DOM)

Telegram has two incompatible web clients, `/k/` and `/a/`; the app navigates to
`https://web.telegram.org/a/`. The `/a/` client is a React app whose class names
are generated, so it cannot use the same anchors as WhatsApp.

The shipped selectors, read from the running page and confirmed to blur live:

- `#MiddleColumn .Message` — message bubbles.
- `#MiddleColumn .MiddleHeader` — conversation header.
- `.chat-list .ListItem.Chat` — chat list rows.

`#MiddleColumn` is a stable element ID and `.chat-list` is the left column's
stable container; the class names (`Message`, `MiddleHeader`, `ListItem Chat`)
are Telegram's own, not generated hashes. The no-match-means-no-blur rule keeps a
stale selector harmless: if none of these matches, that category is simply not
blurred on Telegram and WhatsApp is unaffected.

## Data flow

1. App start: `loadPrefs()` → `privacyBootstrap` + `privacyCSS()` → injected
   script → `<style>` and gate attributes applied before the page paints.
2. User toggles Privacy in the overlay (or hits the shortcut) →
   `window.wagramPrivacyApply(true, reveal)` sets the attribute immediately →
   `wagramPrivacySet(true)` persists to `prefs.json`.
3. User cycles Reveal → `window.wagramPrivacyApply(on, "click")` swaps the
   attribute → `wagramPrivacyRevealSet("click")` persists.
4. Restart or hard navigation: the script re-runs on the new document and
   re-applies `<style>` + gate attributes from the baked bootstrap.

## Error handling

- Bootstrap JSON malformed: script falls back to privacy off.
- Selector matches nothing: no blur, no error.
- Binding call fails (page not ready): the overlay still applies the visual
  change optimistically; persistence retries on the next toggle. The page-side
  state and the file may briefly disagree; the file wins on next load.

## Performance

`app_windows.go` launches WebView2 with `--disable-gpu
--disable-gpu-compositing`, so `filter: blur()` is composited on the CPU.
Blurring the whole conversation scroll area would repaint that area on every
scroll frame. Mitigation: blur only the **bounded containers** — a message
bubble, a chat row, the conversation header — never the message-list container
itself. Each blurred box stays message-sized, and the radius stays modest (6px).
Blur is static while privacy is on, so there is no animation cost beyond the
one-time painted result.

## Limitations

- **`prefs.json` is global, and each account is its own process.** Toggling
  privacy at runtime changes the window you are looking at; other open account
  windows pick the setting up the next time they load a document (restart or
  hard navigation). This matches how Notifications/Lite already behave. Live
  cross-window propagation would need the existing accounts-sync machinery and
  is out of scope for v1.
- Telegram selectors are best-effort and may need occasional maintenance; a
  stale selector degrades to no blur, never to breakage.

## Testing

The repository has no test infrastructure. This change adds one focused test
file, `internal/app/prefs_windows_test.go`, covering the pure functions (no
filesystem, no GUI):

- `privacyEnabled` on an empty prefs returns false (default off).
- `setPrivacyEnabled` then `privacyEnabled` round-trips for both values.
- `privacyReveal` normalizes `""`, `"hard"`, `"hover"`, `"click"`, and garbage
  to the right value.

`scripts/check-js.sh` must pass for the new injected JS (it runs `node --check`).

Webview behaviour is verified by a manual checklist, since it cannot be
automated here:

1. Fresh install: privacy off, nothing blurred.
2. Toggle Privacy on: message text, names, avatars, previews, and media blur.
3. In `hover` mode, hover a message: only that element sharpens.
4. In `click` mode, click a message: it stays sharp until clicked again.
5. In `hard` mode, move the mouse around: nothing reveals.
6. Press `Ctrl+Shift+B`: privacy toggles off (everything sharp) and on again.
7. Restart the app with privacy on: content is blurred again from first paint,
   no flash.
8. Repeat 2-6 on a Telegram account with the verified selectors.

## Files touched

- `internal/app/privacy_windows.go` — new (script, bootstrap, CSS, token helper).
- `internal/app/prefs_windows.go` — two fields, four helpers, load normalization.
- `internal/app/bindings_windows.go` — two bindings.
- `internal/app/overlay_windows.go` — new rows, two helpers, handlers.
- `internal/app/app_windows.go` — one `w.Init` call.
- `internal/app/prefs_windows_test.go` — new.
