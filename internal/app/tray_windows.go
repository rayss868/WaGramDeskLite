//go:build windows

package app

import (
	"encoding/base64"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

type notifyIconData struct {
	cbSize           uint32
	hWnd             uintptr
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            uintptr
	szTip            [128]uint16
	dwState          uint32
	dwStateMask      uint32
	szInfo           [256]uint16
	uVersion         uint32
	szInfoTitle      [64]uint16
	dwInfoFlags      uint32
	guidItem         windows.GUID
	hBalloonIcon     uintptr
}

var (
	gTray notifyIconData

	gTrayMu sync.Mutex
	// Large app icon used as the balloon icon when a message carries no avatar.
	gAppBalloonIcon uintptr
	// Avatar icon of the most recent notification, owned by us.
	gBalloonIcon uintptr
)

func copyUTF16(dst []uint16, s string) {
	src, _ := windows.UTF16FromString(s)
	copy(dst, src)
}

func setTip(n *notifyIconData, s string)       { copyUTF16(n.szTip[:], s) }
func setInfoTitle(n *notifyIconData, s string) { copyUTF16(n.szInfoTitle[:], s) }
func setInfo(n *notifyIconData, s string)      { copyUTF16(n.szInfo[:], s) }

func loadTrayIcon(iconPath string, size uintptr) uintptr {
	pathPtr, _ := windows.UTF16PtrFromString(iconPath)
	hIcon, _, _ := procLoadImageW.Call(0, uintptr(unsafe.Pointer(pathPtr)), imageIcon, size, size, lrLoadFromFile)
	if hIcon != 0 {
		return hIcon
	}
	// Portable builds carry the same icon as the window in resource 2.
	var module windows.Handle
	if windows.GetModuleHandleEx(windows.GET_MODULE_HANDLE_EX_FLAG_UNCHANGED_REFCOUNT, nil, &module) == nil {
		hIcon, _, _ = procLoadImageW.Call(uintptr(module), 2, imageIcon, size, size, 0)
		if hIcon != 0 {
			return hIcon
		}
	}
	hIcon, _, _ = procLoadIconW.Call(0, idiApplication)
	return hIcon
}

func trayAdd(hwnd uintptr, iconPath string) {
	if gTray.hWnd != 0 {
		return
	}
	var nid notifyIconData
	nid.cbSize = uint32(unsafe.Sizeof(nid))
	nid.hWnd = hwnd
	nid.uID = 1
	nid.uFlags = nifMessage | nifIcon | nifTip
	nid.uCallbackMessage = wmTrayCallback
	nid.hIcon = loadTrayIcon(iconPath, 16)
	setTip(&nid, gWindowTitle)
	gAppBalloonIcon = loadTrayIcon(iconPath, 32)
	gTray = nid
	procShellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(&nid)))
}

// trayBalloon shows a notification. iconData, when it decodes to an image, is
// used as the balloon icon so the sender's avatar appears; otherwise the app
// icon is used. Passing nil is fine for app-generated messages.
func trayBalloon(title, message string, iconData []byte) {
	gTrayMu.Lock()
	defer gTrayMu.Unlock()
	if gTray.hWnd == 0 {
		return
	}

	// NIF_INFO alone would drop the icon, tip, and callback flags.
	gTray.uFlags = nifMessage | nifIcon | nifTip | nifInfo
	setInfoTitle(&gTray, title)
	setInfo(&gTray, message)

	avatar := createIconFromImage(decodeIconImage(iconData))
	icon := avatar
	if icon == 0 {
		icon = gAppBalloonIcon
	}
	if icon != 0 {
		gTray.hBalloonIcon = icon
		gTray.dwInfoFlags = niifUser | niifLargeIcon
	} else {
		gTray.dwInfoFlags = niifInfo
	}
	procShellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(&gTray)))

	// Release the previous avatar, not the one just handed to the shell.
	if gBalloonIcon != 0 {
		procDestroyIcon.Call(gBalloonIcon)
	}
	gBalloonIcon = avatar
}

// decodeDataURL unwraps a base64 "data:image/...;base64,..." URL.
func decodeDataURL(s string) []byte {
	const marker = ";base64,"
	if !strings.HasPrefix(s, "data:image/") {
		return nil
	}
	i := strings.Index(s, marker)
	if i < 0 {
		return nil
	}
	data, err := base64.StdEncoding.DecodeString(s[i+len(marker):])
	if err != nil {
		return nil
	}
	return data
}

func trayDelete() {
	if gTray.hWnd == 0 {
		return
	}
	procShellNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(&gTray)))
	gTray.hWnd = 0
	if gBalloonIcon != 0 {
		procDestroyIcon.Call(gBalloonIcon)
		gBalloonIcon = 0
	}
}

func showTrayMenu(hwnd uintptr) {
	procSetFgWindow.Call(hwnd)
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	procAppendMenuW.Call(menu, 0, menuOpen, strPtr("Open WaGram Desk Lite"))
	procAppendMenuW.Call(menu, mfSeparator, 0, 0)

	accounts := loadAccounts()
	for i, a := range accounts {
		flags := uintptr(0)
		if a.ID == gProfileID {
			flags = mfChecked
		}
		procAppendMenuW.Call(menu, flags, uintptr(menuAccountBase+i), strPtr(a.Name))
	}

	procAppendMenuW.Call(menu, mfSeparator, 0, 0)
	procAppendMenuW.Call(menu, 0, menuAdd, strPtr("Add Account"))
	notifFlags := uintptr(0)
	if notificationsEnabled(loadPrefs()) {
		notifFlags = mfChecked
	}
	procAppendMenuW.Call(menu, notifFlags, menuNotif, strPtr("Pop-up Notifications"))
	liteFlags := uintptr(0)
	if liteEnabled(loadPrefs()) {
		liteFlags = mfChecked
	}
	procAppendMenuW.Call(menu, liteFlags, menuLite, strPtr("Lite (shed memory when idle)"))
	procAppendMenuW.Call(menu, mfSeparator, 0, 0)
	procAppendMenuW.Call(menu, 0, menuExit, strPtr("Exit"))

	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	cmd, _, _ := procTrackPopupMenu.Call(menu, tpmReturnCmd|tpmRightBtn|tpmBottom, uintptr(pt.X), uintptr(pt.Y), 0, hwnd, 0)
	procDestroyMenu.Call(menu)

	switch {
	case cmd == menuOpen:
		restoreWindow(hwnd)
	case cmd == menuExit:
		// A deliberate exit opts this account out of the next restore.
		setAutostart(gProfileID, false)
		quitInstance(hwnd)
	case cmd == menuAdd:
		focusAccount(hwnd, addAccount())
	case cmd == menuNotif:
		p := loadPrefs()
		setNotificationsEnabled(&p, !notificationsEnabled(p))
		savePrefs(p)
		// The page asks for the permission again after it changes, so the
		// answer has to move with the setting, not just the balloon gate.
		setNotificationPermission(notificationsEnabled(p))
	case cmd == menuLite:
		p := loadPrefs()
		setLiteEnabled(&p, !liteEnabled(p))
		savePrefs(p)
		// Already on the UI thread here, so the change takes effect at once.
		applyLiteSetting()
	case cmd >= menuAccountBase:
		if i := int(cmd) - menuAccountBase; i < len(accounts) {
			if accounts[i].ID == gProfileID {
				restoreWindow(hwnd)
			} else {
				focusAccount(hwnd, accounts[i])
			}
		}
	}
}
