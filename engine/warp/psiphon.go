package warp

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Psiphon-Labs/psiphon-tunnel-core/MobileLibrary/psi"
	"golang.org/x/net/proxy"
)

// Psiphon 出口（协议层的「叠加」开关，不是一条独立传输）。
//
// 桌面版是起一个 psiphon.exe 子进程 + 本地 SOCKS 端口驱动；APK 里没有可执行
// 文件，所以这里改成**进程内**跑官方的 psiphon-tunnel-core（用户已确认接受
// GPL-3.0）。数据面用的是 tunnel-core 自带的 packet tunnel：
//
//	系统 tun fd ──▶ Psiphon packet tunnel ──▶ Psiphon 服务器 ──▶ 互联网
//	                       ▲
//	                     上游 = 基础协议隧道暴露的本地 SOCKS（默认 WARP）
//
// 也就是说，tun fd 归 Psiphon 所有，基础 WARP 隧道退到 netstack 模式只当上游
// 载体 —— 这正是桌面版「mihomo 做数据面 + Psiphon 做出口」的等价形态，出口
// 国家/城市也就真的是 Psiphon 的出口，而不是 WARP 端点的。
//
// 为什么要有上游：在受限网络里 Psiphon 直连它的服务器同样会被拦，桌面版一直
// 是「WARP 打底、Psiphon 做出口」。UpstreamProxyURL 指向基础隧道的本地 SOCKS，
// Psiphon 自己的 socket 被 BindingToDevice 排除出系统 VPN 后连到 127.0.0.1，
// 环回不进 tun，所以不会绕回自己。

// psiphonUnderlaySocks 是基础协议隧道为 Psiphon 暴露的本地 SOCKS 端口。
// 不能用 1080：那是（netstack 模式下）给用户自测用的常规入口，而且 Psiphon
// 自己也会占用一个端口，两个监听撞一起会让「谁是谁」彻底说不清。
const psiphonUnderlaySocks = "127.0.0.1:1081"

// psiphonNetworkID 是给 tunnel-core 的「当前网络标识」。官方接口要求必须始终
// 返回一个值（取不到就 "UNKNOWN"），因为用它的逻辑（含 tactics）无论如何都要
// 继续走下去。
const psiphonNetworkID = "UNKNOWN"

// noticeTypeLimit 是每种 notice 最多写几行日志。诊断 notice 每个协议、每个端点
// 都会来一条，不限流会把日志刷满；但一条都不记就变成「连不上且不知道为什么」。
const noticeTypeLimit = 6

// psiphonReadyTimeout 是「下完 tun fd 到第一条隧道可用」的上限。Psiphon 要先取
// 服务器列表（Tactics + RemoteServerList）、再逐个协议试拨，在受限网络里比 WARP
// 慢一个量级；桌面版也是这个量级。给太短会把「慢」误判成「失败」。
const psiphonReadyTimeout = 180 * time.Second

// PsiphonOptions 是一次「基础隧道 + Psiphon 出口」会话的参数。
type PsiphonOptions struct {
	// ClientConfigJSON 是官方 client.config 的内容（SponsorId / 服务器列表 URL /
	// 签名公钥都在里面）。缺它就没有服务器可拨，必须由调用方提供。
	ClientConfigJSON string
	// DNS 是给 tunnel-core 用的 DNS 服务器（逗号分隔）。空则让它用系统解析器。
	DNS string
	// DataDir 是 tunnel-core 的 DataRootDirectory：datastore、服务器列表缓存都在
	// 这里。必须可写，否则 Commit 直接失败。
	DataDir string
	// TunFD 是 VpnService 建好的 tun 设备 fd，交给 Psiphon 的 packet tunnel。
	TunFD int

	// ---- 上游（基础协议隧道）----
	UnderlayProto    string // wg | awg | masque | masque-h2 | wwarp，默认 awg
	UnderlayEndpoint string // 可选：钉死 ip:port
	// UnderlayPrefer 是上游上一次成功的端点，会被排到候选最前（见 Options.Prefer）。
	UnderlayPrefer   string
	IPv6             bool
	HandshakeTimeout time.Duration
	VerifyTimeout    time.Duration
	Budget           time.Duration
	Report           func(string)
}

