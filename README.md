<p align="center">
  <img src="assets/banner.png" alt="WaGramDeskLite Banner" width="100%">
</p>

<h3 align="center">Lightweight WhatsApp Desktop Client for Windows</h3>

<p align="center">
  Built with Go + WebView2. No Electron. No bloat. Just WhatsApp.
</p>

---

## About

**WaGramDeskLite** is a minimal, native Windows wrapper for WhatsApp Web. Instead of bundling a full Chromium engine like Electron apps, it uses the WebView2 runtime already present on Windows 10/11 to render WhatsApp Web in a clean, dark-themed window with system tray integration.

## Highlights

| Feature | Details |
|---------|---------|
| Ultra-lightweight | ~3 MB executable, no bundled browser engine |
| Multiple accounts | Run several WhatsApp accounts at once, each with its own session, tray icon and notifications |
| System tray | Minimize to tray on close, restore with single click |
| Dark mode | Native dark title bar and window frame |
| Persistent session | WhatsApp login survives app restarts |
| Window memory | Remembers size, position and maximized state, per account |
| Notifications | Shows the sender's profile picture, falling back to the app icon |
| Calls support | Camera and microphone passthrough for voice/video calls |
| Single instance | One window per account; launching again focuses the window that is already open |
| High-DPI | Full PerMonitorV2 DPI awareness |
| Auto-detect WebView2 | Shows install prompt if WebView2 Runtime is missing |

## Screenshots

### WebView2 Process Usage

<img src="screenshot-webview2-manager.png" alt="WebView2 Manager processes used by WhatsApp Web" width="100%">

### WaGramDeskLite Process Usage

<img src="screenshot-wagramdesklite-task-manager.png" alt="WaGramDeskLite process in Windows Task Manager" width="100%">

## Quick Start

1. Download [`WaGramDeskLiteSetup.exe`](https://github.com/rayss868/WaGramDeskLite/releases/latest) from Releases
2. Run the installer — it adds a "WhatsApp" shortcut to your Start Menu automatically
3. Scan the QR code with your phone
4. Allow camera/mic access when Windows prompts you
5. That's it — your session is saved automatically

> The setup installs the app under `%LOCALAPPDATA%\Programs\WaGramDeskLite` and creates a **WhatsApp** shortcut, so it shows up when you search "wa" in Windows. A portable `WaGramDeskLite.exe` is also available for those who prefer no installer.

### macOS

Coming soon — the macOS build is in progress.

## Multiple Accounts

Open the account switcher from the round button in WhatsApp's left sidebar, just above the Media and profile icons. From there you can:

- **Switch** to another account — the window keeps its current size, position and maximized state, so nothing resizes on the way
- **Add an account** — opens a new window with a fresh QR code
- **Rename** the account you are in
- **Remove** the account you are in, deleting its stored session

Every account runs as its own process with its own storage profile, so all of them stay connected and notify you independently — you never miss a message because an account was not on screen. Accounts left open when you quit are reopened next time; choosing **Exit** from an account's tray menu keeps that one closed.

The tray menu carries the same account list as a shortcut, so you can switch without opening a window first.

> Each account runs its own WebView2 instance, so memory use grows roughly in step with the number of accounts you keep open.

## MCP / AI Agent Access

Each running window also exposes a **Model Context Protocol (MCP)** server on a
loopback HTTP port, so an AI agent can list chats, read the open conversation,
and send a message through the same UI. The endpoint URL and a persistent bearer
token are written to `%APPDATA%\WaGramDeskLite\mcp.json`. See
[docs/MCP.md](docs/MCP.md) for the token model, the tool list, and how to connect
a client.

## Session & Data

All profile data (cookies, localStorage, IndexedDB) is stored locally:

```text
%APPDATA%\WaGramDeskLite\UserData                  # first account
%APPDATA%\WaGramDeskLite\profiles\<id>\UserData    # additional accounts
%APPDATA%\WaGramDeskLite\accounts.json             # the account list
```

Back up these folders to preserve your logins. Deleting one will require re-pairing that device.

## Building from Source

**Prerequisites:**
- [Go](https://go.dev/dl/) 1.22+
- [go-winres](https://github.com/tc-hib/go-winres) (pure Go resource compiler, no MinGW/windres needed)
  ```
  go install github.com/tc-hib/go-winres@latest
  ```

```bash
# One-shot build (compiles resources and the executable into dist/)
./scripts/build.sh

# Or manually:
# go-winres make -arch amd64 --in winres.json   (run inside build/)
# cp build/rsrc_windows_amd64.syso cmd/wagramdesklite/rsrc.syso
# go build -ldflags="-H windowsgui -s -w" -o dist/WaGramDeskLite.exe ./cmd/wagramdesklite
```

The output `dist\WaGramDeskLite.exe` includes embedded icon, DPI manifest, and Windows VERSIONINFO metadata.

## Tech Stack

- **Language:** Go
- **WebView:** [go-webview2](https://github.com/jchv/go-webview2) (Microsoft Edge WebView2)
- **Tray:** Win32 `Shell_NotifyIconW` API
- **Window frame:** `DwmSetWindowAttribute` for dark mode
- **Notifications:** System tray balloon via `NIF_INFO`, with the sender's avatar supplied as a custom `hBalloonIcon`
- **Accounts:** One WebView2 storage profile and one process per account; the switcher is injected into the page inside a shadow root
- **Idle memory:** `ICoreWebView2_19::put_MemoryUsageTargetLevel` while minimised to tray

## Project Layout

```
WaGramDeskLite/
├── cmd/
│   └── wagramdesklite/
│       ├── main.go        # Thin entry point
│       └── rsrc.syso      # Compiled Windows resources (icon, manifest, version) 
├── internal/
│   ├── app/               # WebView2 window, tray, notifications, accounts, state persistence
│   └── audio/             # Core Audio session labeler (volume mixer shows "WhatsApp")
├── assets/                # icon.ico (multi-size), icon.png, banner.png
├── build/                 # winres.json, winres/ data, app.manifest (resources source)
├── scripts/
│   └── build.sh           # One-shot resource + executable build
├── docs/
│   └── MCP.md             # MCP endpoint, token model, agent setup
├── dist/                  # Build output (WaGramDeskLite.exe)
├── go.mod / go.sum        # Go module definition
└── vendor/                # Vendored dependencies
```

## Disclaimer

This project is **not affiliated with or endorsed by WhatsApp LLC or Meta Platforms, Inc.** It is an independent, open-source tool that wraps the official WhatsApp Web interface. Use at your own discretion.

## License

This project does not currently have a declared license.
