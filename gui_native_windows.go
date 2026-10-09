//go:build windows

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/sys/windows"

	"github.com/lxn/walk"

	"github.com/xhwarp/warp-client/internal/core"
)

// guiNativeAvailable reports that this platform builds a native window.
const guiNativeAvailable = true

// fonts is the whole type ramp: eight text sizes plus a monospace face for the
// log console. The palettes live in gui_theme_windows.go.
type fonts struct {
	f8, f9, f10, f10b, f11b, f12b, f13b, f15b, f17b *walk.Font
	fmono                                            *walk.Font
}

// ---------------------------------------------------------------------------
// Model — mutated only on the GUI thread; workers compute off-thread and hand
// results back through mw.Synchronize.
// ---------------------------------------------------------------------------

type natModel struct {
	proto      string
	outerProto string // WARP-in-WARP outer carrier (wg|awg)
	innerProto string // WARP-in-WARP inner tunnel (wg|awg)
	tun        bool
	rescan     bool // force a fresh scan before connecting
	psiphon    bool // Psiphon exit switch: base protocol + Psiphon (never for wwarp)

	endpoints []cachedEndpoint
	pin       string
	scanFresh bool
	scanAt    time.Time

	connecting bool
	working    bool // true while a connect/disconnect goroutine is in flight
	connected  bool
	status     clientState
	latency    int64 // live proxy-group delay ms
	samples    []int64
	upRates    []int64 // live throughput samples, bytes/sec (upload)
	downRates  []int64 // live throughput samples, bytes/sec (download)

	logs []natLog // newest last; capped by addLog

	coreInstalled bool
	coreSize      int64
	coreBusy      bool
	coreVersion   string          // parsed from `mihomo -v` by the capability probe
	capab         map[string]bool // protocol → core can parse its config

	proxyBlocker   string // security suite detected while ProxyWarn is set (banner hint)
	blockerChecked bool   // DetectProxyBlocker ran once (touched only by statusLoop)

	hover  string
	spin   int
	scroll int // node list scroll (rows)

	// Shell state: which page, which tab inside it, and the node list's own
	// view (search text, filter, sort, how many rows fit).
	page            string
	nodeSearch      string
	nodeFilter      string
	nodeSort        string
	nodeVisibleRows int
	ruleTab         string
	setTab          string
	logTab          string
	frozenLogs      []natLog // frozen snapshot while auto-scroll is off

	// set is the persisted preference file, kept in the model so every page
	// renders straight from it.
	set        clientSettings
	setDirty   bool // a setting changed while connected: reconnect applies it
	updLatest  string
	updChecked bool
	updTime    time.Time
	updBusy    bool
	updNote    string
}

// natLog is one line of the log panel.
type natLog struct {
	at    time.Time
	text  string
	lv    int  // 0 = info, 1 = warning, 2 = error
	core  bool // came from the mihomo core, not the client itself
	stage bool // connection pipeline progress: the "sessions" view's material
}

func newNatModel() *natModel {
	return &natModel{
		proto:      protoWG,
		outerProto: protoWG,
		innerProto: protoWG,
		samples:    make([]int64, 0, 240),
		page:       pageHome,
		nodeFilter: "all",
		nodeSort:   "latency",
		ruleTab:    "global",
		setTab:     "general",
		logTab:     "run",
		set:        defaultSettings(),
	}
}

// ---------------------------------------------------------------------------
// UI + spot hit-testing
// ---------------------------------------------------------------------------

type spot struct {
	id      string
	r       walk.Rectangle
	enabled bool
}

type natUI struct {
	mw *walk.MainWindow
	cw *walk.CustomWidget

	m *natModel

	f  fonts
	sp []spot
	k  float64

	ctx    context.Context
	cancel context.CancelFunc

	// repairing keeps only one repair inside this process. Ownership keeps
	// other processes out, but nothing stops this loop from meeting the fail
	// count again while the goroutine it already spawned is still verifying a
	// candidate - and two repairs at once means two sets of core kills,
	// state writes and proxy changes interleaved on one session.
	repairing atomic.Bool
}

func (u *natUI) s(v float64) int { return int(v*u.k + 0.5) }

func (u *natUI) r(x, y, w, h float64) walk.Rectangle {
	return walk.Rectangle{X: u.s(x), Y: u.s(y), Width: u.s(w), Height: u.s(h)}
}

func (u *natUI) p(x, y float64) walk.Point { return walk.Point{X: u.s(x), Y: u.s(y)} }

func (u *natUI) addSpot(id string, r walk.Rectangle, enabled bool) {
	u.sp = append(u.sp, spot{id: id, r: r, enabled: enabled})
}

// ---------------------------------------------------------------------------
// Entry point
// ---------------------------------------------------------------------------

