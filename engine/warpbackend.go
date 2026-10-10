package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/xhwarp/android-engine/warp"
)

// warpBackend 是真正干活的 tunnelBackend：读/注册 WARP 账号 → 建 WG/AWG 隧道
// → 真实请求一次 Cloudflare 拿出口 IP 与归属。
//
// 它是从桌面版 warpscout 引擎搬过来的（见 docs/porting-analysis.md 的 P1）：
// 数据面是 amneziawg-go 的 device + gVisor netstack，**不需要 TUN 设备、
// 不需要 root**。桌面版把这一层交给 mihomo.exe 子进程，安卓上没有子进程可用。
type warpBackend struct {
	workDir string

	mu      sync.Mutex
	session *warp.Session
	tunFD   int
	// 单端点握手超时，来自设置里的「连接超时（秒）」。
	handshake time.Duration
	// 账号在第一次连接时解析并缓存；后续连接（换端点、重连）不再走注册。
	accountJSON string
	// Psiphon 出口的官方 client.config 内容与系统 DNS，由 Kotlin 随包喂入。
	// 为空表示没有随包配置，这时选 Psiphon 会明确报错而不是静默按基础协议走。
	psiphonConfig string
	psiphonDNS    string
	// 每个协议上一次成功用过的端点（proto -> "ip:port"），落盘在 workDir。
	// 下次连接把它排到候选最前面，避免每次重扫一遍才碰上能过数据的端点。
	lastGood map[string]string
}

// lastGoodPath 是「上次成功端点」缓存文件。放在 App 私有目录，随 App 卸载消失。
func (b *warpBackend) lastGoodPath() string {
	return filepath.Join(b.workDir, "last-endpoint.json")
}

// preferEndpoint 取出某协议上次成功的端点（没有则为空串）。
func (b *warpBackend) preferEndpoint(proto string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.lastGood == nil {
		b.lastGood = loadEndpointCache(b.lastGoodPath())
	}
	return b.lastGood[proto]
}

// rememberEndpoint 记下某协议本次成功的端点并落盘。
func (b *warpBackend) rememberEndpoint(proto, endpoint string) {
	if proto == "" || endpoint == "" {
		return
	}
	b.mu.Lock()
	if b.lastGood == nil {
		b.lastGood = loadEndpointCache(b.lastGoodPath())
	}
	if b.lastGood[proto] == endpoint {
		b.mu.Unlock()
		return
	}
	b.lastGood[proto] = endpoint
	snapshot := make(map[string]string, len(b.lastGood))
	for k, v := range b.lastGood {
		snapshot[k] = v
	}
	b.mu.Unlock()
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(b.lastGoodPath()), 0o755)
	_ = os.WriteFile(b.lastGoodPath(), data, 0o600)
}

// loadEndpointCache 读缓存；文件不存在或损坏都当作空（下次重新记住即可）。
func loadEndpointCache(path string) map[string]string {
	out := map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return map[string]string{}
	}
	return out
}

// defaultHandshakeTimeout 与电脑版 TimeoutSec 的默认值（2s）同量级，但手机
// 网络 RTT 更大，给到 3s，否则好端点会被自己的超时误判掉。
const defaultHandshakeTimeout = 3 * time.Second

// SetTimeout 设置单端点握手超时（秒）。0 表示用默认值。
func (b *warpBackend) SetTimeout(sec int) {
	b.mu.Lock()
	if sec <= 0 {
		b.handshake = 0
	} else {
		b.handshake = time.Duration(sec) * time.Second
	}
	b.mu.Unlock()
}

// SetTunFD 记录 VpnService 建好的 tun 设备 fd。fd 为 0 表示退回 netstack
// 模式（进程内隧道，系统看不到），只有自测才这么用。
func (b *warpBackend) SetTunFD(fd int) {
	// 覆盖之前先把上一次遗留的、没人接管的 fd 收掉：引用一旦被覆盖就再也找不到
	// 它了，而 VpnService 的接口只要还有 fd 打开就一直挂着（只有杀进程才恢复）。
	_, _ = b.releaseLeftoverTunFD()
	b.mu.Lock()
	b.tunFD = fd
	b.mu.Unlock()
}

// Counters 报告 tun 设备累计进出的字节数（fd 模式下才有）。
func (b *warpBackend) Counters() (uint64, uint64) {
	b.mu.Lock()
	sess := b.session
	b.mu.Unlock()
	if sess == nil {
		return 0, 0
	}
	return sess.Counters()
}

func newWarpBackend(workDir string) *warpBackend {
	return &warpBackend{workDir: workDir}
}

// accountPath 是缓存的账号文件。放在 App 的 filesDir 下，
// 对应桌面版的 WARP_CLIENT_HOME/warpscout-account.json。
func (b *warpBackend) accountPath() string {
	return filepath.Join(b.workDir, "account.json")
}

