//go:build windows

package app

import "testing"

func TestAccountVolumeDefaultsTo100(t *testing.T) {
	if got := accountVolume(account{ID: "p1"}); got != 100 {
		t.Fatalf("nil volume: want 100, got %d", got)
	}
}

func TestAccountVolumeClampsOutOfRange(t *testing.T) {
	neg := -20
	big := 250
	if got := accountVolume(account{Volume: &neg}); got != 0 {
		t.Fatalf("negative: want 0, got %d", got)
	}
	if got := accountVolume(account{Volume: &big}); got != 100 {
		t.Fatalf("above 100: want 100, got %d", got)
	}
}

func TestSetAccountVolumeStoresClamped(t *testing.T) {
	var a account
	setAccountVolume(&a, 150)
	if a.Volume == nil || *a.Volume != 100 {
		t.Fatalf("want stored 100, got %v", a.Volume)
	}
}

func TestEffectiveVolumeIsZeroWhenMuted(t *testing.T) {
	v := 80
	a := account{Volume: &v}
	setAccountMuted(&a, true)
	if got := effectiveVolume(a); got != 0 {
		t.Fatalf("muted: want 0, got %d", got)
	}
	setAccountMuted(&a, false)
	if got := effectiveVolume(a); got != 80 {
		t.Fatalf("unmuted: want 80, got %d", got)
	}
}