func runNativeGUI(ctx context.Context, _ options) error {
	redirectLogs()
	enableDPIAwareness()

	mw, err := walk.NewMainWindow()
	if err != nil {
		return fmt.Errorf("create window: %w", err)
	}
	if tb := mw.ToolBar(); tb != nil {
		tb.SetVisible(false)
	}
	mw.SetTitle("xiaohe VPN Client")
	if err := mw.SetClientSize(walk.Size{Width: 1080, Height: 700}); err != nil {
		mw.Dispose()
		return fmt.Errorf("set client size: %w", err)
	}
	mw.SetMinMaxSize(walk.Size{Width: 1080, Height: 700}, walk.Size{})

	u := &natUI{
		mw:  mw,
		m:   newNatModel(),
		sp:  make([]spot, 0, 128),
		ctx: ctx,
	}

	// Preferences first, so the window opens on the page, theme and language
	// the user chose last time. Autostart is read back from the registry rather
	// than trusted from the file: the registry is what actually launches the
	// program, and a value removed by hand must not keep showing as "on".
	u.m.set = loadSettings()
	u.m.set.AutoStart = autoStartEnabled()
	if u.m.set.RememberPage {
		u.m.page = u.m.set.Page
	} else {
		u.m.page = pageHome
	}
	u.syncSessionFromSettings()
	u.applyTheme()
	u.applyLang()

	for _, fr := range []struct {
		dst  **walk.Font
		name string
		sz   int
		st   walk.FontStyle
	}{
		{&u.f.f8, "Microsoft YaHei", 8, 0},
		{&u.f.f9, "Microsoft YaHei", 9, 0},
		{&u.f.f10, "Microsoft YaHei", 10, 0},
		{&u.f.f10b, "Microsoft YaHei", 10, walk.FontBold},
		{&u.f.f11b, "Microsoft YaHei", 11, walk.FontBold},
		{&u.f.f12b, "Microsoft YaHei", 12, walk.FontBold},
		{&u.f.f13b, "Microsoft YaHei", 13, walk.FontBold},
		{&u.f.f15b, "Microsoft YaHei", 15, walk.FontBold},
		{&u.f.f17b, "Microsoft YaHei", 17, walk.FontBold},
		{&u.f.fmono, "Consolas", 9, 0},
	} {
		*fr.dst, err = walk.NewFont(fr.name, fr.sz, fr.st)
		if err != nil {
			mw.Dispose()
			return fmt.Errorf("font: %w", err)
		}
	}

	cw, err := walk.NewCustomWidgetPixels(mw, 0, u.paint)
	if err != nil {
		mw.Dispose()
		return err
	}
	cw.SetPaintMode(walk.PaintBuffered)
	cw.SetBoundsPixels(mw.ClientBoundsPixels())
	u.cw = cw

	// The custom widget is the form's only child and must fill it. Without a
	// layout, FormBase.startLayout() (run when the window becomes visible)
	// dereferences a nil layout; a zero-margin VBox stretches the greedy
	// custom widget across the whole client area.
	layout := walk.NewVBoxLayout()
	_ = layout.SetMargins(walk.Margins{})
	_ = layout.SetSpacing(0)
	if err := mw.SetLayout(layout); err != nil {
		mw.Dispose()
		return fmt.Errorf("set layout: %w", err)
	}

	u.ctx, u.cancel = context.WithCancel(ctx)

	mw.SizeChanged().Attach(func() {
		b := mw.ClientBoundsPixels()
		cw.SetBoundsPixels(walk.Rectangle{X: 0, Y: 0, Width: b.Width, Height: b.Height})
		cw.Invalidate()
	})

	cw.MouseDown().Attach(func(x, y int, _ walk.MouseButton) { u.onDown(x, y) })
	cw.MouseMove().Attach(func(x, y int, _ walk.MouseButton) { u.onMove(x, y) })
	cw.MouseWheel().Attach(func(x, y int, b walk.MouseButton) { u.onWheel(x, y, b) })

	go u.statusLoop()
	go u.spinLoop()
	go u.probeCore()         // stats the core, asks it which protocols it can parse
	u.reloadEndpoints(false) // fast cache read on the (main) GUI goroutine

	mw.Show()

	// Auto-connect starts here, after the window exists: the connect pipeline
	// posts its progress through Synchronize, which is a PostMessage and simply
	// waits in the queue until Run drains it below.
	if u.m.set.AutoConnect {
		u.addLog(tr("设置：启动后自动连接"), false)
		u.doConnect()
	}

	_ = mw.Run()

	// Stop the workers before tearing anything down, so no goroutine posts into
	// a disposed window.
	if u.cancel != nil {
		u.cancel()
	}
	// The session choices (protocol, TUN, Psiphon, autostart) are part of the
	// preference file too, so closing the window is the last chance to record
	// them for the next run.
	u.persistSession()
	// State on disk is the source of truth (the UI flag can lag a connect
	// that just finished), so always run the disconnect cleanup: the system
	// proxy must never outlive the window, and the elevated daemon (when in
	// use) makes the restore silent. Safe on an already-disconnected machine.
	_ = runDisconnectCmd(context.Background(), options{mixedPort: core.DefaultMixedPort})
	// Hand health ownership back: this window's claim must not outlive it, or
	// every other process would keep standing aside for an owner that left.
	releaseHealthOwner(os.Getpid())
	u.disposeFonts()
	cw.Dispose()
	mw.Dispose()
	return nil
}

func (u *natUI) disposeFonts() {
	for _, f := range []*walk.Font{
		u.f.f8, u.f.f9, u.f.f10, u.f.f10b, u.f.f11b,
		u.f.f12b, u.f.f13b, u.f.f15b, u.f.f17b, u.f.fmono,
	} {
		if f != nil {
			f.Dispose()
		}
	}
}

func (u *natUI) sync(f func()) {
	if u.cw != nil {
		u.mw.Synchronize(f)
	}
}

func (u *natUI) invalidate() {
	if u.cw != nil {
		_ = u.cw.Invalidate()
	}
}

// ---------------------------------------------------------------------------
// DPI + dark title bar (lazy DLL, no CGO)
// ---------------------------------------------------------------------------

func enableDPIAwareness() {
	user32 := windows.NewLazySystemDLL("user32.dll")
	proc := user32.NewProc("SetProcessDpiAwarenessContext")
	for _, c := range []uintptr{^uintptr(3), ^uintptr(2)} { // -4, -3
		if r, _, _ := proc.Call(c); r != 0 {
			return
		}
	}
	user32.NewProc("SetProcessDPIAware").Call()
}

// ---------------------------------------------------------------------------
// Workers
// ---------------------------------------------------------------------------

