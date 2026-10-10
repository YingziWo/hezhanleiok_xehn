// Package engine is the Go half of the Android app: everything the Kotlin UI
// calls goes through Engine, and gomobile turns this package into an .aar.
//
// Layering (see docs/porting-analysis.md):
//
//	Kotlin / Compose  ──▶  Engine (this package)  ──▶  Backend (tunnel work)
//	                                                        │
//	                          warpscout engine (wg/awg/masque + gVisor) ─┘
//
// The Backend seam exists on purpose. It lets the whole app - VpnService,
// notification, state machine, log plumbing - be built and tested against a
// fake before the warpscout code is moved into this module, which is a
// mechanical but non-trivial step (warpscout is `package main` with package
// level globals; see the migration table in docs/porting-analysis.md).
//
// Deliberate divergences from the Windows client:
//   - No external processes. mihomo.exe and psiphon.exe have no place in an
//     APK; the tunnels are the engine's own.
//   - No state file, no pid lease, no health-guard subprocess. One process, one
//     mutex, one Service lifetime.
//   - No system-proxy code. Android routes through VpnService, so
//     ProxySnapshot/ProxyWarn/NoProxy simply do not exist here.
package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/xhwarp/android-engine/warp"
)

// Version is reported to the UI and written into the bug-report header.
//
// P1: the warpscout engine (WG/AWG + gVisor netstack) is compiled in, so the app
// can build a real tunnel and read the real exit IP. Before this it was a
// placeholder that only reported ErrNotWired.
//
// Keep the doc comments of *bound* symbols ASCII: gomobile copies them into the
// generated Java, and javac on a Chinese Windows defaults to GBK, which turns
// any non-ASCII byte into "unmappable character" and fails the whole bind.
const Version = "0.2.0-android-p1"

// ErrNotWired is returned by the default backend: the UI and the state machine
// are in place, the warpscout engine has not been moved in yet.
var ErrNotWired = errors.New("引擎尚未接入：见 docs/porting-analysis.md 的 P1 步骤")

// LogSink receives one line per engine event. Kotlin implements it and the
// method is called from the engine goroutine, so the implementation must not
// touch the UI directly (post to a handler/coroutine instead).
type LogSink interface {
	OnLog(line string)
}

// Protector excludes one socket from the system VPN. Kotlin implements it with
// VpnService.protect(fd).
//
// This is not optional plumbing. Once VpnService is up, every packet the phone
// sends is routed into our own tun - including the encrypted UDP the tunnel uses
// to reach Cloudflare. Without this the tunnel swallows its own transport: the
// VPN key appears, the app says connected, and no page loads.
//
// Keep the doc comment ASCII: gomobile copies it into the generated Java, and
// javac on a Chinese Windows reads sources as GBK (see build-engine.ps1).
type Protector interface {
	// Protect returns true when the socket was successfully excluded.
	Protect(fd int) bool
}

// Request is one connect attempt. gomobile binds this as a plain class; every
// field is a basic type on purpose.
type Request struct {
	Proto    string // wg | awg | masque | masque-h2
	Endpoint string // ip:port, empty means "pick from the cached scan"
	Psiphon  bool   // stack a Psiphon exit on top of the base protocol
	IPv6     bool
	MTU      int
}

// Result is what a successful backend hands back.
type Result struct {
	ExitIP           string
	Country          string
	Endpoint         string
	PsiphonSocksPort int
}

// tunnelBackend establishes and tears down the tunnel. Implemented by the
// warpscout port; faked in tests.
//
// Deliberately unexported. gomobile generates a Java proxy for every exported
// interface so Kotlin could implement it, and this one cannot be expressed in
// Java - it takes a func(string) and returns a struct. Leaving it exported made
// gobind emit an incomplete proxy and fail the whole bind with
// "missing method Connect". Kotlin must never provide a tunnel anyway.
type tunnelBackend interface {
	// Connect blocks until the tunnel carries traffic, ctx is cancelled, or it
	// fails. The context is what makes a connect abortable: closing the tunnel
	// is not enough, because a dial that is already in flight has to be told to
	// give up.
	Connect(ctx context.Context, req Request, report func(string)) (Result, error)
	// Close releases the tunnel, the netstack and any listening port.
	Close() error
}

