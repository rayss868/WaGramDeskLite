//go:build windows

package audio

import "testing"

func TestClampPercent(t *testing.T) {
	cases := []struct{ in, want int }{
		{-5, 0}, {0, 0}, {50, 50}, {100, 100}, {150, 100},
	}
	for _, c := range cases {
		if got := clampPercent(c.in); got != c.want {
			t.Errorf("clampPercent(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestSetVolumeClampsStoredTarget(t *testing.T) {
	SetVolume(150)
	if got := volPercent.Load(); got != 100 {
		t.Fatalf("want stored 100, got %d", got)
	}
	SetVolume(-5)
	if got := volPercent.Load(); got != 0 {
		t.Fatalf("want stored 0, got %d", got)
	}
	SetVolume(80)
	if got := volPercent.Load(); got != 80 {
		t.Fatalf("want stored 80, got %d", got)
	}
}
