//go:build windows

package app

import (
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	webview2 "github.com/jchv/go-webview2"
)

// agentRequestTimeout bounds how long a Go->JS call waits for the page.
const agentRequestTimeout = 8 * time.Second
// agentRequestTimeoutLong bounds long-running agent calls (media upload and
// send polling can legitimately take tens of seconds).
const agentRequestTimeoutLong = 60 * time.Second

var (
	agentMu      sync.Mutex
	agentPending = map[string]chan string{}
	agentSeq     int64
)

// jsQuote renders s as a JavaScript string literal.
func jsQuote(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(b)
}

// agentReply resolves a pending Go->JS request. The injected script calls the
// bound function of this name with the request id and its JSON result.
func agentReply(id, result string) {
	agentMu.Lock()
	ch := agentPending[id]
	delete(agentPending, id)
	agentMu.Unlock()
	if ch != nil {
		ch <- result
	}
}

// callAgent asks the page's injected agent to run fn with args and waits for
// its JSON result. It must not be called from the UI thread.
func callAgent(w webview2.WebView, fn string, args any) (json.RawMessage, error) {
	return callAgentWithTimeout(w, fn, args, agentRequestTimeout)
}

// callAgentLong is callAgent with the long request timeout, for operations
// that legitimately take tens of seconds (media upload + send polling).
func callAgentLong(w webview2.WebView, fn string, args any) (json.RawMessage, error) {
	return callAgentWithTimeout(w, fn, args, agentRequestTimeoutLong)
}

func callAgentWithTimeout(w webview2.WebView, fn string, args any, timeout time.Duration) (json.RawMessage, error) {
	id := strconv.FormatInt(atomic.AddInt64(&agentSeq, 1), 10)
	ch := make(chan string, 1)
	agentMu.Lock()
	agentPending[id] = ch
	agentMu.Unlock()

	payload, err := json.Marshal(args)
	if err != nil {
		agentMu.Lock()
		delete(agentPending, id)
		agentMu.Unlock()
		return nil, err
	}
	js := "window.wagramAgent && window.wagramAgent.call(" +
		jsQuote(id) + "," + jsQuote(fn) + "," + jsQuote(string(payload)) + ");"
	w.Dispatch(func() { w.Eval(js) })

	select {
	case res := <-ch:
		return json.RawMessage(res), nil
	case <-time.After(timeout):
		agentMu.Lock()
		delete(agentPending, id)
		agentMu.Unlock()
		return nil, errors.New("agent request timed out")
	}
}