// Engine is the gomobile-visible object. Create exactly one and keep it for the
// process lifetime (the VpnService owns it).
type Engine struct {
	workDir string
	sess    *session

	mu        sync.Mutex
	backend   tunnelBackend
	cancel    context.CancelFunc
	tunFD     int
	statsStop chan struct{}
}

// statsInterval is how often the data plane is sampled for the home screen's
// throughput and latency. The desktop UI used 2s as well.
const statsInterval = 2 * time.Second

// NewEngine wires an engine to a writable directory (the app's filesDir). The
// directory replaces the Windows client's WARP_CLIENT_HOME: account, cache and
// logs live there.
func NewEngine(workDir string) *Engine {
	e := &Engine{workDir: workDir, sess: newSession()}
	// 真正的数据面：WARP 账号 + WG/AWG 隧道 + gVisor netstack。
	// 装不上（workDir 不可写等）也只影响连不上，不影响 UI。
	e.setBackend(newWarpBackend(workDir))
	return e
}

// warpBackendOf 取出真实后端，用于账号导入与本地 SOCKS 端口查询。
// 测试里装的是假后端，这时返回 nil。
func (e *Engine) warpBackendOf() *warpBackend {
	e.mu.Lock()
	defer e.mu.Unlock()
	wb, _ := e.backend.(*warpBackend)
	return wb
}

// SetAccountJSON 让 Kotlin 直接喂一份账号（例如放在 assets 里的 account.json），
// 绕开 api.cloudflareclient.com 不可达时的注册步骤。
func (e *Engine) SetAccountJSON(raw string) {
	if wb := e.warpBackendOf(); wb != nil {
		wb.SetAccountJSON(raw)
		e.sess.logf("[engine] 已导入外部账号")
	}
}

// SetPsiphonConfig hands the engine the official Psiphon client config and the
// DNS servers to use. Both are plain strings so gomobile binds the method.
//
// The config JSON is required for the Psiphon exit: it carries the SponsorId,
// server-list URLs and signature keys. dnsServers is comma-delimited.
func (e *Engine) SetPsiphonConfig(clientConfigJSON, dnsServers string) {
	if wb := e.warpBackendOf(); wb != nil {
		wb.SetPsiphonConfig(clientConfigJSON, dnsServers)
		e.sess.logf("[engine] 已导入 Psiphon 客户端配置（%d 字节）", len(clientConfigJSON))
	}
}

// AccountJSON 返回当前账号 JSON（没有则为空串）。存下来下次导入即可免注册。
func (e *Engine) AccountJSON() string {
	if wb := e.warpBackendOf(); wb != nil {
		return wb.AccountJSON()
	}
	return ""
}

// SocksAddr 返回隧道自带的本地 SOCKS5 监听地址（未连接时为空）。
func (e *Engine) SocksAddr() string {
	if wb := e.warpBackendOf(); wb != nil {
		return wb.SocksAddr()
	}
	return ""
}

// setBackend installs the tunnel implementation. Called once at startup by the
// Android backend itself (same package); the scaffold's default refuses to
// connect with ErrNotWired. Unexported for the reason on tunnelBackend.
func (e *Engine) setBackend(b tunnelBackend) {
	e.mu.Lock()
	e.backend = b
	e.mu.Unlock()
}

// SetLogSink installs the Kotlin log callback.
func (e *Engine) SetLogSink(sink LogSink) { e.sess.setSink(sink) }

// SetTunFD hands the file descriptor of the tun device that Android's
// VpnService already created to the engine.
//
// This is the one thing the warpscout engine cannot do for itself: VpnService
// owns the interface and gives the app a fully configured fd, while
// amneziawg-go ships tun_darwin/tun_linux/tun_windows but no tun_android. The
// Android backend therefore wraps *this* fd as its tun.Device instead of
// asking the kernel for a new interface (see docs/porting-analysis.md, P1).
func (e *Engine) SetTunFD(fd int) {
	e.mu.Lock()
	e.tunFD = fd
	backend := e.backend
	e.mu.Unlock()
	if wb, ok := backend.(*warpBackend); ok {
		wb.SetTunFD(fd)
	}
	e.sess.logf("[engine] tun fd = %d", fd)
}

