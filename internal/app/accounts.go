//go:build linux

package app

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

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

func accountFor(id string) account {
	for _, a := range loadAccounts() {
		if a.ID == id {
			a.Service = a.Service.normalize()
			return a
		}
	}
	return account{ID: id, Name: "Account", Service: ServiceWhatsApp}
}

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

func setAutostart(id string, on bool) {
	mutateAccounts(func(accounts []account) []account {
		for i := range accounts {
			if accounts[i].ID == id {
				accounts[i].Autostart = on
			}
		}
		return accounts
	})
}

// nextProfileID picks the lowest unused "pN" so profile folders stay readable.
func nextProfileID(accounts []account) string {
	used := map[string]bool{}
	for _, a := range accounts {
		used[a.ID] = true
	}
	for n := 2; ; n++ {
		id := "p" + strconv.Itoa(n)
		if !used[id] {
			return id
		}
	}
}

// nextAccountName picks the lowest unused "Account N".
func nextAccountName(accounts []account) string {
	used := map[int]bool{}
	for _, a := range accounts {
		var n int
		if _, err := fmt.Sscanf(strings.TrimSpace(a.Name), "Account %d", &n); err == nil {
			used[n] = true
		}
	}
	nums := make([]int, 0, len(used))
	for n := range used {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	for n := 1; ; n++ {
		if !used[n] {
			return "Account " + strconv.Itoa(n)
		}
	}
}

// ensureAccount returns the named account, registering it first if the list has
// never seen it. This keeps an explicit --profile launch self-describing and
// lets a deleted accounts.json rebuild itself.
func ensureAccount(id string) account {
	var found account
	mutateAccounts(func(accounts []account) []account {
		for _, a := range accounts {
			if a.ID == id {
				found = a
				return accounts
			}
		}
		found = account{ID: id, Name: nextAccountName(accounts), Autostart: true, Service: ServiceWhatsApp}
		return append(accounts, found)
	})
	return found
}

// addAccount registers a new profile and returns it.
func addAccount() account {
	return addServiceAccount(ServiceWhatsApp)
}

// addServiceAccount registers a new profile for the given service.
func addServiceAccount(svc Service) account {
	var created account
	mutateAccounts(func(accounts []account) []account {
		created = account{
			ID:        nextProfileID(accounts),
			Name:      nextAccountName(accounts),
			Autostart: true,
			Service:   svc.normalize(),
		}
		return append(accounts, created)
	})
	return created
}

func renameAccount(id, name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	mutateAccounts(func(accounts []account) []account {
		for i := range accounts {
			if accounts[i].ID == id {
				accounts[i].Name = name
			}
		}
		return accounts
	})
}

// removeAccount drops the entry and deletes its stored session. The default
// account is the app itself and cannot be removed.
func removeAccount(id string) bool {
	if id == defaultProfileID {
		return false
	}
	dir := profileDir(id)
	mutateAccounts(func(accounts []account) []account {
		out := accounts[:0]
		for _, a := range accounts {
			if a.ID != id {
				out = append(out, a)
			}
		}
		return out
	})
	// WebView2 may still be releasing handles under the profile directory.
	for attempt := 0; attempt < 3; attempt++ {
		if os.RemoveAll(dir) == nil {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	return true
}
