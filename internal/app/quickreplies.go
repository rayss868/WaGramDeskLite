//go:build linux

package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"wagramdesklite/internal/webview"
)

// quickReply expands a /token typed in the composer into Text.
type quickReply struct {
	Token string `json:"token"`
	Text  string `json:"text"`
}

func quickRepliesPath() string {
	return filepath.Join(getConfigDir(), "quickreplies.json")
}

func loadQuickReplies() []quickReply {
	b, err := os.ReadFile(quickRepliesPath())
	if err != nil {
		return nil
	}
	var list []quickReply
	if err := json.Unmarshal(b, &list); err != nil {
		return nil
	}
	return list
}

func saveQuickReplies(list []quickReply) {
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(quickRepliesPath(), b, 0644)
}

// normalizeToken makes every stored shortcut start with a single slash so the
// composer's exact-match lookup in the agent always agrees with what is saved.
func normalizeToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	if !strings.HasPrefix(token, "/") {
		token = "/" + token
	}
	return token
}

func saveQuickReply(token, text string) []quickReply {
	token = normalizeToken(token)
	if token == "" {
		return loadQuickReplies()
	}
	list := loadQuickReplies()
	found := false
	for i := range list {
		if list[i].Token == token {
			list[i].Text = text
			found = true
			break
		}
	}
	if !found {
		list = append(list, quickReply{Token: token, Text: text})
	}
	saveQuickReplies(list)
	return list
}

func deleteQuickReply(token string) []quickReply {
	list := loadQuickReplies()
	out := list[:0]
	for _, q := range list {
		if q.Token != token {
			out = append(out, q)
		}
	}
	saveQuickReplies(out)
	return out
}

// pushQuickReplies hands the current shortcuts to the page, which keeps them in
// a map and swaps an exact-match token for its text as the user types.
func pushQuickReplies(w webview.WebView) {
	m := map[string]string{}
	for _, q := range loadQuickReplies() {
		m[q.Token] = q.Text
	}
	b, err := json.Marshal(m)
	if err != nil {
		return
	}
	js := "window.wagramAgent && window.wagramAgent.setQuickReplies(" + jsQuote(string(b)) + ");"
	w.Dispatch(func() { w.Eval(js) })
}
