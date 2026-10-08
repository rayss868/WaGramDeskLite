//go:build linux

package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
)

const defaultProfileID = "default"

type accountsFile struct {
	Accounts []account `json:"accounts"`
}

func profileDir(id string) string {
	if id == defaultProfileID {
		return getConfigDir()
	}
	dir := filepath.Join(getConfigDir(), "profiles", id)
	_ = os.MkdirAll(dir, 0755)
	return dir
}
func userDataDirFor(id string) string {
	dir := filepath.Join(profileDir(id), "UserData")
	_ = os.MkdirAll(dir, 0755)
	return dir
}
func windowStatePathFor(id string) string { return filepath.Join(profileDir(id), "window.json") }
func accountsPath() string                { return filepath.Join(getConfigDir(), "accounts.json") }
func readAccounts() []account {
	var f accountsFile
	if data, err := os.ReadFile(accountsPath()); err == nil {
		data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
		if json.Unmarshal(data, &f) != nil && len(bytes.TrimSpace(data)) > 0 {
			_ = os.Rename(accountsPath(), accountsPath()+".bad")
		}
	}
	if len(f.Accounts) == 0 || f.Accounts[0].ID != defaultProfileID {
		rest := make([]account, 0, len(f.Accounts))
		var def *account
		for i := range f.Accounts {
			if f.Accounts[i].ID == defaultProfileID {
				def = &f.Accounts[i]
				continue
			}
			rest = append(rest, f.Accounts[i])
		}
		if def == nil {
			def = &account{ID: defaultProfileID, Name: "Account 1", Autostart: true, Service: ServiceWhatsApp}
		}
		f.Accounts = append([]account{*def}, rest...)
	}
	for i := range f.Accounts {
		f.Accounts[i].Service = f.Accounts[i].Service.normalize()
	}
	return f.Accounts
}
func writeAccounts(accounts []account) {
	data, err := json.MarshalIndent(accountsFile{Accounts: accounts}, "", "  ")
	if err == nil {
		_ = os.WriteFile(accountsPath(), data, 0644)
	}
}
func mutateAccounts(fn func([]account) []account) []account {
	var out []account
	withAccountsLock(func() { out = fn(readAccounts()); writeAccounts(out) })
	return out
}
func loadAccounts() []account {
	var out []account
	withAccountsLock(func() { out = readAccounts() })
	return out
}
