//go:build windows

package app

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	webview2 "github.com/jchv/go-webview2"
)

// The MCP server exposes this window's page to AI agents over JSON-RPC 2.0 on
// a loopback HTTP endpoint. It is implemented with the standard library only
// because the project vendors its dependencies and adds none.
const mcpProtocolVersion = "2025-06-18"

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type mcpEndpoint struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

type mcpServer struct {
	w     webview2.WebView
	token string

	// pace guards the gap between consecutive /api/send calls so a batch is
	// spread out over time rather than fired back-to-back.
	paceMu   sync.Mutex
	lastSend time.Time
}

// waitPace blocks until at least paceMs has elapsed since the previous send
// started, then records this send's start time. It is a rate floor, not a
// lock, so it spreads a batch out rather than serialising whole sends.
func (s *mcpServer) waitPace(paceMs int) {
	s.paceMu.Lock()
	defer s.paceMu.Unlock()
	if !s.lastSend.IsZero() {
		if wait := time.Duration(paceMs)*time.Millisecond - time.Since(s.lastSend); wait > 0 {
			time.Sleep(wait)
		}
	}
	s.lastSend = time.Now()
}

// mcpListenAddr is the fixed loopback address the endpoint binds to, so an
// agent's config stays valid across launches. If it is already taken (a second
// account, or another app), the server falls back to a random free port and
// records the actual address in the discovery file.
const mcpListenAddr = "127.0.0.1:5987"

var mcpStartOnce sync.Once

// startMCPServer launches the loopback MCP endpoint once per process.
func startMCPServer(w webview2.WebView) {
	mcpStartOnce.Do(func() {
		ln, err := net.Listen("tcp", mcpListenAddr)
		if err != nil {
			ln, err = net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				return
			}
		}
		srv := &mcpServer{w: w, token: persistentToken()}
		mux := http.NewServeMux()
		mux.HandleFunc("/mcp", srv.handle)
		mux.HandleFunc("/api/send", srv.handleSend)
		go func() { _ = http.Serve(ln, mux) }()
		writeMCPEndpoint(ln.Addr().String(), srv.token)
	})
}

// persistentToken returns this machine's MCP token, generating and storing one
// on first use. Keeping it (rather than rotating per launch) lets an agent's
// config keep working across restarts.
func persistentToken() string {
	// Each account gets its own token so an agent bound to one account's
	// endpoint cannot reuse its credential against another account's endpoint.
	name := "mcp-token"
	if gProfileID != defaultProfileID {
		name = "mcp-token-" + gProfileID
	}
	path := filepath.Join(getConfigDir(), name)
	if b, err := os.ReadFile(path); err == nil {
		if t := strings.TrimSpace(string(b)); t != "" {
			return t
		}
	}
	t := randomToken()
	_ = os.WriteFile(path, []byte(t), 0600)
	return t
}

func randomToken() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// writeMCPEndpoint records the URL and token so an agent can find and
// authenticate to this window. One file per account keeps concurrent windows
// from overwriting each other.
func writeMCPEndpoint(addr, token string) {
	ep := mcpEndpoint{URL: "http://" + addr + "/mcp", Token: token}
	b, err := json.MarshalIndent(ep, "", "  ")
	if err != nil {
		return
	}
	name := "mcp.json"
	if gProfileID != defaultProfileID {
		name = "mcp-" + gProfileID + ".json"
	}
	_ = os.WriteFile(filepath.Join(getConfigDir(), name), b, 0600)
}

func (s *mcpServer) handle(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		rw.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if strings.TrimSpace(r.Header.Get("Authorization")) != "Bearer "+s.token {
		rw.WriteHeader(http.StatusUnauthorized)
		return
	}
	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRPC(rw, rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error"}})
		return
	}
	resp, notify := s.dispatch(req)
	if notify {
		rw.WriteHeader(http.StatusAccepted)
		return
	}
	writeRPC(rw, resp)
}