func (o *PsiphonOptions) report(msg string) {
	if o.Report != nil {
		o.Report(msg)
	}
}

// psiphonExit 是一次 Psiphon 运行的句柄：SOCKS 端口、就绪信号、以及最近一次
// 出错原因（失败时要能说出「为什么」而不是干等超时）。
type psiphonExit struct {
	socksPort atomic.Int32
	ready     chan struct{}
	readyOnce sync.Once
	lastErr   atomic.Pointer[string]

	// 每种 notice 已经写过多少行（限流用）。
	noticeMu   sync.Mutex
	noticeSeen map[string]int
}

func newPsiphonExit() *psiphonExit {
	return &psiphonExit{ready: make(chan struct{})}
}

// SocksAddr 是 Psiphon 本地 SOCKS 的地址（就绪前为空）。
func (p *psiphonExit) SocksAddr() string {
	port := p.socksPort.Load()
	if port <= 0 {
		return ""
	}
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port)))
}

// stop 停掉进程内的 controller（幂等：psi.Stop 对未启动的 controller 是 no-op）。
func (p *psiphonExit) stop() { psi.Stop() }

func (p *psiphonExit) readyNow() bool {
	if p.socksPort.Load() <= 0 {
		return false
	}
	select {
	case <-p.ready:
		return true
	default:
		return false
	}
}

func (p *psiphonExit) lastError() string {
	if e := p.lastErr.Load(); e != nil {
		return *e
	}
	return ""
}

// forwardNotice 把一条 notice 写进 App 日志，按类型限流（错误类不限）。
func (p *psiphonExit) forwardNotice(kind, raw string, report func(string)) {
	if report == nil || kind == "" {
		return
	}
	p.noticeMu.Lock()
	if p.noticeSeen == nil {
		p.noticeSeen = map[string]int{}
	}
	p.noticeSeen[kind]++
	n := p.noticeSeen[kind]
	p.noticeMu.Unlock()

	loud := kind == "Error" || kind == "InternalError" || kind == "Warning" ||
		kind == "Tunnels" || kind == "ListeningSocksProxyPort"
	if !loud && n > noticeTypeLimit {
		return
	}
	report("Psiphon[" + kind + "] " + trimNotice(raw))
}

// trimNotice 只取 notice 里的 message / data，并截断：完整的一条诊断 notice 可以
// 带上整组网络参数，直接塞进日志面板会淹掉别的东西。
func trimNotice(raw string) string {
	var n struct {
		Data    json.RawMessage `json:"data"`
		Message string          `json:"message"`
	}
	if json.Unmarshal([]byte(raw), &n) != nil {
		return clipText(raw, 240)
	}
	if n.Message != "" {
		return clipText(n.Message, 240)
	}
	if len(n.Data) > 0 {
		return clipText(string(n.Data), 240)
	}
	return ""
}