// SetAccountJSON 让上层（Kotlin）直接喂一份账号，跳过网络注册。
//
// 这份 JSON 还可以内嵌 masque / outer 两台附加设备（见 assets/account.json）。
// 拆出来落盘后，MASQUE 与 WARP-in-WARP 也能复用随包凭据，而不是在被墙的网络里
// 现注册 —— 那一步几乎必然失败。
func (b *warpBackend) SetAccountJSON(raw string) {
	b.mu.Lock()
	b.accountJSON = raw
	workDir := b.workDir
	b.mu.Unlock()
	warp.SplitEmbeddedAccounts(workDir, raw)
}

// AccountJSON 返回当前账号（可能为空），供上层持久化或展示。
func (b *warpBackend) AccountJSON() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.accountJSON
}

// SetPsiphonConfig 喂入官方的 Psiphon client.config 与系统 DNS。
//
// 这是进程内 Psiphon 能不能起来的唯一前提：client.config 里带着 SponsorId、
// 服务器列表 URL 与签名公钥，缺了它 tunnel-core 会报「零个可用服务器」。
//
//	dns 逗号分隔（gobind 不支持切片），空则让 tunnel-core 用系统解析器。
func (b *warpBackend) SetPsiphonConfig(clientConfigJSON, dns string) {
	b.mu.Lock()
	b.psiphonConfig = clientConfigJSON
	b.psiphonDNS = dns
	b.mu.Unlock()
}

// connectPsiphon 建「基础隧道（上游）+ 进程内 Psiphon 出口」。
//
// 这里的出口 IP/国家来自**通过 Psiphon SOCKS 的实测**，所以界面显示的就是
// Psiphon 的出口，而不是上游 WARP 端点的 —— 桌面版历史上正是把两者混在一起，
// 才出现「Psiphon 出口在澳洲，显示的却不是」。
func (b *warpBackend) connectPsiphon(
	ctx context.Context, req Request, report func(string),
	handshake time.Duration, tunFD int) (Result, error) {

	// 上游可以是任意一种传输：它在这里的角色只是「一个能出网的本地 SOCKS」，
	// WG / AWG / MASQUE / WARP-in-WARP 都满足（见 warp/psiphon.go 的 DialPsiphon）。
	// 空 proto 由 warp 侧按 awg 处理。
	b.mu.Lock()
	clientConfig := b.psiphonConfig
	dns := b.psiphonDNS
	b.mu.Unlock()
	if clientConfig == "" {
		return Result{}, fmt.Errorf("缺少官方 Psiphon client.config（需随包提供，并调用 SetPsiphonConfig 喂入）")
	}
	if tunFD <= 0 {
		return Result{}, fmt.Errorf("Psiphon 需要系统 VPN 的 tun fd，当前为 %d", tunFD)
	}

	sess, err := warp.DialPsiphon(ctx, warp.PsiphonOptions{
		ClientConfigJSON: clientConfig,
		DNS:              dns,
		DataDir:          filepath.Join(b.workDir, "psiphon"),
		TunFD:            tunFD,
		UnderlayProto:    req.Proto,
		UnderlayEndpoint: req.Endpoint,
		UnderlayPrefer:   b.preferEndpoint(req.Proto),
		IPv6:             req.IPv6,
		HandshakeTimeout: handshake,
		VerifyTimeout:    8 * time.Second,
		Budget:           60 * time.Second,
		Report:           report,
	})
	if err != nil {
		return Result{}, err
	}
	b.rememberEndpoint(req.Proto, sess.Endpoint)

	b.mu.Lock()
	b.session = sess
	b.mu.Unlock()

	return Result{
		ExitIP:   sess.ExitIP,
		Country:  sess.Country,
		Endpoint: sess.Endpoint,
	}, nil
}

