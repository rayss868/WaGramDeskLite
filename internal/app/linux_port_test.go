//go:build linux

package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLinuxAccountsPersistAndUseSeparateProfiles(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	acct := ensureAccount(defaultProfileID)
	if acct.ID != defaultProfileID || acct.Name == "" {
		t.Fatalf("unexpected default account: %#v", acct)
	}
	added := addServiceAccount(ServiceTelegram)
	if added.ID == defaultProfileID || added.Service != ServiceTelegram {
		t.Fatalf("unexpected added account: %#v", added)
	}
	path := filepath.Join(getConfigDir(), "accounts.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("accounts file not persisted: %v", err)
	}
	if got := accountFor(added.ID); got.Service != ServiceTelegram {
		t.Fatalf("service not persisted: %#v", got)
	}
	if !removeAccount(added.ID) {
		t.Fatal("removeAccount returned false")
	}
}

func TestValidProfileID(t *testing.T) {
	for _, id := range []string{"a", "account-2", "x_y", "Default9"} {
		if !validProfileID(id) {
			t.Errorf("expected valid id %q", id)
		}
	}
	for _, id := range []string{"", "../escape", "x/y", "space here", "a@b", "123456789012345678901234567890123"} {
		if validProfileID(id) {
			t.Errorf("expected invalid id %q", id)
		}
	}
}