// handleSend implements POST /api/send: multipart form with phone (required),
// text (optional) and file (optional image/document). Same bearer token as /mcp.
func (s *mcpServer) handleSend(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeSendError(rw, http.StatusMethodNotAllowed, "method not allowed, use POST")
		return
	}
	if strings.TrimSpace(r.Header.Get("Authorization")) != "Bearer "+s.token {
		writeSendError(rw, http.StatusUnauthorized, "unauthorized")
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeSendError(rw, http.StatusBadRequest, "parse multipart: "+err.Error())
		return
	}
	phone := normalizePhone(r.FormValue("phone"))
	if phone == "" {
		writeSendError(rw, http.StatusBadRequest, "phone required (digits, country code, e.g. 62812...)")
		return
	}
	text := strings.TrimSpace(r.FormValue("text"))
	asMode := strings.ToLower(strings.TrimSpace(r.FormValue("as")))
	if asMode != "" && asMode != "image" && asMode != "media" && asMode != "document" && asMode != "file" && asMode != "sticker" {
		writeSendError(rw, http.StatusBadRequest, "as must be image/media, document/file, sticker (or empty for auto)")
		return
	}
	// Humanizer knobs (all optional, milliseconds; jitter is a 0..1 fraction).
	// They pace the attach-menu steps so a send does not look like a bot, and
	// give a slow WhatsApp Web time to mount the right file input.
	humanizer := humanizerFromForm(r)
	paceMs := paceFromForm(r)
	var fileB64, fileName, mime string
	if fh, hdr, err := r.FormFile("file"); err == nil {
		defer fh.Close()
		buf, err := io.ReadAll(io.LimitReader(fh, 25<<20))
		if err != nil {
			writeSendError(rw, http.StatusBadRequest, "read file: "+err.Error())
			return
		}
		if len(buf) == 0 {
			writeSendError(rw, http.StatusBadRequest, "empty file")
			return
		}
		fileName = hdr.Filename
		if fileName == "" {
			fileName = "upload.bin"
		}
		mime = hdr.Header.Get("Content-Type")
		if mime == "" {
			mime = sniffMime(fileName)
		}
		fileB64 = base64.StdEncoding.EncodeToString(buf)
	}
	if text == "" && fileB64 == "" {
		writeSendError(rw, http.StatusBadRequest, "nothing to send: provide text and/or file")
		return
	}
	res, err := s.sendByPhone(phone, text, fileB64, fileName, mime, asMode, humanizer, paceMs)
	if err != nil {
		if errors.Is(err, errSendTimeout) {
			writeSendError(rw, http.StatusGatewayTimeout, err.Error())
			return
		}
		writeSendError(rw, http.StatusBadGateway, err.Error())
		return
	}
	rw.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(rw).Encode(map[string]any{"ok": true, "phone": phone, "result": res})
}

var errSendTimeout = errors.New("send: timed out waiting for the chat to open")

// normalizePhone reduces a phone number to digits only, dropping spaces,
// dashes and a leading +. The country code is part of the digits.
func normalizePhone(p string) string {
	p = strings.ReplaceAll(strings.ReplaceAll(p, " ", ""), "-", "")
	p = strings.TrimPrefix(p, "+")
	var b strings.Builder
	for _, r := range p {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func sniffMime(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".pdf":
		return "application/pdf"
	case ".txt":
		return "text/plain"
	default:
		return "application/octet-stream"
	}
}

// humanizerFromForm reads the optional pacing knobs from a /api/send request.
// Every value is a millisecond count except jitter (a 0..1 fraction). Only the
// keys the caller actually supplied are included, so the agent falls back to
// its own defaults for the rest. base (or the legacy delay) sets the default
// for open/item/send when those are not given individually.
func humanizerFromForm(r *http.Request) map[string]any {
	msKeys := []string{"base", "open", "item", "send", "typing"}
	h := map[string]any{}
	for _, k := range msKeys {
		if v, ok := formMs(r, k); ok {
			h[k] = v
		}
	}
	// delay is the historical alias for base.
	if _, ok := h["base"]; !ok {
		if v, ok := formMs(r, "delay"); ok {
			h["base"] = v
		}
	}
	if s := strings.TrimSpace(r.FormValue("jitter")); s != "" {
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			if f < 0 {
				f = 0
			}
			if f > 1 {
				f = 1
			}
			h["jitter"] = f
		}
	}
	if len(h) == 0 {
		return nil
	}
	return h
}

// formMs parses a single millisecond knob, accepting 0..60000 and returning
// ok=false when the field is absent or out of range.
func formMs(r *http.Request, name string) (int, bool) {
	s := strings.TrimSpace(r.FormValue(name))
	if s == "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || n > 60000 {
		return 0, false
	}
	return n, true
}

// paceFromForm reads the optional server-side pacing knob: the minimum gap
// enforced between consecutive sends.
func paceFromForm(r *http.Request) int {
	if v, ok := formMs(r, "pace"); ok {
		return v
	}
	return 0
}

func writeSendError(rw http.ResponseWriter, code int, msg string) {
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(code)
	_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "error": msg})
}

// callAgentValue runs an agent function and returns its unwrapped value,
// surfacing the {ok, value, error} envelope's error as a Go error.
func callAgentValue(w webview2.WebView, fn string, args any) (json.RawMessage, error) {
	res, err := callAgent(w, fn, args)
	if err != nil {
		return nil, err
	}
	return unwrapAgentValue(fn, res)
}

