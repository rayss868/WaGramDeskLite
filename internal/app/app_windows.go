//go:build windows

package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"

	"wagramdesklite/internal/audio"
)

const (
	windowTitle = "WaGram Desk Lite"
	appURL      = "https://web.whatsapp.com"
	mutexName   = "WaGramDeskLiteSingleInstanceMutex"
	userAgent   = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36"
)

var (
	// Identity of the account this process serves.
	gProfileID = defaultProfileID
	// Name only, kept apart from the window title so a balloon can name the
	// account without carrying the whole app title.
	gAccountName = "Account"
	// Service badge ("WA"/"TG") for the same reason: the balloon says which
	// messenger and which account without repeating the app name.
	gServiceBadge = serviceBadge(ServiceWhatsApp)
	gWindowTitle  = windowTitle
	// Set when the user asks to remove this account; acted on after the
	// WebView2 instance is torn down and its files are unlocked.
	gPendingRemove bool
	// Kept so a tray balloon click can be handed to the page without
	// threading the webview down through the window procedure.
	gWebView webview2.WebView
)

func checkSingleInstance() (uintptr, bool) {
	namePtr, _ := syscall.UTF16PtrFromString(mutexName + "_" + gProfileID)
	handle, _, err := procCreateMutex.Call(0, 1, uintptr(unsafe.Pointer(namePtr)))
	if err == windows.ERROR_ALREADY_EXISTS {
		titlePtr, _ := syscall.UTF16PtrFromString(gWindowTitle)
		hwnd, _, _ := procFindWindow.Call(0, uintptr(unsafe.Pointer(titlePtr)))
		if hwnd != 0 {
			procShowWindow.Call(hwnd, swRestore)
			procSetFgWindow.Call(hwnd)
		}
		return handle, false
	}
	return handle, true
}

func getConfigDir() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = os.Getenv("APPDATA")
		if configDir == "" {
			configDir = "."
		}
	}
	dir := filepath.Join(configDir, "WaGramDeskLite")
	_ = os.MkdirAll(dir, 0755)
	return dir
}

func getUserDataDir() string {
	return userDataDirFor(gProfileID)
}

