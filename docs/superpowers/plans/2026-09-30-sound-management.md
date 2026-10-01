# Sound Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give every account its own playback volume, so two WhatsApp accounts and a Telegram account can each carry a different level, set from the account panel.

**Architecture:** Each account is already its own OS process with its own WebView2 audio session. The process reads its account's stored level from `accounts.json` and applies it to its own sessions through WASAPI (`ISimpleAudioVolume::SetMasterVolume`), inside the session-scan loop `internal/audio` already runs. No cross-process coordination.

**Tech Stack:** Go (windows build), `github.com/moutend/go-wca/pkg/wca` (WASAPI, vendored), the existing shadow-DOM overlay panel (vanilla JS embedded in a Go backtick string).

## Global Constraints

- Windows only: every touched `.go` file keeps `//go:build windows`.
- Volume range is **0–100**; a `nil` stored volume reads as **100**.
- Effective level applied to a session is `0` when the account is muted, else the stored volume.
- Level passed to WASAPI is `float32(percent) / 100`.
- The per-account level lives in `accounts.json` on the `account` struct, written only through the existing `mutateAccounts` (which holds the cross-process lock).
- The panel row goes in the **main panel** (`renderList`), directly after the `⚙ Settings` row and before Rename/Remove.
- `−`/`+` step by **10**, clamped to 0–100. `-`/`+` and the speaker icon are the only click targets on the row.
- The injected overlay script must keep passing `./scripts/check-js.sh`.
- Commit style matches the repo: imperative, sentence case, no prefix (e.g. `Add per-account volume state to the accounts`).

---

### Task 1: Per-account volume state

**Files:**
- Modify: `internal/app/accounts_windows.go` (add fields and helpers)
- Test: `internal/app/accounts_volume_windows_test.go` (create)

**Interfaces:**
- Consumes: nothing new.
- Produces:
  - `account.Volume *int` (`json:"volume,omitempty"`), `account.Muted bool` (`json:"muted,omitempty"`)
  - `accountVolume(a account) int`
  - `setAccountVolume(p *account, v int)`
  - `accountMuted(a account) bool`
  - `setAccountMuted(p *account, on bool)`
  - `effectiveVolume(a account) int`

- [ ] **Step 1: Write the failing test**

Create `internal/app/accounts_volume_windows_test.go`:

```go
//go:build windows

package app

import "testing"

func TestAccountVolumeDefaultsTo100(t *testing.T) {
	if got := accountVolume(account{ID: "p1"}); got != 100 {
		t.Fatalf("nil volume: want 100, got %d", got)
	}
}

func TestAccountVolumeClampsOutOfRange(t *testing.T) {
	neg := -20
	big := 250
	if got := accountVolume(account{Volume: &neg}); got != 0 {
		t.Fatalf("negative: want 0, got %d", got)
	}
	if got := accountVolume(account{Volume: &big}); got != 100 {
		t.Fatalf("above 100: want 100, got %d", got)
	}
}

func TestSetAccountVolumeStoresClamped(t *testing.T) {
	var a account
	setAccountVolume(&a, 150)
	if a.Volume == nil || *a.Volume != 100 {
		t.Fatalf("want stored 100, got %v", a.Volume)
	}
}

func TestEffectiveVolumeIsZeroWhenMuted(t *testing.T) {
	v := 80
	a := account{Volume: &v}
	setAccountMuted(&a, true)
	if got := effectiveVolume(a); got != 0 {
		t.Fatalf("muted: want 0, got %d", got)
	}
	setAccountMuted(&a, false)
	if got := effectiveVolume(a); got != 80 {
		t.Fatalf("unmuted: want 80, got %d", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/app/ -run 'Volume|Effective' -v`
Expected: FAIL to build — `undefined: accountVolume`, `setAccountVolume`, `setAccountMuted`, `effectiveVolume`.

- [ ] **Step 3: Write the implementation**