// callAgentValueLong is callAgentValue with the long request timeout, for
// operations that legitimately take tens of seconds (media upload + send).
func callAgentValueLong(w webview2.WebView, fn string, args any) (json.RawMessage, error) {
	res, err := callAgentLong(w, fn, args)
	if err != nil {
		return nil, err
	}
	return unwrapAgentValue(fn, res)
}

func unwrapAgentValue(fn string, res json.RawMessage) (json.RawMessage, error) {
	var env struct {
		OK    bool            `json:"ok"`
		Value json.RawMessage `json:"value"`
		Error string          `json:"error"`
	}
	if err := json.Unmarshal(res, &env); err != nil {
		return res, nil
	}
	if !env.OK {
		return nil, fmt.Errorf("agent %s: %s", fn, env.Error)
	}
	return env.Value, nil
}

// chatOpen reports whether a real conversation panel is mounted (strong
// compose-box marker), avoiding the composer_ready false positive that also
// matches the home-screen search box.
func chatOpen(w webview2.WebView) (bool, error) {
	v, err := callAgentValue(w, "chat_open", nil)
	if err != nil {
		return false, err
	}
	var r struct {
		Open bool `json:"open"`
	}
	if err := json.Unmarshal(v, &r); err != nil {
		return false, err
	}
	return r.Open, nil
}

// sendByPhone opens a chat addressed to the given phone number and sends an
// optional text and/or a file to it. Navigating changes the active chat for
// this window.
func (s *mcpServer) sendByPhone(phone, text, fileB64, fileName, mime, asMode string, humanizer map[string]any, paceMs int) (any, error) {
	// Pacing: enforce a minimum gap between this send and the previous one so a
	// batch of sends is spread out instead of firing back-to-back.
	if paceMs > 0 {
		s.waitPace(paceMs)
	}
	// Open the chat addressed to the phone number. Navigate at most twice:
	// re-issuing the deep link in a tight loop makes WhatsApp Web reload the
	// whole SPA repeatedly, which the user experiences as endless reloading.
	// The readiness check uses chat_open (a strong compose-box marker), not
	// composer_ready whose loose selectors also match the home search box.
	ready := false
	var lastState json.RawMessage
	var openErr error
	for attempt := 0; attempt < 2 && !ready; attempt++ {
		if _, err := callAgentValue(s.w, "open_chat_phone", map[string]any{"phone": phone}); err != nil {
			// Navigation can tear down the JS context before the reply
			// reaches Go, so a timed-out call may still have navigated.
			// Let the chat-open poll decide instead of failing outright.
			openErr = err
		}
		deadline := time.Now().Add(25 * time.Second)
		for {
			if ok, _ := chatOpen(s.w); ok {
				ready = true
				break
			}
			if time.Now().After(deadline) {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
		if !ready {
			if v, err := callAgentValue(s.w, "page_state", nil); err == nil {
				lastState = v
			}
			time.Sleep(2 * time.Second)
		}
	}
	if lastState != nil {
		_ = os.WriteFile(filepath.Join(os.TempDir(), "wagram_send_debug.json"), lastState, 0644)
	}
	if !ready {
		if openErr != nil {
			return nil, fmt.Errorf("%w: chat for phone %s did not open (last open-chat error: %v)", errSendTimeout, phone, openErr)
		}
		return nil, fmt.Errorf("%w: chat for phone %s did not open", errSendTimeout, phone)
	}
	out := map[string]any{"phone": phone}
	if fileB64 != "" {
		v, err := callAgentValueLong(s.w, "send_media", map[string]any{
			"data":      fileB64,
			"filename":  fileName,
			"mime":      mime,
			"caption":   text,
			"as":        asMode,
			"humanizer": humanizer,
		})
		if err != nil {
			return nil, err
		}
		var sr struct {
			Sent bool            `json:"sent"`
			File string          `json:"file"`
			Err  string          `json:"error"`
			Dbg  json.RawMessage `json:"dbg"`
		}
		if err := json.Unmarshal(v, &sr); err == nil && !sr.Sent {
			if sr.Err != "" {
				return nil, fmt.Errorf("send media %s: %s (dbg=%s)", fileName, sr.Err, string(sr.Dbg))
			}
			return nil, fmt.Errorf("send media %s: stuck in upload preview (dbg=%s)", fileName, string(sr.Dbg))
		}
		out["file"] = v
		return out, nil
	}
	v, err := callAgentValue(s.w, "send_message", map[string]any{"text": text})
	if err != nil {
		return nil, err
	}
	out["text"] = v
	return out, nil
}

func (s *mcpServer) dispatch(req rpcRequest) (rpcResponse, bool) {
	resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "wagramdesklite", "version": "1.0.0"},
		}
	case "notifications/initialized", "notifications/cancelled":
		return resp, true
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		resp.Result = map[string]any{"tools": mcpTools()}
	case "tools/call":
		resp.Result = s.callTool(req.Params)
	default:
		resp.Error = &rpcError{Code: -32601, Message: "method not found: " + req.Method}
	}
	return resp, false
}