// SetVPNState tells the engine whether the Android system VPN is up. The UI
// shows it, and a tunnel is never reported as usable while it is false.
func (e *Engine) SetVPNState(up bool) {
	e.sess.setVPN(up)
}

// SetProtector installs the socket excluder. Must be called before Connect, and
// must stay installed for the whole session: every new socket the tunnel opens
// needs it.
func (e *Engine) SetProtector(p Protector) {
	if p == nil {
		warp.SetProtector(nil)
		return
	}
	warp.SetProtector(protectorFunc(func(fd int) bool { return p.Protect(fd) }))
	e.sess.logf("[engine] socket protector 已安装")
}

// protectorFunc adapts the bound interface to the package-internal one without
// exporting anything else to gomobile.
type protectorFunc func(fd int) bool

func (f protectorFunc) Protect(fd int) bool { return f(fd) }

// ProbeLocalJSON reports the address the internet currently sees for this phone,
// as {"ok":true,"ip":"...","country":"XX"}.
//
// It blocks for up to a few seconds, so Kotlin must call it off the UI thread.
// The disconnected home screen shows this as "your IP": it is the same probe the
// exit readout uses, so the two numbers are always comparable.
func (e *Engine) ProbeLocalJSON() string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ip, country, ok := warp.ProbeNetwork(ctx, 8*time.Second)
	if !ok {
		return `{"ok":false}`
	}
	b, err := json.Marshal(map[string]any{"ok": true, "ip": ip, "country": country})
	if err != nil {
		return `{"ok":false}`
	}
	return string(b)
}

// SetTimeout sets the per-endpoint handshake timeout in seconds (0 = default).
// Comes from the settings screen's "connection timeout".
func (e *Engine) SetTimeout(sec int) {
	if wb := e.warpBackendOf(); wb != nil {
		wb.SetTimeout(sec)
	}
}

// MTU reports the device MTU the engine will use. The VpnService.Builder must
// set the same value: a tun whose MTU disagrees with what the device expects
// makes the kernel hand up packets the tunnel then has to fragment or drop.
func (e *Engine) MTU() int { return defaultMTU }

const defaultMTU = 1280

// TunFD reports the fd installed by SetTunFD (0 when there is none).
func (e *Engine) TunFD() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.tunFD
}

// VersionString reports the engine build.
func (e *Engine) VersionString() string { return Version }

// dataCounters reads the tun device's cumulative byte counters.
func (e *Engine) dataCounters() (uint64, uint64) {
	e.mu.Lock()
	backend := e.backend
	e.mu.Unlock()
	if wb, ok := backend.(*warpBackend); ok {
		return wb.Counters()
	}
	return 0, 0
}