func clipText(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// handleNotice 解析一条 notice。这里是「就绪」的唯一判据，形状与官方一致：
//
//	{"noticeType":"ListeningSocksProxyPort","data":{"port":N},"timestamp":"..."}
//	{"noticeType":"Tunnels","data":{"count":N},"timestamp":"..."}
//
// 只有 SOCKS 端口**和**至少一条隧道同时到齐才算就绪：只听端口会得到一个
// 「端口开着、每个请求都被丢弃」的假成功，正是出口读数最容易骗人的形态。
func (p *psiphonExit) handleNotice(raw string, report func(string)) {
	var n struct {
		Notice string          `json:"noticeType"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &n); err != nil {
		return
	}
	// 先把 notice 本身写进日志（按类型限流）：Psiphon 的失败原因几乎全部只出现在
	// notice 里 —— 服务器列表抓不到、tactics 失败、每个协议的拨号失败。之前关掉
	// 诊断 notice 又不记录，等于把唯一能定位问题的信息丢了。
	p.forwardNotice(n.Notice, raw, report)

	switch n.Notice {
	case "ListeningSocksProxyPort":
		var d struct {
			Port int `json:"port"`
		}
		if json.Unmarshal(n.Data, &d) == nil && d.Port > 0 {
			p.socksPort.Store(int32(d.Port))
			if report != nil {
				report(fmt.Sprintf("Psiphon 本地 SOCKS 已监听 127.0.0.1:%d", d.Port))
			}
		}
	case "Tunnels":
		var d struct {
			Count int `json:"count"`
		}
		if json.Unmarshal(n.Data, &d) == nil && d.Count > 0 {
			p.readyOnce.Do(func() { close(p.ready) })
			if report != nil {
				report(fmt.Sprintf("Psiphon 隧道已建立（%d 条）", d.Count))
			}
		}
	case "ConnectingServer":
		// 这一条带 establishedTunnelsCount，是「已经有隧道在跑」的另一个来源；
		// 版本之间 notice 名字变过，多认一个就少一种「明明通了却报超时」。
		var d struct {
			Count    int    `json:"establishedTunnelsCount"`
			Protocol string `json:"protocol"`
			Region   string `json:"region"`
		}
		if json.Unmarshal(n.Data, &d) == nil && d.Count > 0 {
			p.readyOnce.Do(func() { close(p.ready) })
			if report != nil {
				report(fmt.Sprintf("Psiphon 出口 %s (%s)", d.Region, d.Protocol))
			}
		}
	case "Error", "InternalError", "Warning":
		var d struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(n.Data, &d)
		msg := d.Message
		if msg == "" {
			msg = string(n.Data)
		}
		s := n.Notice + ": " + msg
		p.lastErr.Store(&s)
		if report != nil {
			report("Psiphon " + s)
		}
	}
}

// waitReady 等到「有隧道 + SOCKS 端口」都就绪，或超时/取消。
func (p *psiphonExit) waitReady(ctx context.Context, timeout time.Duration, report func(string)) error {
	deadline := time.Now().Add(timeout)
	for {
		if p.readyNow() {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			if e := p.lastError(); e != "" {
				return fmt.Errorf("Psiphon 在 %s 内没有建立隧道（最近错误：%s）", timeout, e)
			}
			return fmt.Errorf("Psiphon 在 %s 内没有建立隧道", timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// psiphonProvider 是 tunnel-core 回调宿主的实现（官方 MobileLibrary 里那一套的
// 进程内版本）。BindToDevice 是其中唯一不能省的一个：Psiphon 自己的 socket 若不
// 排除出系统 VPN，它会连到我们自己的 tun 上，形成自噬环，表现为「已连接、什么都
// 打不开」。这里直接复用引擎里已经装好的 VpnService.protect。
type psiphonProvider struct {
	ex     *psiphonExit
	dns    string
	report func(string)
}

func (p *psiphonProvider) Notice(noticeJSON string) { p.ex.handleNotice(noticeJSON, p.report) }

func (p *psiphonProvider) OnAccessToken(string) {}

// 1 = 有网络。宿主侧的联通性判断在安卓上没有额外信息可用，而 tunnel-core 自己
// 会按 notice 重试。
func (p *psiphonProvider) HasNetworkConnectivity() int { return 1 }

func (p *psiphonProvider) BindToDevice(fd int) (string, error) {
	if pr := currentProtector(); pr != nil && pr.Protect(fd) {
		return "", nil
	}
	// 保护失败仍返回 nil：让 tunnel-core 自己按拨号失败去报错，比在这里把
	// 整条链路打断更容易看出「到底哪一步不通」。
	return "", nil
}

func (p *psiphonProvider) IPv6Synthesize(string) string { return "" }

func (p *psiphonProvider) HasIPv6Route() int { return 0 }

// 网络标识（用于 tactics 选择与缓存键）。
//
// 官方接口文档写得很死：**必须始终返回一个值**，取不到就返回 "UNKNOWN" ——
// 因为用它的逻辑（含 tactics）无论如何都要继续。之前这里返回空串，属于违约。
func (p *psiphonProvider) GetNetworkID() string { return psiphonNetworkID }

func (p *psiphonProvider) GetDNSServersAsString() string { return p.dns }

// buildPsiphonConfig 从官方 client.config 出发，只覆盖运行期必须改的字段。
//
// 凭据（SponsorId / PropagationChannelId / 服务器列表 URL 与签名公钥）全部沿用
// 官方文件，一个字都不动 —— 少了它们 Psiphon 会报「零个可用服务器」。
func buildPsiphonConfig(baseJSON string, tunFD int, dataDir, upstream, dns string) (string, error) {
	if baseJSON == "" {
		return "", fmt.Errorf("缺少官方 client.config（SponsorId / 服务器列表 URL / 签名公钥都在里面）")
	}
	cfg := map[string]any{}
	if err := json.Unmarshal([]byte(baseJSON), &cfg); err != nil {
		return "", fmt.Errorf("解析 client.config: %w", err)
	}

	// 数据面：packet tunnel 直接吃 VpnService 的 tun fd。TunnelPoolSize 必须为 1。
	cfg["PacketTunnelTunFileDescriptor"] = tunFD
	cfg["TunnelPoolSize"] = 1

	// 本地 SOCKS 端口交给系统选，端口从 ListeningSocksProxyPort notice 读回来；
	// HTTP 代理关掉，没有消费者，少一个监听少一份出错面。
	cfg["LocalSocksProxyPort"] = 0
	cfg["LocalHttpProxyPort"] = 0
	cfg["DisableLocalHTTPProxy"] = true

	cfg["DataRootDirectory"] = dataDir
	cfg["UpstreamProxyURL"] = upstream

	// 诊断 notice 必须打开。关掉它 = 把「为什么连不上」全部藏起来：服务器列表抓取
	// 失败、Tactics 失败、每个协议的拨号失败都在诊断 notice 里，只剩一句「没建立
	// 隧道」。日志量由 handleNotice 的按类型限流兜住。
	cfg["EmitDiagnosticNotices"] = true
	cfg["EmitDiagnosticNetworkParameters"] = false

	// 迁移字段指向本机其它 Psiphon 安装的 datastore，安卓上必然不存在，
	// 留着只会让它去 adopt 一个不该碰的目录。
	for _, k := range []string{
		"MigrateDataStoreDirectory",
		"MigrateObfuscatedServerListDownloadDirectory",
		"MigrateRemoteServerListDownloadFilename",
		"MigrateUpgradeDownloadFilename",
	} {
		delete(cfg, k)
	}

	b, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// startPsiphon 装配配置、启动 controller 并等到就绪。
func startPsiphon(ctx context.Context, o PsiphonOptions, underlaySocks string) (*psiphonExit, error) {
	if o.TunFD <= 0 {
		return nil, fmt.Errorf("Psiphon 需要系统 tun fd，当前为 %d", o.TunFD)
	}
	if o.DataDir == "" {
		return nil, fmt.Errorf("Psiphon 缺少可写的数据目录")
	}
	if err := os.MkdirAll(o.DataDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建 Psiphon 数据目录: %w", err)
	}

	upstream := ""
	if underlaySocks != "" {
		upstream = "socks5://" + underlaySocks
	}
	cfgJSON, err := buildPsiphonConfig(o.ClientConfigJSON, o.TunFD, o.DataDir, upstream, o.DNS)
	if err != nil {
		return nil, err
	}

	ex := newPsiphonExit()
	provider := &psiphonProvider{ex: ex, dns: o.DNS, report: o.report}
	o.report("启动 Psiphon（进程内）…")
	// 第四个参数 useDeviceBinder=true：Psiphon 的 socket 必须被排除出系统 VPN。
	// 后两个 false：不使用 IPv6 合成器与 IPv6 路由探测。
	if err := psi.Start(cfgJSON, "", "", provider, true, false, false); err != nil {
		return nil, fmt.Errorf("启动 Psiphon 失败: %w", err)
	}
	// Psiphon 起来了：tun fd 现在归它的 packet tunnel（tun.(*Client).Stop 会
	// device.Close），失败路径不要再由我们关 —— 见 tunfd.go 的所有权说明。
	ClaimTunFD()
	stopOnError := true
	defer func() {
		if stopOnError {
			psi.Stop()
		}
	}()

	if err := ex.waitReady(ctx, psiphonReadyTimeout, o.report); err != nil {
		return nil, err
	}
	stopOnError = false
	return ex, nil
}

// DialPsiphon establishes "underlay tunnel + in-process Psiphon exit" and makes
// one real exit measurement through the Psiphon SOCKS.
//
// Exported for the engine package, which binds it to the Psiphon switch.
func DialPsiphon(ctx context.Context, o PsiphonOptions) (*Session, error) {
	underlayProto := o.UnderlayProto
	if underlayProto == "" {
		underlayProto = protoAWG
	}

	// 上游可以是**任意一种**传输：它们的职责只是「把出口做成一个本地 SOCKS 端口」，
	// WG / AWG / MASQUE / WARP-in-WARP 都能做到（netstack 模式下各自会开 SOCKS）。
	//
	// 这里必须走 Dial 而不是直接调某个具体拨号函数：Dial 才做协议分派，也才会
	// 调 Options.fill() 补默认值。踩过：直接调 dialWGTunnel 时 Ports 是空的，
	// 一个候选都不试就报「0 次尝试都没握手成功」——Psiphon 开着时连 WG 都连不上。
	savedTunFD := tunFD
	tunFD = 0 // tun fd 这一轮归 Psiphon 的 packet tunnel，上游退成 netstack
	under, err := Dial(ctx, Options{
		Proto:            underlayProto,
		Endpoint:         o.UnderlayEndpoint,
		Prefer:           o.UnderlayPrefer,
		IPv6:             o.IPv6,
		Sample:           2,
		HandshakeTimeout: o.HandshakeTimeout,
		VerifyTimeout:    o.VerifyTimeout,
		Budget:           o.Budget,
		SocksListen:      psiphonUnderlaySocks,
		Report:           o.Report,
	})
	tunFD = savedTunFD
	if err != nil {
		return nil, fmt.Errorf("Psiphon 上游（%s）建立失败: %w", underlayProto, err)
	}
	o.report(fmt.Sprintf("上游就绪（%s 出口 %s），叠加 Psiphon…", underlayProto, under.ExitIP))

	ex, err := startPsiphon(ctx, o, under.SocksAddr())
	if err != nil {
		under.Close()
		return nil, err
	}

	// 出口实测必须走 Psiphon 的 SOCKS：这样界面上的 IP/国家就是 Psiphon 的出口，
	// 而不是上游 WARP 的 —— 桌面版历史上正是把这两个混在一起才「显示的不是」。
	ip, country, ok := probeViaSocks(ctx, ex.SocksAddr(), o.VerifyTimeout)
	if !ok {
		psi.Stop()
		under.Close()
		return nil, fmt.Errorf("Psiphon 已连接但出口验证失败（/meta 与 /trace 都没通过 SOCKS %s）", ex.SocksAddr())
	}

	under.ExitIP, under.Country = ip, country
	under.psiphon = ex
	o.report(fmt.Sprintf("Psiphon 出口 %s (%s)", ip, country))
	return under, nil
}

// probeViaSocks 通过一个本地 SOCKS5 真发一次请求，拿回出口 IP 与归属。
//
// 用 x/net/proxy 而不是自己写握手：它对域名走 ATYP_DOMAIN，也就是 DNS 在出口侧
// 解析，不会把查询泄漏到本机网络。
func probeViaSocks(ctx context.Context, socksAddr string, timeout time.Duration) (string, string, bool) {
	if socksAddr == "" {
		return "", "", false
	}
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	dialer, err := proxy.SOCKS5("tcp", socksAddr, nil, proxy.Direct)
	if err != nil {
		return "", "", false
	}
	cd, ok := dialer.(proxy.ContextDialer)
	if !ok {
		return "", "", false
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DisableKeepAlives: true,
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return cd.DialContext(ctx, network, addr)
			},
		},
	}
	body, ok := fetchMeta(reqCtx, client, metaURL)
	if !ok {
		body, ok = fetchMeta(reqCtx, client, traceURL)
	}
	if !ok {
		return "", "", false
	}
	s := &Session{}
	s.applyMeta(body)
	if s.ExitIP == "" && s.Country == "" {
		return "", "", false
	}
	return s.ExitIP, s.Country, true
}