func (s *mcpServer) callTool(params json.RawMessage) any {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return toolError("invalid params: " + err.Error())
	}
	switch p.Name {
	case "app_status":
		return toolText(s.statusText())
	case "list_chats":
		return s.agentTool("list_chats", p.Arguments)
	case "open_chat":
		return s.agentTool("open_chat", p.Arguments)
	case "read_messages":
		return s.agentTool("read_messages", p.Arguments)
	case "conversation_summary":
		return s.agentTool("conversation_summary", p.Arguments)
	case "media_debug":
		return s.agentTool("media_debug", p.Arguments)
	case "send_message":
		return s.agentTool("send_message", p.Arguments)
	case "export_chat":
		return s.agentTool("read_messages", p.Arguments)
	default:
		return toolError("unknown tool: " + p.Name)
	}
}

// agentTool drives the page through the injected agent and unwraps its
// {ok, value|error} envelope into an MCP tool result.
func (s *mcpServer) agentTool(fn string, args json.RawMessage) any {
	var a any
	if len(args) > 0 {
		if err := json.Unmarshal(args, &a); err != nil {
			a = nil
		}
	}
	res, err := callAgent(s.w, fn, a)
	if err != nil {
		return toolError(err.Error())
	}
	var env struct {
		OK    bool            `json:"ok"`
		Value json.RawMessage `json:"value"`
		Error string          `json:"error"`
	}
	if err := json.Unmarshal(res, &env); err != nil {
		return toolText(string(res))
	}
	if !env.OK {
		return toolError(env.Error)
	}
	return toolText(string(env.Value))
}

func (s *mcpServer) statusText() string {
	info := map[string]any{
		"account": gAccountName,
		"service": gServiceBadge,
	}
	if res, err := callAgent(s.w, "status", nil); err == nil {
		info["page"] = json.RawMessage(res)
	}
	b, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return ""
	}
	return string(b)
}

func toolText(text string) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}}
}

func toolError(text string) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": true,
	}
}

func writeRPC(rw http.ResponseWriter, resp rpcResponse) {
	rw.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(rw).Encode(resp)
}

func mcpTools() []map[string]any {
	return []map[string]any{
		{
			"name":        "app_status",
			"description": "Report which service and account this window shows, and whether the chat page is ready.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			"name":        "list_chats",
			"description": "List the chats visible in the chat list, with name, last-message preview, unread count, and the contact's phone number (empty for groups).",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			"name":        "open_chat",
			"description": "Open a conversation by name so read_messages and send_message can act on it. Matching is case-insensitive.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "The chat name to open, as returned by list_chats."},
				},
				"required": []string{"name"},
			},
		},
		{
			"name":        "read_messages",
			"description": "Read the messages of the currently open conversation, plus a chat object with the contact's name, phone number, and is_saved/is_group flags (phone and is_saved are false/empty for groups).",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit": map[string]any{"type": "integer", "description": "Maximum number of recent messages to return."},
				},
			},
		},
		{
			"name":        "conversation_summary",
			"description": "Summarize the open conversation for reply decisions: the chat object (contact name, phone, is_saved/is_group), last message, whether the last message is incoming (should_reply), incoming vs outgoing counts, and the full message list as context.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit": map[string]any{"type": "integer", "description": "Maximum number of recent messages to include in the summary."},
				},
			},
		},
		{
			"name":        "media_debug",
			"description": "Dump live DOM state for media inputs (temporary diagnostics).",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"clickAttach": map[string]any{"type": "boolean", "description": "Open the attach (plus) menu before dumping."},
					"clickItem":   map[string]any{"type": "string", "description": "Text substring of a menu item to click (input click suppressed)."},
				},
			},
		},
		{
			"name":        "send_message",
			"description": "Send a text message in the currently open conversation.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text": map[string]any{"type": "string", "description": "The message text to send."},
				},
				"required": []string{"text"},
			},
		},
		{
			"name":        "export_chat",
			"description": "Return the messages of the currently open conversation for export.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit": map[string]any{"type": "integer"},
				},
			},
		},
	}
}