func (u *natUI) statusLoop() {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	var blocker string
	// Consecutive failed tunnel probes. An endpoint DPI tears down
	// mid-session leaves a Connected light over a dark tunnel — the exact
	// "shows connected but every page fails" state this client must never
	// show. Three misses in a row (≥6s, and each probe already waits 8s on
	// a hanging tunnel) is death; one slow answer must not trip it.
	tunnelFails := 0
	const tunnelFailLimit = 3
	for {
		select {
		case <-u.ctx.Done():
			return
		case <-t.C:
		}
		st, err := loadState()
		if err != nil {
			st = clientState{}
		}
		// The proxy-blocked banner names the usual suspect once, off the GUI
		// thread; the result outlives the loop variable.
		if st.ProxyWarn != "" && !u.m.blockerChecked {
			u.m.blockerChecked = true
			blocker = core.DetectProxyBlocker()
		}
		var exIP, cc, colo, city string
		var lat int64 = -1
		var up, down int64 = -1, -1
		if st.Connected {
			// Start from the persisted values and only override with fields the
			// probe actually returned: a CN egress gets 403/empty JSON from
			// speed.cloudflare.com/meta, and blanking the country/colo that the
			// connect itself verified makes them flicker off right after
			// "Connected" appears.
			exIP, cc, colo, city = st.ExitIP, st.Country, st.Colo, st.NodeCity
			if probe, perr := fetchExitProbe(st.MixedPort); perr == nil {
				tunnelFails = 0
				if probe.ExitIP != "" {
					exIP = probe.ExitIP
				}
				if probe.Country != "" {
					cc = probe.Country
				}
				if probe.Colo != "" {
					colo = probe.Colo
				}
				if probe.NodeCity != "" {
					city = probe.NodeCity
				}
			} else {
				tunnelFails++
			}
			if d, err := core.ProxyDelay(st.APIPort, "WARP", "http://www.gstatic.com/generate_204"); err == nil {
				lat = d
			}
			if u2, d2, err := core.Traffic(st.APIPort); err == nil {
				up, down = u2, d2
			}
			// Ownership decides who is allowed to repair, and it is decided by
			// the claim: this GUI only gets it when nobody else holds it -
			// which in practice means the detached guard from a CLI connect
			// keeps the job it already has, and this window stays a watcher.
			// Only a session with no living owner (the guard killed outright,
			// then no process caught it) leaves the claim free for this tick.
			// "Auto repair" is the user's switch over all of this: off means the
			// session is left alone, and this window does not even claim it, so
			// nobody else stands aside for a window that has opted out.
			ownsHealth := false
			if u.m.set.AutoRepair {
				ownsHealth = claimHealthOwner(os.Getpid())
			}

			// Tunnel death: the state says Connected but the data plane has
			// not answered three probes in a row. Repair it first - rotate to
			// another cached endpoint - and only stop when nothing left can
			// carry traffic. Disconnecting used to be the first move, which
			// put every repair on the user as another click, and the same dead
			// endpoint answered that click because only a failing verify ever
			// marked it dead. Never fires while a connect/disconnect/switch is
			// in flight: those own the state. Never fires unowned either -
			// another process already owns repair, and doing it anyway is how
			// two schedules end up killing each other's cores.
			if tunnelFails >= tunnelFailLimit && ownsHealth && !u.m.working && !u.m.connecting && u.repairing.CompareAndSwap(false, true) {
				tunnelFails = 0
				u.sync(func() {
					u.addLog("数据面无响应（端点已失效）：正在自动切换到备用端点…", true)
				})
				go func(st clientState) {
					defer u.repairing.Store(false)
					logf := func(s string) {
						u.sync(func() { u.addLog(s, false) })
					}
					opts := optionsFromState(st)
					tried := map[string]bool{}
					newState, ok := repairOnce(u.ctx, opts, st.Endpoint, tried, logf)
					if !ok {
						// Every cached candidate failed too: the cache is
						// stale as a whole. One fresh scan, then admit defeat.
						logf("缓存端点全部失活，重新扫描…")
						opts.scanNow = true
						newState, ok = repairOnce(u.ctx, opts, st.Endpoint, tried, logf)
					}
					if ok {
						u.sync(func() {
							u.m.status = newState
							u.m.connected = true
							u.addLog("已自动恢复：新端点 "+newState.Endpoint, false)
							u.invalidate()
						})
						return
					}
					giveUp(logf)
					u.sync(func() {
						u.m.connected = false
						u.m.connecting = false
						u.m.working = false
						if st2, lerr := loadState(); lerr == nil {
							u.m.status = st2
						}
						u.addLog("隧道已断开：代理已恢复、核心已停止。请重新点击连接", true)
						u.invalidate()
					})
				}(st)
			}
		} else {
			tunnelFails = 0
		}
		u.sync(func() {
			st.ExitIP, st.Country, st.Colo, st.NodeCity = exIP, cc, colo, city
			u.m.status = st
			u.m.proxyBlocker = blocker
			u.m.connected = st.Connected
			if lat >= 0 {
				u.m.latency = lat
				u.m.samples = append(u.m.samples, lat)
				if len(u.m.samples) > 120 {
					u.m.samples = u.m.samples[len(u.m.samples)-120:]
				}
			}
			if up >= 0 && down >= 0 {
				u.m.upRates = append(u.m.upRates, up)
				u.m.downRates = append(u.m.downRates, down)
				if len(u.m.upRates) > 120 {
					u.m.upRates = u.m.upRates[len(u.m.upRates)-120:]
				}
				if len(u.m.downRates) > 120 {
					u.m.downRates = u.m.downRates[len(u.m.downRates)-120:]
				}
			}
			if !st.Connected && (len(u.m.upRates) > 0 || len(u.m.downRates) > 0) {
				u.m.upRates, u.m.downRates = nil, nil
			}
			// Never clobber the busy flag while a connect/disconnect is in
			// flight: first-use connects spend minutes scanning, and wiping
			// the spinner every poll made the button look dead and allowed
			// double-clicks to start overlapping connects.
			if !st.Connected && !u.m.working {
				u.m.connecting = false
			}
			u.invalidate()
		})
	}
}

func (u *natUI) spinLoop() {
	t := time.NewTicker(60 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-u.ctx.Done():
			return
		case <-t.C:
		}
		u.sync(func() {
			if u.m.connecting {
				u.m.spin++
				u.invalidate()
			}
		})
	}
}

func (u *natUI) reloadEndpoints(force bool) {
	proto := u.m.proto
	outer, inner := u.m.outerProto, u.m.innerProto
	if force {
		go func() {
			opts := defaultConnectOptions(proto, false, true)
			if proto == protoWWarp {
				// Mirror runConnectCmd's chain normalisation: a wwarp scan is
				// the inner scan through an auto-picked outer endpoint, never
				// a scan of "wwarp" as a wire protocol.
				opts.outerProto = outer
				opts.innerProto = inner
				if err := ensureAccount(u.ctx, opts); err != nil {
					u.sync(func() {
						u.addLog("账号加载失败: "+err.Error(), true)
						u.invalidate()
					})
					return
				}
				u.sync(func() { u.addLog("[WARP-in-WARP] 选择外层端点…", false) })
				endpoint, err := pickOuterEndpoint(u.ctx, opts)
				if err != nil {
					u.sync(func() {
						u.addLog("外层端点获取失败: "+err.Error(), true)
						u.invalidate()
					})
					return
				}
				opts.through = endpoint
				opts.proto = opts.outerProto
				u.sync(func() {
					u.addLog(fmt.Sprintf("[WARP-in-WARP] 外层端点 %s，扫描内层端点…", endpoint), false)
				})
			}
			u.sync(func() { u.addLog("重新扫描 WARP 端点（可能需要 1-2 分钟）…", false) })
			_, _, err := runCachedScan(u.ctx, opts)
			u.sync(func() {
				if err != nil {
					u.addLog("扫描失败: "+err.Error(), true)
				} else {
					u.addLog("扫描完成", false)
				}
				u.applyEndpoints(proto)
				u.invalidate()
			})
		}()
		return
	}
	u.applyEndpoints(proto)
}

