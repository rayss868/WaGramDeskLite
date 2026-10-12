//go:build !linux && !windows

package app

func withAccountsLock(fn func()) { fn() }
