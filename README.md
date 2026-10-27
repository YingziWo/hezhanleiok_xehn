# warp-client

A Windows client for Cloudflare WARP that scans for the best endpoint and
connects through it in one click — built on top of
[`vernette/warpscout`](https://github.com/vernette/warpscout)'s scanning engine
with an embedded Clash (mihomo) core for actual traffic routing.

## What it does

| capability                          | status |
|-------------------------------------|--------|
| WARP endpoint scan (2-phase probe + tunnel verify) | ✅ reused from warpscout |
| Account register / rotate           | ✅ reused from warpscout |
| AmneziaWG (AWG) core                 | ✅ `amneziawg-go` userspace (scan) + mihomo `amnezia-wg-option` (routing) |
| WireGuard                           | ✅ mihomo `type: wireguard` |
| MASQUE / MASQUE-H2                  | ✅ mihomo `type: masque` + `network: h2` |
| WARP-in-WARP (nested)               | ✅ mihomo `dialer-proxy` chains |
| Psiphon exit (V1, base + Psiphon)   | ✅ `psiphon.exe` sidecar over a localhost SOCKS; `connect --psiphon` |
| Exit IP / latency / country display | ✅ live probe via mixed-port + cached scan meta |
| Scan-result + account cache (TTL)   | ✅ `internal/cache` (6h TTL, JSON, survives reload) |
| System-proxy mode (default)         | ✅ WinINET registry + `InternetSetOption` notify |
| TUN global mode (optional)          | ✅ mihomo `tun:` block (needs admin; wintun is embedded) |
| Native GUI (double-click portable)    | ✅ sidebar shell — Home/Settings/About/Logs, light+dark theme, 中文/English |
| Portable ZIP, core separated        | ✅ `pack.ps1` → v2rayN-style layout |
| Core one-click download/update      | ✅ GitHub releases auto-provision |

## Quick start (portable, v2rayN style)

```powershell
# 1. unzip anywhere writable
# 2. double-click warp-client.exe  →  native window opens (Proton style)
# 3. click 连接 — first run auto-registers a WARP account + scans
```

ZIP layout (`pack.ps1` assembles it):

```
warp-client\
├── warp-client.exe       ← the only program file: double-click opens the GUI;
│                           arguments run the CLI in the calling terminal
├── core\
│   ├── mihomo.exe        ← the Clash core — replace this file to update it
│   ├── psiphon.exe       ← optional: Psiphon exit sidecar (GPL-3.0)
│   ├── client.config     ← optional: official Psiphon credentials (needed with psiphon.exe)
│   └── server_list.dat   ← optional: initial Psiphon server list
└── README.txt
```

The ZIP is assembled from a fresh staging tree and never contains
`warpscout-account.json`, state, settings, caches or logs: a new device has no
credentials, so its first connect/scan registers a new WARP account there. Do
not copy an old `warpscout-account.json` along with the program unless you
intend to reuse that device identity.

The executable auto-anchors `WARP_CLIENT_HOME` to its own folder (portable
mode), so account, cache, state, config and logs all stay next to the exe —
delete the folder and everything is gone.

### Updating the core later (核心分离)

The GUI never embeds mihomo; it manages `core\mihomo.exe` as a plain file:

- **One click**: dashboard → 「更新核心」 downloads the latest
  `mihomo-windows-amd64` release.
- **Manual**: drop any mihomo build into `core\mihomo.exe` — no rebuild, no
  version lock. `warp-client.exe` also accepts `-core PATH` to point elsewhere.

### The GUI

`warp-client.exe gui` (or a bare double-click) opens a
**native Windows window** — self-drawn on [`lxn/walk`](https://github.com/lxn/walk)
(a single `CustomWidgetPixels` canvas; no browser, no webview, no runtime UI
dependency). The layout is a 1080x700 grid scaled by the display DPI, so it
stays sharp from 100% to 250% Windows scaling.

A left rail switches between four pages, and the header carries the two
appearance switches (theme, language) so neither needs a trip to settings:

| page | contents |
|---|---|
| **Home** | connect/disconnect circle with the session clock, current exit (flag, colo, latency, up/down), protocol selector with live core-capability state, selected node detail + live throughput chart |
| **Settings** | one page, four cards: capture (system proxy or VPN/TUN, local proxy port, LAN access, API port, address family with protocol gating, MTU), routing (proxy mode, default node, split tunnelling, domain lists, geo split), startup/behaviour (autostart, auto-connect, rescan, auto-repair, remember page, timeout), DNS & exit (presets, no-DNS, Psiphon + region) |
| **About** | versions, data/core paths, licences, client and core updates |
| **Logs** | run log (level-tagged console), connection history (the connect pipeline's own steps), diagnostics (copyable) |

Theme: **light (default)**, dark, or follow the system — switched live, title
bar included. Language: **中文 (default)** or English; every string goes through
one table, so a missing translation falls back to Chinese instead of showing a
key.

#### Screenshots

**Home** — the connect circle with the session clock, the live exit (flag,
colo, latency, throughput) and the protocol picker:

![Home](docs/screenshots/01-home.png)

**Settings** — one scrollable page: capture (system proxy or VPN/TUN, local
port, LAN access, API port, address family, MTU) and routing (preset, default
node, direct/proxy domain lists):

![Settings — capture and routing](docs/screenshots/02-settings-capture.png)

…then startup/behaviour (autostart, auto-connect, rescan, auto-repair,
timeout) and DNS & exit:

![Settings — behaviour and DNS](docs/screenshots/03-settings-behaviour.png)

**Logs** — runtime log, connection history and copyable diagnostics:

![Logs](docs/screenshots/04-logs.png)

**About** — versions, paths, licences, client and core updates:

![About](docs/screenshots/05-about.png)

The same window in English (the language pill lives in the Home header, so it
never needs a trip to settings):

![English UI](docs/screenshots/06-logs-en.png)

Preferences live in `settings.json` next to the exe (portable layout) and are
applied to the engine on connect: proxy mode and the domain lists become mihomo
rules, per-app split becomes `PROCESS-NAME` rules, and DNS / MTU / address
family go into the rendered config. The region split is offered only when the
core's `geosite.dat` / `geoip.metadb` are already present — mihomo tries to
download them as soon as a `GEOSITE` rule appears and refuses the whole config
when it cannot, so the switch stays off rather than producing a config that
cannot start.

The GUI runs the exact same code path as the CLI
(`defaultConnectOptions` → `runConnectCmd`), so behaviour cannot drift.

## Architecture

```
warp-client (package main)            the warpscout engine (verbatim) +
                                     the client orchestration layer
├── cli.go / flags.go                command registry + flag wiring
├── main.go                          scan command (warpscout)
├── register.go                      account registration (warpscout)
├── tunnel.go / masque.go / nest.go  userspace tunnels: AWG/MASQUE/WARP-in-WARP
├── wgconf.go                        native .conf + mihomo proxy rendering
├── client.go          ★             full mihomo config renderer + cache glue
├── connect.go         ★             connect/disconnect/status/list commands
├── gui.go             ★             GUI/CLI shared entry (defaultConnectOptions)
├── gui_dispatch.go    ★             `gui` command + double-click entry
├── gui_native_windows.go ★          window, model, paint dispatch, background loops
├── gui_theme_windows.go ★           light/dark palettes, theme resolve, dark title bar
├── gui_i18n_windows.go ★            中文/English string table (tr/trf)
├── gui_draw_windows.go ★            canvas primitives + widgets (cards, chips, switches)
├── gui_shell_windows.go ★           sidebar + page header + page routing
├── gui_page_home_windows.go ★       Home page
├── gui_page_nodes_windows.go ★      Nodes page (not in the sidebar; node choice is the Settings pin)
├── gui_page_settings_windows.go ★   Settings page (capture / routing / startup / DNS & exit)
├── gui_page_logs_windows.go ★       Logs page (run / history / diagnostics)
├── gui_actions_windows.go ★         id → action routing, settings actions
├── gui_dialogs_windows.go ★         modal input/confirm, autostart, prefs plumbing
├── gui_native_stub.go ★             non-Windows no-op (native window = disabled)
├── settings.go        ★             settings.json model, load/save, normalisation
│
├── internal/cache     ★             TTL cache store (account + scan results)
└── internal/core      ★             mihomo sidecar manager + system proxy
    ├── core.go                      process lifecycle, config write, readiness
    ├── download.go                  locate/download mihomo core binary
    ├── proxy_windows.go             WinINET system proxy set/unset
    └── proxy_other.go               non-Windows no-op stub
```

`★` = new code added on top of the warpscout engine.

## The two "cores" the client introduces

1. **AWG core** — `amneziawg-go` (already a warpscout dependency). It is the
   userspace AmneziaWG implementation used both for *scanning* (the tunnel that
   verifies an endpoint really exits where it claims) and as the basis the
   mihomo proxy renders into.
2. **Clash core** — mihomo (MetaCubeX/mihomo), run as a **sidecar binary**. This
   is the same pattern every mature Clash GUI (Clash Verge, FlClash, …) uses:
   the client writes a full `config.yaml`, spawns the core, controls it over
   the `external-controller` REST API, and points the OS at its mixed port.
   mihomo natively speaks `wireguard` (+ `amnezia-wg-option`), `masque`
   (+ `network: h2`) and `dialer-proxy` chains, so a single embedded core
   covers all four protocols + WARP-in-WARP for routing.

## Build

Requires Go 1.26+ (module targets 1.26.3). On networks where
`proxy.golang.org` is unreachable, point Go at a mirror:

```powershell
$env:GOMODCACHE='E:\xiaohe\xhwarp\.gomod'
$env:GOCACHE='E:\xiaohe\xhwarp\.gocache'
$env:GOPATH='E:\xiaohe\xhwarp\.gopath'
$env:GOPROXY='https://goproxy.cn,direct'

# one program file: double-click opens the GUI, arguments run the CLI
go build -trimpath -ldflags "-s -w -H windowsgui -X main.version=0.3.0 -X main.windowsGUIBuild=1" -o warp-client.exe .

# everything above + the portable ZIP in dist\ (the supported release path):
powershell -ExecutionPolicy Bypass -File .\pack.ps1 [-MihomoPath <mihomo.exe>] [-PsiphonExePath <psiphon.exe>] [-PsiphonConfigPath <client.config>]
```

The native window links a Win32 manifest (comctl32 v6) embedded into
`rsrc_windows_amd64.syso` — without it `lxn/walk` fails at startup with
`TTM_ADDTOOL failed`. `pack.ps1` regenerates the `.syso` from `app.manifest`
using `akavel/rsrc` (install once with `go install github.com/akavel/rsrc@latest`).

`-H windowsgui` strips the console subsystem flag, so a double-click never
flashes a terminal window. The same binary still serves the CLI: with a
subcommand it attaches to the caller's console and reopens its standard
streams. With no console attached the GUI redirects all output (its own and the
mihomo core's) to `warp-client.log` next to the exe. Because Windows does not
wait for a GUI-subsystem process, `cmd` returns to its prompt immediately while
register/connect keep running in the background; their output stays attached to
that terminal.

## Usage

```powershell
# 0. no arguments at all → the native GUI window (double-click equivalent)
warp-client.exe
warp-client.exe gui      # force the native window

# 1. register a WARP account
warp-client register

# 2. connect — scans (cached 6h), picks best endpoint, starts core, sets proxy
warp-client connect -p awg
warp-client connect -p wg
warp-client connect -p masque
warp-client connect -p masque-h2
warp-client connect --tun            # TUN global mode (needs admin)
warp-client connect --scan-now       # force re-scan, ignore cache
warp-client connect -p awg --psiphon # base protocol + Psiphon exit (final IP = Psiphon)

# 3. show current exit IP / latency / country
warp-client status

# 4. list cached endpoints
warp-client list
warp-client list -p wg

# 5. disconnect — stops core, restores prior system-proxy state
warp-client disconnect

# raw engine commands (from warpscout) still work:
warp-client scan -best -p awg
warp-client scan -conf - -conf-type mihomo -p awg
```

### Portable / custom home

When `WARP_CLIENT_HOME` is unset, the client auto-anchors its home to the
executable's folder (if writable — e.g. the extracted ZIP) and falls back to
`%APPDATA%\warp-client` otherwise. Set the variable explicitly to pin it:

```powershell
$env:WARP_CLIENT_HOME='D:\warp-portable'
warp-client connect
```

### Psiphon exit (V1)

Psiphon is an **exit**, not a fifth protocol. `connect --psiphon` brings the base
protocol (WG / AWG / MASQUE) up first, points Psiphon's upstream at that localhost
SOCKS, and makes Psiphon the final egress — so the public exit IP becomes
Psiphon's. The session is a composition: `status` shows e.g. `session=AWG-Psiphon`,
while `state.Proto` stays the base protocol (`awg`) and the switch lives in
`psiphon_mode`.

Assets — both required; `connect --psiphon` fails fast with a clear message if
either is missing:

| asset | default location | override |
|---|---|---|
| Psiphon executable | `core\psiphon.exe` | `--psiphon-exe` flag, else `PSIPHON_EXE` env |
| official client config | `core\client.config`, then `core\psiphon.config` | `PSIPHON_CONFIG` env |

The client config carries the `SponsorId` / `PropagationChannelId` /
`RemoteServerListURLs` / `ObfuscatedServerListRootURLs` and their signature keys
— without it Psiphon discovers zero servers and never builds a tunnel. A
`server_list.dat` next to it is used as the initial server list.

```powershell
warp-client connect -p awg --psiphon
warp-client status            # session=AWG-Psiphon  endpoint=…  exit=<Psiphon exit IP>
```

`psiphon-tunnel-core` is **GPL-3.0**. It ships as a separate `psiphon.exe`
process driven over a localhost SOCKS and is never linked into this binary.
`pack.ps1` bundles `psiphon.exe` / `client.config` / `server_list.dat` into
`core\` when present (or when passed via `-PsiphonExePath` /
`-PsiphonConfigPath`).

Because the ZIP ships GPL-3.0 binaries (mihomo too), the pack also carries the
license text and the obligations inventory: see
[`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md) and
`licenses/GPL-3.0.txt` in the ZIP. **Legal review is required before public
distribution** — that file is an engineering inventory, not a legal conclusion.

### TUN mode

`--tun` adds a `tun:` block to the mihomo config and routes all traffic through
a virtual interface instead of the system proxy. Requirements:

- Run the client as **administrator** (elevated). The mihomo build this client
  ships embeds wintun, so no extra `wintun.dll` is required.

## Tests

```powershell
go test ./...          # hermetic suite

# + real-core integration test (renders configs for all 4 protocols, boots a
#   real mihomo, asserts controller + proxy registration + mixed-port):
$env:WARP_TEST_MIHOMO='path\to\mihomo.exe'
go test -v -run TestMihomoAcceptsRenderedConfig .
```

`internal/cache` — put/get round-trip, TTL expiry, reload survival, corrupt-file
recovery. `internal/core` — config validation, atomic write, readiness timeout.
`package main` — cached-scan round-trip, state persistence, `list`/`status`
clean degradation, endpoint ranking tie-breaks, portable-home override, and
(with `WARP_TEST_MIHOMO`) the live core integration across `awg` / `wg` /
`masque` / `masque-h2`.