// protoOK reports whether the installed core parses this protocol's config.
// Before the probe finishes (or with no core installed) it optimistically
// returns true: the connect pipeline runs the authoritative gate anyway.
func (u *natUI) protoOK(p string) bool {
	if !u.m.coreInstalled || u.m.capab == nil {
		return true
	}
	return u.m.capab[p]
}

// probeCore stats the core binary and, when present, asks it which protocols
// it can actually parse (`mihomo -t` on a minimal config per protocol). The
// result drives the protocol tiles (unsupported ones are visibly marked) and
// the version line. Runs off the GUI thread; probeCoreCapabilities caches, so
// repeat calls are cheap.
func (u *natUI) probeCore() {
	dir, err := coreDir()
	if err != nil {
		return
	}
	u.sync(func() { u.refreshCoreCard(dir) })
	bin := core.CoreInDir(dir)
	if _, err := os.Stat(bin); err != nil {
		return // not installed yet; doCore re-probes after the download
	}
	ver, supported, perr := probeCoreCapabilities(bin, dir)
	u.sync(func() {
		u.m.coreVersion = ver
		u.m.capab = supported
		if perr != nil {
			u.addLog("Core 能力探测失败: "+perr.Error(), true)
		} else {
			var ok []string
			for _, r := range protoRuns {
				if supported[r.name] {
					ok = append(ok, protoLabel(r.name))
				}
			}
			u.addLog("Core "+ver+" 就绪，支持协议: "+strings.Join(ok, "、"), false)
		}
		u.invalidate()
	})
}

func (u *natUI) applyEndpoints(proto string) {
	cs, ok := cachedScanFor(proto, false)
	if !ok || len(cs.Results) == 0 {
		u.m.endpoints = nil
		u.m.scanFresh = false
		return
	}
	u.m.endpoints = cs.Results
	u.m.scanAt = cs.At
	u.m.scanFresh = time.Since(cs.At) < scanTTL
}

// ---------------------------------------------------------------------------
// Actions
// ---------------------------------------------------------------------------

// retryProxySet re-runs the host-side WinINET edit for a live connect whose
// first attempt security software blocked — the user has typically just added
// the client to the suite's trust list. The tunnel itself was already verified
// by the connect, so this only touches the proxy values and the state file.
func (u *natUI) retryProxySet() {
	st, err := loadState()
	if err != nil || !st.Connected || st.MixedPort == 0 {
		return
	}
	u.sync(func() { u.addLog("重新设置系统代理…", false) })
	snap, err := core.SetSystemProxy(st.MixedPort)
	if err != nil && proxyHelperAllowed() {
		u.sync(func() { u.addLog("请求管理员授权设置系统代理（仅本次需要 UAC 确认）…", false) })
		if herr := core.SetSystemProxyElevated(st.MixedPort); herr == nil {
			err = nil
		}
	}
	if err != nil {
		u.sync(func() { u.addLog("系统代理仍被拦截: "+err.Error(), true) })
		return
	}
	if raw, jerr := json.Marshal(snap); jerr == nil {
		st.ProxySnap = raw
	}
	st.ProxyWarn = ""
	if serr := saveState(st); serr != nil {
		u.sync(func() { u.addLog("状态保存失败: "+serr.Error(), true) })
		return
	}
	u.sync(func() { u.addLog("系统代理设置成功", false) })
}

func (u *natUI) addLog(text string, err bool) {
	lv := 0
	if err {
		lv = 2
	}
	u.addLogFull(text, lv, false)
}

// addLogFull appends one line to the log panel; core lines are dimmed.
func (u *natUI) addLogFull(text string, lv int, core bool) {
	u.appendLog(text, lv, core, false)
}

// addLogStage records a line produced by the connection pipeline itself. Those
// lines are the "sessions" view: attempts, endpoint choice, failure and
// disconnect, without the core's own chatter.
func (u *natUI) addLogStage(text string) { u.appendLog(text, 0, false, true) }

func (u *natUI) appendLog(text string, lv int, core, stage bool) {
	u.m.logs = append(u.m.logs, natLog{at: time.Now(), text: text, lv: lv, core: core, stage: stage})
	if n := len(u.m.logs); n > 400 {
		u.m.logs = u.m.logs[n-400:]
	}
}

func (u *natUI) action(id string) {
	m := u.m
	switch {
	case id == "connect":
		if m.connecting {
			return
		}
		if m.connected {
			u.doDisconnect()
		} else {
			u.doConnect()
		}
	case strings.HasPrefix(id, "proto:"):
		p := strings.TrimPrefix(id, "proto:")
		if p != m.proto {
			// Capability gate: never switch to (let alone render) a protocol
			// the installed core cannot parse — say so instead.
			if !u.protoOK(p) {
				u.addLog(trf("当前 Core 不支持 %s 协议，请升级 Core（Mihomo 官方最新版）", protoLabel(p)), true)
				return
			}
			m.proto = p
			m.pin = ""
			m.scroll = 0
			u.reloadEndpoints(false)
			u.invalidate()
			// Hot switch: a live connection swaps protocols without making
			// the user disconnect by hand first.
			if m.connected {
				u.doSwitch()
			}
		}
	case id == "tun":
		if !m.connected {
			m.tun = !m.tun
			u.invalidate()
		}
	case id == "rescan":
		if !m.connected {
			m.rescan = !m.rescan
			u.invalidate()
		}
	case id == "psiphon":
		m.psiphon = !m.psiphon
		u.invalidate()
		// Hot switch, like the protocol chips: a live connection swaps its exit
		// without making the user disconnect by hand first.
		if m.connected && m.proto != protoWWarp && !m.working && !m.connecting {
			u.doSwitch()
		}
	case id == "outer:wg" || id == "outer:awg":
		p := strings.TrimPrefix(id, "outer:")
		if m.outerProto != p && !m.working && !m.connecting {
			m.outerProto = p
			u.invalidate()
			if m.connected && m.proto == protoWWarp {
				u.doSwitch()
			}
		}
	case id == "inner:wg" || id == "inner:awg":
		p := strings.TrimPrefix(id, "inner:")
		if m.innerProto != p && !m.working && !m.connecting {
			m.innerProto = p
			u.invalidate()
			if m.connected && m.proto == protoWWarp {
				u.doSwitch()
			}
		}
	case id == "pin:clear":
		m.pin = ""
		u.invalidate()
	case strings.HasPrefix(id, "row:"):
		m.pin = strings.TrimPrefix(id, "row:")
		u.invalidate()
	case id == "core":
		u.doCore()
	case id == "rescannow":
		u.reloadEndpoints(true)
	case id == "logclear":
		u.m.logs = nil
		u.m.frozenLogs = nil
		u.invalidate()
	case id == "proxyretry":
		go u.retryProxySet()
	case id == "logcopy":
		if len(u.m.logs) > 0 {
			var b strings.Builder
			for _, l := range u.m.logs {
				b.WriteString(l.at.Format("15:04:05 "))
				b.WriteString(l.text)
				b.WriteString("\r\n")
			}
			if err := walk.Clipboard().SetText(b.String()); err != nil {
				u.addLog(tr("复制失败")+": "+err.Error(), true)
			} else {
				u.addLog(tr("日志已复制到剪贴板"), false)
			}
			u.invalidate()
		}
	default:
		// Page switcher, settings, about and update: all defined in
		// gui_pages_windows.go, keyed by their id prefix.
		u.actionPages(id)
	}
}

