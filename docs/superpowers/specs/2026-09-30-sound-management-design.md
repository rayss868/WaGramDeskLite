# Sound Management — Per-Account Volume

**Status:** design approved (2026-09-30)
**Branch:** `feat/sound-management` (cut from `feat/privacy-mode`)

## Problem

Every running account shares whatever volume the system gives its web view. There
is no way to make account A louder than account B — a WhatsApp account that
fires a dozen notifications an hour and a Telegram account used for occasional
messages sound exactly the same, and the only lever is the machine's master
volume (or the Windows volume mixer, per application, which is all-or-nothing
once every account collapses under the same label).

The goal is a **per-account volume**, so two WhatsApp accounts and a Telegram
account can each carry their own level.

## What "volume" controls

The volume covers **all audio from the account's web view** — notification
sounds, voice notes, calls, and inline media. This is the only interpretation
the platform can deliver reliably: WhatsApp Web and Telegram Web play their
notification sounds through the same audio session as everything else, so
"notification sounds only" would require intercepting page audio, which is
fragile and page-specific.

The effect is the one the user could already produce by hand in the Windows
volume mixer, except it is scoped per account and set from the app.

## Why this is possible at all

Each account is already its own **operating-system process**: `spawnAccount`
launches `WaGramDeskLite.exe --profile <id>` (`instance_windows.go:56`), so every
account owns its own WebView2 instance and therefore its own WASAPI audio
session. That is what makes a per-account volume natural — each process sets the
volume of its own session, with no cross-process coordination.

`internal/audio` already walks this process's audio sessions via WASAPI
(`go-wca`) to rename them in the volume mixer (`audio_windows.go`). Extending
that loop to also set each session's volume is a small step on an existing
foundation.

## Architecture

**Chosen approach:** WASAPI per-session master volume, with the level stored
per account.

Each account process reads its own stored level and applies it to its own
WebView2 audio sessions with `ISimpleAudioVolume::SetMasterVolume`. Because the
process only touches its own sessions, two accounts never need to talk to each
other.

Rejected alternatives:

- **Cross-process enumeration from one window.** A single window enumerates
  every account's sessions and sets them. Strictly more complex: it needs to map
  a session's PID back to an account, and the level still has to be persisted per
  account and re-applied by that account's own process on start — which is the
  chosen approach anyway. No gain.
- **Inject JS to set `<audio>` element volume.** Fragile and page-specific;
  misses Web Audio and calls. Rejected.

## Data model

The per-account level lives in `accounts.json`, alongside the account's other
attributes (`id`, `name`, `autostart`, `service`). The `account` struct gains two
fields:

```go
type account struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Autostart bool    `json:"autostart"`
	Service   Service `json:"service,omitempty"`
	// Volume is this account's playback level, 0-100. Nil means 100, so
	// accounts written before this setting existed play at full volume.
	Volume *int `json:"volume,omitempty"`
	// Muted silences the account without losing its level. Effective volume
	// is 0 while muted, and the stored Volume is restored on unmute.
	Muted bool `json:"muted,omitempty"`
}
```

Writes go through the existing `mutateAccounts`, which already serialises
read-modify-write across processes with a named mutex (`withAccountsLock`,
`accountssync_windows.go:51`) — this matters because each account is a separate
process writing the same file.

Helpers (pure, unit-testable), mirroring the existing `notificationsEnabled` /
`setNotificationsEnabled` shape:

- `accountVolume(a account) int` — `nil` → 100, otherwise clamped to 0–100.
- `setAccountVolume(p *account, v int)` — stores the clamped value.
- `accountMuted(a account) bool` and `setAccountMuted(p *account, on bool)`.
- `effectiveVolume(a account) int` — `0` when muted, else `accountVolume(a)`.

## The audio package

`internal/audio` gains a volume target and applies it inside the loop that
already enumerates this process's sessions. All COM stays on the single
goroutine the package already locks to its thread.

New/changed surface:

```go
// StartLabeler renames and volume-sets this process's WebView2 audio sessions.
// label is the name shown in the volume mixer; volumePercent is 0-100.
func StartLabeler(label string, volumePercent int)

// SetVolume updates the target level and applies it to the current sessions
// without waiting for the next scan.
func SetVolume(percent int)
```

Inside the existing scan loop (`audio_windows.go:108-114`), for every session
whose PID belongs to this process, in addition to `SetDisplayName`, the loop
queries `ISimpleAudioVolume` (IID exposed by the vendored `go-wca`) and calls
`SetMasterVolume(level, nil)` with `level = percent / 100`.

