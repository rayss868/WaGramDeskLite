//go:build windows

package app

// accountOverlayScript renders the in-page account switcher. It lives in a
// shadow root attached to documentElement, so WhatsApp's styles and React
// re-renders cannot reach it and ours cannot leak into the page.
const accountOverlayScript = `
(function () {
	if (typeof window.wagramAccountsState !== 'function') { return; }

	var CSS = [
		'.btn { position: fixed; left: 12px; bottom: 14px; width: 38px; height: 38px;',
		'  border-radius: 50%; background: #202c33; color: #e9edef; border: 1px solid #2a3942;',
		'  cursor: pointer; font: 600 14px system-ui, sans-serif; display: flex;',
		'  align-items: center; justify-content: center; opacity: .7; box-sizing: border-box; }',
		'.btn:hover { opacity: 1; background: #2a3942; }',
		'.panel { position: fixed; left: 12px; bottom: 60px; width: 258px; background: #233138;',
		'  border: 1px solid #2a3942; border-radius: 10px; padding: 6px; color: #e9edef;',
		'  font: 400 13px system-ui, sans-serif; box-shadow: 0 8px 28px rgba(0,0,0,.45); }',
		'.hdr { padding: 8px 10px 6px; font-size: 11px; letter-spacing: .08em;',
		'  text-transform: uppercase; color: #8696a0; }',
		'.row { display: flex; align-items: center; gap: 8px; padding: 8px 10px;',
		'  border-radius: 6px; cursor: pointer; }',
		'.row:hover { background: #2a3942; }',
		'.row.on { cursor: default; }',
		'.tick { width: 14px; color: #00a884; }',
		'.nm { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }',
		'.sep { height: 1px; background: #2a3942; margin: 6px 4px; }',
		'.act { display: flex; align-items: center; gap: 8px; padding: 8px 10px;',
		'  border-radius: 6px; cursor: pointer; color: #d1d7db; }',
		'.act:hover { background: #2a3942; }',
		'.act.danger:hover { background: #3c2226; color: #f15c6d; }',
		'.gl { width: 14px; text-align: center; color: #8696a0; }',
		'.badge { font-size: 10px; font-weight: 700; padding: 1px 5px; border-radius: 4px;',
		'  background: #2a3942; color: #00a884; margin-left: auto; }',
		'.badge.tg { color: #37aee2; }',
		'.tabs { display: flex; gap: 4px; padding: 4px; }',
		'.tabs button { flex: 1; padding: 6px 4px; border-radius: 6px; border: 1px solid #2a3942;',
		'  background: #202c33; color: #8696a0; cursor: pointer;',
		'  font: 500 12px system-ui, sans-serif; }',
		'.tabs button.on { background: #2a3942; color: #e9edef; }',
		'.viewrow { display: flex; align-items: center; gap: 8px; padding: 8px 10px;',
		'  border-radius: 6px; cursor: pointer; color: #d1d7db; }',
		'.viewrow:hover { background: #2a3942; }',
		'.step { padding: 0 6px; border-radius: 4px; background: #2a3942; color: #d1d7db;',
		'  cursor: pointer; font-weight: 700; }',
		'.step:hover { background: #3b4a54; }',
		'.volsteps { margin-left: auto; display: flex; gap: 4px; }',
		'.msg { padding: 8px 10px; color: #8696a0; line-height: 1.45; }',
		'input { width: 100%; box-sizing: border-box; padding: 8px 10px; border-radius: 6px;',
		'  border: 1px solid #2a3942; background: #111b21; color: #e9edef;',
		'  font: 400 13px system-ui, sans-serif; }',
		'input:focus { outline: none; border-color: #00a884; }',
		'.btns { display: flex; gap: 6px; padding: 8px 4px 4px; }',
		'.btns button { flex: 1; padding: 8px; border-radius: 6px; border: 1px solid #2a3942;',
		'  background: #202c33; color: #e9edef; cursor: pointer;',
		'  font: 500 13px system-ui, sans-serif; }',
		'.btns button:hover { background: #2a3942; }',
		'.btns button.go { background: #00a884; border-color: #00a884; color: #0b141a; }',
		'.btns button.go.danger { background: #f15c6d; border-color: #f15c6d; }'
	].join(' ');

	var host = null, root = null, state = null, mode = 'list', open = false, pageTab = 'all';

	function svcOf(a) {
		return (a && a.service === 'telegram') ? 'telegram' : 'whatsapp';
	}

	function badgeOf(a) {
		return svcOf(a) === 'telegram' ? 'TG' : 'WA';
	}

	function prefsOf() {
		if (state && state.prefs && (state.prefs.viewMode === 'pages' || state.prefs.viewMode === 'tabs')) {
			return state.prefs.viewMode;
		}
		if (state && state.prefs && state.prefs.ViewMode === 'pages') { return 'pages'; }
		return 'tabs';
	}

	function notifOf() {
		if (state && state.prefs) {
			if (typeof state.prefs.notifications === 'boolean') { return state.prefs.notifications; }
			if (typeof state.prefs.Notifications === 'boolean') { return state.prefs.Notifications; }
		}
		return true;
	}

	function liteOf() {
		if (state && state.prefs) {
			if (typeof state.prefs.lite === 'boolean') { return state.prefs.lite; }
			if (typeof state.prefs.Lite === 'boolean') { return state.prefs.Lite; }
		}
		return true;
	}

	function privOf() {
		if (state && state.prefs) {
			if (typeof state.prefs.privacy === 'boolean') { return state.prefs.privacy; }
			if (typeof state.prefs.Privacy === 'boolean') { return state.prefs.Privacy; }
		}
		return false;
	}

	function revealOf() {
		if (state && state.prefs) {
			var v = state.prefs.privacyReveal || state.prefs.PrivacyReveal;
			if (v === 'hover' || v === 'click') { return v; }
		}
		return 'hard';
	}

	function currentAccount() {
		if (!state || !state.accounts) { return null; }
		for (var i = 0; i < state.accounts.length; i++) {
			if (state.accounts[i].id === state.current) { return state.accounts[i]; }
		}
		return null;
	}

	function volOf() {
		var a = currentAccount();
		return a && typeof a.volume === 'number' ? a.volume : 100;
	}

	function mutedOf() {
		var a = currentAccount();
		return !!(a && a.muted);
	}

	function stepVolume(delta) {
		var a = currentAccount();
		if (!a) { return; }
		var next = (typeof a.volume === 'number' ? a.volume : 100) + delta;
		if (next < 0) { next = 0; }
		if (next > 100) { next = 100; }
		// Adjusting the level while muted also unmutes, so the change is audible.
		if (typeof window.wagramVolumeMute === 'function' && a.muted) { window.wagramVolumeMute(false); }
		if (typeof window.wagramVolumeSet === 'function') { window.wagramVolumeSet(next); }
		a.volume = next;
		a.muted = false;
		render();
	}

	function mount() {
		if (host && document.documentElement.contains(host)) { return; }
		host = document.createElement('div');
		host.setAttribute('data-wagramdesklite', 'accounts');
		// The host lives in WhatsApp's light DOM, so its own box must be pinned
		// down explicitly; page rules could otherwise hide it and take the
		// shadow content with it.
		var pin = {
			display: 'block', position: 'fixed', left: '0', top: '0',
			width: '0', height: '0', margin: '0', padding: '0', border: '0',
			visibility: 'visible', opacity: '1', 'z-index': '2147483000'
		};
		Object.keys(pin).forEach(function (k) {
			host.style.setProperty(k, pin[k], 'important');
		});
		document.documentElement.appendChild(host);
		root = host.attachShadow({ mode: 'open' });
		try {
			var sheet = new CSSStyleSheet();
			sheet.replaceSync(CSS);
			root.adoptedStyleSheets = [sheet];
		} catch (e) {
			var tag = document.createElement('style');
			tag.textContent = CSS;
			root.appendChild(tag);
		}
		render();
	}

	function refresh() {
		return window.wagramAccountsState().then(function (s) {
			state = s;
			render();
		}).catch(function () {});
	}

	function el(tag, cls, text) {
		var n = document.createElement(tag);
		if (cls) { n.className = cls; }
		if (text !== undefined) { n.textContent = text; }
		return n;
	}

	function currentName() {
		if (!state) { return 'Account'; }
		for (var i = 0; i < state.accounts.length; i++) {
			if (state.accounts[i].id === state.current) { return state.accounts[i].name; }
		}
		return 'Account';
	}

	function currentIndex() {
		if (!state) { return 1; }
		for (var i = 0; i < state.accounts.length; i++) {
			if (state.accounts[i].id === state.current) { return i + 1; }
		}
		return 1;
	}

	// WhatsApp's rail groups Media and You in a footer section, and spaces its
	// items on a 44px pitch. Sitting our button directly above that section
	// keeps it clear of both and makes it track any resize, since the rail's own
	// box is what we measure.
	function anchorButton() {
		var btn = root && root.querySelector('.btn');
		if (!btn) { return; }
		var footer = document.querySelector('[data-testid="navbar-footer-section"]');
		var r = footer ? footer.getBoundingClientRect() : null;
		if (r && r.width > 0 && r.height > 0) {
			var size = Math.round(r.width);
			var gap = Math.round(size * 0.1);
			// The rail is measured while the page is still settling, and a
			// half-laid-out box can put the button above the viewport, where it
			// stays invisible. Never let the measurement take it off-screen.
			var top = Math.max(4, Math.round(r.top - size - gap));
			btn.style.left = Math.round(r.left) + 'px';
			btn.style.top = top + 'px';
			btn.style.bottom = 'auto';
			btn.style.width = size + 'px';
			btn.style.height = size + 'px';
			btn.style.fontSize = Math.max(12, Math.round(size * 0.36)) + 'px';
			return;
		}
		// No rail yet (the QR screen has none): rest in the corner instead.
		btn.style.left = '12px';
		btn.style.top = 'auto';
		btn.style.bottom = '14px';
		btn.style.width = '38px';
		btn.style.height = '38px';
		btn.style.fontSize = '14px';
	}

	function anchorPanel(panel) {
		var btn = root && root.querySelector('.btn');
		if (!btn) { return; }
		var r = btn.getBoundingClientRect();
		panel.style.left = Math.round(r.right + 8) + 'px';
		panel.style.bottom = Math.round(window.innerHeight - r.bottom) + 'px';
	}

	function render() {
		if (!root) { return; }
		root.querySelectorAll('.btn, .panel').forEach(function (n) { n.remove(); });

		var btn = el('button', 'btn', String(currentIndex()));
		btn.title = 'Accounts - ' + currentName();
		btn.addEventListener('click', function (ev) {
			ev.stopPropagation();
			open = !open;
			if (open) { mode = 'list'; refresh(); } else { render(); }
		});
		root.appendChild(btn);
		anchorButton();
		if (!open) { return; }

		var panel = el('div', 'panel');
		panel.addEventListener('click', function (ev) { ev.stopPropagation(); });
		if (mode === 'rename') { renderRename(panel); }
		else if (mode === 'confirm') { renderConfirm(panel); }
		else if (mode === 'settings') { renderSettings(panel); }
		else if (mode === 'export') { renderExport(panel); }
		else if (mode === 'quickreplies') { renderQuickReplies(panel); }
		else if (mode === 'scheduler') { renderScheduler(panel); }
		else if (mode === 'webhooks') { renderWebhooks(panel); }
		else { renderList(panel); }
		root.appendChild(panel);
		anchorPanel(panel);
	}

	function renderList(panel) {
		panel.appendChild(el('div', 'hdr', 'Accounts'));
		var view = prefsOf();
		if (view === 'pages') {
			var tabs = el('div', 'tabs');
			[['whatsapp', 'WhatsApp'], ['telegram', 'Telegram'], ['all', 'All']].forEach(function (t) {
				var b = el('button', pageTab === t[0] ? 'on' : '', t[1]);
				b.addEventListener('click', function () { pageTab = t[0]; render(); });
				tabs.appendChild(b);
			});
			panel.appendChild(tabs);
		}
		var accounts = state ? state.accounts : [];
		accounts.forEach(function (a) {
			if (view === 'pages' && pageTab !== 'all' && svcOf(a) !== pageTab) { return; }
			var isCurrent = a.id === state.current;
			var row = el('div', isCurrent ? 'row on' : 'row');
			row.appendChild(el('span', 'tick', isCurrent ? '✓' : ''));
			row.appendChild(el('span', 'nm', a.name));
			var badge = el('span', 'badge' + (svcOf(a) === 'telegram' ? ' tg' : ''), badgeOf(a));
			row.appendChild(badge);
			if (!isCurrent) {
				row.addEventListener('click', function () {
					window.wagramAccountSwitch(a.id);
					open = false;
					render();
				});
			}
			panel.appendChild(row);
		});

		panel.appendChild(el('div', 'sep'));

		[['whatsapp', 'Add WhatsApp'], ['telegram', 'Add Telegram']].forEach(function (t) {
			var add = el('div', 'act');
			add.appendChild(el('span', 'gl', '+'));
			add.appendChild(el('span', 'nm', t[1]));
			add.addEventListener('click', function () {
				if (typeof window.wagramAccountAddService === 'function') {
					window.wagramAccountAddService(t[0]);
				} else {
					window.wagramAccountAdd();
				}
				open = false;
				render();
			});
			panel.appendChild(add);
		});

		var set = el('div', 'act');
		set.appendChild(el('span', 'gl', '⚙'));
		set.appendChild(el('span', 'nm', 'Settings'));
		set.addEventListener('click', function () { mode = 'settings'; render(); });
		panel.appendChild(set);

		var volRow = el('div', 'viewrow');
		var volIcon = el('span', 'gl', mutedOf() ? '🔇' : '🔊');
		volIcon.addEventListener('click', function (ev) {
			ev.stopPropagation();
			var on = !mutedOf();
			if (typeof window.wagramVolumeMute === 'function') { window.wagramVolumeMute(on); }
			var a = currentAccount();
			if (a) { a.muted = on; }
			render();
		});
		volRow.appendChild(volIcon);
		volRow.appendChild(el('span', 'nm', mutedOf() ? 'Volume: Muted' : 'Volume: ' + volOf() + '%'));
		var steps = el('div', 'volsteps');
		var minus = el('span', 'step', '−');
		minus.addEventListener('click', function (ev) { ev.stopPropagation(); stepVolume(-10); });
		var plus = el('span', 'step', '+');
		plus.addEventListener('click', function (ev) { ev.stopPropagation(); stepVolume(10); });
		steps.appendChild(minus);
		steps.appendChild(plus);
		volRow.appendChild(steps);
		panel.appendChild(volRow);

		var ren = el('div', 'act');
		ren.appendChild(el('span', 'gl', '✎'));
		ren.appendChild(el('span', 'nm', 'Rename this account'));
		ren.addEventListener('click', function () { mode = 'rename'; render(); });
		panel.appendChild(ren);

		// The first account is the app's own profile and cannot be removed.
		if (state && state.accounts.length && state.current !== state.accounts[0].id) {
			var del = el('div', 'act danger');
			del.appendChild(el('span', 'gl', '✕'));
			del.appendChild(el('span', 'nm', 'Remove this account'));
			del.addEventListener('click', function () { mode = 'confirm'; render(); });
			panel.appendChild(del);
		}
	}

	function renderSettings(panel) {
		panel.appendChild(el('div', 'hdr', 'Settings'));
		var back = el('div', 'act');
		back.appendChild(el('span', 'gl', '‹'));
		back.appendChild(el('span', 'nm', 'Back'));
		back.addEventListener('click', function () { mode = 'list'; render(); });
		panel.appendChild(back);

		var view = prefsOf();
		var toggle = el('div', 'viewrow');
		toggle.appendChild(el('span', 'gl', view === 'pages' ? '▦' : '▤'));
		toggle.appendChild(el('span', 'nm', view === 'pages' ? 'View: Pages (switch to Tabs)' : 'View: Tabs (switch to Pages)'));
		toggle.addEventListener('click', function () {
			var next = view === 'pages' ? 'tabs' : 'pages';
			if (typeof window.wagramPrefsSet === 'function') {
				window.wagramPrefsSet(next).then(function () { setTimeout(refresh, 80); });
			}
			if (state && state.prefs) {
				if ('viewMode' in state.prefs) { state.prefs.viewMode = next; }
				state.prefs.ViewMode = next;
			}
			render();
		});
		panel.appendChild(toggle);

		var notifOn = notifOf();
		var notif = el('div', 'viewrow');
		notif.appendChild(el('span', 'gl', notifOn ? '🔔' : '🔕'));
		notif.appendChild(el('span', 'nm', notifOn ? 'Notifications: On (turn off)' : 'Notifications: Off (turn on)'));
		notif.addEventListener('click', function () {
			var next = !notifOn;
			if (typeof window.wagramNotificationsSet === 'function') {
				window.wagramNotificationsSet(next).then(function () { setTimeout(refresh, 80); });
			}
			if (state && state.prefs) {
				if ('notifications' in state.prefs) { state.prefs.notifications = next; }
				state.prefs.Notifications = next;
			}
			notifOn = next;
			render();
		});
		panel.appendChild(notif);

		var liteOn = liteOf();
		var lite = el('div', 'viewrow');
		lite.appendChild(el('span', 'gl', liteOn ? '☾' : '☀'));
		lite.appendChild(el('span', 'nm', liteOn ? 'Lite: On (shed memory when idle)' : 'Lite: Off (always full speed)'));
		lite.addEventListener('click', function () {
			var next = !liteOn;
			if (typeof window.wagramLiteSet === 'function') {
				window.wagramLiteSet(next).then(function () { setTimeout(refresh, 80); });
			}
			if (state && state.prefs) {
				if ('lite' in state.prefs) { state.prefs.lite = next; }
				state.prefs.Lite = next;
			}
			liteOn = next;
			render();
		});
		panel.appendChild(lite);

		var privOn = privOf();
		var priv = el('div', 'viewrow');
		priv.appendChild(el('span', 'gl', privOn ? '◉' : '○'));
		priv.appendChild(el('span', 'nm', privOn ? 'Privacy: On (blur messages)' : 'Privacy: Off (blur messages)'));
		priv.addEventListener('click', function () {
			var next = !privOn;
			if (typeof window.wagramPrivacyApply === 'function') { window.wagramPrivacyApply(next, revealOf()); }
			if (typeof window.wagramPrivacySet === 'function') {
				window.wagramPrivacySet(next).then(function () { setTimeout(refresh, 80); });
			}
			if (state && state.prefs) {
				if ('privacy' in state.prefs) { state.prefs.privacy = next; }
				state.prefs.Privacy = next;
			}
			privOn = next;
			render();
		});
		panel.appendChild(priv);

		var curReveal = revealOf();
		var reveal = el('div', 'viewrow');
		reveal.appendChild(el('span', 'gl', curReveal === 'hard' ? '▤' : curReveal === 'hover' ? '▦' : '▣'));
		reveal.appendChild(el('span', 'nm',
			curReveal === 'hard' ? 'Reveal: Hard (no peek)'
			: curReveal === 'hover' ? 'Reveal: Hover (peek on hover)'
			: 'Reveal: Click (click to peek)'));
		reveal.addEventListener('click', function () {
			var now = revealOf();
			var next = now === 'hard' ? 'hover' : now === 'hover' ? 'click' : 'hard';
			if (typeof window.wagramPrivacyApply === 'function') { window.wagramPrivacyApply(privOf(), next); }
			if (typeof window.wagramPrivacyRevealSet === 'function') {
				window.wagramPrivacyRevealSet(next).then(function () { setTimeout(refresh, 80); });
			}
			if (state && state.prefs) {
				if ('privacyReveal' in state.prefs) { state.prefs.privacyReveal = next; }
				state.prefs.PrivacyReveal = next;
			}
			render();
		});
		panel.appendChild(reveal);

		panel.appendChild(el('div', 'hdr', 'Tools'));
		var tools = [
			['⬇', 'Export chat', 'export'],
			['⚡', 'Quick replies', 'quickreplies'],
			['⏱', 'Scheduled messages', 'scheduler'],
			['🔗', 'Webhooks', 'webhooks']
		];
		tools.forEach(function (t) {
			var row = el('div', 'viewrow');
			row.appendChild(el('span', 'gl', t[0]));
			row.appendChild(el('span', 'nm', t[1]));
			row.addEventListener('click', function () { mode = t[2]; render(); });
			panel.appendChild(row);
		});
	}

	function subBack(panel, label) {
		panel.appendChild(el('div', 'hdr', label));
		var back = el('div', 'act');
		back.appendChild(el('span', 'gl', '‹'));
		back.appendChild(el('span', 'nm', 'Back'));
		back.addEventListener('click', function () { mode = 'settings'; render(); });
		panel.appendChild(back);
	}

	function renderExport(panel) {
		subBack(panel, 'Export chat');
		var result = el('div', 'msg', 'Choose a format. The open conversation is saved to the exports folder.');
		panel.appendChild(result);
		var btns = el('div', 'btns');
		['TXT', 'JSON', 'HTML'].forEach(function (fmt) {
			var b = el('button', '', fmt);
			b.addEventListener('click', function () {
				if (typeof window.wagramAgent === 'undefined' || !window.wagramAgent.readMessages) {
					result.textContent = 'Open a chat first.';
					return;
				}
				window.wagramAgent.readMessages(500).then(function (data) {
					return window.wagramExportWrite(fmt.toLowerCase(), data);
				}).then(function (path) {
					if (path.indexOf('error:') === 0) {
						result.textContent = path;
						return;
					}
					var name = path.split(/[\\/]/).pop();
					result.textContent = 'Exported: ' + name;
					if (typeof window.wagramExportReveal === 'function') {
						window.wagramExportReveal(path);
					}
				});
			});
			btns.appendChild(b);
		});
		panel.appendChild(btns);
	}

	function renderQuickReplies(panel) {
		subBack(panel, 'Quick replies');
		var listBox = el('div', '');
		panel.appendChild(listBox);
		var show = function (list) {
			listBox.textContent = '';
			if (!list || !list.length) { listBox.appendChild(el('div', 'msg', 'No shortcuts yet.')); return; }
			list.forEach(function (q) {
				var row = el('div', 'viewrow');
				row.appendChild(el('span', 'gl', '⚡'));
				row.appendChild(el('span', 'nm', q.token + ' → ' + q.text));
				var del = el('button', '', '×');
				del.addEventListener('click', function (ev) {
					ev.stopPropagation();
					window.wagramQuickReplyDelete(q.token).then(show);
				});
				row.appendChild(del);
				listBox.appendChild(row);
			});
		};
		var token = document.createElement('input');
		token.placeholder = '/token';
		panel.appendChild(token);
		var text = document.createElement('input');
		text.placeholder = 'Expansion text';
		panel.appendChild(text);
		var btns = el('div', 'btns');
		var add = el('button', 'go', 'Add');
		add.addEventListener('click', function () {
			window.wagramQuickReplySave(token.value, text.value).then(function (list) {
				token.value = ''; text.value = '';
				show(list);
			});
		});
		btns.appendChild(add);
		panel.appendChild(btns);
		if (typeof window.wagramQuickRepliesState === 'function') {
			window.wagramQuickRepliesState().then(show);
		}
	}

	function renderWebhooks(panel) {
		subBack(panel, 'Webhooks');
		var listBox = el('div', '');
		panel.appendChild(listBox);
		var show = function (list) {
			listBox.textContent = '';
			if (!list || !list.length) { listBox.appendChild(el('div', 'msg', 'No webhooks yet. Incoming messages are POSTed to these URLs.')); return; }
			list.forEach(function (url) {
				var row = el('div', 'viewrow');
				row.appendChild(el('span', 'gl', '🔗'));
				row.appendChild(el('span', 'nm', url));
				var del = el('button', '', '×');
				del.addEventListener('click', function (ev) {
					ev.stopPropagation();
					window.wagramWebhookDelete(url).then(show);
				});
				row.appendChild(del);
				listBox.appendChild(row);
			});
		};
		var input = document.createElement('input');
		input.placeholder = 'https://example.com/hook';
		panel.appendChild(input);
		var btns = el('div', 'btns');
		var add = el('button', 'go', 'Add');
		add.addEventListener('click', function () {
			window.wagramWebhookAdd(input.value.trim()).then(function (list) {
				input.value = '';
				show(list);
			});
		});
		btns.appendChild(add);
		panel.appendChild(btns);
		if (typeof window.wagramWebhooksState === 'function') {
			window.wagramWebhooksState().then(show);
		}
	}

	function renderScheduler(panel) {
		subBack(panel, 'Scheduled messages');
		var listBox = el('div', '');
		panel.appendChild(listBox);
		var show = function (list) {
			listBox.textContent = '';
			if (!list || !list.length) { listBox.appendChild(el('div', 'msg', 'Nothing scheduled.')); return; }
			list.forEach(function (m) {
				var row = el('div', 'viewrow');
				row.appendChild(el('span', 'gl', '⏱'));
				row.appendChild(el('span', 'nm', new Date(m.sendAt * 1000).toLocaleString() + ' — ' + m.text));
				var del = el('button', '', '×');
				del.addEventListener('click', function (ev) {
					ev.stopPropagation();
					window.wagramScheduleDelete(m.id).then(show);
				});
				row.appendChild(del);
				listBox.appendChild(row);
			});
		};
		var text = document.createElement('input');
		text.placeholder = 'Message to send to the open chat';
		panel.appendChild(text);
		var when = document.createElement('input');
		when.type = 'datetime-local';
		panel.appendChild(when);
		var btns = el('div', 'btns');
		var add = el('button', 'go', 'Schedule');
		add.addEventListener('click', function () {
			var ts = Math.floor(Date.parse(when.value) / 1000);
			if (!text.value || isNaN(ts)) { return; }
			window.wagramScheduleAdd(text.value, ts).then(function (list) {
				text.value = ''; when.value = '';
				show(list);
			});
		});
		btns.appendChild(add);
		panel.appendChild(btns);
		if (typeof window.wagramScheduleState === 'function') {
			window.wagramScheduleState().then(show);
		}
	}

	function renderRename(panel) {
		panel.appendChild(el('div', 'hdr', 'Rename account'));
		var input = document.createElement('input');
		input.value = currentName();
		input.maxLength = 40;
		panel.appendChild(input);

		var commit = function () {
			var name = input.value.trim();
			if (name) { window.wagramAccountRename(state.current, name); }
			mode = 'list';
			setTimeout(refresh, 80);
		};
		input.addEventListener('keydown', function (ev) {
			if (ev.key === 'Enter') { commit(); }
			if (ev.key === 'Escape') { mode = 'list'; render(); }
		});

		var btns = el('div', 'btns');
		var cancel = el('button', '', 'Cancel');
		cancel.addEventListener('click', function () { mode = 'list'; render(); });
		var save = el('button', 'go', 'Save');
		save.addEventListener('click', commit);
		btns.appendChild(cancel);
		btns.appendChild(save);
		panel.appendChild(btns);
		setTimeout(function () { input.focus(); input.select(); }, 0);
	}

	function renderConfirm(panel) {
		panel.appendChild(el('div', 'hdr', 'Remove account'));
		panel.appendChild(el('div', 'msg',
			'Remove "' + currentName() + '"? Its WhatsApp session will be deleted from ' +
			'this computer and you will need to scan the QR code again.'));
		var btns = el('div', 'btns');
		var cancel = el('button', '', 'Cancel');
		cancel.addEventListener('click', function () { mode = 'list'; render(); });
		var go = el('button', 'go danger', 'Remove');
		go.addEventListener('click', function () { window.wagramAccountRemove(state.current); });
		btns.appendChild(cancel);
		btns.appendChild(go);
		panel.appendChild(btns);
	}

	document.addEventListener('click', function () {
		if (open) { open = false; render(); }
	});

	function reanchor() {
		anchorButton();
		var panel = root && root.querySelector('.panel');
		if (panel) { anchorPanel(panel); }
	}

	function boot() {
		try {
			mount();
			refresh();
			watch();
		} catch (e) {
			// <html> may not exist yet at document-created time; the
			// DOMContentLoaded handler below calls this again.
		}
	}

	window.addEventListener('resize', reanchor);

	var railEl = null, railResize = null, watching = false;

	// The footer we sit above is React-owned. It gets replaced when the chat
	// list is rebuilt, and its box also moves while the app settles, so the
	// anchor is re-measured on every throttled tick and not only when the
	// element identity changes. Measuring an unchanged element is the whole
	// point: an earlier version returned early on identity, which left the
	// button parked at a half-laid-out position (a negative top, off-screen)
	// for the rest of the session.
	function trackTheRail() {
		if (!document.documentElement || !document.documentElement.contains(host)) {
			boot();
		}
		var rail = document.querySelector('[data-testid="navbar-footer-section"]');
		if (rail !== railEl) {
			railEl = rail;
			if (railResize) {
				railResize.disconnect();
				if (rail) { railResize.observe(rail); }
			}
		}
		reanchor();
	}

	// A subtree observer sees every mutation, but its callback runs behind a
	// throttle: a churning page costs a few rect reads a second and a quiet page
	// costs nothing at all. That is the property a setInterval could not have.
	function watch() {
		if (watching || !document.documentElement) { return; }
		watching = true;
		if (typeof ResizeObserver !== 'undefined') {
			railResize = new ResizeObserver(reanchor);
		}
		if (typeof MutationObserver !== 'undefined') {
			// A mutation that lands inside the throttle window still has to be
			// acted on, or the last layout of a burst is the one state we never
			// measure. The trailing timer drains it.
			var lastCheck = 0, trailing = 0;
			var onMutations = function () {
				var wait = 250 - (Date.now() - lastCheck);
				if (wait > 0) {
					if (!trailing) {
						trailing = setTimeout(function () {
							trailing = 0;
							lastCheck = Date.now();
							trackTheRail();
						}, wait);
					}
					return;
				}
				lastCheck = Date.now();
				trackTheRail();
			};
			new MutationObserver(onMutations).observe(document.body || document.documentElement, { childList: true, subtree: true });

			// Our host is a direct child of <html>, so a hard navigation that
			// replaces the document's children is outside the observer above.
			new MutationObserver(onMutations).observe(document.documentElement, { childList: true });
		}
		trackTheRail();
	}

	if (document.readyState === 'loading') {
		document.addEventListener('DOMContentLoaded', boot);
	} else {
		boot();
	}
})();
`