// Connect 建隧道。失败时**必须**把系统 VPN 一起收掉 —— 见下面 defer 里的说明。
func (b *warpBackend) Connect(ctx context.Context, req Request, report func(string)) (result Result, err error) {
	// 连接失败时收掉还挂在系统 VPN 上的 tun fd。
	//
	// VpnService 的接口是「只要还有 fd 打开就一直挂着」。建立成功时 fd 会交给
	// 隧道设备或 Psiphon 的 packet tunnel（由它们在自己的 Close 里关），但失败时
	// 可能谁都还没接管 —— 例如带 Psiphon 时上游在 netstack 里就失败了。这时没人
	// 关 fd，表现就是**界面显示未连接、系统 VPN 却一直开着**，手机所有流量都流进
	// 一个没人读的 tun；用户实测只有退出 App 才恢复（进程消失，fd 被系统回收）。
	defer func() {
		if err == nil {
			return
		}
		if fd, released := b.releaseLeftoverTunFD(); released {
			report(fmt.Sprintf("已关闭系统 tun（fd %d）：没人接管的 fd 会让系统 VPN 一直挂着", fd))
		}
	}()

	if req.Proto == "" {
		req.Proto = "awg"
	}
	mtu := req.MTU
	if mtu <= 0 {
		mtu = 1280
	}

	b.mu.Lock()
	accountJSON := b.accountJSON
	b.mu.Unlock()

	// 1) 账号：本地缓存 → 外部喂入 → 现注册。注册要联网，在部分网络里会失败，
	//    所以错误里必须写清「怎么办」。
	report("准备 WARP 账号…")
	raw, err := warp.LoadOrRegisterAccount(ctx, b.accountPath(), accountJSON)
	if err != nil {
		return Result{}, err
	}
	b.mu.Lock()
	b.accountJSON = raw
	b.mu.Unlock()
	report("账号就绪")

	// 2) 隧道参数。TunFD 决定数据面：有 fd 就是系统 VPN（系统负责路由，
	//    我们只加解密），没有就是 netstack（进程内隧道，系统看不到）。
	b.mu.Lock()
	tunFD := b.tunFD
	handshake := b.handshake
	b.mu.Unlock()
	if handshake <= 0 {
		handshake = defaultHandshakeTimeout
	}
	// Psiphon 出口时系统 tun fd 归 tunnel-core 的 packet tunnel：基础隧道必须退成
	// 进程内 netstack，只当 Psiphon 的上游载体（见 warp/psiphon.go）。
	backendTunFD := tunFD
	if req.Psiphon {
		backendTunFD = 0
	}
	warp.Apply(warp.Config{
		IPv6:            req.IPv6,
		TunFD:           backendTunFD,
		MTU:             mtu,
		MasqueCredsPath: filepath.Join(b.workDir, "masque.json"),
		OuterCredsPath:  filepath.Join(b.workDir, "outer.json"),
	})
	if req.Psiphon {
		return b.connectPsiphon(ctx, req, report, handshake, tunFD)
	}
	if tunFD > 0 {
		report(fmt.Sprintf("数据面 = 系统 VPN（tun fd %d, MTU %d）", tunFD, mtu))
	} else {
		report("数据面 = 进程内 netstack（系统 VPN 未启用）")
	}

	// 3) 拨号 + 验证。fd 模式下这一步的出口验证是走**真实系统网络**做的，
	//    所以它同时证明了「系统 VPN 真的在路由」，而不只是握手成功。
	sess, err := warp.Dial(ctx, warp.Options{
		Proto:    req.Proto,
		Endpoint: req.Endpoint,
		Prefer:   b.preferEndpoint(req.Proto),
		IPv6:     req.IPv6,
		Sample:   2, // 14 个 /24 × 2 = 28 个候选
		// 单次尝试短一点，才能在被墙的端口上快速换下一个；具体值来自设置。
		HandshakeTimeout: handshake,
		VerifyTimeout:    8 * time.Second,
		// 到点就报失败，不能让界面一直转。
		Budget:      60 * time.Second,
		SocksListen: "127.0.0.1:1080",
		Report:      report,
	})
	if err != nil {
		return Result{}, err
	}
	// 记住这个能用的端点，下次连接直接先试它。
	b.rememberEndpoint(req.Proto, sess.Endpoint)

	b.mu.Lock()
	b.session = sess
	b.mu.Unlock()

	return Result{
		ExitIP:   sess.ExitIP,
		Country:  sess.Country,
		Endpoint: sess.Endpoint,
	}, nil
}

// releaseLeftoverTunFD 关掉一个**还没有被隧道设备/Psiphon 接管**的 tun fd，
// 返回 (fd, 是否真的关了)。接线见 warp.ReleaseTunFD 与 warp/tunfd_owner.go。
//
// 关成功后把记录清成 0：这个 fd 号随时可能被系统复用给别的 socket，留着它
// 就等于留了一次「重复关闭无关 fd」的机会。
func (b *warpBackend) releaseLeftoverTunFD() (int, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	fd := b.tunFD
	if !warp.ReleaseTunFD(fd) {
		return fd, false
	}
	b.tunFD = 0
	return fd, true
}

func (b *warpBackend) Close() error {
	b.mu.Lock()
	sess := b.session
	b.session = nil
	b.mu.Unlock()
	if sess == nil {
		// 没有会话 = 上一次连接是失败的：失败路径已经尽力关过 fd，这里是第二道
		// 保险（例如用户在「连接失败」状态下直接点断开）。已经被设备接管时
		// ReleaseTunFD 是空操作，不会误关别人的 fd。
		_, _ = b.releaseLeftoverTunFD()
		return nil
	}
	sess.Close()
	return nil
}

// SocksAddr 报告本地 SOCKS 监听地址（没有会话时为空）。
func (b *warpBackend) SocksAddr() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.session == nil {
		return ""
	}
	return b.session.SocksAddr()
}
