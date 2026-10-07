//go:build windows

package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	webview2 "github.com/jchv/go-webview2"
)

// scheduledMessage is a message queued to be sent to the open conversation at
// SendAt (Unix seconds).
type scheduledMessage struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	SendAt int64  `json:"sendAt"`
}

func scheduledPath() string {
	return filepath.Join(getConfigDir(), "scheduled.json")
}

func loadScheduled() []scheduledMessage {
	b, err := os.ReadFile(scheduledPath())
	if err != nil {
		return nil
	}
	var list []scheduledMessage
	if err := json.Unmarshal(b, &list); err != nil {
		return nil
	}
	return list
}

func saveScheduled(list []scheduledMessage) {
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(scheduledPath(), b, 0644)
}

func addScheduled(text string, sendAt int64) []scheduledMessage {
	list := loadScheduled()
	id := strconv.FormatInt(time.Now().UnixNano(), 10)
	list = append(list, scheduledMessage{ID: id, Text: text, SendAt: sendAt})
	saveScheduled(list)
	return list
}

func deleteScheduled(id string) []scheduledMessage {
	list := loadScheduled()
	out := list[:0]
	for _, m := range list {
		if m.ID != id {
			out = append(out, m)
		}
	}
	saveScheduled(out)
	return out
}

var schedulerOnce sync.Once

// startScheduler sends due messages on a timer. The timer runs off the UI
// thread, so its agent round trips are safe.
func startScheduler(w webview2.WebView) {
	schedulerOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				runDueScheduled(w)
			}
		}()
	})
}

func runDueScheduled(w webview2.WebView) {
	list := loadScheduled()
	if len(list) == 0 {
		return
	}
	now := time.Now().Unix()
	var keep []scheduledMessage
	changed := false
	for _, m := range list {
		if m.SendAt <= now {
			_, _ = callAgent(w, "send_message", map[string]any{"text": m.Text})
			changed = true
			continue
		}
		keep = append(keep, m)
	}
	if changed {
		saveScheduled(keep)
	}
}
