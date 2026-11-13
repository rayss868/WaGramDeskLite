//go:build windows

package app

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	gWinProc uintptr
	gOldProc uintptr
)

func setDarkWindowFrame(hwnd uintptr) {
	darkMode := int32(1)
	// Try standard DWMWA_USE_IMMERSIVE_DARK_MODE (Win10 20H1+ & Win11)
	procDwmSetAttr.Call(
		hwnd,
		uintptr(DWMWA_USE_IMMERSIVE_DARK_MODE),
		uintptr(unsafe.Pointer(&darkMode)),
		unsafe.Sizeof(darkMode),
	)
	// Try older Win10 build
	procDwmSetAttr.Call(
		hwnd,
		uintptr(DWMWA_USE_IMMERSIVE_DARK_MODE_BEFORE_20H1),
		uintptr(unsafe.Pointer(&darkMode)),
		unsafe.Sizeof(darkMode),
	)

	// Set dark caption color (COLORREF: 0x00111B21 WhatsApp Dark Header: RGB 17, 27, 33)
	captionColor := uint32(0x00211B11) // 0x00BBGGRR
	procDwmSetAttr.Call(
		hwnd,
		uintptr(DWMWA_CAPTION_COLOR),
		uintptr(unsafe.Pointer(&captionColor)),
		unsafe.Sizeof(captionColor),
	)

	// Set white caption text (RGB 255, 255, 255)
	textColor := uint32(0x00FFFFFF)
	procDwmSetAttr.Call(
		hwnd,
		uintptr(DWMWA_TEXT_COLOR),
		uintptr(unsafe.Pointer(&textColor)),
		unsafe.Sizeof(textColor),
	)
}

func windowProc(hwnd, msg, wp, lp uintptr) uintptr {
	// Any of these means the user is at the window: bring WebView2 back to
	// full speed and restart the idle countdown.
	switch msg {
	case wmMouseMove, wmKeyDown, wmLButtonDown, wmLButtonUp, wmRButtonUp, wmLButtonDblClk, wmActivate:
		markActivity()
	}
	switch msg {
	case wmClose:
		// Close-to-tray: hide the window and keep running in the background.
		rememberAccount(gProfileID)
		saveWindowBounds(hwnd)
		procShowWindow.Call(hwnd, swHide)
		enterLowMemory()
		go trayBalloon("WaGram Desk Lite", "Masih berjalan di system tray. Klik ikon untuk membuka kembali.", nil)
		return 0
	case wmShowWindow:
		if wp != 0 {
			markActivity()
		} else {
			// Hidden by a tab switch rather than by Close: nobody is looking
			// at this window either, so it sheds its caches the same way.
			enterLowMemory()
		}
		r, _, _ := procCallWindowProcW.Call(gOldProc, hwnd, msg, wp, lp)
		return r
	case wmTimer:
		if wp == uintptr(timerIdle) {
			idleTick(hwnd)
		}
		return 0
	case wmApplyLite:
		applyLiteSetting()
		return 0
	case wmTrayCallback:
		switch uint32(lp) & 0xFFFF {
		case ninBalloonUserClick:
			// The popup was for a specific conversation, so bring the window
			// forward and let the page open it.
			restoreWindow(hwnd)
			openLastNotificationChat()
		case wmLButtonUp, wmLButtonDblClk:
			restoreWindow(hwnd)
		case wmRButtonUp:
			showTrayMenu(hwnd)
		}
		return 0
	default:
		r, _, _ := procCallWindowProcW.Call(gOldProc, hwnd, msg, wp, lp)
		return r
	}
}

func installWindowSubclass(hwnd uintptr) {
	gWinProc = windows.NewCallback(windowProc)
	gOldProc, _, _ = procSetWindowLongPtr.Call(hwnd, uintptr(gwlpWndProc), gWinProc)
}