// agentScript is injected into every page. It exposes window.wagramAgent, a
// small RPC shim: Go calls wagramAgent.call(id, fn, argsJson); the page runs the
// matching AGENT function and answers via the bound wagramAgentReply(id, json).
const agentScript = `
(function () {
	if (window.wagramAgent) { return; }

	function q(sel) { try { return document.querySelector(sel); } catch (e) { return null; } }
	function qa(sel) { try { return Array.prototype.slice.call(document.querySelectorAll(sel)); } catch (e) { return []; } }
	function first(sels) {
		for (var i = 0; i < sels.length; i++) { var el = q(sels[i]); if (el) { return el; } }
		return null;
	}
	function all(sels) {
		for (var i = 0; i < sels.length; i++) { var els = qa(sels[i]); if (els.length) { return els; } }
		return [];
	}
	function txt(el) { return el ? (el.innerText || el.textContent || '').replace(/\s+/g, ' ').trim() : ''; }
	// WhatsApp's chat rows expose an accessibility label like
	// "23 unread messages Marketplace 23". The current build runs the count
	// and the mute/bell icon ligature text straight into the name
	// ("101 unread messagesGeneralic-notifications-off101"), so strip the
	// leading "N unread message(s)", any trailing icon + repeated count, and
	// any stray variation selectors left behind by emoji in the name.
	function stripUnread(s) {
		s = (s || '').trim();
		var m = s.match(/^\d+\s+unread\s+messages?\s*(.*)$/i);
		if (!m) { return s; }
		return m[1]
			.replace(/(?:\s*ic-[a-z0-9-]*)+\s*\d*$/i, '')
			.replace(/\s+\d+$/, '')
			.replace(/^[\s\uFE0E\uFE0F]+/, '')
			.replace(/[\s\uFE0E\uFE0F]+$/, '');
	}
	// The row's secondary cell packs the last-message preview together with the
// unread badge and the mute/read/media status icons. Those icons are drawn as
// ligatures ("ic-image", "ic-notifications-off", "wds-ic-read") whose text the
// cell's textContent picks up, running them into the preview. Clone the cell,
// drop the unread badge, and drop any element whose whole text is such a
// ligature, so only the real message text is left.
	function previewText(prevEl) {
		if (!prevEl) { return ''; }
		var clone = prevEl.cloneNode(true);
		var badges = clone.querySelectorAll('[data-testid="icon-unread-count"], .unread, .badge');
		for (var i = 0; i < badges.length; i++) {
			if (badges[i].parentNode) { badges[i].parentNode.removeChild(badges[i]); }
		}
		var els = clone.querySelectorAll('*');
		for (var j = els.length - 1; j >= 0; j--) {
			var t = (els[j].textContent || '').trim();
			if (/^(?:wds-)?ic-[a-z0-9-]+$/i.test(t) && els[j].parentNode) {
				els[j].parentNode.removeChild(els[j]);
			}
		}
		return stripUnread(txt(clone)).replace(/\s+/g, ' ').trim();
	}
	// WhatsApp selects a chat on pointer/mouse-down, not on a bare click, so a
	// synthetic click() alone is ignored. Dispatch the whole sequence and let
	// it bubble to the row's handler.
	function fireClick(el) {
		if (!el) { return; }
		try { el.scrollIntoView({ block: 'center' }); } catch (e) {}
		var opts = { bubbles: true, cancelable: true, view: window, detail: 1 };
		try { el.dispatchEvent(new PointerEvent('pointerdown', opts)); } catch (e) {}
		try { el.dispatchEvent(new MouseEvent('mousedown', opts)); } catch (e) {}
		try { el.dispatchEvent(new PointerEvent('pointerup', opts)); } catch (e) {}
		try { el.dispatchEvent(new MouseEvent('mouseup', opts)); } catch (e) {}
		try { el.dispatchEvent(new MouseEvent('click', opts)); } catch (e) {}
	}
	function host() { return location.host || ''; }
	function isWA() { return host().indexOf('whatsapp') !== -1; }
	function isTG() { return host().indexOf('telegram') !== -1; }

	function composer() {
		return first([
			'[data-testid="conversation-compose-box-input"]',
			'footer div[contenteditable="true"]',
			'#message-input div[contenteditable="true"]',
			'.input-field-input[contenteditable="true"]',
			'div[contenteditable="true"][role="textbox"]'
		]);
	}

	function setComposer(text) {
		var box = composer();
		if (!box) { throw new Error('composer not found'); }
		box.focus();
		var sel = window.getSelection();
		var range = document.createRange();
		range.selectNodeContents(box);
		sel.removeAllRanges();
		sel.addRange(range);
		// Lexical does not honour the selection on execCommand('insertText'),
		// so leftover text gets appended to instead of replaced. Clear the box
		// explicitly before typing; the first pass is the keyboard path and
		// the second pass covers any residual DOM the editor left behind.
		try { document.execCommand('delete'); } catch (e) {}
		try { range.deleteContents(); } catch (e) {}
		if (boxText(box) !== '') { box.textContent = ''; }
		document.execCommand('insertText', false, text);
		// execCommand fires its own input event, which is enough for React to
		// enable the send button. Do not re-dispatch a synthetic event whose
		// data carries the text: Lexical reads event.data from it and inserts
		// the text a second time, turning "test" into "testtest".
		return true;
	}

	function sendButton() {
		return first([
			'[data-testid="send"]',
			'button[aria-label="Send"]',
			'button[aria-label="Kirim"]',
			'button[aria-label="Send message"]',
			'button[aria-label="Kirim pesan"]',
			'span[data-icon="send"]',
			'button.send'
		]);
	}

	// WhatsApp's send button reacts to the full pointer sequence rather than a
	// bare click(), the same way its chat rows do. When no button is in the DOM
	// yet, fall back to a complete Enter press on the composer.
	function clickSend() {
		var btn = sendButton();
		if (btn) {
			fireClick(btn.closest('button') || btn);
			return true;
		}
		var box = composer();
		if (box) {
			var opts = { key: 'Enter', code: 'Enter', keyCode: 13, which: 13, bubbles: true, cancelable: true };
			try { box.dispatchEvent(new KeyboardEvent('keydown', opts)); } catch (e) {}
			try { box.dispatchEvent(new KeyboardEvent('keypress', opts)); } catch (e) {}
			try { box.dispatchEvent(new KeyboardEvent('keyup', opts)); } catch (e) {}
			return true;
		}
		return false;
	}

	// The left-pane search filters the chat list and also surfaces contacts
	// you have never chatted with, which is how a chat below the fold (or a
	// brand-new one) becomes reachable. WhatsApp has changed this input
	// repeatedly, so try the known hooks first and then fall back to "the
	// contenteditable (or text input) that is not the composer".
	function searchBox() {
		var named = first([
			'[data-testid="chat-list-search"]',
			'#side div[contenteditable="true"][data-tab="3"]',
			'div[aria-label="Search input textbox"]',
			'input[aria-label="Search input textbox"]',
			'#side input[type="text"]',
			'#side input[type="search"]'
		]);
		if (named) { return named; }
		var comp = composer();
		var boxes = qa('div[contenteditable="true"]');
		for (var i = 0; i < boxes.length; i++) {
			if (boxes[i] !== comp) { return boxes[i]; }
		}
		var inputs = qa('input');
		for (var j = 0; j < inputs.length; j++) {
			var it = (inputs[j].type || '').toLowerCase();
			if (it === 'text' || it === 'search') { return inputs[j]; }
		}
		return null;
	}

	// Type into a contenteditable editor or a native text input. WhatsApp's
	// search is a plain <input> whose value must be written through the native
	// setter so React notices the change; the composer is a contenteditable
	// that takes execCommand. Returns what actually landed, for diagnostics.
	function setBoxText(box, text) {
		box.focus();
		if (box.tagName === 'INPUT' || box.tagName === 'TEXTAREA') {
			var proto = box.tagName === 'TEXTAREA' ? window.HTMLTextAreaElement.prototype : window.HTMLInputElement.prototype;
			var desc = Object.getOwnPropertyDescriptor(proto, 'value');
			if (desc && desc.set) { desc.set.call(box, text); } else { box.value = text; }
			box.dispatchEvent(new Event('input', { bubbles: true }));
			return boxText(box);
		}
		var sel = window.getSelection();
		var range = document.createRange();
		range.selectNodeContents(box);
		sel.removeAllRanges();
		sel.addRange(range);
		var ok = false;
		try { ok = document.execCommand('insertText', false, text); } catch (e) {}
		if (!ok || boxText(box) === '') {
			try {
				range.deleteContents();
				range.insertNode(document.createTextNode(text));
			} catch (e2) {
				box.textContent = text;
			}
			try { box.dispatchEvent(new InputEvent('input', { bubbles: true, inputType: 'insertText', data: text })); } catch (e3) {}
		}
		return boxText(box);
	}

	function boxText(box) {
		var v = box.value !== undefined ? box.value : (box.innerText || box.textContent || '');
		return (v || '').trim();
	}

	var AGENT = {};

	AGENT.status = function () {
		return {
			service: isTG() ? 'telegram' : (isWA() ? 'whatsapp' : 'unknown'),
			ready: !!composer(),
			title: document.title
		};
	};

	AGENT.list_chats = function () {
		var rows = all([
			'#pane-side div[data-id]',
			'[data-testid="cell-frame-container"]',
			'#pane-side [role="listitem"]',
			'.chat-list .ListItem',
			'a.chat-item'
		]);
		var out = [];
		for (var i = 0; i < rows.length && i < 200; i++) {
			var r = rows[i];
			var nameEl = r.querySelector('[data-testid="cell-frame-title"]') || r.querySelector('span[title]') || r.querySelector('.title') || r.querySelector('.user-title');
			var prevEl = r.querySelector('[data-testid="cell-frame-secondary"]') || r.querySelector('.preview') || r.querySelector('.last-message');
			var unreadEl = r.querySelector('[data-testid="icon-unread-count"]') || r.querySelector('.unread') || r.querySelector('.badge');
			var idAttr = r.getAttribute('data-id');
			if (!idAttr && r.closest) {
				var anc = r.closest('[data-id]');
				if (anc) { idAttr = anc.getAttribute('data-id'); }
			}
			if (!idAttr) {
				var idNode = r.querySelector('[data-id]');
				if (idNode) { idAttr = idNode.getAttribute('data-id'); }
			}
			var av = r.querySelector('img');
			var name = stripUnread(txt(nameEl));
			out.push({
				name: name,
				preview: previewText(prevEl),
				unread: txt(unreadEl),
				id: idAttr || '',
				phone: phoneFromText(name),
				avatar: av ? av.getAttribute('data-plain-text') || av.src || '' : '',
				active: r.getAttribute('aria-selected') === 'true' || ('' + r.className).indexOf('active') !== -1
			});
		}
		return { chats: out };
	};

	// Report chats that currently carry an unread badge: name, preview of the
	// newest message, and unread count. The Go side diffs this against what it
	// already saw and fires webhooks only for the deltas.
	AGENT.incoming_events = function () {
		var r = AGENT.list_chats();
		var out = [];
		var chats = (r && r.chats) || [];
		for (var i = 0; i < chats.length; i++) {
			out.push({
				id: chats[i].id || '',
				name: chats[i].name,
				preview: chats[i].preview,
				phone: chats[i].phone || '',
				avatar: chats[i].avatar || '',
				unread: parseInt(chats[i].unread, 10) || 0
			});
		}
		return { chats: out };
	};

	// Open a conversation by name so the tools that act on the open
	// conversation (read_messages, send_message) can target it. Matching is
	// case-insensitive and falls back to a substring hit.
	AGENT.open_chat = function (args) {
		var want = (args && args.name) ? String(args.name).trim().toLowerCase() : '';
		if (!want) { throw new Error('name required'); }
		var rows = all([
			'[data-testid="cell-frame-container"]',
			'#pane-side [role="listitem"]',
			'.chat-list .ListItem',
			'a.chat-item'
		]);
		for (var i = 0; i < rows.length && i < 200; i++) {
			var r = rows[i];
			var nameEl = r.querySelector('[data-testid="cell-frame-title"]') || r.querySelector('span[title]') || r.querySelector('.title') || r.querySelector('.user-title');
			var name = stripUnread(txt(nameEl));
			if (name.toLowerCase() === want || name.toLowerCase().indexOf(want) !== -1) {
				fireClick(r.closest('[role="button"]') || r);
				return { opened: true, name: name };
			}
		}
		// Not visible: type it into the search box so the row renders, then
		// the caller retries open_chat once the list has filtered.
		var box = searchBox();
		if (box) {
			setBoxText(box, args.name);
			return { opened: false, searched: true, name: args.name };
		}
		return { opened: false, name: (args && args.name) || '' };
	};

	// WhatsApp's current build drops the message-in/message-out classes and
	// marks each text bubble with data-pre-plain-text instead, so try that
	// first and fall back to the older hooks. Take whichever selector yields
	// the most rows rather than the first that matches anything.
	function messageRows() {
		var sels = [
			'[data-pre-plain-text]',
			'div.message-in, div.message-out',
			'[data-testid="msg-container"]',
			'.MessageList .message',
			'.bubbles .message'
		];
		var best = [];
		for (var i = 0; i < sels.length; i++) {
			var els = qa(sels[i]);
			if (els.length > best.length) { best = els; }
		}
		return best;
	}

	function isScrollable(el) {
		if (!el || el.scrollHeight <= el.clientHeight + 20) { return false; }
		var oy = '';
		try { oy = window.getComputedStyle(el).overflowY; } catch (e) {}
		return oy === 'auto' || oy === 'scroll';
	}

	function scrollableWithin(root) {
		if (!root) { return null; }
		if (isScrollable(root)) { return root; }
		var nodes = root.querySelectorAll('div');
		for (var i = 0; i < nodes.length; i++) {
			if (isScrollable(nodes[i])) { return nodes[i]; }
		}
		return null;
	}

	function messageScroller() {
		var found = scrollableWithin(q('[data-testid="conversation-panel-messages"]'));
		if (found) { return found; }
		return scrollableWithin(q('#main'));
	}

	// Direction is marked by the bubble tail icon: tail-out for a message we
	// sent, tail-in for one we received. Walk up from the text node until an
	// ancestor owns a tail, which is the message row.
	function rowOutgoing(el) {
		var node = el;
		for (var i = 0; i < 12 && node; i++) {
			var out = node.querySelectorAll ? node.querySelectorAll('[data-icon="tail-out"]').length : 0;
			var inn = node.querySelectorAll ? node.querySelectorAll('[data-icon="tail-in"]').length : 0;
			if (out + inn > 0) { return out >= inn; }
			node = node.parentElement;
		}
		return false;
	}

	// The bubble's own timestamp is embedded in data-pre-plain-text as
	// "[14:37, 10/5/2026] sender: "; fall back to the meta node.
	function rowTime(el) {
		var ptt = el.getAttribute ? (el.getAttribute('data-pre-plain-text') || '') : '';
		var m = ptt.match(/^\[(\d{1,2}:\d{2})/);
		if (m) { return m[1]; }
		var meta = el.querySelector('[data-testid="msg-meta"]') || el.querySelector('.meta') || el.querySelector('.time');
		return txt(meta);
	}

	function msgSig(r) {
		var body = r.querySelector('.selectable-text') || r.querySelector('[data-testid="msg-text"]') || r.querySelector('.text-content');
		return txt(body) + '\u0001' + rowTime(r);
	}

	function collectMessages(rows, limit) {
		var out = [];
		var start = Math.max(0, rows.length - limit);
		for (var i = start; i < rows.length; i++) {
			var r = rows[i];
			var body = r.querySelector('.selectable-text') || r.querySelector('[data-testid="msg-text"]') || r.querySelector('.text-content');
			out.push({ text: txt(body || r), time: rowTime(r), outgoing: rowOutgoing(r) });
		}
		return out;
	}

	// WhatsApp does not expose a chat's JID in the DOM: there is no data-id
	// anywhere in this build, and window.Store is not reachable. The phone
	// number is only present as visible text. For an unsaved contact the chat
	// title is the raw number; for a saved contact it lives in the Contact
	// info drawer, which we must open, read and close.
	//
	// phoneFromText reduces a rendered number ("+62 812-2681-5608") to digits
	// ("6281226815608"); '' when the text is not a phone number.
	function phoneFromText(t) {
		var m = ('' + (t || '')).match(/\+?\d[\d\s()\-]{7,}/);
		if (!m) { return ''; }
		var d = m[0].replace(/[^0-9]/g, '');
		return d.length >= 8 ? d : '';
	}

	// Read the currently open Contact info drawer: whether it is a group, and
	// the individual contact's phone number. A group drawer lists member
	// numbers an individual already has, so a group never yields a phone.
	function drawerInfo() {
		var drawer = q('[data-testid="chat-info-drawer"]');
		if (!drawer) { return { open: false, group: false, phone: '' }; }
		var group = !!drawer.querySelector('[data-testid="group-info-header"]');
		var phone = '';
		if (!group) {
			var els = drawer.querySelectorAll('*');
			for (var i = 0; i < els.length; i++) {
				if (els[i].childElementCount > 0) { continue; }
				var p = phoneFromText((els[i].textContent || '').trim());
				if (p) { phone = p; break; }
			}
		}
		return { open: true, group: group, phone: phone };
	}

	// Identify the open conversation from the header title. An unsaved
	// contact's title is the raw number, so is_saved starts false (tentative);
	// a saved contact's title is a name, and the Contact info drawer then
	// confirms it (and rules out a group).
	function currentChat() {
		var titleEl = q('[data-testid="conversation-info-header-chat-title"]');
		var name = titleEl ? txt(titleEl) : '';
		return { name: name, phone: phoneFromText(name), is_saved: false, is_group: false };
	}

	// name -> {phone, is_group, is_saved} once the drawer has been read, so a
	// repeat read of the same chat does not reopen the drawer.
	var chatInfoCache = {};

	// Resolve the open chat with its phone number, saved flag and group flag,
	// then call cb(chat). Number from the title for an unsaved contact;
	// otherwise the Contact info drawer is opened, read and closed (once per
	// chat). is_saved is only asserted once the drawer confirms a non-group.
	function chatWithPhone(cb) {
		var chat = currentChat();
		if (!chat.name) { cb(chat); return; }
		if (chat.phone) { cb(chat); return; }
		var cached = chatInfoCache[chat.name];
		if (cached) {
			chat.phone = cached.phone;
			chat.is_group = cached.is_group;
			chat.is_saved = cached.is_saved;
			cb(chat);
			return;
		}
		var hdr = q('[data-testid="conversation-info-header"]') || q('#main header');
		var btn = hdr ? (hdr.closest('[role="button"]') || hdr.querySelector('[role="button"]') || hdr) : null;
		if (!btn) { cb(chat); return; }
		try { btn.click(); } catch (e) {}
		setTimeout(function () {
			var info = drawerInfo();
			var closer = info.open ? (q('[data-testid="chat-info-drawer"]') || document).querySelector('[aria-label="Close"]') : null;
			if (closer) { try { closer.click(); } catch (e) {} }
			chat.is_group = info.group;
			chat.is_saved = info.open && !info.group;
			chat.phone = info.phone;
			chatInfoCache[chat.name] = { phone: chat.phone, is_group: chat.is_group, is_saved: chat.is_saved };
			cb(chat);
		}, 750);
	}

	// Read the open conversation. WhatsApp keeps only a window of message rows
	// in the DOM, so when the caller wants more than is rendered we scroll the
	// pane toward the top, accumulating rows until we have enough or run out.
	AGENT.read_messages = function (args) {
		var limit = (args && args.limit) ? args.limit : 50;
		var scroller = messageScroller();
		if (!scroller) {
			return new Promise(function (resolve) {
				var msgs = collectMessages(messageRows(), limit);
				chatWithPhone(function (chat) { resolve({ messages: msgs, chat: chat }); });
			});
		}
		return new Promise(function (resolve) {
			var seen = {};
			var acc = [];
			function snapshot() {
				var rows = messageRows();
				for (var i = rows.length - 1; i >= 0; i--) {
					var r = rows[i];
					var sig = msgSig(r);
					if (!seen[sig]) { seen[sig] = 1; acc.push(r); }
				}
			}
			function finish() {
				try { scroller.scrollTop = scroller.scrollHeight; } catch (e) {}
				acc.reverse();
				var msgs = collectMessages(acc, limit);
				chatWithPhone(function (chat) { resolve({ messages: msgs, chat: chat }); });
			}
			// Bound the loop by wall-clock time, not step count, so a busy page
			// (each setTimeout delayed) can never push us past the RPC timeout.
			// Any throw also resolves instead of hanging the request.
			var started = Date.now();
			function step() {
				try {
					snapshot();
				} catch (e) {
					finish();
					return;
				}
				if (acc.length >= limit || Date.now() - started > 3000) { finish(); return; }
				try { scroller.scrollTop = Math.max(0, scroller.scrollTop - scroller.clientHeight); } catch (e2) {}
				setTimeout(step, 250);
			}
			step();
		});
	};

	// Summarize the open conversation for the reply flow: the last message,
	// whether a reply is warranted (last message is incoming), and the
	// incoming vs outgoing split. The full message list is returned too so
	// the caller has complete context instead of re-reading on its own.
	AGENT.conversation_summary = function (args) {
		var limit = (args && args.limit) ? args.limit : 50;
		var r = AGENT.read_messages({ limit: limit });
		function summarize(msgs, chat) {
			msgs = msgs || [];
			var incoming = 0, outgoing = 0;
			for (var i = 0; i < msgs.length; i++) {
				if (msgs[i].outgoing) { outgoing++; } else { incoming++; }
			}
			var last = msgs.length ? msgs[msgs.length - 1] : null;
			return {
				chat: chat || currentChat(),
				total: msgs.length,
				incoming_count: incoming,
				outgoing_count: outgoing,
				last_message: last,
				should_reply: !!(last && !last.outgoing),
				messages: msgs
			};
		}
		if (r && typeof r.then === 'function') {
			return r.then(function (v) { return summarize((v && v.messages) || [], (v && v.chat) || null); });
		}
		return summarize((r && r.messages) || [], (r && r.chat) || null);
	};

	// Give React a tick to register the typed text (and enable the send
	// button) before clicking, then confirm the composer emptied, which is
	// the real signal that the message left.
	AGENT.send_message = function (args) {
		if (!args || !args.text) { throw new Error('text required'); }
		setComposer(args.text);
		return new Promise(function (resolve) {
			setTimeout(function () {
				var clicked = clickSend();
				setTimeout(function () {
					var box = composer();
					var left = box ? boxText(box) : '';
					resolve({ sent: clicked && left === '', remaining: left });
				}, 150);
			}, 150);
		});
	};

	AGENT.composer_set = function (args) {
		return { ok: setComposer((args && args.text) || '') };
	};

	AGENT.composer_ready = function () {
		return { ready: !!composer() };
	};

	// chat_open reports whether a real conversation panel is mounted.
	// composer() alone is a false positive on the home screen: its loose
	// selectors also match the search box (div[contenteditable][role=textbox]),
	// which made the send flow race the SPA navigation and time out.
	AGENT.chat_open = function () {
		var strong = !!q('[data-testid="conversation-compose-box-input"]');
		var footerBox = !!q('footer div[contenteditable="true"]');
		return { open: strong || footerBox, strong: strong, footer: footerBox, href: location.href };
	};

	AGENT.page_state = function () {
		return { href: location.href, title: document.title, ready: !!composer() };
	};

	// open_chat_phone opens a chat by phone number WITHOUT any navigation:
	// it types the digits into the chat-list search box and clicks the first
	// result row (for a phone-number search the direct conversation is always
	// the top hit). The old deep-link approach (location.assign to
	// /send?phone=) forces a full SPA reload and bounces back to home when
	// the store is not ready, which the user sees as endless reloading.
	// Note: rows show the contact NAME, never the number, so matching digits
	// against row text cannot work — clicking the first row is the reliable
	// signal.
	AGENT.open_chat_phone = function (args) {
		var phone = ((args && args.phone) || '').replace(/[^0-9]/g, '');
		if (!phone) { return { navigated: false }; }
		function isOpen() {
			return !!q('[data-testid="conversation-compose-box-input"]') || !!q('footer div[contenteditable="true"]');
		}
		function rows() {
			return all([
				'[data-testid="cell-frame-container"]',
				'#pane-side [role="listitem"]',
				'.chat-list .ListItem',
				'a.chat-item'
			]);
		}
		function clickRow(row) {
			fireClick(row.closest && row.closest('[role="button"]') ? row.closest('[role="button"]') : row);
		}
		return new Promise(function (resolve) {
			function done(opened, via) {
				try {
					var box = searchBox();
					if (box && boxText(box)) { setBoxText(box, ''); }
				} catch (e) {}
				resolve({ navigated: opened, opened: opened, via: via, phone: phone });
			}
			var box = searchBox();
			if (!box) { resolve({ navigated: false, reason: 'no-search-box', phone: phone }); return; }
			setBoxText(box, phone);
			setTimeout(function () {
				var list = rows();
				if (!list.length) { done(false, 'search-no-results'); return; }
				clickRow(list[0]);
				setTimeout(function () { done(isOpen(), 'search-first'); }, 1200);
			}, 1800);
		});
	};

	// media_debug dumps live DOM state for diagnosing stuck media previews.
	// args.clickAttach opens the attach (plus) menu and dumps the menu items.
	// args.clickItem="<text>" also clicks a matching menu item (with the file
	// input's own click() suppressed so no OS dialog opens) and reports which
	// input accept string that item tried to open, plus any showOpenFilePicker
	// calls (the File System Access API path modern WhatsApp Web may use).
	AGENT.media_debug = function (args) {
		function inputsDump() {
			var out = [];
			try {
				var els = document.querySelectorAll('input[type="file"]');
				for (var i = 0; i < els.length; i++) {
					var par = els[i].closest('[role="menuitem"],[role="button"],button,[data-testid]');
					out.push({
						accept: els[i].getAttribute('accept') || '',
						multiple: !!els[i].multiple,
						files: (els[i].files ? els[i].files.length : 0),
						par: par ? ((par.getAttribute('aria-label') || '') + '|' + (par.getAttribute('data-testid') || '')) : ''
					});
				}
			} catch (e0) {}
			return out;
		}
		function footerDump() {
			var out = [];
			try {
				var btns = document.querySelectorAll('footer [role="button"],footer button');
				for (var i = 0; i < btns.length && out.length < 30; i++) {
					var ic = '';
					try { var sp = btns[i].querySelector('span[data-icon]'); if (sp) { ic = sp.getAttribute('data-icon') || ''; } } catch (e1) {}
					out.push({ aria: (btns[i].getAttribute('aria-label') || ''), tid: (btns[i].getAttribute('data-testid') || ''), icon: ic });
				}
			} catch (e2) {}
			return out;
		}
		function menuDump() {
			var out = [];
			try {
				var items = document.querySelectorAll('[role="menuitem"],[role="menu"] *,[data-testid^="mi-"]');
				for (var i = 0; i < items.length && out.length < 40; i++) {
					var t = (items[i].textContent || '').trim();
					var al = items[i].getAttribute('aria-label') || '';
					if (!t && !al) { continue; }
					out.push({ tid: (items[i].getAttribute('data-testid') || ''), aria: al, text: t.slice(0, 40) });
				}
				// Attach-menu items may be plain div[role=button]/li rather than
				// role=menuitem, so also scan those outside the footer.
				var btns = document.querySelectorAll('div[role="button"],li');
				for (var b = 0; b < btns.length && out.length < 60; b++) {
					if (btns[b].closest && btns[b].closest('footer')) { continue; }
					var bt = (btns[b].textContent || '').trim();
					var ba = btns[b].getAttribute('aria-label') || '';
					if (!ba && (!bt || bt.length > 40)) { continue; }
					out.push({ tid: (btns[b].getAttribute('data-testid') || ''), aria: ba, text: bt.slice(0, 40) });
				}
			} catch (e3) {}
			return out;
		}
		function base() {
			var comp = composer();
			return { href: location.href, title: document.title, inputs: inputsDump(), footer: footerDump(), menu: menuDump(), composer: comp ? txt(comp) : '' };
		}
		function findAttach() {
			var btns = document.querySelectorAll('footer [role="button"],footer button');
			for (var i = 0; i < btns.length; i++) {
				var ic = '', al = (btns[i].getAttribute('aria-label') || '').toLowerCase();
				try { var sp = btns[i].querySelector('span[data-icon]'); if (sp) { ic = sp.getAttribute('data-icon') || ''; } } catch (e) {}
				if (ic === 'plus' || ic === 'plus-rounded' || al.indexOf('attach') !== -1 || al.indexOf('lampirkan') !== -1) { return btns[i]; }
			}
			return null;
		}
		if (!(args && args.clickAttach)) { return base(); }
		var btn = findAttach();
		var clickedAttach = false, clickMethod = '';
		if (btn) { try { btn.click(); clickedAttach = true; clickMethod = 'native'; } catch (e4) {} }
		var want = (args && args.clickItem) || '';
		if (want) {
			// Suppress the input's own click() so probing a menu item never
			// opens a native OS file dialog, and record what it tried to open.
			window.__dbgOrigInputClick = HTMLInputElement.prototype.click;
			window.__dbgInputClicks = [];
			HTMLInputElement.prototype.click = function () {
				if (this.type === 'file') { window.__dbgInputClicks.push(this.getAttribute('accept') || ''); return; }
				return window.__dbgOrigInputClick.apply(this, arguments);
			};
			window.__dbgOrigPicker = window.showOpenFilePicker;
			window.__dbgPickerCalls = 0;
			try { window.showOpenFilePicker = function () { window.__dbgPickerCalls++; return Promise.reject(new Error('blocked')); }; } catch (e5) {}
		}
		return new Promise(function (resolve) {
			function finish(afterMenu, method, itemClicked) {
				var want2 = (args && args.clickItem) || '';
				if (!want2) { resolve({ clickedAttach: clickedAttach, clickMethod: method, afterMenu: afterMenu }); return; }
				var itemHit = itemClicked || '';
				var items = document.querySelectorAll('[role="menuitem"],[role="menu"] *,[data-testid^="mi-"],div[role="button"],li');
				for (var j = 0; j < items.length; j++) {
					if (items[j].closest && items[j].closest('footer')) { continue; }
					var t = (items[j].textContent || '').toLowerCase();
					var a = (items[j].getAttribute('aria-label') || '').toLowerCase();
					if (t.indexOf(String(want2).toLowerCase()) !== -1 || a.indexOf(String(want2).toLowerCase()) !== -1) { try { fireClick(items[j]); itemHit = (items[j].textContent || items[j].getAttribute('aria-label') || '').trim().slice(0, 40); } catch (e6) {} break; }
				}
				setTimeout(function () {
					var res = { clickedAttach: clickedAttach, clickMethod: method, clickItem: want2, itemClicked: itemHit, inputClickHits: window.__dbgInputClicks || [], pickerCalls: window.__dbgPickerCalls || 0, afterMenu: afterMenu, afterItem: base() };
					try { HTMLInputElement.prototype.click = window.__dbgOrigInputClick; } catch (e7) {}
					try { window.showOpenFilePicker = window.__dbgOrigPicker; } catch (e8) {}
					resolve(res);
				}, 1000);
			}
			setTimeout(function () {
				var afterMenu = base();
				// If the native click did not open a menu, try the full pointer
				// sequence before giving up.
				if (afterMenu.menu.length === 0 && btn) {
					try { fireClick(btn); } catch (e9) {}
					setTimeout(function () { finish(base(), 'native+fire', ''); }, 1200);
					return;
				}
				finish(afterMenu, clickMethod, '');
			}, 1200);
		});
	};

	// send_media attaches one file (base64) to the open chat and sends it,
	// with an optional text caption. Three modes:
	//   sticker  - WhatsApp keeps a persistent input[accept="image/*"] in the
	//              compose area; injecting an image there produces a sticker.
	//   media    - open the attach menu and choose "Photos & videos".
	//   document - open the attach menu and choose "Document".
	// The menu items are div[role=button] with an aria-label. Choosing one makes
	// WhatsApp open its own file input for that kind; we intercept the input's
	// click() (or showOpenFilePicker) and hand it our File instead of the OS
	// dialog, so the resulting message type matches the menu choice. Returns a
	// Promise; the Go bridge unwraps it.
	AGENT.send_media = function (args) {
		var b64 = (args && args.data) || '';
		var name = (args && args.filename) || 'upload.bin';
		var mime = (args && args.mime) || 'application/octet-stream';
		var caption = (args && args.caption) || '';
		var asMode = ((args && args.as) || '').toLowerCase();
		if (!b64) { return { sent: false, error: 'data required' }; }
		var bin;
		try { bin = atob(b64); } catch (e) { return { sent: false, error: 'bad base64' }; }
		var bytes = new Uint8Array(bin.length);
		for (var i = 0; i < bin.length; i++) { bytes[i] = bin.charCodeAt(i); }
		var file;
		try { file = new File([bytes], name, { type: mime }); }
		catch (e) { return { sent: false, error: 'file ctor: ' + e }; }

		var wantSticker = asMode === 'sticker';
		var wantDoc = asMode === 'document' || asMode === 'file' || (asMode === '' && mime.indexOf('image/') !== 0);
		// Humanizer. Each stage waits a configurable time, and every wait is
		// jittered so the sequence of clicks does not look like a bot on a
		// metronome. Callers tune it per request via args.humanizer:
		//   base   - default for every stage below (ms)
		//   jitter - random fraction added to each wait, 0..1
		//   open   - pause before opening the attach menu (ms)
		//   item   - pause before clicking the menu item, and between retries (ms)
		//   send   - pause after the preview mounts, before clicking send (ms)
		//   typing - per-character delay while typing the caption (ms, 0 = instant)
		// args.delay is kept as a backward-compatible alias for base.
		var hum = (args && args.humanizer) || {};
		function hnum(v, d) { var n = parseInt(v, 10); return (isFinite(n) && n >= 0) ? n : d; }
		var humBase = hnum((args && args.delay), hnum(hum.base, 1500));
		var humJit = (hum.jitter === undefined || hum.jitter === null) ? 0.6 : Math.min(1, Math.max(0, parseFloat(hum.jitter) || 0));
		var openMs = hnum(hum.open, humBase);
		var itemMs = hnum(hum.item, humBase);
		var sendMs = hnum(hum.send, humBase);
		var typingMs = hnum(hum.typing, 0);
		function jit(ms) { return ms > 0 ? ms + Math.floor(Math.random() * Math.ceil(ms * humJit)) : 0; }

		function injectInto(input) {
			var dt = new DataTransfer();
			dt.items.add(file);
			input.files = dt.files;
			input.dispatchEvent(new Event('change', { bubbles: true }));
		}
		function attachBtn() {
			var btns = document.querySelectorAll('footer [role="button"],footer button');
			for (var i = 0; i < btns.length; i++) {
				var ic = '', al = (btns[i].getAttribute('aria-label') || '').toLowerCase();
				try { var sp = btns[i].querySelector('span[data-icon]'); if (sp) { ic = sp.getAttribute('data-icon') || ''; } } catch (e) {}
				if (ic === 'ic-attach-file' || al === 'attach' || al.indexOf('lampirkan') !== -1) { return btns[i]; }
			}
			return null;
		}
		function menuItem(label) {
			// Match only the attach-menu items, by their aria-label (the DOM
			// probe confirmed they carry aria-label "Document" / "Photos &
			// videos"). A textContent fallback matched document message bubbles
			// already in the chat, so the menu never opened and the send stuck.
			var els = document.querySelectorAll('div[role="button"],li,[role="menuitem"]');
			var want = label.toLowerCase();
			for (var j = 0; j < els.length; j++) {
				if (els[j].closest && els[j].closest('footer')) { continue; }
				var al = (els[j].getAttribute('aria-label') || '').toLowerCase();
				if (al === want || al.indexOf(want) === 0) { return els[j]; }
			}
			return null;
		}
		function stickerInput() {
			var els = document.querySelectorAll('input[type="file"]');
			for (var i = 0; i < els.length; i++) {
				var acc = (els[i].getAttribute('accept') || '').toLowerCase();
				if (acc.indexOf('image') !== -1 && acc.indexOf('video') === -1) { return els[i]; }
			}
			return els.length ? els[0] : null;
		}
		// typeCaption types the caption one character at a time with a human
		// per-key delay, then calls done. With typing off it inserts the whole
		// string in one go.
		function typeCaption(box, text, done) {
			var i = 0;
			(function step() {
				if (i >= text.length) { done(); return; }
				try { document.execCommand('insertText', false, text.charAt(i)); } catch (e) {}
				i++;
				setTimeout(step, typingMs + Math.floor(Math.random() * Math.ceil(typingMs * 0.5)));
			})();
		}
		// pollSend waits for WhatsApp's media/document preview, inserts the
		// caption once the caption box mounts, waits a human pause after the
		// send button appears, clicks it, and resolves when the preview closes.
		function pollSend(resolve, via) {
			var tries = 0, clicked = false, btnSel = '', capDone = false, capBusy = false, seenAt = 0;
			// One jittered send pause for this call, applied once the send
			// button is on screen (so a big upload is not clicked too early).
			var sendDelay = jit(sendMs);
			// The poll window must outlast the send pause and any caption typing
			// without hitting the RPC timeout.
			var maxTries = 60 + Math.ceil((sendDelay + typingMs * caption.length) / 500);
			var timer = setInterval(function () {
				tries++;
				if (caption && !capDone && !capBusy) {
					var capBox = first(['div[aria-label="Add a caption"]', '[data-testid="media-caption-input"]', 'div[role="textbox"][aria-label*="caption"]']);
					if (capBox) {
						capBusy = true;
						try {
							capBox.focus();
							try { document.execCommand('selectAll', false, null); } catch (eCap1) {}
							if (typingMs > 0) {
								typeCaption(capBox, caption, function () { capBusy = false; capDone = true; });
							} else {
								document.execCommand('insertText', false, caption);
								capBusy = false; capDone = true;
							}
						} catch (eCap) { capBusy = false; capDone = true; }
					}
				}
				var btn = first([
					'[data-testid="send"]',
					'div[role="button"][aria-label="Send"]',
					'button[aria-label="Send"]',
					'button[aria-label="Kirim"]',
					'span[data-icon="wds-ic-send-filled"]',
					'span[data-icon="send"]'
				]);
				if (btn) {
					if (!seenAt) { seenAt = Date.now(); }
					// Do not send mid-typing, and let the preview settle first.
					if (!capBusy && !clicked && Date.now() - seenAt >= sendDelay) {
						var sels = ['[data-testid="send"]', 'div[role="button"][aria-label="Send"]', 'button[aria-label="Send"]', 'button[aria-label="Kirim"]', 'span[data-icon="wds-ic-send-filled"]', 'span[data-icon="send"]'];
						for (var si = 0; si < sels.length; si++) { try { if (document.querySelector(sels[si])) { btnSel = sels[si]; break; } } catch (eSi) {} }
						var el = btn.closest ? (btn.closest('[role="button"],button') || btn) : btn;
						try { fireClick(el); } catch (e3) {}
						clicked = true;
					}
				}
				// The media-preview marker alone is not proof of completion (it
				// is usually absent from the DOM), so also require the media
				// send button to be gone.
				var panelGone = !first([
					'[data-testid="media-preview"]',
					'[data-testid="preview"]',
					'div[role="button"][aria-label="Send"]',
					'span[data-icon="wds-ic-send-filled"]'
				]);
				if (clicked && panelGone && tries > 2) {
					clearInterval(timer);
					resolve({ sent: true, file: name, as: asMode, via: via });
				}
				if (tries > maxTries) {
					clearInterval(timer);
					resolve({ sent: false, file: name, stuck: true, clicked: clicked, btnSel: btnSel, as: asMode, via: via, tries: tries, dbg: { href: location.href, title: document.title } });
				}
			}, 500);
		}

		if (wantSticker) {
			var sIn = stickerInput();
			if (!sIn) { return { sent: false, error: 'sticker input not found' }; }
			try { injectInto(sIn); } catch (e) { return { sent: false, error: 'inject: ' + e }; }
			return new Promise(function (resolve) { pollSend(resolve, 'sticker:direct'); });
		}

		// media / document: open the attach menu and choose the matching item.
		var label = wantDoc ? 'document' : 'photos';
		var ab = attachBtn();
		if (!ab) { return { sent: false, error: 'attach button not found' }; }
		var origClick = HTMLInputElement.prototype.click;
		var origPicker = window.showOpenFilePicker;
		var injected = false, hits = [];
		HTMLInputElement.prototype.click = function () {
			if (this.type === 'file') {
				hits.push(this.getAttribute('accept') || '');
				if (!injected) { injected = true; try { injectInto(this); } catch (e) {} }
				return;
			}
			return origClick.apply(this, arguments);
		};
		try { window.showOpenFilePicker = function () { return Promise.resolve([{ getFile: function () { return Promise.resolve(file); } }]); }; } catch (eP) {}
		return new Promise(function (resolve) {
			var waited = 0, opened = false;
			function tryItem() {
				var item = menuItem(label);
				// Human pause first, then open the menu only when the item is
				// not already on screen (a second attach click would close it).
				if (!item && !opened) {
					opened = true;
					try { ab.click(); } catch (eA) {}
					setTimeout(tryItem, jit(itemMs));
					return;
				}
				// Wait for the menu to mount before clicking: a click on a
				// not-yet-rendered item silently falls through to the sticker
				// input, which sends a document as a sticker.
				if (!item && waited < 8) { waited++; setTimeout(tryItem, jit(itemMs)); return; }
				var itemHit = item ? (item.getAttribute('aria-label') || item.textContent || '').trim().slice(0, 40) : '';
				if (item) { try { fireClick(item); } catch (eI) {} }
				setTimeout(function () {
					try { HTMLInputElement.prototype.click = origClick; } catch (eR1) {}
					try { window.showOpenFilePicker = origPicker; } catch (eR2) {}
					if (!injected) {
						// The item may not have called input.click(); fall back to
						// injecting into the input whose accept matches the kind.
						// A document input accepts "*" (all files) or an
						// application/* type; the sticker input's "image/*" must
						// not match, or a document would be sent as a sticker.
						var els = document.querySelectorAll('input[type="file"]');
						for (var m = 0; m < els.length; m++) {
							var a2 = (els[m].getAttribute('accept') || '').toLowerCase();
							var isDocA = a2 === '*' || a2.indexOf('*/*') !== -1 || a2.indexOf('application/') !== -1;
							if (wantDoc ? isDocA : (a2.indexOf('image') !== -1 && a2.indexOf('video') !== -1)) {
								try { injectInto(els[m]); injected = true; } catch (eF) {}
								break;
							}
						}
					}
					pollSend(resolve, (wantDoc ? 'document' : 'media') + ':menu=' + itemHit + ' hits=' + hits.join(',') + ' injected=' + injected + ' waited=' + waited);
				}, jit(itemMs));
			}
			setTimeout(tryItem, jit(openMs));
		});
	};

	AGENT.export_chat = function (args) {
		return AGENT.read_messages(args);
	};

	// Quick replies: a /token that exactly matches a saved shortcut is swapped
	// for its text as the user types. The map is pushed from Go.
	var quickReplies = {};

	document.addEventListener('input', function (ev) {
		var box = composer();
		if (!box || ev.target !== box) { return; }
		var t = (box.innerText || box.textContent || '').trim();
		if (!t || t.charAt(0) !== '/') { return; }
		var repl = quickReplies[t];
		if (typeof repl === 'string' && repl) { setComposer(repl); }
	}, true);

	window.wagramAgent = {
		setQuickReplies: function (json) {
			try { quickReplies = JSON.parse(json) || {}; } catch (e) { quickReplies = {}; }
		},
		readMessages: function (limit) {
			try {
				var r = AGENT.read_messages({ limit: limit });
				if (r && typeof r.then === 'function') {
					return r.then(function (v) { return JSON.stringify(v); },
						function () { return '{"messages":[]}'; });
				}
				return Promise.resolve(JSON.stringify(r));
			} catch (e) { return Promise.resolve('{"messages":[]}'); }
		},
		setComposer: function (text) {
			try { setComposer(text); return true; } catch (e) { return false; }
		},
		call: function (id, fn, argsJson) {
			function reply(result) {
				try { window.wagramAgentReply(id, JSON.stringify(result)); } catch (e2) {}
			}
			try {
				var f = AGENT[fn];
				if (typeof f !== 'function') { throw new Error('unknown agent fn: ' + fn); }
				var args = argsJson ? JSON.parse(argsJson) : {};
				var r = f(args);
				if (r && typeof r.then === 'function') {
					r.then(function (v) { reply({ ok: true, value: v }); },
						function (e) { reply({ ok: false, error: String((e && e.message) || e) }); });
				} else {
					reply({ ok: true, value: r });
				}
			} catch (e) {
				reply({ ok: false, error: String((e && e.message) || e) });
			}
		}
	};

	// Pull the saved shortcuts once the page is alive. Doing it here rather
	// than from Go avoids racing the document load.
	try {
		if (typeof window.wagramQuickRepliesState === 'function') {
			window.wagramQuickRepliesState().then(function (list) {
				var m = {};
				for (var i = 0; i < (list || []).length; i++) { m[list[i].token] = list[i].text; }
				window.wagramAgent.setQuickReplies(JSON.stringify(m));
			});
		}
	} catch (e3) {}
})();
`
