//go:build linux

package app

import (
	"encoding/json"
	"errors"
	"html"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type chatMessage struct {
	Text     string `json:"text"`
	Time     string `json:"time"`
	Outgoing bool   `json:"outgoing"`
}

// writeExport renders the messages the page scraped into the exports folder and
// returns the file path. It takes the JSON straight from the agent so the write
// never has to call back into the UI thread.
func writeExport(format string, data []byte) (string, error) {
	var env struct {
		Messages []chatMessage `json:"messages"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return "", err
	}
	msgs := env.Messages
	if len(msgs) == 0 {
		return "", errors.New("no messages to export")
	}

	dir := filepath.Join(getConfigDir(), "exports")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	ext := strings.ToLower(strings.TrimSpace(format))
	if ext != "json" && ext != "html" {
		ext = "txt"
	}
	path := filepath.Join(dir, "chat-"+time.Now().Format("20060102-150405")+"."+ext)

	var body []byte
	switch ext {
	case "json":
		b, err := json.MarshalIndent(msgs, "", "  ")
		if err != nil {
			return "", err
		}
		body = b
	case "html":
		var b strings.Builder
		b.WriteString("<!doctype html><meta charset=\"utf-8\"><title>Chat export</title>\n")
		b.WriteString("<style>body{font-family:sans-serif;max-width:720px;margin:2rem auto}" +
			"div{margin:.3rem 0;padding:.4rem .6rem;border-radius:.5rem;background:#f0f0f0}" +
			"div.out{background:#d9fdd3;text-align:right}small{opacity:.6}</style>\n")
		for _, m := range msgs {
			cls := ""
			if m.Outgoing {
				cls = " class=\"out\""
			}
			b.WriteString("<div" + cls + ">" + html.EscapeString(m.Text) +
				"<br><small>" + html.EscapeString(m.Time) + "</small></div>\n")
		}
		body = []byte(b.String())
	default:
		var b strings.Builder
		for _, m := range msgs {
			who := "them"
			if m.Outgoing {
				who = "me"
			}
			b.WriteString("[" + m.Time + "] " + who + ": " + m.Text + "\n")
		}
		body = []byte(b.String())
	}
	if err := os.WriteFile(path, body, 0644); err != nil {
		return "", err
	}
	return path, nil
}