// startStats samples the data plane while a session is up: bytes in and out for
// the throughput readout, plus one round trip through the tunnel for the latency.
//
// The desktop client read these from mihomo's REST API (/traffic,
// /proxies/WARP/delay). There is no mihomo on Android, so the numbers come from
// the tun device itself - which is the better source anyway: it counts what the
// phone really sent, not what a proxy says it forwarded.
func (e *Engine) startStats() {
	e.mu.Lock()
	if e.statsStop != nil {
		e.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	e.statsStop = stop
	// Latency is only meaningful with the system VPN up: without it the request
	// never enters the tunnel and we would be measuring the phone's own uplink
	// while labelling it "connection latency".
	throughVPN := e.tunFD > 0
	e.mu.Unlock()

	go func() {
		ticker := time.NewTicker(statsInterval)
		defer ticker.Stop()
		var lastTx, lastRx uint64
		var lastAt time.Time
		var latency int64
		// 连续探测失败计数 / 是否已经就这次故障写过日志（只在状态翻转时写一行，
		// 免得日志被刷屏）。
		var probeFails int
		var probeWarned bool
		for {
			select {
			case <-stop:
				// 断开后清零：界面上「共 0 B」才是真的没在传，留着上次会话的
				// 累计值会让人以为还在跑。
				e.sess.setStats(0, 0, 0, 0, 0)
				return
			case <-ticker.C:
			}
			tx, rx := e.dataCounters()
			now := time.Now()
			var up, down int64
			if !lastAt.IsZero() {
				if secs := now.Sub(lastAt).Seconds(); secs > 0 {
					up = int64(float64(tx-lastTx) / secs)
					down = int64(float64(rx-lastRx) / secs)
				}
			}
			lastTx, lastRx, lastAt = tx, rx, now

			if throughVPN {
				if ms, ok := warp.ProbeLatency(context.Background(), 3*time.Second); ok {
					latency = ms
					probeFails = 0
					if probeWarned {
						probeWarned = false
						e.sess.logf("[engine] 隧道探测恢复")
					}
				} else {
					// 「已连接但上不了网」最难查的就是分不清「隧道从没通过」和
					// 「通了一会儿被切断」。这里在连续失败时明确写一行，日志里
					// 就能看出是哪种。
					probeFails++
					if probeFails == 3 && !probeWarned {
						probeWarned = true
						e.sess.logf("[engine] 隧道连续 %d 次探测失败：出口已不可达（可能被切断）", probeFails)
					}
				}
			}
			e.sess.setStats(latency, up, down, int64(tx), int64(rx))
		}
	}()
}

func (e *Engine) stopStats() {
	e.mu.Lock()
	stop := e.statsStop
	e.statsStop = nil
	e.mu.Unlock()
	if stop != nil {
		close(stop)
	}
}

// StatusJSON returns the current snapshot as a JSON object. The UI polls this
// (or the engine pushes through LogSink); polling is what makes the Android
// port need no state file.
//
// JSON and not a struct: gobind skips a method whose parameter or return type is
// a Go struct passed by value (it writes "skipped method ... with unsupported
// parameter or return types" into the generated Java and binds nothing), so a
// bound Status() Snapshot would simply not exist on the Kotlin side. Kotlin
// parses this with the platform's org.json - no extra dependency.
func (e *Engine) StatusJSON() string {
	b, err := json.Marshal(e.sess.snapshot())
	if err != nil {
		// A Snapshot is all basic types; failing to encode it is not something
		// the UI could act on, so answer with a valid object carrying the state.
		return `{"state":"error","message":"snapshot encode failed"}`
	}
	return string(b)
}

// status is the typed view, for this package and its tests.
func (e *Engine) status() Snapshot { return e.sess.snapshot() }

// Logs returns the in-memory log ring as one string, for the log screen.
func (e *Engine) Logs() string { return e.sess.logs() }

// Connect starts a tunnel and returns immediately: Android's UI thread must not
// block, and a connect can take tens of seconds. Progress arrives through
// StatusJSON() and the LogSink.
//
// Cancellation is a first-class path (unlike the desktop GUI, where a click
// during "connecting" used to be swallowed): Disconnect during connecting moves
// the session to stopping and cancels the attempt.
//
// Every parameter is a basic type on purpose. An earlier signature took a
// Request struct by value and gobind dropped the whole method - it binds struct
// fields as accessors on a generated class but refuses a struct in a method
// signature ("skipped method Engine_.Connect with unsupported parameter or
// return types"), which surfaced as "Unresolved reference 'connect'" in Kotlin.
func (e *Engine) Connect(proto, endpoint string, psiphon, ipv6 bool, mtu int) error {
	req := Request{Proto: proto, Endpoint: endpoint, Psiphon: psiphon, IPv6: ipv6, MTU: mtu}
	e.mu.Lock()
	backend := e.backend
	e.mu.Unlock()

	if backend == nil {
		backend = unwiredBackend{}
		e.sess.logf("[engine] 未安装 Backend，使用占位实现")
	}
	if err := e.sess.transition(StateConnecting, func(s *Snapshot) {
		s.Proto, s.Endpoint, s.Message = req.Proto, req.Endpoint, "连接中…"
		s.ExitIP, s.Country, s.SinceMS = "", "", 0
	}); err != nil {
		return err
	}

	// 一次连接一个可取消的 context：Disconnect 调它，正在飞的拨号/握手/探测
	// 都会立刻收手，而不是等它自己跑完。
	ctx, cancel := context.WithCancel(context.Background())
	e.mu.Lock()
	e.cancel = cancel
	e.mu.Unlock()

	go func() {
		defer cancel()
		e.sess.logf("[engine] Connect proto=%s endpoint=%s psiphon=%v", req.Proto, req.Endpoint, req.Psiphon)
		res, err := backend.Connect(ctx, req, func(msg string) { e.sess.logf("[tunnel] %s", msg) })
		cancelled := ctx.Err() != nil

		if err != nil {
			// A cancelled connect is an expected outcome, not a failure.
			if cancelled {
				e.sess.logf("[engine] 连接已取消")
				_ = e.sess.transition(StateStopping, func(s *Snapshot) { s.Message = "正在取消…" })
				_ = e.sess.transition(StateIdle, func(s *Snapshot) { s.Message = "未连接" })
				return
			}
			e.sess.logf("[engine] 连接失败: %v", err)
			_ = e.sess.transition(StateError, func(s *Snapshot) { s.Message = err.Error() })
			return
		}

		if cancelled {
			// The cancel raced a connect that had already committed: undo it
			// rather than leaving a tunnel the user asked to abandon.
			e.sess.logf("[engine] 取消请求已到，正在回收刚建立的隧道")
			_ = backend.Close()
			_ = e.sess.transition(StateStopping, func(s *Snapshot) { s.Message = "正在取消…" })
			_ = e.sess.transition(StateIdle, func(s *Snapshot) { s.Message = "未连接" })
			return
		}

		e.sess.setExit(req.Proto, res.Endpoint, res.ExitIP, res.Country)
		if err := e.sess.transition(StateConnected, nil); err != nil {
			e.sess.logf("[engine] %v", err)
		}
		e.startStats()
		e.sess.logf("[engine] 已连接，出口 %s (%s)", res.ExitIP, res.Country)
	}()
	return nil
}

// Disconnect tears the tunnel down. Safe to call in any state: from idle it is
// a no-op, from connecting it cancels the attempt.
func (e *Engine) Disconnect() error {
	snap := e.sess.snapshot()
	if snap.State == StateIdle {
		return nil
	}
	// The sampler only makes sense while a session exists; leaving it running
	// would keep probing a tunnel that is on its way down and paint a stale
	// latency back onto the disconnected screen.
	e.stopStats()
	e.mu.Lock()
	backend := e.backend
	cancel := e.cancel
	e.cancel = nil
	e.mu.Unlock()

	if err := e.sess.transition(StateStopping, func(s *Snapshot) { s.Message = "断开中…" }); err != nil {
		// Already stopping or in error: still make sure the resources go.
		e.sess.logf("[engine] Disconnect: %v", err)
	}
	if cancel != nil {
		cancel()
	}
	if backend != nil {
		if err := backend.Close(); err != nil {
			e.sess.logf("[engine] 关闭隧道失败: %v", err)
			_ = e.sess.transition(StateError, func(s *Snapshot) { s.Message = err.Error() })
			return err
		}
	}
	err := e.sess.transition(StateIdle, func(s *Snapshot) {
		s.Message = "未连接"
		s.ExitIP, s.Country, s.SinceMS, s.Proto, s.Endpoint = "", "", 0, "", ""
	})
	if err != nil {
		// Stopping -> Idle is the only legal move here; anything else means a
		// bug worth seeing in the log rather than hiding.
		e.sess.logf("[engine] 状态收敛失败: %v", err)
	}
	return nil
}

// unwiredBackend is the placeholder until the warpscout port lands. It fails
// loudly instead of pretending to connect, so the UI's error path is exercised
// from day one.
type unwiredBackend struct{}

func (unwiredBackend) Connect(context.Context, Request, func(string)) (Result, error) {
	return Result{}, fmt.Errorf("%w", ErrNotWired)
}

func (unwiredBackend) Close() error { return nil }
