//go:build linux

package app

import (
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

func getConfigDir() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		base = filepath.Join(os.Getenv("HOME"), ".config")
	}
	dir := filepath.Join(base, "WaGramDeskLite")
	_ = os.MkdirAll(dir, 0700)
	return dir
}

var accountsLock sync.Mutex

func withAccountsLock(fn func()) {
	accountsLock.Lock()
	defer accountsLock.Unlock()
	f, err := os.OpenFile(filepath.Join(getConfigDir(), "accounts.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		fn()
		return
	}
	defer f.Close()
	if syscall.Flock(int(f.Fd()), syscall.LOCK_EX) != nil {
		fn()
		return
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	fn()
}

type windowState struct {
	Saved     bool `json:"saved"`
	X         int  `json:"x"`
	Y         int  `json:"y"`
	Width     int  `json:"width"`
	Height    int  `json:"height"`
	Maximized bool `json:"maximized"`
}