Because the loop keeps running (fast scan every 2s, slow scan every 15s), a
session created later — Chromium creates the audio session on first playback —
is picked up and set on a subsequent tick. `SetVolume` stores the new target in
an atomic and pokes a channel the loop selects on, so a change from the panel is
applied immediately rather than at the next tick.

The session label also stops being hard-coded `"WhatsApp"`: the caller passes
`serviceLabel(service) + " — " + name`, so Telegram sessions no longer show up
as "WhatsApp" in the volume mixer.

## Panel UI

A `Volume` row is added to the **main panel** (`renderList`), directly after the
`⚙ Settings` row and before Rename/Remove:

```
┌─ ACCOUNTS ─────────┐
│ ✓ Personal      WA │
│   Work           TG │
│ ────────────────── │
│ + Add WhatsApp     │
│ + Add Telegram     │
│ ⚙ Settings        │
│ 🔊 Volume: 80% − + │
│ ✎ Rename this acct │
│ ✕ Remove this acct │
└────────────────────┘
```

- The label shows `Volume: N%`, or `Volume: Muted` when muted.
- `−` and `+` step by 10, clamped to 0–100. They are small hit targets
  (`.step` spans) appended to the right of the row.
- Clicking the speaker icon toggles mute; the icon is `🔊` normally and `🔇`
  when muted. The stored level survives a mute/unmute cycle.
- The `−`/`+` spans and the icon are the only click targets. Clicking the row's
  label text does nothing.

The row edits **the account this window belongs to** (`state.current`). Helpers
read it from the existing `accountsView` snapshot. `currentAccount()` mirrors the
inline lookup the panel already uses for the account name (overlay_windows.go:155):

```js
function currentAccount() {
  if (!state || !state.accounts) { return null; }
  for (var i = 0; i < state.accounts.length; i++) {
    if (state.accounts[i].id === state.current) { return state.accounts[i]; }
  }
  return null;
}
function volOf() {
  var a = currentAccount();
  return a && typeof a.volume === 'number' ? a.volume : 100;
}
function mutedOf() {
  var a = currentAccount();
  return !!(a && a.muted);
}
```

A small `.step` CSS rule is added next to `.viewrow` for the `−`/`+` targets.

## Bindings

Two bindings follow the existing load-prefs → mutate → save shape
(`bindings_windows.go`), except they mutate `accounts.json`:

```go
wagramVolumeSet(percent int)  // set this account's Volume, then audio.SetVolume(effective)
wagramVolumeMute(on bool)     // set this account's Muted, then audio.SetVolume(effective)
```

Both operate on `gProfileID` (the account this process serves) and call
`audio.SetVolume` with the resulting effective level so the change is audible
at once.

## Error handling and edge cases

- **Session not yet created.** Chromium creates the audio session on first
  playback, so the very first sound after launch can play at the default level
  before the loop sets it. This is the same latency the existing labeler already
  has, and it self-corrects within one scan.
- **Audio device change.** The mixer handles are rebuilt when the default
  endpoint disappears (`releaseMixer` / `openMixer`); the level is re-applied to
  the new sessions on the next tick.
- **Volume 0 vs. muted.** Both are silent; the row renders `Muted` only from the
  `Muted` flag, so a level of 0 with mute off reads `Volume: 0%`.
- **Default account.** A `nil` volume (accounts written before this feature)
  reads as 100 and never changes the stored file until the user edits it.
- **Startup application.** The app reads its account's effective volume at
  startup and passes it to `StartLabeler`, so the level is in place before the
  first scan completes.

## Testing

- **Unit (pure helpers, `internal/app`):** `accountVolume` maps `nil`→100 and
  clamps `<0` / `>100`; `setAccountVolume` stores the clamped value;
  `effectiveVolume` returns 0 when muted and the clamped level otherwise. Table
  driven, alongside the existing `prefs_windows_test.go` tests.
- **Build/static:** `go build ./...`, `go vet ./...`, and `./scripts/check-js.sh`
  (which validates the injected overlay script).
- **Live check (CDP, as used for privacy mode):** open the panel, confirm the
  Volume row renders and `−`/`+` change the label and persist to `accounts.json`.
- **Live check (WASAPI read-back):** after setting a level, read the session's
  master volume back via `ISimpleAudioVolume::GetMasterVolume` and confirm it
  matches — evidence that the level reached the audio session, not just the
  config file.

## Out of scope

- Per-account custom notification sounds (only the level is managed).
- A global "sound" overview screen listing every account at once.
- Muting only notification sounds while leaving media at full volume.