func (u *natUI) doConnect() {
	m := u.m
	if m.connecting || m.working {
		return
	}
	m.connecting = true
	m.working = true
	u.connectAsync()
}

// connectAsync runs the connect pipeline in the background; the caller has
// already set the busy flags (doConnect for a plain connect, doSwitch after
// its disconnect phase finished on the GUI thread).
func (u *natUI) connectAsync() {
	m := u.m
	u.persistSession()
	o := defaultConnectOptions(m.proto, m.tun, m.rescan)
	// Settings-page knobs ride on top of the session choices: ports, DNS,
	// IPv6, MTU, system proxy and split tunnelling.
	m.set.apply(&o)
	// Only an endpoint the user explicitly pinned becomes -pick. Falling back
	// to the best endpoint here would pin the connect to that single
	// candidate: no rotation to the next cached endpoint, no fresh-scan retry
	// - one half-dead endpoint then fails the whole connect (and every
	// protocol's best going half-dead looks like "nothing connects").
	o.pick = m.pin
	// Psiphon is an orthogonal exit switch, never a protocol: V1 supports it on
	// a plain WG/AWG/MASQUE underlay only, so it is ignored for a chain.
	if m.psiphon && m.proto != protoWWarp {
		o.psiphonMode = true
	}
	if m.proto == protoWWarp {
		// WARP-in-WARP: runConnectCmd normalises this into the engine's chain
		// form (outer carrier + inner tunnel + auto-picked outer endpoint);
		// the GUI only contributes the two protocol choices.
		o.outerProto = m.outerProto
		o.innerProto = m.innerProto
	}
	proto := m.proto
	// Tee the mihomo core's stdout into the log panel: the pipe is drained by a
	// scanner that feeds each line back to the GUI thread, and closes by itself
	// once the core dies (EOF) or the window does (ctx).
	pr, pw := io.Pipe()
	o.coreLog = pw
	go func() {
		sc := bufio.NewScanner(pr)
		// Closing the read end unblocks the exec copier once the scanner exits
		// (window closed), so the pipe can never wedge the child's stdout.
		defer pr.Close()
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			select {
			case <-u.ctx.Done():
				return
			default:
			}
			lv, msg := parseCoreLine(line)
			u.sync(func() { u.addLogFull(msg, lv, true) })
		}
	}()
	go func() {
		connectStage = u.connectStageHook
		defer func() { connectStage = nil }()
		err := runConnectCmd(u.ctx, o)
		_ = pw.Close()
		u.sync(func() {
			u.m.connecting = false
			u.m.working = false
			// Reflect the outcome immediately: leaving connected untouched here
			// let the button flash "connect" between the spinner stopping and
			// the status loop's next 2s poll picking up the new state.
			u.m.connected = err == nil
			// Refresh the status from disk too: the cached one predates this
			// connect, and its stale proxy_warn flashed the blocked banner for
			// a second even when this connect set the proxy fine.
			if st, lerr := loadState(); lerr == nil {
				u.m.status = st
			}
			if err != nil {
				u.addLog(err.Error(), true)
			} else {
				u.addLog("已连接", false)
			}
			u.applyEndpoints(proto)
			u.invalidate()
		})
	}()
	u.invalidate()
}

func (u *natUI) connectStageHook(s string) {
	u.sync(func() {
		u.addLogStage(s)
		u.invalidate()
	})
}

func (u *natUI) doDisconnect() {
	// Not connecting: the button shows a busy "断开中" while working, so the
	// connect animation/label must not appear for a disconnect.
	u.m.working = true
	go func() {
		err := runDisconnectCmd(u.ctx, options{mixedPort: core.DefaultMixedPort})
		u.sync(func() {
			u.m.connecting = false
			u.m.working = false
			u.m.connected = false
			if err != nil {
				u.addLog(err.Error(), true)
			} else {
				u.addLog("已断开", false)
			}
			// Refresh from disk so the tiles/banner reflect the disconnect
			// immediately instead of on the next 2s poll.
			if st, lerr := loadState(); lerr == nil {
				u.m.status = st
			}
			u.invalidate()
		})
	}()
	u.invalidate()
}

// doSwitch swaps protocols on a live connection: disconnect, then reconnect
// with the new selection. The working flag keeps the button busy for the
// whole sequence; the elevated proxy daemon (when needed) makes both the
// restore and the re-set silent — no second UAC prompt.
func (u *natUI) doSwitch() {
	m := u.m
	if m.working || m.connecting {
		return
	}
	m.connecting = true
	m.working = true
	u.addLog("切换协议：断开当前连接并重连…", false)
	go func() {
		_ = runDisconnectCmd(u.ctx, options{mixedPort: core.DefaultMixedPort})
		u.sync(func() { u.connectAsync() })
	}()
	u.invalidate()
}