func webView2RuntimeInstalled() bool {
	clsid := `Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`
	roots := []string{
		`SOFTWARE\WOW6432Node\` + clsid,
		`SOFTWARE\` + clsid,
	}
	for _, root := range roots {
		pathPtr, _ := windows.UTF16PtrFromString(root)
		var key windows.Handle
		if err := windows.RegOpenKeyEx(windows.HKEY_LOCAL_MACHINE, pathPtr, 0, windows.KEY_READ, &key); err == nil {
			windows.RegCloseKey(key)
			return true
		}
	}
	return false
}

func messageBox(parent uintptr, message, title string, flags uintptr) int {
	titlePtr, _ := windows.UTF16PtrFromString(title)
	msgPtr, _ := windows.UTF16PtrFromString(message)
	r, _, _ := procMessageBoxW.Call(parent, uintptr(unsafe.Pointer(msgPtr)), uintptr(unsafe.Pointer(titlePtr)), flags)
	return int(r)
}

func showErrorDialog(message string) {
	messageBox(0, message, windowTitle, mbIconError)
}

// Run starts the WaGramDeskLite window and blocks until the app exits.
// It returns the process exit code.
func Run() int {
	profileID, explicit := profileFromArgs()
	acct := ensureAccount(profileID)
	gProfileID = profileID
	gAccountName = acct.Name
	gServiceBadge = serviceBadge(acct.Service)
	gWindowTitle = windowTitleFor(acct)

	_, isSingle := checkSingleInstance()
	if !isSingle {
		return 0
	}
	setAutostart(gProfileID, true)

	if !webView2RuntimeInstalled() {
		showErrorDialog("WebView2 Runtime tidak ditemukan.\n\nSilakan install Microsoft Edge WebView2 Runtime dari:\nhttps://developer.microsoft.com/microsoft-edge/webview2/")
		return 1
	}

	userDataDir := getUserDataDir()
	executablePath, _ := os.Executable()
	iconFullPath := filepath.Join(filepath.Dir(executablePath), "icon.ico")

	opts := webview2.WebViewOptions{
		Window:    nil,
		Debug:     false,
		DataPath:  userDataDir,
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  windowTitle,
			Width:  1100,
			Height: 750,
			IconId: 2,
			Center: true,
		},
	}

	// Trim memory footprint: limit renderer count and disable unused Chromium
	// components (SmartScreen, in-app PDF viewer, background networking).
	// Read by WebView2 loader when the environment is created.
	//
	// max-old-space-size caps V8's old space. It does not free memory by
	// itself; it makes GC run sooner. Too low a value crashes the renderer on
	// large chat histories, so 512 MB is deliberately conservative.
	//
	// Do not drop --disable-gpu because it looks like a naive tweak: measured
	// on integrated graphics, hardware compositing put ~590 MB of texture
	// memory in the GPU process and pushed the total from 773 MB to 1440 MB.
	// low-end-device-mode is worth about 70 MB across three runs per config.
	_ = os.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS",
		"--js-flags=--max-old-space-size=512 --renderer-process-limit=1 --process-per-site --disable-site-isolation-trials --disable-gpu --disable-gpu-compositing --enable-low-end-device-mode --disable-features=SitePerProcess,IsolateOrigins,OutOfProcessNetworkService,msWebOOUI,msPdfOOUI,msSmartScreenProtection --disable-background-networking --disable-component-update --no-first-run --disable-sync")

	w := webview2.NewWithOptions(opts)
	if w == nil {
		showErrorDialog("Gagal menginisialisasi WebView2. Pastikan Microsoft Edge WebView2 Runtime terinstall.")
		return 1
	}
	// Registered before w.Destroy so it runs after it, once WebView2 has let go
	// of the profile's files.
	defer func() {
		if gPendingRemove {
			removeAccount(gProfileID)
		}
	}()
	defer w.Destroy()
	gWebView = w
	// Registered after the Destroy defer, so it runs first: a balloon click
	// during teardown must not reach a webview that is already gone.
	defer func() { gWebView = nil }()
	initMemoryControl(w)
	enableContextMenu(w)
	initNotificationPermission(w)
	audio.StartLabeler(serviceLabel(acct.Service)+" - "+acct.Name, effectiveVolume(acct))

	// Only an implicit launch reopens the accounts from the previous session;
	// an explicit --profile starts exactly the one account it names.
	if !explicit {
		for _, a := range loadAccounts() {
			if a.ID != gProfileID && a.Autostart {
				spawnAccount(a.ID)
			}
		}
	}

	hwnd := uintptr(w.Window())
	setDarkWindowFrame(hwnd)

	// Restore window size/position from the previous session, if any.
	applyWindowState(hwnd, loadWindowState())

	installWindowSubclass(hwnd)
	startIdleTimer(hwnd)
	trayAdd(hwnd, iconFullPath)
	defer trayDelete()

	w.SetTitle(gWindowTitle)
	registerBindings(w, hwnd)

	uaJSON, _ := json.Marshal(userAgent)

	// Inject JS: User-Agent spoofing + Notification API polyfill connecting to the native tray balloon.
	initScript := fmt.Sprintf(`
		// UserAgent override
		Object.defineProperty(navigator, 'userAgent', {
			get: () => %[1]s
		});
		Object.defineProperty(navigator, 'appVersion', {
			get: () => %[1]s
		});

		%[2]s
	`, string(uaJSON), notificationPolyfillJS)

	w.Init(initScript)
	w.Init(accountOverlayScript)
	w.Init(privacyScript(loadPrefs()))
	w.Init(agentScript)
	startMCPServer(w)
	startScheduler(w)
	startWebhookWatcher(w)
	w.Navigate(serviceURL(ensureAccount(profileID).Service))
	w.Run()

	return 0
}
