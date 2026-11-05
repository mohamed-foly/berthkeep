# Berthkeep

Every browser gets its berth.

Berthkeep is a small desktop tool for the Chromium browsers your scripts,
MCP servers and AI agents drive over the **Chrome DevTools Protocol (CDP)**.
It gives each browser a named **session**: its own profile folder (logins,
cookies), its own debugging port, and settings such as headless or windowed.
You start and stop sessions from a window, the tray or the command line, and
your code asks for an endpoint by name instead of a hard-coded port.

- One profile per session, stored in one place, never mixed with your everyday browser.
- A port per session, with conflicts flagged before two sessions collide.
- Profiles you keep, profiles wiped on every stop, or quick temp sessions that remove themselves.
- Switch a running session between headless and windowed; tabs reopen on the same port.
- Light: one ~14 MB program, about 9 MB of memory in the tray.

## Install

Download the archive for your system from the Releases page and unzip it anywhere.

| System | Start it |
|---|---|
| Windows | Double-click `berthkeepw.exe` (no console window). `berthkeep.exe` is the command-line program. |
| macOS / Linux | Run `berthkeep` (with no arguments it opens the window). |
| Linux without a desktop | Use the `_server` archive: command line and API only; the UI opens in a browser. |

To add a Start-menu entry, put the commands on your PATH and start in the tray at login:

```
berthkeep install --path --autostart
berthkeep uninstall      # undoes it
```

Requirements: a Chromium-family browser (Chrome, Edge, Chromium or Brave). On
Windows the window uses WebView2, which ships with Windows 10 and 11. On Linux
the desktop build needs GTK 4 and WebKitGTK 6.

## Quick start

```
berthkeep new "Shop admin" --label shop --start      # prints http://127.0.0.1:9222
berthkeep list
berthkeep endpoint shop-admin --ensure               # starts it if needed, prints the endpoint
berthkeep mode shop-admin headless                   # same port, tabs reopened
berthkeep stop shop-admin
```

`bkeep` is a short alias for `berthkeep` once installed with `--path`.

## Use a session from code

Ask for the endpoint by name, so ports can change without touching your code:

```js
// Node + Playwright
import { execFileSync } from 'node:child_process';
import { chromium } from 'playwright';
const url = execFileSync('berthkeep', ['endpoint', 'shop-admin', '--ensure']).toString().trim();
const browser = await chromium.connectOverCDP(url);
```

```python
# Python + Playwright
import subprocess
from playwright.sync_api import sync_playwright
url = subprocess.check_output(["berthkeep", "endpoint", "shop-admin", "--ensure"], text=True).strip()
with sync_playwright() as p:
    browser = p.chromium.connect_over_cdp(url)
```

MCP servers take the endpoint directly, for example
`chrome-devtools-mcp --browserUrl http://127.0.0.1:9222` or
`@playwright/mcp --cdp-endpoint http://127.0.0.1:9222`. The session drawer in
the window shows these snippets with the right port filled in.

## Commands

| Command | What it does |
|---|---|
| `berthkeep` / `ui` | Open the window (starts the background service if needed) |
| `new <title>` | Create a session: `--temp`, `--ephemeral`, `--port n`, `--clone <session>`, `--adopt <dir>`, `--browser id`, `--headless`, `--label a,b`, `--url u`, `--start` |
| `list` | Sessions with live state (`--json`, `--label x`) |
| `start` / `stop` / `restart` | Control a session (`stop --all`) |
| `mode <session> headless\|headed` | Relaunch in the other mode on the same port |
| `persist <session>` | Keep a temporary session's profile (moves after it stops, or `--now`) |
| `endpoint <session>` | Print the endpoint (`--ensure`, `--ws`, `--json`) |
| `edit` / `rm` | Change details, delete (kept profiles go to the trash) |
| `tabs` / `open <session> <url>` | List or open tabs |
| `detect` / `adopt <port\|pid\|dir>` | Find browsers already running with a debugging port and manage them |
| `browsers` / `doctor` / `home` | Installed browsers, a health check, where the data lives |
| `serve` / `gui` / `quit` | Run the service headless, with window and tray, or stop it |

Exit codes: 0 ok, 1 usage, 2 not found, 3 conflict (port or profile busy), 4 the browser failed to start.

## Where your data lives

Everything is in one data folder. `berthkeep home` prints it.

1. `--home <dir>` or the `BERTHKEEP_HOME` environment variable, if set;
2. a `data` folder next to the program (portable use), if it exists or the folder is writable;
3. otherwise `%LOCALAPPDATA%\berthkeep`, `~/Library/Application Support/berthkeep` or `~/.local/share/berthkeep`.

It holds `config.json`, `registry.json` (your sessions, with a `.bak` copy),
the profiles under `profiles/` and `temp/`, deleted profiles in `trash/`, and
browser logs. **Profiles contain your logged-in cookies: never share or commit
this folder.**

## Security

- Everything listens on `127.0.0.1` only. Browsers are never exposed to your network.
- The local API needs a per-run token for any change, rejects other host names and only accepts JSON. Web pages cannot drive it.
- Berthkeep never adds `--remote-allow-origins`, so web pages cannot attach to your browsers' debugging ports.
- Chrome 136 and later ignore the debugging port on your everyday profile. Berthkeep refuses to adopt that folder and offers a clone instead.

## Good to know

- Use `127.0.0.1`, not `localhost`: some systems resolve `localhost` to IPv6 first, and browsers listen on IPv4.
- Moving or cloning a profile keeps its logins on the same computer and user account only.
- Desktop apps built on Electron can also expose CDP; register them with a custom executable.

## Build from source

Requires Go 1.26 or newer.

```
go test ./...                                   # unit tests
go test -tags integration ./...                 # real-browser tests (temp profiles, ports 19222+)
go run ./tools/build -version v0.1.0            # archives for this machine in dist/
go run ./tools/build -os linux -variant server  # headless build without GUI libraries
```

Windows builds need no C compiler. The macOS and Linux desktop builds use cgo
and must be built on those systems.

## License

MIT