func (u *natUI) doCore() {
	if u.m.coreBusy {
		return
	}
	dir, err := coreDir()
	if err != nil {
		u.addLog(err.Error(), true)
		return
	}
	u.m.coreBusy = true
	u.addLog("下载/检查 Clash 核心…", false)
	go func() {
		_, err := core.EnsureBinary(dir, "")
		u.sync(func() {
			u.m.coreBusy = false
			if err != nil {
				u.addLog("核心失败: "+err.Error(), true)
			} else {
				u.addLog("核心已就绪", false)
			}
			u.refreshCoreCard(dir)
			u.invalidate()
		})
		if err == nil {
			// The binary may have been replaced at the same path: drop the
			// cached probe result and re-ask the new core what it supports.
			resetCoreCapabilityCache()
			go u.probeCore()
		}
	}()
	u.invalidate()
}

func (u *natUI) refreshCoreCard(dir string) {
	u.m.coreInstalled = false
	u.m.coreSize = 0
	if fi, err := os.Stat(core.CoreInDir(dir)); err == nil && !fi.IsDir() {
		u.m.coreInstalled = true
		u.m.coreSize = fi.Size()
	}
}

// ---------------------------------------------------------------------------
// Hit-testing input
// ---------------------------------------------------------------------------

func (u *natUI) hit(x, y int) (spot, bool) {
	for i := len(u.sp) - 1; i >= 0; i-- {
		s := u.sp[i]
		if x >= s.r.X && x < s.r.X+s.r.Width && y >= s.r.Y && y < s.r.Y+s.r.Height {
			return s, true
		}
	}
	return spot{}, false
}

func (u *natUI) onDown(x, y int) {
	if s, ok := u.hit(x, y); ok && s.enabled {
		u.action(s.id)
	}
}

func (u *natUI) onMove(x, y int) {
	if s, ok := u.hit(x, y); ok {
		if u.m.hover != s.id {
			u.m.hover = s.id
			u.invalidate()
		}
		return
	}
	if u.m.hover != "" {
		u.m.hover = ""
		u.invalidate()
	}
}

// onWheel scrolls the node list. The row count is whatever the last paint
// managed to fit, so the wheel can never scroll past the end; the wheel only
// acts below the toolbar, which keeps it from reacting to scrolling over the
// page header.
func (u *natUI) onWheel(x, y int, b walk.MouseButton) {
	delta := walk.MouseWheelEventDelta(b)
	if delta == 0 || u.m.nodeVisibleRows == 0 || y < u.s(shellBodyY+52) {
		return
	}
	max := len(u.visibleNodeRows()) - u.m.nodeVisibleRows
	if max < 0 {
		max = 0
	}
	u.m.scroll -= delta / 40
	if u.m.scroll < 0 {
		u.m.scroll = 0
	}
	if u.m.scroll > max {
		u.m.scroll = max
	}
	u.invalidate()
}

// ---------------------------------------------------------------------------
// Painting
// ---------------------------------------------------------------------------

func (u *natUI) paint(c *walk.Canvas, _ walk.Rectangle) error {
	b := u.mw.ClientBoundsPixels()
	pxW := b.Width
	if pxW < 10 {
		pxW = 1080
	}
	// The design grid is 1080x700; the display scales it, and every widget
	// converts through u.r/u.s.
	u.k = float64(pxW) / 1080.0
	u.sp = u.sp[:0]

	if err := c.FillRectanglePixels(u.brush(th.window), walk.Rectangle{X: 0, Y: 0, Width: b.Width, Height: b.Height}); err != nil {
		return err
	}
	u.paintShell(c)
	return nil
}

func (u *natUI) measure(c *walk.Canvas, s string, f *walk.Font) int {
	b, _, _ := c.MeasureTextPixels(s, f, walk.Rectangle{X: 0, Y: 0, Width: 10000, Height: 200}, walk.TextSingleLine|walk.TextNoPrefix)
	return b.Width
}

func (u *natUI) drawSpinner(c *walk.Canvas, cx, cy, radius float64, phase int) {
	for i := 0; i < 12; i++ {
		ang := float64(phase)*0.9 + float64(i)*30.0
		rad := ang * math.Pi / 180.0
		cs, sn := math.Cos(rad), math.Sin(rad)
		x1 := cx + cs*radius
		y1 := cy + sn*radius
		x2 := cx + cs*(radius-8)
		y2 := cy + sn*(radius-8)
		// Fade from the accent toward the panel colour so the arc appears to
		// rotate on the light background.
		t := float64(i) / 11.0
		p := u.pen(rx(byte(245-t*186), byte(246-t*155), byte(249-t*30)))
		_ = c.DrawLinePixels(p, u.p(x1, y1), u.p(x2, y2))
		p.Dispose()
	}
}

// speedText formats a bytes/sec rate for the traffic card.
func speedText(bps int64) string {
	switch {
	case bps >= 1<<20:
		return fmt.Sprintf("%.1f MB/s", float64(bps)/(1<<20))
	case bps >= 1<<10:
		return fmt.Sprintf("%.1f KB/s", float64(bps)/(1<<10))
	default:
		return fmt.Sprintf("%d B/s", bps)
	}
}