In `internal/app/accounts_windows.go`, add the two fields to the `account` struct so it reads:

```go
type account struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Autostart records whether this account was open when the app last ran.
	Autostart bool `json:"autostart"`
	// Service is whatsapp (default) or telegram. Empty means whatsapp
	// for accounts written before multi-service support.
	Service Service `json:"service,omitempty"`
	// Volume is this account's playback level, 0-100. Nil means 100, so
	// accounts written before this setting existed play at full volume.
	Volume *int `json:"volume,omitempty"`
	// Muted silences the account without losing its level. Effective volume
	// is 0 while muted, and the stored Volume is restored on unmute.
	Muted bool `json:"muted,omitempty"`
}
```

Then, directly below `accountFor`, add:

```go
// accountVolume returns this account's playback level, 0-100, defaulting to
// 100 for accounts written before the setting existed.
func accountVolume(a account) int {
	if a.Volume == nil {
		return 100
	}
	return clampVolume(*a.Volume)
}

func setAccountVolume(p *account, v int) {
	c := clampVolume(v)
	p.Volume = &c
}

func accountMuted(a account) bool { return a.Muted }

func setAccountMuted(p *account, on bool) { p.Muted = on }

// effectiveVolume is the level actually applied: silent while muted.
func effectiveVolume(a account) int {
	if a.Muted {
		return 0
	}
	return accountVolume(a)
}

func clampVolume(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/app/ -run 'Volume|Effective' -v`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/app/accounts_windows.go internal/app/accounts_volume_windows_test.go
git commit -m "Add per-account volume state to the accounts"
```

---

### Task 2: Apply the level and wire it through the app

**Files:**
- Modify: `internal/audio/audio_windows.go`
- Test: `internal/audio/volume_windows_test.go` (create)
- Modify: `internal/app/app_windows.go:178` (the `StartLabeler` call site)
- Modify: `internal/app/bindings_windows.go` (add two bindings and the `audio` import)

**Interfaces:**
- Consumes: `effectiveVolume(account) int`, `setAccountVolume`, `setAccountMuted` (Task 1); `serviceLabel(Service) string`, `gProfileID`, `mutateAccounts`, `loadAccounts`.
- Produces:
  - `func StartLabeler(label string, volumePercent int)` — signature change; the old call `audio.StartLabeler()` no longer compiles.
  - `func SetVolume(percent int)`
  - `var volPercent atomic.Int32`, `func clampPercent(p int) int`
  - page globals `window.wagramVolumeSet(percent)` and `window.wagramVolumeMute(on)`

- [ ] **Step 1: Write the failing test**

Create `internal/audio/volume_windows_test.go`:

```go
//go:build windows

package audio

import "testing"

