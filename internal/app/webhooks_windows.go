//go:build windows

package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	webview2 "github.com/jchv/go-webview2"
)

// webhooks.json holds the URL list this window POSTs incoming-message events
// to. One file per machine (shared by all accounts); the secret that signs the
// events is per-account, like the MCP token.

func webhooksPath() string {
	return filepath.Join(getConfigDir(), "webhooks.json")
}

func loadWebhooks() []string {
	b, err := os.ReadFile(webhooksPath())
	if err != nil {
		return nil
	}
	var list []string
	if err := json.Unmarshal(b, &list); err != nil {
		return nil
	}
	return list
}

func saveWebhooks(list []string) {
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(webhooksPath(), b, 0644)
}

func addWebhook(url string) []string {
	url = strings.TrimSpace(url)
	list := loadWebhooks()
	for _, u := range list {
		if u == url {
			return list
		}
	}
	list = append(list, url)
	saveWebhooks(list)
	return list
}

func deleteWebhook(url string) []string {
	list := loadWebhooks()
	out := list[:0]
	for _, u := range list {
		if u != url {
			out = append(out, u)
		}
	}
	saveWebhooks(out)
	return out
}

// webhookSecret returns this account's secret, generating and storing one on
// first use. Receivers verify the X-Wagram-Secret header against it.
func webhookSecret() string {
	name := "webhook-secret"
	if gProfileID != defaultProfileID {
		name = "webhook-secret-" + gProfileID
	}
	path := filepath.Join(getConfigDir(), name)
	if b, err := os.ReadFile(path); err == nil {
		if s := strings.TrimSpace(string(b)); s != "" {
			return s
		}
	}
	s := randomToken()
	_ = os.WriteFile(path, []byte(s), 0600)
	return s
}

var webhookOnce sync.Once

// startWebhookWatcher polls the chat list every few seconds, and for every
// chat whose unread count went up it POSTs an incoming-message event to each
// configured webhook URL. The loop runs off the UI thread like the scheduler,
// and the agent round trip is bounded by the same timeout as other tools.
func startWebhookWatcher(w webview2.WebView) {
	webhookOnce.Do(func() {
		go func() {
			seen := map[string]int{}
			primed := false
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				primed = pollIncoming(w, seen, primed)
			}
		}()
	})
}

func pollIncoming(w webview2.WebView, seen map[string]int, primed bool) bool {
	urls := loadWebhooks()
	if len(urls) == 0 {
		return primed
	}
	val, err := callAgentString(w, "incoming_events")
	if err != nil {
		return primed
	}
	var res struct {
		Chats []chatRow `json:"chats"`
	}
	if err := json.Unmarshal([]byte(val), &res); err != nil {
		return primed
	}
	keys := chatRowKeys(res.Chats)
	// First poll only records the baseline so messages that were already
	// unread before launch are not replayed. Wait for a real chat list before
	// priming, otherwise an empty list while the page loads would make every
	// existing unread chat look like a fresh event on the next poll.
	if !primed {
		if len(res.Chats) == 0 {
			return false
		}
		for i, c := range res.Chats {
			seen[keys[i]] = c.Unread
		}
		return true
	}
	now := time.Now()
	for i, c := range res.Chats {
		key := keys[i]
		prev, ok := seen[key]
		if !ok {
			// A chat that was not visible before (or was unread 0) now has a
			// fresh unread badge, so this is a new incoming message.
			prev = 0
		}
		if c.Unread > prev {
			postWebhooks(urls, map[string]any{
				"event":   "message.incoming",
				"account": gAccountName,
				"profile": gProfileID,
				"service": gServiceBadge,
				"chat":    c.Name,
				"phone":   c.Phone,
				"text":    c.Preview,
				"unread":  c.Unread,
				"time":    now.Format("15:04"),
			})
		}
		seen[key] = c.Unread
	}
	// A chat that drops out of the poll is just scrolled out of the virtualized
	// DOM list, not read — keep its last unread count so it does not refire the
	// same event when it scrolls back in.
	return true
}

// chatRow is one row of the agent's incoming_events payload.
type chatRow struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Preview string `json:"preview"`
	Phone   string `json:"phone"`
	Avatar  string `json:"avatar"`
	Unread  int    `json:"unread"`
}

// chatRowKeys returns one stable key per row: the chatKey of the row, with an
// occurrence index appended when two rows still collide (e.g. two communities
// each exposing a "General" subgroup, both with no avatar). Without the suffix
// those rows share a single slot in the watcher's seen map, so their unread
// counts overwrite each other and the larger one looks like a fresh increase
// on every poll — refiring the webhook while it sits unread. Order of
// appearance is stable between polls, so a colliding row keeps its key.
func chatRowKeys(rows []chatRow) []string {
	keys := make([]string, len(rows))
	counts := map[string]int{}
	for i, c := range rows {
		base := chatKey(c.ID, c.Name, c.Avatar)
		keys[i] = fmt.Sprintf("%s#%d", base, counts[base])
		counts[base]++
	}
	return keys
}

// chatKey returns the stable identity for a chat row: the DOM data-id when the
// page exposes one, otherwise a composite of display name and avatar URL. Two
// chats can share a display name (duplicate group names), and two chats can
// share an avatar URL (same group photo), so neither alone is unique — but the
// pair keeps every row in the chat list distinguishable.
func chatKey(id, name, avatar string) string {
	if id != "" {
		return id
	}
	return name + "\x00" + avatar
}

// callAgentString runs an agent function and returns its value as a JSON
// string, unwrapping the {ok, value, error} envelope.
func callAgentString(w webview2.WebView, fn string) (string, error) {
	res, err := callAgent(w, fn, nil)
	if err != nil {
		return "", err
	}
	var env struct {
		OK    bool            `json:"ok"`
		Value json.RawMessage `json:"value"`
		Error string          `json:"error"`
	}
	if err := json.Unmarshal(res, &env); err != nil {
		return string(res), err
	}
	if !env.OK {
		return "", fmt.Errorf("%s", env.Error)
	}
	return string(env.Value), nil
}

// postWebhooks sends one event to every configured URL, non-blocking, with a
// 5-second timeout and the account secret in a header the receiver can verify.
func postWebhooks(urls []string, payload map[string]any) {
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	secret := webhookSecret()
	client := &http.Client{Timeout: 5 * time.Second}
	for _, u := range urls {
		u := u
		go func() {
			req, err := http.NewRequest(http.MethodPost, u, bytes.NewReader(b))
			if err != nil {
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Wagram-Secret", secret)
			req.Header.Set("X-Wagram-Profile", gProfileID)
			resp, err := client.Do(req)
			if err != nil {
				return
			}
			_ = resp.Body.Close()
		}()
	}
}