// drawFlag paints a small flag chip for an ISO alpha-2 code with plain canvas
// fills: stripes and a glyph approximate every flag traffic can realistically
// exit through; unknown codes get a neutral chip with the code itself.
func (u *natUI) drawFlag(c *walk.Canvas, x, y, w, h float64, cc string) {
	if cc == "" {
		return
	}
	white := rx(255, 255, 255)
	stripes := func(cols ...walk.Color) { // horizontal bands
		sh := h / float64(len(cols))
		for i, col := range cols {
			u.fillRect(c, col, u.r(x, y+float64(i)*sh, w, sh+0.5))
		}
	}
	vstripes := func(cols ...walk.Color) { // vertical bands
		sw := w / float64(len(cols))
		for i, col := range cols {
			u.fillRect(c, col, u.r(x+float64(i)*sw, y, sw+0.5, h))
		}
	}
	disc := func(col walk.Color, dx, dy, dw, dh float64) {
		b := u.brush(col)
		_ = c.FillEllipsePixels(b, u.r(x+dx*w, y+dy*h, dw*w, dh*h))
		b.Dispose()
	}
	star := func(col walk.Color, fx float64) {
		u.textC(c, "★", u.f.f8, col, u.r(x+fx*w, y+2, w*0.45, h-4))
	}
	switch cc {
	case "CN":
		u.fillRect(c, rx(238, 28, 37), u.r(x, y, w, h))
		star(rx(255, 222, 0), 0.28)
	case "SG":
		stripes(rx(239, 51, 64), white)
		star(rx(255, 222, 0), 0.05)
	case "US":
		for i := 0; i < 7; i++ {
			col := rx(178, 34, 52)
			if i%2 == 1 {
				col = white
			}
			u.fillRect(c, col, u.r(x, y+float64(i)*h/7, w, h/7+0.5))
		}
		u.fillRect(c, rx(60, 59, 110), u.r(x, y, w*0.42, h*0.55))
	case "JP":
		u.fillRect(c, white, u.r(x, y, w, h))
		disc(rx(188, 0, 45), 0.26, 0.22, 0.44, 0.56)
	case "DE":
		stripes(rx(0, 0, 0), rx(221, 0, 0), rx(255, 206, 0))
	case "FR":
		vstripes(rx(0, 35, 149), white, rx(237, 41, 57))
	case "IT":
		vstripes(rx(0, 146, 70), white, rx(206, 43, 55))
	case "NL":
		stripes(rx(174, 28, 40), white, rx(33, 70, 139))
	case "RU":
		stripes(white, rx(0, 57, 166), rx(213, 43, 30))
	case "GB":
		u.fillRect(c, rx(1, 33, 105), u.r(x, y, w, h))
		u.fillRect(c, white, u.r(x, y+h*0.4, w, h*0.2))
		u.fillRect(c, white, u.r(x+w*0.4, y, w*0.2, h))
		u.fillRect(c, rx(200, 16, 46), u.r(x, y+h*0.44, w, h*0.12))
		u.fillRect(c, rx(200, 16, 46), u.r(x+w*0.44, y, w*0.12, h))
	case "KR":
		u.fillRect(c, white, u.r(x, y, w, h))
		disc(rx(205, 46, 58), 0.25, 0.28, 0.34, 0.34)
		disc(rx(0, 71, 160), 0.25, 0.42, 0.34, 0.34)
	case "HK":
		u.fillRect(c, rx(222, 41, 16), u.r(x, y, w, h))
		star(white, 0.28)
	case "TW":
		u.fillRect(c, rx(254, 0, 0), u.r(x, y, w, h))
		u.fillRect(c, rx(0, 0, 149), u.r(x, y, w*0.5, h*0.5))
		star(white, 0.05)
	case "IN":
		stripes(rx(255, 153, 51), white, rx(19, 136, 8))
		disc(rx(0, 0, 128), 0.35, 0.3, 0.3, 0.4)
	case "BR":
		u.fillRect(c, rx(0, 156, 59), u.r(x, y, w, h))
		disc(rx(255, 223, 0), 0.15, 0.15, 0.7, 0.7)
		disc(rx(0, 39, 118), 0.3, 0.32, 0.4, 0.36)
	case "CA":
		u.fillRect(c, white, u.r(x, y, w, h))
		u.fillRect(c, rx(216, 6, 33), u.r(x, y, w*0.25, h))
		u.fillRect(c, rx(216, 6, 33), u.r(x+w*0.75, y, w*0.25, h))
		star(rx(216, 6, 33), 0.3)
	case "SE":
		u.fillRect(c, rx(0, 106, 167), u.r(x, y, w, h))
		u.fillRect(c, rx(252, 204, 2), u.r(x+w*0.25, y, w*0.14, h))
		u.fillRect(c, rx(252, 204, 2), u.r(x, y+h*0.42, w, h*0.16))
	case "CH":
		u.fillRect(c, rx(218, 41, 28), u.r(x, y, w, h))
		u.fillRect(c, white, u.r(x+w*0.42, y+h*0.15, w*0.16, h*0.7))
		u.fillRect(c, white, u.r(x+w*0.15, y+h*0.42, w*0.7, h*0.16))
	case "ES":
		u.fillRect(c, rx(170, 21, 27), u.r(x, y, w, h*0.25))
		u.fillRect(c, rx(241, 191, 0), u.r(x, y+h*0.25, w, h*0.5))
		u.fillRect(c, rx(170, 21, 27), u.r(x, y+h*0.75, w, h*0.25))
	case "MX":
		vstripes(rx(0, 104, 71), white, rx(206, 17, 38))
	case "VN":
		u.fillRect(c, rx(218, 37, 29), u.r(x, y, w, h))
		star(rx(255, 205, 0), 0.28)
	case "TH":
		u.fillRect(c, rx(165, 25, 49), u.r(x, y, w, h*0.15))
		u.fillRect(c, white, u.r(x, y+h*0.15, w, h*0.15))
		u.fillRect(c, rx(45, 42, 74), u.r(x, y+h*0.3, w, h*0.4))
		u.fillRect(c, white, u.r(x, y+h*0.7, w, h*0.15))
		u.fillRect(c, rx(165, 25, 49), u.r(x, y+h*0.85, w, h*0.15))
	case "AE":
		u.fillRect(c, rx(0, 129, 63), u.r(x, y, w, h))
		u.fillRect(c, white, u.r(x+w*0.25, y+h/3, w*0.75, h/3+0.5))
		u.fillRect(c, rx(0, 0, 0), u.r(x+w*0.25, y+2*h/3, w*0.75, h/3+0.5))
		u.fillRect(c, rx(239, 51, 64), u.r(x, y, w*0.25, h))
	case "TR":
		u.fillRect(c, rx(227, 10, 23), u.r(x, y, w, h))
		disc(white, 0.15, 0.25, 0.3, 0.5)
		disc(rx(227, 10, 23), 0.22, 0.28, 0.3, 0.44)
		star(white, 0.5)
	case "AU", "NZ":
		u.fillRect(c, rx(1, 33, 105), u.r(x, y, w, h))
		u.fillRect(c, white, u.r(x, y+h*0.18, w*0.45, h*0.14))
		u.fillRect(c, white, u.r(x+w*0.16, y, w*0.14, h*0.55))
		u.fillRect(c, rx(200, 16, 46), u.r(x+w*0.19, y, w*0.08, h*0.55))
		u.fillRect(c, rx(200, 16, 46), u.r(x, y+h*0.21, w*0.45, h*0.08))
		if cc == "AU" {
			star(white, 0.55)
		} else {
			star(rx(200, 16, 46), 0.55)
		}
	case "ID":
		u.fillRect(c, rx(206, 17, 38), u.r(x, y, w, h*0.5))
		u.fillRect(c, white, u.r(x, y+h*0.5, w, h*0.5))
	case "MY":
		for i := 0; i < 7; i++ {
			col := rx(255, 0, 0)
			if i%2 == 1 {
				col = white
			}
			u.fillRect(c, col, u.r(x, y+float64(i)*h/7, w, h/7+0.5))
		}
		u.fillRect(c, rx(1, 0, 102), u.r(x, y, w*0.4, h*0.55))
		star(rx(255, 222, 0), 0.02)
	case "PH":
		u.fillRect(c, rx(0, 56, 168), u.r(x, y, w, h*0.5))
		u.fillRect(c, rx(206, 17, 38), u.r(x, y+h*0.5, w, h*0.5))
		disc(white, 0.05, 0.3, 0.35, 0.4)
	default:
		u.fillRound(c, th.chip, u.r(x, y, w, h), 2)
		u.textC(c, cc, u.f.f8, th.dim, u.r(x, y, w, h))
	}
	u.strokeRound(c, th.line, u.r(x, y, w, h), 2)
}

