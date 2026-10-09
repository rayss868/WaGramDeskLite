//go:build linux

package app

import (
	"os/exec"
	"path/filepath"
	"strings"

	"wagramdesklite/internal/paste"
	"wagramdesklite/internal/webview"
)

// accountsView is the snapshot the overlay script reads to render the account
// list and the settings rows.
type accountsView struct {
	Current  string    `json:"current"`
	Accounts []account `json:"accounts"`
	Prefs    prefs     `json:"prefs"`
}

// registerBindings exposes the host functions the injected scripts call.
func registerBindings(w webview.WebView, hwnd uintptr) {
	// WebKitGTK drops image payloads from paste events; the shim in
	// internal/paste reads them back from the OS clipboard through this.
	_ = w.Bind("wagramReadClipboardImage", paste.ReadImage)
	_ = w.Bind("sendNativeNotification", func(title, body, iconDataURL string) {
		if !notificationsEnabled(loadPrefs()) {
			return
		}
		icon := decodeDataURL(iconDataURL)
		// The balloon is the one surface where several accounts share a single
		// tray icon, so its title names the account instead of repeating the
		// app name. The sender, which the page passed as the title, moves into
		// the message so nothing is lost.
		sender := strings.TrimSpace(title)
		text := strings.TrimSpace(body)
		switch {
		case sender != "" && text != "":
			text = sender + ": " + text
		case text == "":
			text = sender
		}
		// Short by design: the title has room for one line, and it has to say
		// both which messenger and which account, since two WhatsApp windows
		// would otherwise read the same.
		go trayBalloon(gServiceBadge+" - "+gAccountName, text, icon)
	})
	_ = w.Bind("wagramNotificationsSet", func(on bool) {
		p := loadPrefs()
		setNotificationsEnabled(&p, on)
		savePrefs(p)
		setNotificationPermission(on)
	})
	_ = w.Bind("wagramLiteSet", func(on bool) {
		p := loadPrefs()
		setLiteEnabled(&p, on)
		savePrefs(p)
		// The binding may run off the UI thread, and both the memory target and
		// the eco-QoS handoff need it; hop over before touching them.
		// Linux backend has no WebView2 memory or EcoQoS APIs.
	})
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
	_ = w.Bind("wagramVolumeSet", func(percent int) {
		mutateAccounts(func(accounts []account) []account {
			for i := range accounts {
				if accounts[i].ID == gProfileID {
					setAccountVolume(&accounts[i], percent)
				}
			}
			return accounts
		})
		// Linux desktop audio mixing is managed by the system mixer; retain per-account preference only.
	})
	_ = w.Bind("wagramVolumeMute", func(on bool) {
		mutateAccounts(func(accounts []account) []account {
			for i := range accounts {
				if accounts[i].ID == gProfileID {
					setAccountMuted(&accounts[i], on)
				}
			}
			return accounts
		})
		// Linux desktop audio mixing is managed by the system mixer; retain per-account preference only.
	})

	_ = w.Bind("wagramAccountsState", func() accountsView {
		return accountsView{Current: gProfileID, Accounts: loadAccounts(), Prefs: loadPrefs()}
	})
	_ = w.Bind("wagramAccountSwitch", func(id string) {
		for _, a := range loadAccounts() {
			if a.ID == id && a.ID != gProfileID {
				selectAccount(hwnd, a)
				return
			}
		}
	})
	_ = w.Bind("wagramAccountAdd", func() {
		focusAccount(hwnd, addAccount())
	})
	_ = w.Bind("wagramAccountAddService", func(svc string) {
		focusAccount(hwnd, addServiceAccount(Service(svc)))
	})
	_ = w.Bind("wagramPrefsSet", func(mode string) {
		p := loadPrefs()
		if mode == string(ViewPages) {
			p.ViewMode = ViewPages
			savePrefs(p)
			showAllAccounts()
		} else {
			p.ViewMode = ViewTabs
			if p.ActiveID == "" {
				p.ActiveID = gProfileID
			}
			savePrefs(p)
			showOnlyAccount(p.ActiveID)
		}
	})
	_ = w.Bind("wagramAccountRename", func(id, name string) {
		renameAccount(id, name)
		if id == gProfileID {
			w.Dispatch(func() { applyAccountName(hwnd, accountFor(id)) })
		}
	})
	// Only the account you are looking at can be removed: another instance owns
	// its own window and files, and has no channel to be told to shut down.
	_ = w.Bind("wagramAccountRemove", func(id string) {
		if id != gProfileID || id == defaultProfileID {
			return
		}
		w.Dispatch(func() {
			gPendingRemove = true
			quitInstance(hwnd)
		})
	})
	// The injected agent answers Go->JS requests through this binding.
	_ = w.Bind("wagramAgentReply", agentReply)

	// Export takes the messages the page already scraped, so the binding only
	// writes a file and never blocks on the UI thread.
	_ = w.Bind("wagramExportWrite", func(format, dataJSON string) string {
		path, err := writeExport(format, []byte(dataJSON))
		if err != nil {
			return "error: " + err.Error()
		}
		return path
	})

	// Reveal opens Explorer with the exported file selected, so the overlay can
	// show a short confirmation instead of the long absolute path.
	_ = w.Bind("wagramExportReveal", func(path string) string {
		if path == "" {
			return "no path"
		}
		if err := exec.Command("xdg-open", filepath.Dir(path)).Start(); err != nil {
			return "error: " + err.Error()
		}
		return "opened"
	})

	// Webhook endpoints: the overlay lists configured URLs page-side and pairs
	// them with add/delete, just like quick replies.
	_ = w.Bind("wagramWebhooksState", func() []string {
		return loadWebhooks()
	})
	_ = w.Bind("wagramWebhookAdd", func(url string) []string {
		return addWebhook(url)
	})
	_ = w.Bind("wagramWebhookDelete", func(url string) []string {
		return deleteWebhook(url)
	})

	_ = w.Bind("wagramQuickRepliesState", func() []quickReply {
		return loadQuickReplies()
	})
	_ = w.Bind("wagramQuickReplySave", func(token, text string) []quickReply {
		list := saveQuickReply(token, text)
		pushQuickReplies(w)
		return list
	})
	_ = w.Bind("wagramQuickReplyDelete", func(token string) []quickReply {
		list := deleteQuickReply(token)
		pushQuickReplies(w)
		return list
	})

	_ = w.Bind("wagramScheduleState", func() []scheduledMessage {
		return loadScheduled()
	})
	_ = w.Bind("wagramScheduleAdd", func(text string, sendAtUnix int64) []scheduledMessage {
		return addScheduled(text, sendAtUnix)
	})
	_ = w.Bind("wagramScheduleDelete", func(id string) []scheduledMessage {
		return deleteScheduled(id)
	})
}
