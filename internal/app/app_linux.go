//go:build linux

package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"wagramdesklite/internal/paste"
	"wagramdesklite/internal/webview"
)

const (
	windowTitle = "WaGram Desk Lite"
	appURL      = "https://web.whatsapp.com"
	userAgent   = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36"
)

var (
	gProfileID     = defaultProfileID
	gAccountName   = "Account"
	gServiceBadge  = serviceBadge(ServiceWhatsApp)
	gWindowTitle   = windowTitle
	gPendingRemove bool
	gWebView       webview.WebView
	instanceLock   *os.File
)

func profileFromArgs() (string, bool) {
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		var id string
		switch {
		case strings.HasPrefix(args[i], "--profile="):
			id = strings.TrimPrefix(args[i], "--profile=")
		case args[i] == "--profile" && i+1 < len(args):
			i++
			id = args[i]
		default:
			continue
		}
		if validProfileID(id) {
			return id, true
		}
		return defaultProfileID, false
	}
	return defaultProfileID, false
}
func validProfileID(id string) bool {
	if id == "" || len(id) > 32 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}
func windowTitleFor(a account) string {
	return windowTitle + " — " + a.Name + " [" + string(serviceBadge(a.Service.normalize())) + "]"
}
func checkSingleInstance() (uintptr, bool) {
	f, e := os.OpenFile(filepath.Join(profileDir(gProfileID), "instance.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return 0, true
	}
	if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		_ = exec.Command("wmctrl", "-a", gWindowTitle).Run()
		_ = f.Close()
		return 0, false
	}
	instanceLock = f
	return 0, true
}
func spawnAccount(id string) {
	exe, e := os.Executable()
	if e != nil {
		return
	}
	c := exec.Command(exe, "--profile", id)
	if c.Start() == nil && c.Process != nil {
		_ = c.Process.Release()
	}
}
func focusAccount(_ uintptr, a account)  { spawnAccount(a.ID) }
func selectAccount(_ uintptr, a account) { focusAccount(0, a) }
func showOnlyAccount(id string)          { p := loadPrefs(); p.ActiveID = id; savePrefs(p) }
func showAllAccounts()                   {}
func applyAccountName(_ uintptr, a account) {
	gAccountName, gServiceBadge, gWindowTitle = a.Name, serviceBadge(a.Service), windowTitleFor(a)
	if gWebView != nil {
		gWebView.Dispatch(func() { gWebView.SetTitle(gWindowTitle) })
	}
}
func quitInstance(_ uintptr) {
	if gWebView != nil {
		gWebView.Terminate()
	}
}
func trayBalloon(title, message string, _ []byte) {
	if _, e := exec.LookPath("notify-send"); e == nil {
		_ = exec.Command("notify-send", title, message).Start()
	}
}
func decodeDataURL(string) []byte { return nil }
func openLastNotificationChat() {
	if gWebView != nil {
		gWebView.Dispatch(func() { gWebView.Eval("window.wagramNotificationClick && window.wagramNotificationClick()") })
	}
}
func setNotificationPermission(bool)             {}
func initNotificationPermission(webview.WebView) {}
func initMemoryControl(webview.WebView)          {}
func enableContextMenu(webview.WebView)          {}
func startIdleTimer(uintptr)                     {}
func setDarkWindowFrame(uintptr)                 {}
func checkWebKitGTK() error                      { return nil }
func Run() int {
	profileID, explicit := profileFromArgs()
	acct := ensureAccount(profileID)
	gProfileID, gAccountName = profileID, acct.Name
	gServiceBadge, gWindowTitle = serviceBadge(acct.Service), windowTitleFor(acct)
	_, single := checkSingleInstance()
	if !single {
		return 0
	}
	setAutostart(gProfileID, true)
	if !explicit {
		for _, a := range loadAccounts() {
			if a.ID != gProfileID && a.Autostart {
				spawnAccount(a.ID)
			}
		}
	}
	w := webview.New(false)
	if w == nil {
		fmt.Fprintln(os.Stderr, "Failed to initialize WebKitGTK WebView")
		return 1
	}
	defer w.Destroy()
	gWebView = w
	defer func() {
		gWebView = nil
		if gPendingRemove {
			removeAccount(gProfileID)
		}
	}()
	w.SetTitle(gWindowTitle)
	w.Init(`Object.defineProperty(navigator,'userAgent',{get:()=>` + fmt.Sprintf("%q", userAgent) + `});`)
	w.Init(paste.Script)
	w.Init(accountOverlayScript)
	w.Init(privacyScript(loadPrefs()))
	w.Init(agentScript)
	registerBindings(w, 0)
	startMCPServer(w)
	startScheduler(w)
	startWebhookWatcher(w)
	w.Navigate(serviceURL(acct.Service))
	w.Run()
	return 0
}