// ---------------------------------------------------------------------------
// Formatting helpers
// ---------------------------------------------------------------------------

// parseCoreLine maps one mihomo logfmt line (time="..." level=... msg="...")
// to a log level and a clean message for the log panel.
func parseCoreLine(line string) (int, string) {
	lv := 0
	switch extractLogfmt(line, "level") {
	case "warning", "warn":
		lv = 1
	case "error", "fatal":
		lv = 2
	}
	if msg := extractLogfmt(line, "msg"); msg != "" {
		return lv, msg
	}
	return lv, line
}

// extractLogfmt pulls the value of key from a logfmt line. Values are quoted
// (msg="...") or bare words (level=info).
func extractLogfmt(line, key string) string {
	i := strings.Index(line, key+"=")
	if i < 0 {
		return ""
	}
	rest := line[i+len(key)+1:]
	if !strings.HasPrefix(rest, `"`) {
		if j := strings.IndexByte(rest, ' '); j >= 0 {
			rest = rest[:j]
		}
		return rest
	}
	rest = rest[1:]
	if j := strings.IndexByte(rest, '"'); j >= 0 {
		return rest[:j]
	}
	return rest
}

// coloCountry maps a Cloudflare colo code (metro IATA) to its ISO country,
// so the endpoint list and status tiles can show a flag next to it.
var coloCountry = map[string]string{
	// United States
	"LAX": "US", "SEA": "US", "SJC": "US", "SFO": "US", "DFW": "US",
	"IAD": "US", "EWR": "US", "JFK": "US", "ORD": "US", "MIA": "US",
	"ATL": "US", "BOS": "US", "PHX": "US", "DEN": "US", "LAS": "US",
	"PDX": "US", "MCI": "US", "SLC": "US", "DTW": "US", "MSP": "US",
	"CLT": "US", "BNA": "US", "PIT": "US", "CLE": "US", "IND": "US",
	"CMH": "US", "RDU": "US", "TPA": "US", "AUS": "US", "SAT": "US",
	// Canada
	"YYZ": "CA", "YVR": "CA", "YOW": "CA", "YUL": "CA", "YYC": "CA",
	// Latin America
	"GRU": "BR", "GIG": "BR", "POA": "BR", "CWB": "BR", "BOG": "CO",
	"LIM": "PE", "SCL": "CL", "EZE": "AR", "MEX": "MX", "QRO": "MX",
	"CCS": "VE", "SJO": "CR", "PTY": "PA",
	// Europe
	"LHR": "GB", "MAN": "GB", "EDI": "GB", "AMS": "NL", "FRA": "DE",
	"MUC": "DE", "DUS": "DE", "HAM": "DE", "BER": "DE", "CDG": "FR",
	"MRS": "FR", "LYS": "FR", "MXP": "IT", "FCO": "IT", "MAD": "ES",
	"BCN": "ES", "ARN": "SE", "CPH": "DK", "OSL": "NO", "HEL": "FI",
	"WAW": "PL", "VIE": "AT", "ZRH": "CH", "GVA": "CH", "BRU": "BE",
	"LIS": "PT", "PRG": "CZ", "BUD": "HU", "OTP": "RO", "SOF": "BG",
	"ATH": "GR", "DUB": "IE", "LUX": "LU", "KEF": "IS",
	// Middle East / Africa
	"DXB": "AE", "FJR": "AE", "DOH": "QA", "KWI": "KW", "RUH": "SA",
	"TLV": "IL", "IST": "TR", "JNB": "ZA", "CPT": "ZA", "NBO": "KE",
	"LOS": "NG", "CAI": "EG", "CMN": "MA",
	// Asia-Pacific
	"SIN": "SG", "HKG": "HK", "NRT": "JP", "KIX": "JP", "ICN": "KR",
	"TPE": "TW", "BOM": "IN", "DEL": "IN", "MAA": "IN", "BLR": "IN",
	"BKK": "TH", "SGN": "VN", "HAN": "VN", "MNL": "PH", "CGK": "ID",
	"DPS": "ID", "KUL": "MY", "PEN": "MY", "SYD": "AU", "MEL": "AU",
	"PER": "AU", "BNE": "AU", "AKL": "NZ", "WLG": "NZ",
	// CIS
	"SVO": "RU", "LED": "RU", "ALA": "KZ", "TAS": "UZ", "KBP": "UA",
	"MSQ": "BY",
}

// coloFlag resolves a colo or country label to an ISO alpha-2 code for the
// flag painter. Two-letter values (the trace probe's country) pass through.
func coloFlag(v string) string {
	v = strings.ToUpper(strings.TrimSpace(v))
	if len(v) == 2 {
		return v
	}
	return coloCountry[v]
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func latencyText(ms int64) string {
	if ms <= 0 {
		return "—"
	}
	return strconv.FormatInt(ms, 10) + "ms"
}

func sizeText(b int64) string {
	if b >= 1<<20 {
		return strconv.FormatFloat(float64(b)/(1<<20), 'f', 1, 64) + " MB"
	}
	return strconv.FormatInt(b/1024, 10) + " KB"
}

func avgSamples(v []int64) float64 {
	if len(v) == 0 {
		return 0
	}
	var s int64
	for _, x := range v {
		s += x
	}
	return float64(s) / float64(len(v))
}