func TestClampPercent(t *testing.T) {
	cases := []struct{ in, want int }{
		{-5, 0}, {0, 0}, {50, 50}, {100, 100}, {150, 100},
	}
	for _, c := range cases {
		if got := clampPercent(c.in); got != c.want {
			t.Errorf("clampPercent(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestSetVolumeClampsStoredTarget(t *testing.T) {
	SetVolume(150)
	if got := volPercent.Load(); got != 100 {
		t.Fatalf("want stored 100, got %d", got)
	}
	SetVolume(-5)
	if got := volPercent.Load(); got != 0 {
		t.Fatalf("want stored 0, got %d", got)
	}
	SetVolume(80)
	if got := volPercent.Load(); got != 80 {
		t.Fatalf("want stored 80, got %d", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/audio/ -run 'Clamp|SetVolume' -v`
Expected: FAIL to build — `undefined: clampPercent`, `undefined: volPercent`, `undefined: SetVolume`.

- [ ] **Step 3: Write the implementation**

In `internal/audio/audio_windows.go`, add `"sync/atomic"` to the import block (keep the rest), then add the package state just below the `const` block:

```go
var (
	// volPercent is the level applied to this process's sessions, 0-100.
	volPercent atomic.Int32
	// volWake nudges the labeler loop to re-apply the level at once.
	volWake = make(chan struct{}, 1)
)

// SetVolume stores a new target level and wakes the labeler so it is applied
// to the current sessions without waiting for the next scan.
func SetVolume(percent int) {
	volPercent.Store(int32(clampPercent(percent)))
	select {
	case volWake <- struct{}{}:
	default:
	}
}

func clampPercent(p int) int {
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}

// wait sleeps for d, but returns early if SetVolume pokes volWake.
func wait(d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-volWake:
	}
}

// applyVolume sets the session's master volume. Errors are ignored: a session
// that does not expose ISimpleAudioVolume simply keeps its default level.
func applyVolume(control *wca.IAudioSessionControl2, level float32) {
	var sav *wca.ISimpleAudioVolume
	if err := control.PutQueryInterface(wca.IID_ISimpleAudioVolume, &sav); err != nil || sav == nil {
		return
	}
	defer sav.Release()
	_ = sav.SetMasterVolume(level, nil)
}
```

Change `StartLabeler` to take the label and starting level, apply them, and wake on `volWake`. Replace the whole function with:

```go
// StartLabeler renames and volumes the audio sessions belonging to this
// process's WebView2 renderers, so the app's audio appears under label in the
// Windows volume mixer and plays at the configured level.
//
// The scan is deliberately lopsided. It enumerates audio sessions on every
// tick, but only walks the system process table when a session names a process
// it has not classified yet. That walk is a snapshot of every process on the
// machine, and running it every two seconds for the life of the app was the
// expensive half of the loop this replaced.
func StartLabeler(label string, volumePercent int) {
	volPercent.Store(int32(clampPercent(volumePercent)))
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		if err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED); err != nil {
			return
		}
		defer ole.CoUninitialize()

		var (
			enumerator *wca.IMMDeviceEnumerator
			device     *wca.IMMDevice
			manager    *wca.IAudioSessionManager2
		)
		releaseMixer := func() {
			if manager != nil {
				manager.Release()
			}
			if device != nil {
				device.Release()
			}
			if enumerator != nil {
				enumerator.Release()
			}
			manager, device, enumerator = nil, nil, nil
		}
		defer releaseMixer()

		// ours is the set of WebView2 PIDs under this process. seen is the
		// session PIDs from the previous scan, which is how a genuinely new
		// session is told apart from one that has been there all along.
		var ours map[uint32]bool
		seen := map[uint32]bool{}
		quiet := 0

		for {
			if manager == nil {
				enumerator, device, manager = openMixer()
				if manager == nil {
					wait(slowScan)
					continue
				}
			}

			sessions, err := collectSessions(manager)
			if err != nil {
				// The endpoint went away: device switch or driver restart.
				releaseMixer()
				wait(fastScan)
				continue
			}

			current := make(map[uint32]bool, len(sessions))
			fresh := false
			for _, s := range sessions {
				current[s.pid] = true
				if !seen[s.pid] {
					fresh = true
				}
			}
			seen = current

			if fresh {
				ours = waGramDeskLiteWebViewProcesses(uint32(syscall.Getpid()))
			}
			for _, s := range sessions {
				if ours[s.pid] {
					name := label
					_ = s.control.SetDisplayName(&name, nil)
					applyVolume(s.control, float32(volPercent.Load())/100)
				}
				s.control.Release()
			}

			if fresh {
				quiet = 0
				wait(fastScan)
				continue
			}
			quiet++
			if quiet >= quietScansBeforeSlow {
				wait(slowScan)
			} else {
				wait(fastScan)
			}
		}
	}()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/audio/ -run 'Clamp|SetVolume' -v`
Expected: PASS (2 tests). The `app` package does not compile until Step 5 updates the caller; run only the audio package tests for this step (`go test ./internal/audio/`), which builds just that package.

- [ ] **Step 5: Update the startup call site**

In `internal/app/app_windows.go`, change the line

```go
	audio.StartLabeler()
```

to

```go
	audio.StartLabeler(serviceLabel(acct.Service)+" - "+acct.Name, effectiveVolume(acct))
```

(`acct` is the local from `Run()` at line 108; it is in scope here.)

- [ ] **Step 6: Add the bindings**

In `internal/app/bindings_windows.go`, add the audio import so the import block reads:

```go
import (
	"strings"

	"github.com/jchv/go-webview2"

	"wagramdesklite/internal/audio"
)
```

Then, immediately after the `wagramPrivacyRevealSet` binding, add:

```go
	_ = w.Bind("wagramVolumeSet", func(percent int) {
		var eff int
		mutateAccounts(func(accounts []account) []account {
			for i := range accounts {
				if accounts[i].ID == gProfileID {
					setAccountVolume(&accounts[i], percent)
					eff = effectiveVolume(accounts[i])
				}
			}
			return accounts
		})
		audio.SetVolume(eff)
	})
	_ = w.Bind("wagramVolumeMute", func(on bool) {
		var eff int
		mutateAccounts(func(accounts []account) []account {
			for i := range accounts {
				if accounts[i].ID == gProfileID {
					setAccountMuted(&accounts[i], on)
					eff = effectiveVolume(accounts[i])
				}
			}
			return accounts
		})
		audio.SetVolume(eff)
	})
```

- [ ] **Step 7: Build and vet**

Run: `go build ./... && go vet ./...`
Expected: exit 0, no output.

- [ ] **Step 8: Verify the bindings land on the page**

With the debug port enabled (temporary `--remote-debugging-port=9222` appended to the WebView2 flags), run the app and evaluate in the page:

```js
typeof window.wagramVolumeSet + "," + typeof window.wagramVolumeMute
```

Expected: `"function,function"`. Then call `window.wagramVolumeSet(40)` and confirm `accounts.json` shows `"volume": 40` for the current account (`%APPDATA%\WaGramDeskLite\accounts.json`).

- [ ] **Step 9: Commit**

```bash
git add internal/audio/audio_windows.go internal/audio/volume_windows_test.go internal/app/app_windows.go internal/app/bindings_windows.go
git commit -m "Apply the per-account volume to the audio sessions"
```

---

### Task 3: The Volume row in the panel

**Files:**
- Modify: `internal/app/overlay_windows.go` (helpers, one CSS rule, one row in `renderList`)

**Interfaces:**
- Consumes: `window.wagramVolumeSet`, `window.wagramVolumeMute` (Task 2); the `accounts` snapshot entries now carry `volume` and `muted`.
- Produces: nothing consumed by later tasks.

- [ ] **Step 1: Add the CSS**

In the style array (next to the `.viewrow:hover` entry), add:

```go
		'.step { padding: 0 6px; border-radius: 4px; background: #2a3942; color: #d1d7db;',
		'  cursor: pointer; font-weight: 700; }',
		'.step:hover { background: #3b4a54; }',
		'.volsteps { margin-left: auto; display: flex; gap: 4px; }',
```

- [ ] **Step 2: Add the helpers**

Directly after `function revealOf() { ... }`, add:

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

	function stepVolume(delta) {
		var a = currentAccount();
		if (!a) { return; }
		var next = (typeof a.volume === 'number' ? a.volume : 100) + delta;
		if (next < 0) { next = 0; }
		if (next > 100) { next = 100; }
		// Adjusting the level while muted also unmutes, so the change is audible.
		if (typeof window.wagramVolumeMute === 'function' && a.muted) { window.wagramVolumeMute(false); }
		if (typeof window.wagramVolumeSet === 'function') { window.wagramVolumeSet(next); }
		a.volume = next;
		a.muted = false;
		render();
	}
```

- [ ] **Step 3: Add the row**

In `renderList`, after `panel.appendChild(set);` (the Settings row) and before `var ren = el('div', 'act');`, insert:

```js
		var volRow = el('div', 'viewrow');
		var volIcon = el('span', 'gl', mutedOf() ? '🔇' : '🔊');
		volIcon.addEventListener('click', function (ev) {
			ev.stopPropagation();
			var on = !mutedOf();
			if (typeof window.wagramVolumeMute === 'function') { window.wagramVolumeMute(on); }
			var a = currentAccount();
			if (a) { a.muted = on; }
			render();
		});
		volRow.appendChild(volIcon);
		volRow.appendChild(el('span', 'nm', mutedOf() ? 'Volume: Muted' : 'Volume: ' + volOf() + '%'));
		var steps = el('div', 'volsteps');
		var minus = el('span', 'step', '−');
		minus.addEventListener('click', function (ev) { ev.stopPropagation(); stepVolume(-10); });
		var plus = el('span', 'step', '+');
		plus.addEventListener('click', function (ev) { ev.stopPropagation(); stepVolume(10); });
		steps.appendChild(minus);
		steps.appendChild(plus);
		volRow.appendChild(steps);
		panel.appendChild(volRow);
```

- [ ] **Step 4: Validate the injected script**

Run: `bash ./scripts/check-js.sh`
Expected: exit 0 (the overlay script parses).

Then build and vet:

Run: `go build ./... && go vet ./...`
Expected: exit 0.

- [ ] **Step 5: Live check via CDP**

With the debug port on, open the panel and confirm:
- the main panel shows a `Volume: 100% − +` row between `Settings` and `Rename this account`;
- clicking `−` changes the label to `Volume: 90%` and `accounts.json` records `"volume": 90`;
- clicking the 🔊 icon switches the label to `Volume: Muted`;
- reading the session back via `ISimpleAudioVolume::GetMasterVolume` matches (bubble/header/row check as in the privacy live check).

- [ ] **Step 6: Commit**

```bash
git add internal/app/overlay_windows.go
git commit -m "Add the per-account Volume row to the panel"
```

---

## Self-Review

**Spec coverage:**
- "all audio from the account's web view" → Task 2 applies `SetMasterVolume` to the whole session. ✅
- "per-account level stored in accounts.json" → Task 1 fields + Task 2 writes via `mutateAccounts`. ✅
- "WASAPI per-session master volume" → Task 2. ✅
- `StartLabeler(label, volumePercent)` / `SetVolume` → Task 2. ✅
- session label per-account instead of hard-coded "WhatsApp" → Task 2 call site (`serviceLabel + " - " + name`). ✅
- Volume row in the main panel after `⚙ Settings`, stepper −/+, icon mute → Task 3. ✅
- helpers `volOf`/`mutedOf`/`currentAccount` → Task 3. ✅
- bindings `wagramVolumeSet`/`wagramVolumeMute` → Task 2. ✅
- edge cases (session-not-yet-created, device change, 0 vs muted, nil default, startup application) → Task 2 loop re-applies on every scan and after `releaseMixer`; Task 2 also passes the startup level. ✅
- unit tests (`accountVolume` nil→100 + clamp, `setAccountVolume`, `effectiveVolume`) → Task 1. ✅
- live checks (CDP row + WASAPI read-back) → Task 2 step 8, Task 3 step 5. ✅
- out of scope items are not implemented. ✅

**Placeholder scan:** no TBD/TODO; every code step carries the full code.

**Type consistency:** `accountVolume`/`setAccountVolume`/`accountMuted`/`setAccountMuted`/`effectiveVolume` (Task 1) are used unchanged in Task 2; `clampPercent`/`volPercent`/`SetVolume`/`wait`/`applyVolume` (Task 2) are used unchanged within Task 2; `volOf`/`mutedOf`/`currentAccount`/`stepVolume` (Task 3) match the spec. `StartLabeler(label string, volumePercent int)` is defined and called within Task 2.
