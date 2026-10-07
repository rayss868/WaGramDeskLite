//go:build windows

package app

import "testing"

// Two communities can each expose a "General" subgroup with no avatar, so
// chatKey collides. chatRowKeys must still give each row a distinct key, or
// the watcher's seen map merges them and refires one webhook on every poll.
func TestChatRowKeysDisambiguatesCollisions(t *testing.T) {
	rows := []chatRow{
		{Name: "General", Unread: 1},
		{Name: "Moyra by SRBYTE.id", Avatar: "photo-a", Unread: 3},
		{Name: "General", Unread: 0},
	}
	keys := chatRowKeys(rows)

	if keys[0] == keys[2] {
		t.Fatalf("colliding rows share a key: %q == %q", keys[0], keys[2])
	}
	if keys[0] == keys[1] || keys[1] == keys[2] {
		t.Fatalf("distinct rows share a key: %q %q %q", keys[0], keys[1], keys[2])
	}

	// Same rows in the same order must yield the same keys, so the baseline
	// recorded on one poll still matches on the next.
	again := chatRowKeys(rows)
	for i := range keys {
		if keys[i] != again[i] {
			t.Fatalf("key unstable at %d: %q != %q", i, keys[i], again[i])
		}
	}
}

// A row that carries a data-id is identified by it, so the key is just the id
// with a zero occurrence suffix.
func TestChatRowKeysUsesID(t *testing.T) {
	keys := chatRowKeys([]chatRow{{ID: "12345", Name: "Ignored"}})
	if keys[0] != "12345#0" {
		t.Fatalf("want id-based key, got %q", keys[0])
	}
}