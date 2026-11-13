//go:build windows

package app

import (
	"os"
	"os/exec"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func windowTitleFor(a account) string {
	return windowTitle + " — " + a.Name + " [" + string(serviceBadge(a.Service.normalize())) + "]"
}

// validProfileID guards the profile name before it reaches the filesystem.
func validProfileID(id string) bool {
	if id == "" || len(id) > 32 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// profileFromArgs reports which account this process was launched for, and
// whether it was named explicitly. A plain launch uses the remembered account.
func profileFromArgs() (string, bool) {
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		var id string
		switch {
		case strings.HasPrefix(args[i], "--profile="):
			id = strings.TrimPrefix(args[i], "--profile=")
		case args[i] == "--profile" && i+1 < len(args):
			id = args[i+1]
		default:
			continue
		}
		if validProfileID(id) {
			return id, true
		}
		return defaultProfileID, false
	}
	if id := loadPrefs().ActiveID; validProfileID(id) {
		for _, a := range loadAccounts() {
			if a.ID == id {
				return id, false
			}
		}
	}
	return defaultProfileID, false
}

func spawnAccount(id string) {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(exe, "--profile", id)
	if cmd.Start() == nil && cmd.Process != nil {
		_ = cmd.Process.Release()
	}
}

// accountHWND finds the top-level window of another account's process by
// its unique title. Returns 0 when that account is not running.
func accountHWND(a account) uintptr {
	titlePtr, _ := windows.UTF16PtrFromString(windowTitleFor(a))
	hwnd, _, _ := procFindWindow.Call(0, uintptr(unsafe.Pointer(titlePtr)))
	return hwnd
}

// showOnlyAccount implements tabs mode: only the named account stays
// visible, every other running account window is hidden. The choice is
// persisted so every window renders the same active tab.
func showOnlyAccount(id string) {
	p := loadPrefs()
	p.ActiveID = id
	savePrefs(p)
	for _, a := range loadAccounts() {
		hwnd := accountHWND(a)
		if hwnd == 0 {
			continue
		}
		if a.ID == id {
			procShowWindow.Call(hwnd, swRestore)
			procSetFgWindow.Call(hwnd)
		} else {
			procShowWindow.Call(hwnd, swHide)
		}
	}
}

// showAllAccounts implements pages mode: every running account window is
// made visible. Positioning is left to the user (and Windows).
func showAllAccounts() {
	for _, a := range loadAccounts() {
		if hwnd := accountHWND(a); hwnd != 0 {
			procShowWindow.Call(hwnd, swRestore)
		}
	}
}

// selectAccount is the tab click path: in tabs mode it hides the other
// windows, in pages mode it just brings the target forward.
func selectAccount(from uintptr, a account) {
	if loadPrefs().ViewMode == ViewPages {
		focusAccount(from, a)
		return
	}
	target := accountHWND(a)
	if target != 0 {
		st, _ := captureWindowState(from)
		applyWindowState(target, st)
		showOnlyAccount(a.ID)
		return
	}
	// Not running yet: remember the choice, leave geometry for first paint.
	p := loadPrefs()
	p.ActiveID = a.ID
	savePrefs(p)
	if st, _ := captureWindowState(from); st.Saved {
		saveWindowStateTo(windowStatePathFor(a.ID), st)
	}
	spawnAccount(a.ID)
}

// focusAccount brings another account's window forward, starting it if it is
// not running yet.
func focusAccount(from uintptr, a account) {
	// Carry this window's placement over so switching reads as one window
	// changing account, rather than a smaller new window appearing.
	st, _ := captureWindowState(from)
	titlePtr, _ := windows.UTF16PtrFromString(windowTitleFor(a))
	target, _, _ := procFindWindow.Call(0, uintptr(unsafe.Pointer(titlePtr)))
	if target != 0 {
		applyWindowState(target, st)
		procSetFgWindow.Call(target)
		return
	}
	// Not running yet: leave the geometry where the new process will read it,
	// so its very first paint is already the right size.
	if st.Saved {
		saveWindowStateTo(windowStatePathFor(a.ID), st)
	}
	spawnAccount(a.ID)
}

func applyAccountName(hwnd uintptr, a account) {
	gAccountName = a.Name
	gServiceBadge = serviceBadge(a.Service)
	gWindowTitle = windowTitleFor(a)
	titlePtr, _ := windows.UTF16PtrFromString(gWindowTitle)
	procSetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(titlePtr)))

	gTrayMu.Lock()
	defer gTrayMu.Unlock()
	if gTray.hWnd == 0 {
		return
	}
	gTray.uFlags = nifMessage | nifIcon | nifTip
	setTip(&gTray, gWindowTitle)
	procShellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(&gTray)))
}

func quitInstance(hwnd uintptr) {
	rememberAccount(gProfileID)
	saveWindowBounds(hwnd)
	trayDelete()
	procPostQuitMessage.Call(0)
}
