package warp

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	connectip "github.com/Diniboy1123/connect-ip-go"
	"github.com/Diniboy1123/usque/api"
	"github.com/amnezia-vpn/amneziawg-go/tun"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/yosida95/uritemplate/v3"
	"golang.org/x/net/http2"
)

// ---------------------------------------------------------------------------
// MASQUE：CONNECT-IP（RFC 9484），底层可以是 HTTP/3 或 HTTP/2
// ---------------------------------------------------------------------------
//
// 这段是从桌面版 warpscout/masque.go **整段搬过来的**（那一版已经在真机网络上
// 跑过、端点池也是实测出来的），只改了三处适配安卓：
//
//  1. 数据面：有 VpnService 的 tun fd 就用系统 tun（全机路由），没有才用
//     gVisor netstack（桌面/自测）。映射关系是「一个 IP 包 = 一个 CONNECT-IP
//     datagram」，两边都是裸 IP 包，所以直接换设备即可。
//  2. 凭据：桌面版从 warpscout 的 account 文件里取 masque 段；这里用一个
//     独立的 masque.json（原因见下）。
//  3. 拨号驱动：桌面版由扫描器驱动 handshake，这里由 dialMasque 驱动。
//
// 为什么 MASQUE 必须用**独立的一套账号**：登记（PATCH /reg/{id}）会把设备改成
// key_type=secp256r1 / tunnel_type=masque，同一个账号里原来的 curve25519 私钥
// 就再也对不上了 —— 拿 WG 的账号去做 MASQUE，会把 WG 模式弄坏。
//
// 关键事实（都来自桌面版的实测注释，不是猜的）：
//   - 客户端证书是本地自签的：Cloudflare 从不签发客户端证书，认证完全靠
//     「登记过的那把公钥」；反过来客户端 pin 的是服务端公钥（usque 的
//     PrepareTlsConfig 就是干这个）。
//   - QUIC 只从每个 /24 的 ::1/::2 提供；HTTP/2 覆盖整个 /24。
//   - 同一个端点同一时刻连得上、下一次超时，所以单次失败不说明问题。

// Values the official client uses, copied from Diniboy1123/usque (internal/, so
// not importable). The SNI never matches the endpoint address, which is why
// usque pins the peer's public key instead of verifying the certificate.
const (
	masqueConnectURI  = "https://cloudflareaccess.com"
	masqueDefaultSNI  = "consumer-masque.cloudflareclient.com"
	masqueKeepalive   = 30 * time.Second
	masqueIdleTimeout = 5 * time.Second
	// Cloudflare never signs the client certificate: it only carries the key
	// enrolled through the API, and that enrolment is the whole authentication.
	masqueCertLifetime = 24 * time.Hour
	// Headroom over the MTU so an oversized frame is read rather than truncated.
	masqueReadBuf = tunnelMTU + 128
	// The same endpoint with the same SNI answers on one attempt and times out on
	// the next, so a single failure says nothing. Measured on a filtered network,
	// same endpoints and same SNI: one try scored 1-3 of 14 working, three tries
	// scored 11-13. Below this the scan reports mostly false negatives.
	masqueDefaultAttempts = 3
)

// MASQUE is served from the ::1 and ::2 of each block, not from the WireGuard
// pools: measured, every other address in 162.159.198.0/24 and every wg pool
// address answers QUIC but rejects the TLS handshake outright. v6 has two such
// blocks and so four addresses - 16 random addresses sampled out of either /120
// answered nothing, while all four of these worked (20-21 of 28 ip:port pairs,
// the usual flap), reproduced on two hosts in different regions.
var (
	masquePoolsV4 = []netip.Prefix{
		netip.MustParsePrefix("162.159.198.1/32"),
		netip.MustParsePrefix("162.159.198.2/32"),
	}
	masquePoolsV6 = []netip.Prefix{
		netip.MustParsePrefix("2606:4700:103::1/128"),
		netip.MustParsePrefix("2606:4700:103::2/128"),
		netip.MustParsePrefix("2606:4700:104::1/128"),
		netip.MustParsePrefix("2606:4700:104::2/128"),
	}
)

// CONNECT-IP over TCP+TLS/HTTP2 is Cloudflare's own fallback transport, and the
// QUIC measurement above says nothing about it: usque defaults to 162.159.198.2
// and documents no v6 address at all
// (https://github.com/Diniboy1123/usque/wiki/HTTP-2-support). How much of these
// ranges answers over TCP is what this pool widens.
var (
	masqueH2PoolsV4 = []netip.Prefix{
		netip.MustParsePrefix("162.159.198.0/24"),
		netip.MustParsePrefix("162.159.199.0/24"),
	}
	// The v6 side is two whole /48s, not the /120 the QUIC addresses sit in: every
	// sampled ip:port pair worked across both, on two hosts in different regions.
	// The neighbouring blocks are dead on both - 101, 102 (Zero Trust), 105, 106
	// and the wg pools - so the range really is these two.
	masqueH2PoolsV6 = []netip.Prefix{
		netip.MustParsePrefix("2606:4700:103::/48"),
		netip.MustParsePrefix("2606:4700:104::/48"),
	}
)

// The API hands this list out per account, but it is the same everywhere and
// usque's own config.json has no port field at all, so it is a constant.
var masqueEndpointPorts = []int{443, 500, 1701, 4500, 4443, 8443, 8095}

// Set by loadMasqueAccount, like warpPrivateKey for the WireGuard family.
var masqueAcct *account

// 每次会话的 MASQUE 凭据文件路径，由 Config 注入（与 WG 账号分开存）。
var masqueCredsFile string

// masqueMTU 与系统 tun 用同一个 MTU：Kotlin 的 VpnService.Builder 和引擎
// 必须是同一个值，否则内核会按错误尺寸切包。
func masqueMTU() int {
	if tunMTU > 0 {
		return tunMTU
	}
	return tunnelMTU
}

// -masque-sni overrides this. The endpoint never matches the SNI anyway and the
// peer is pinned by public key, so the name is free to be anything DPI lets
// through - the MASQUE equivalent of the AmneziaWG init packet.
var masqueSNI = masqueDefaultSNI

// The MASQUE flow lives on its own API version with its own client headers, and
// registers in two steps: POST /reg mints a device the same way the Android
// client does, PATCH /reg/{id} swaps its key for an ECDSA one and switches the
// tunnel type. Done through warpscout's own doJSON so -proxy, -interface and the
// tunnel fallback keep working.
const (
	masqueRegURL           = "https://api.cloudflareclient.com/v0a4471/reg"
	masqueClientVersion    = "a-6.35-4471"
	masqueUserAgent        = "WARP for Android"
	masqueKeyType          = "secp256r1"
	masqueTunnelType       = "masque"
	masqueRegKeyType       = "curve25519"
	masqueRegTunnelType    = "wireguard"
	masqueRegModel         = "PC"
	masqueRegLocale        = "en_US"
	masqueRegDeviceName    = "Xehn Android"
	masqueRegOSVersion     = "10"
	masqueRegSerialNumLen  = 8
	masqueRegPlaceholderKB = 32
)

type masqueRegResp struct {
	ID     string `json:"id"`
	Token  string `json:"token"`
	Config struct {
		Peers []struct {
			PublicKey string `json:"public_key"`
		} `json:"peers"`
		Interface struct {
			Addresses struct {
				V4 string `json:"v4"`
				V6 string `json:"v6"`
			} `json:"addresses"`
		} `json:"interface"`
	} `json:"config"`
}

func randomB64(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf), nil
}

// registerMasque 两步注册：先按 WireGuard 的样子开一台设备（key 只是占位，
// API 只看形状），再把它的 key 换成 ECDSA 并切到 masque。
func registerMasque(ctx context.Context, client *http.Client) (*account, error) {
	placeholder, err := randomB64(masqueRegPlaceholderKB)
	if err != nil {
		return nil, err
	}
	serial, err := randomB64(masqueRegSerialNumLen)
	if err != nil {
		return nil, err
	}
	body, err := doJSONAs(ctx, client, http.MethodPost, masqueRegURL, "",
		masqueUserAgent, masqueClientVersion, map[string]any{
			"key":           placeholder,
			"install_id":    "",
			"fcm_token":     "",
			"tos":           time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
			"model":         masqueRegModel,
			"serial_number": serial,
			"os_version":    masqueRegOSVersion,
			"key_type":      masqueRegKeyType,
			"tunnel_type":   masqueRegTunnelType,
			"locale":        masqueRegLocale,
		})
	if err != nil {
		return nil, fmt.Errorf("POST /reg: %w", err)
	}
	var first masqueRegResp
	if err := json.Unmarshal(body, &first); err != nil {
		return nil, fmt.Errorf("parse masque reg response: %w", err)
	}
	if first.ID == "" || first.Token == "" {
		return nil, fmt.Errorf("masque reg response missing id or token")
	}

	privDER, pubDER, err := genECKeyPair()
	if err != nil {
		return nil, err
	}
	body, err = doJSONAs(ctx, client, http.MethodPatch, masqueRegURL+"/"+first.ID, first.Token,
		masqueUserAgent, masqueClientVersion, map[string]string{
			"key":         base64.StdEncoding.EncodeToString(pubDER),
			"key_type":    masqueKeyType,
			"tunnel_type": masqueTunnelType,
			"name":        masqueRegDeviceName,
		})
	if err != nil {
		return nil, fmt.Errorf("PATCH /reg/%s: %w", first.ID, err)
	}
	var enrolled masqueRegResp
	if err := json.Unmarshal(body, &enrolled); err != nil {
		return nil, fmt.Errorf("parse masque enrol response: %w", err)
	}
	if len(enrolled.Config.Peers) == 0 || enrolled.Config.Peers[0].PublicKey == "" {
		return nil, fmt.Errorf("masque enrol response missing peer public key")
	}
	return &account{
		ID:            first.ID,
		Token:         first.Token,
		PrivateKey:    base64.StdEncoding.EncodeToString(privDER),
		PeerPublicKey: enrolled.Config.Peers[0].PublicKey,
		IPv4:          enrolled.Config.Interface.Addresses.V4,
		IPv6:          enrolled.Config.Interface.Addresses.V6,
	}, nil
}

// ensureMasqueAccount 拿到可用的 MASQUE 凭据：优先读缓存，否则现注册一套。
//
// 注册要联网，而系统 VPN 此时已经建立 —— 所以必须走 regTransport（它的
// Control 钩子里挂了 protect），否则这些请求会被自己的 tun 吞掉。
func ensureMasqueAccount(ctx context.Context, report func(string)) (*account, error) {
	if masqueCredsFile != "" {
		if a, err := loadAccount(masqueCredsFile); err == nil && a.PrivateKey != "" && a.PeerPublicKey != "" {
			report("复用已登记的 MASQUE 凭据")
			return &a, nil
		}
	}
	report("准备 MASQUE 账号（首次需要注册 + 登记 ECDSA 公钥）…")
	client := &http.Client{Timeout: 25 * time.Second, Transport: regTransport(nil)}
	a, err := registerMasque(ctx, client)
	if err != nil {
		return nil, fmt.Errorf("MASQUE 注册失败: %w", err)
	}
	if masqueCredsFile != "" {
		if serr := saveAccount(masqueCredsFile, *a); serr != nil {
			report(fmt.Sprintf("警告：MASQUE 凭据保存失败（下次会重新登记）：%v", serr))
		}
	}
	report(fmt.Sprintf("MASQUE 登记完成：隧道地址 %s %s", a.IPv4, a.IPv6))
	return a, nil
}

func genECKeyPair() (privDER, pubDER []byte, err error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	if privDER, err = x509.MarshalECPrivateKey(priv); err != nil {
		return nil, nil, err
	}
	if pubDER, err = x509.MarshalPKIXPublicKey(&priv.PublicKey); err != nil {
		return nil, nil, err
	}
	return privDER, pubDER, nil
}

// selfSignedCert 造客户端证书。内容不参与校验，但 NotBefore 往前挪一小时：
// 手机时钟与服务器差几分钟是常态，卡在 notBefore 上就是握手直接失败。
func selfSignedCert(priv *ecdsa.PrivateKey) ([][]byte, error) {
	der, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{
		SerialNumber: big.NewInt(0),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(masqueCertLifetime),
	}, &x509.Certificate{}, &priv.PublicKey, priv)
	if err != nil {
		return nil, err
	}
	return [][]byte{der}, nil
}

func (a account) tlsConfig() (*tls.Config, error) {
	privDER, err := base64.StdEncoding.DecodeString(a.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("masque private key: %w", err)
	}
	priv, err := x509.ParseECPrivateKey(privDER)
	if err != nil {
		return nil, fmt.Errorf("masque private key: %w", err)
	}
	block, _ := pem.Decode([]byte(a.PeerPublicKey))
	if block == nil {
		return nil, fmt.Errorf("masque peer public key is not PEM")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("masque peer public key: %w", err)
	}
	peerPub, ok := parsed.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("masque peer public key is not ECDSA")
	}
	cert, err := selfSignedCert(priv)
	if err != nil {
		return nil, err
	}
	return api.PrepareTlsConfig(priv, peerPub, cert, masqueSNI, false)
}

func (a account) localAddrs() ([]netip.Addr, error) {
	var out []netip.Addr
	for _, s := range []string{a.IPv4, a.IPv6} {
		if addr, err := netip.ParseAddr(s); err == nil {
			out = append(out, addr)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("masque account carries no interface address")
	}
	return out, nil
}

// The netstack side outlives any single peer: handshake swaps the CONNECT-IP
// connection underneath it, exactly like IpcSet swaps the WireGuard peer.
type masqueTunnel struct {
	// ipStack 在 fd 模式下为 nil：系统 tun 就是数据面，不需要用户态协议栈。
	*ipStack
	dev     tun.Device
	counter tunCounter
	mtu     int
	tlsCfg  *tls.Config
	h2      bool

	mu   sync.Mutex
	conn *connectip.Conn
	drop func()
}

// quic-go and connect-ip-go write straight to the standard logger - a receive
// buffer warning, then a line per closed stream, and every endpoint swap closes
// one. Nothing here uses that logger, so the whole thing is muted rather than
// left to scribble over the app log.
var muteLibLog sync.Once

func newMasqueTunnel(h2 bool, mtu int, report func(string)) (*masqueTunnel, error) {
	muteLibLog.Do(func() { log.SetOutput(io.Discard) })
	if masqueAcct == nil {
		return nil, fmt.Errorf("no MASQUE device enrolled")
	}
	tlsCfg, err := masqueAcct.tlsConfig()
	if err != nil {
		return nil, err
	}

	var (
		dev     tun.Device
		stack   *ipStack
		counter tunCounter
	)
	if tunFD > 0 {
		// 安卓：VpnService 已经把 tun 建好了，我们只负责把包搬进 CONNECT-IP。
		dev, counter, err = newFdTunDevice(tunFD, mtu)
		if err != nil {
			return nil, err
		}
		report("MASQUE 数据面 = 系统 VPN")
	} else {
		// 桌面/自测：进程内 gVisor 协议栈，系统看不到这条隧道。
		local, lerr := masqueAcct.localAddrs()
		if lerr != nil {
			return nil, lerr
		}
		dev, stack, err = newIPStack(local, mtu)
		if err != nil {
			return nil, err
		}
		report("MASQUE 数据面 = 进程内 netstack")
	}

	t := &masqueTunnel{ipStack: stack, dev: dev, counter: counter, mtu: mtu, tlsCfg: tlsCfg, h2: h2}
	go t.pumpOut()
	return t, nil
}

func (t *masqueTunnel) ports() []int    { return masqueEndpointPorts }
func (t *masqueTunnel) attempts() int   { return masqueDefaultAttempts }
func (t *masqueTunnel) stack() *ipStack { return t.ipStack }

func (t *masqueTunnel) counters() (uint64, uint64) {
	if t.counter == nil {
		return 0, 0
	}
	return t.counter.Counters()
}

func (t *masqueTunnel) Close() {
	t.dropPeer()
	if t.dev != nil {
		_ = t.dev.Close()
	}
}

func (t *masqueTunnel) dropPeer() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.drop != nil {
		t.drop()
		t.drop = nil
	}
	t.conn = nil
}

func (t *masqueTunnel) currentConn() *connectip.Conn {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.conn
}

// One reader for the whole tunnel: the device is reused across peers, so a
// per-peer reader would be left blocked in dev.Read after a swap.
func (t *masqueTunnel) pumpOut() {
	buf := make([]byte, masqueReadBuf)
	bufs := [][]byte{buf}
	sizes := []int{0}
	for {
		if _, err := t.dev.Read(bufs, sizes, 0); err != nil {
			return
		}
		conn := t.currentConn()
		if conn == nil {
			continue
		}
		if _, err := conn.WritePacket(buf[:sizes[0]]); err != nil {
			continue
		}
	}
}

func (t *masqueTunnel) pumpIn(ctx context.Context, conn *connectip.Conn) {
	buf := make([]byte, masqueReadBuf)
	for ctx.Err() == nil {
		n, err := conn.ReadPacket(buf, true)
		if err != nil {
			return
		}
		if _, err := t.dev.Write([][]byte{buf[:n]}, 0); err != nil {
			return
		}
	}
}

// dialConnectIP 建立一条 CONNECT-IP 连接，**套接字由我们自己开**。
//
// 为什么不用 usque 的 api.ConnectTunnel：它内部是裸的 net.ListenUDP / net.Dialer，
// 这些 socket 不在 VpnService.protect 的名单里。系统 VPN 一建立，它们的第一个包
// 就被我们自己的 tun 吞掉 —— 而 CONNECT-IP 还没建起来，包只会被丢，握手永远完不
// 成，界面上只看到「连不上」，还看不到原因。所以这里用带 protectControl 的
// ListenConfig / Dialer 重建同一套流程（quic-go + connect-ip-go 本来就是我们直接
// 依赖的，两边都是自己的代码路径，没有多引入任何东西）。
//
// HTTP/3：UDP + quic-go。HTTP/2：TCP + TLS + http2，两者都是 Cloudflare 的正式传输。
func dialConnectIP(ctx context.Context, tlsCfg *tls.Config, quicCfg *quic.Config, endpoint string, h2 bool) (*net.UDPConn, *http3.Transport, *connectip.Conn, error) {
	template := uritemplate.MustNew(masqueConnectURI)
	headers := http.Header{"User-Agent": []string{""}}

	if h2 {
		hp := headers.Clone()
		hp.Set("cf-connect-proto", "cf-connect-ip")
		hp.Set("pq-enabled", "false")
		client, err := protectedHTTP2Client(tlsCfg, endpoint)
		if err != nil {
			return nil, nil, nil, err
		}
		ipConn, _, err := connectip.DialH2(ctx, client, template, hp)
		if err != nil {
			if strings.Contains(err.Error(), "tls: access denied") {
				return nil, nil, nil, fmt.Errorf("MASQUE 登录被拒：本设备的凭据没有登记到 Cloudflare Access（%w）", err)
			}
			return nil, nil, nil, err
		}
		return nil, nil, ipConn, nil
	}

	remote, err := net.ResolveUDPAddr("udp", endpoint)
	if err != nil {
		return nil, nil, nil, err
	}
	udpConn, err := protectedUDPConn(ctx, remote.IP.To4() == nil)
	if err != nil {
		return nil, nil, nil, err
	}
	// ConnectionIDLength 不设时后端偶发 PROTOCOL_VIOLATION 并直接关连接。
	qtr := &quic.Transport{Conn: udpConn, ConnectionIDLength: 20}
	conn, err := qtr.Dial(ctx, remote, tlsCfg, quicCfg)
	if err != nil {
		_ = udpConn.Close()
		return nil, nil, nil, err
	}
	tr := &http3.Transport{
		EnableDatagrams: true,
		AdditionalSettings: map[uint64]uint64{
			// SETTINGS_H3_DATAGRAM_00：已废弃，但官方客户端仍然在发。
			0x276: 1,
		},
		DisableCompression: true,
	}
	ipConn, _, err := connectip.Dial(ctx, tr.NewClientConn(conn), template, "cf-connect-ip", headers, true)
	if err != nil {
		_ = tr.Close()
		_ = conn.CloseWithError(0, "connect-ip dial failed")
		_ = udpConn.Close()
		return nil, nil, nil, err
	}
	return udpConn, tr, ipConn, nil
}

// protectedUDPConn 开一个已被 VpnService.protect 排除出系统 VPN 的 UDP socket。
func protectedUDPConn(ctx context.Context, v6 bool) (*net.UDPConn, error) {
	network := "udp4"
	if v6 {
		network = "udp6"
	}
	lc := net.ListenConfig{Control: protectControlFn}
	pc, err := lc.ListenPacket(ctx, network, ":0")
	if err != nil {
		return nil, err
	}
	uc, ok := pc.(*net.UDPConn)
	if !ok {
		_ = pc.Close()
		return nil, fmt.Errorf("udp listen 返回了 %T", pc)
	}
	return uc, nil
}

// protectedHTTP2Client 与 usque 的 newHTTP2Client 同构，区别只有一处：拨号用的是
// 带 protectControl 的 dialer（同样是为了不被自己的 tun 吞掉）。
func protectedHTTP2Client(base *tls.Config, endpoint string) (*http.Client, error) {
	cfg := base.Clone()
	cfg.NextProtos = []string{"h2"}
	dialer := &net.Dialer{Control: protectControlFn}
	tr := &http2.Transport{
		DialTLSContext: func(ctx context.Context, network, _ string, _ *tls.Config) (net.Conn, error) {
			conn, err := dialer.DialContext(ctx, network, endpoint)
			if err != nil {
				return nil, err
			}
			tlsConn := tls.Client(conn, cfg)
			if err := tlsConn.HandshakeContext(ctx); err != nil {
				_ = conn.Close()
				return nil, err
			}
			return tlsConn, nil
		},
	}
	return &http.Client{Transport: tr}, nil
}

// A successful dial is the MASQUE equivalent of the WireGuard handshake, but a
// weaker signal: measured on a filtered network, every endpoint dialled and only
// two passed data, so callers must still verify with /meta.
func (t *masqueTunnel) handshake(ctx context.Context, endpoint string, timeout time.Duration) bool {
	return t.handshakeErr(ctx, endpoint, timeout) == nil
}

// handshakeErr 与 handshake 相同，但把失败原因带出来。拨号失败必须能被说出来：
// 「42 个候选一个都没建起 CONNECT-IP」这种一句话日志没法定位是网络、凭据还是
// 版本问题。
func (t *masqueTunnel) handshakeErr(ctx context.Context, endpoint string, timeout time.Duration) error {
	t.dropPeer()

	// The deadline may only bound the dial: over HTTP/2 the CONNECT-IP stream is
	// one long-lived request carried by this very context, so a WithTimeout - or
	// the usual defer cancel() - kills the tunnel the moment the dial returns.
	dialCtx, cancelDial := context.WithCancel(ctx)
	dialDeadline := time.AfterFunc(timeout, cancelDial)
	// Both are set because a blocked endpoint stalls in two different places:
	// quic-go's own handshake, and the CONNECT-IP request that never answers.
	udpConn, tr, conn, err := dialConnectIP(dialCtx, t.tlsCfg,
		&quic.Config{EnableDatagrams: true, KeepAlivePeriod: masqueKeepalive, HandshakeIdleTimeout: timeout, MaxIdleTimeout: masqueIdleTimeout},
		endpoint, t.h2)
	if err != nil {
		cancelDial()
		closeMasqueTransport(udpConn, tr, conn)
		return err
	}
	if !dialDeadline.Stop() {
		cancelDial()
		closeMasqueTransport(udpConn, tr, conn)
		return fmt.Errorf("拨号超时（%s）", timeout)
	}

	pumpCtx, cancelPump := context.WithCancel(context.Background())
	go t.pumpIn(pumpCtx, conn)

	t.mu.Lock()
	t.conn = conn
	t.drop = func() {
		cancelPump()
		cancelDial()
		closeMasqueTransport(udpConn, tr, conn)
	}
	t.mu.Unlock()
	return nil
}

func closeMasqueTransport(udpConn *net.UDPConn, tr *http3.Transport, conn *connectip.Conn) {
	if conn != nil {
		conn.Close()
	}
	if tr != nil {
		tr.Close()
	}
	if udpConn != nil {
		udpConn.Close()
	}
}

// masqueCandidateList 给出要按顺序试的 "ip:port"。
//
// 顺序是刻意排的：**先把 443 上的所有地址过一遍，再换端口**。MASQUE 的失败
// 绝大多数是「这台边缘这次没响应」，换地址比换端口有效得多；而 443 是唯一
// 在所有网络里都最可能放行的端口。
func masqueCandidateList(o Options, h2 bool) []string {
	if o.Endpoint != "" {
		host, port, err := net.SplitHostPort(o.Endpoint)
		if err != nil {
			return []string{o.Endpoint}
		}
		return []string{net.JoinHostPort(host, port)}
	}

	v4, v6 := masquePoolsV4, masquePoolsV6
	if h2 {
		v4, v6 = masqueH2PoolsV4, masqueH2PoolsV6
	}
	var addrs []netip.Addr
	if !o.IPv6 {
		addrs = append(addrs, prefixAddrs(v4, masqueSampleV4)...)
		addrs = append(addrs, prefixAddrs(v6, masqueSampleV6)...)
	} else {
		addrs = append(addrs, prefixAddrs(v6, masqueSampleV6)...)
		addrs = append(addrs, prefixAddrs(v4, masqueSampleV4)...)
	}

	ports := masqueEndpointPorts
	var out []string
	for i, port := range ports {
		// 第一轮只走 443；其余端口只在 443 全灭之后才试。
		if i > 0 && len(out) > 0 {
			break
		}
		_ = i
		for _, a := range addrs {
			out = append(out, net.JoinHostPort(a.String(), itoa(port)))
		}
	}
	if len(out) == 0 {
		return nil
	}
	// 443 单独排在最前，再排其余端口 × 地址。
	var rest []string
	for _, port := range ports[1:] {
		for _, a := range addrs {
			rest = append(rest, net.JoinHostPort(a.String(), itoa(port)))
		}
	}
	return append(out, rest...)
}

// 每个网段采样多少地址。/32 与 /128 就是它自己；/24 才有得挑。
const (
	masqueSampleV4 = 2
	masqueSampleV6 = 4
)

func prefixAddrs(pools []netip.Prefix, want int) []netip.Addr {
	var out []netip.Addr
	for _, p := range pools {
		if p.Bits() == p.Addr().BitLen() {
			out = append(out, p.Addr())
			continue
		}
		out = append(out, sampleInPrefix(p, want)...)
	}
	return out
}

// sampleInPrefix 在一个网段里随机取 n 个地址。整个 /24 有 256 个、/48 更是
// 天文数字，全扫既不现实也没意义。
func sampleInPrefix(p netip.Prefix, n int) []netip.Addr {
	base := p.Masked().Addr()
	hostBits := p.Addr().BitLen() - p.Bits()
	if hostBits <= 0 {
		return []netip.Addr{base}
	}
	limit := 1 << min(hostBits, 16)
	out := make([]netip.Addr, 0, n)
	for i := 0; i < n; i++ {
		off := uint64(randInt(limit))
		out = append(out, addOffset(base, off))
	}
	return out
}

func itoa(v int) string { return fmt.Sprintf("%d", v) }

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// dialMasque 是 MASQUE 的拨号入口。流程与 WG 那条路一致 —— 逐个候选握手，
// 第一个「能握手且能过真实数据」的胜出 —— 只是候选来自 MASQUE 自己的实测池，
// 而且两端点之间要重换 CONNECT-IP 会话（桌面版的 handshake 已经处理了这件事）。
func dialMasque(ctx context.Context, o Options) (*Session, error) {
	acct, err := ensureMasqueAccount(ctx, o.report)
	if err != nil {
		return nil, err
	}
	masqueAcct = acct

	mtu := masqueMTU()
	h2 := o.Proto == protoMASQUEH2
	tn, err := newMasqueTunnel(h2, mtu, o.report)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			tn.Close()
		}
	}()

	// 提醒：系统 tun 的源地址是 Kotlin 写死的 172.16.0.2，而 MASQUE 分到的
	// 地址来自这套独立账号。两者不一致时包会被对端丢掉，必须说出来，
	// 而不是让人对着「已连接但打不开网页」猜。
	if tunFD > 0 && acct.IPv4 != "" && !hasPrefix(acct.IPv4, "172.16.0.2") {
		o.report(fmt.Sprintf("警告：MASQUE 分配的地址是 %s，与系统 tun 的 172.16.0.2 不一致，可能无法转发", acct.IPv4))
	}

	cands := masqueCandidateList(o, h2)
	if len(cands) == 0 {
		return nil, fmt.Errorf("MASQUE 没有候选端点")
	}
	transport := "HTTP/3"
	if h2 {
		transport = "HTTP/2"
	}
	o.report(fmt.Sprintf("MASQUE(%s) 候选端点 %d 个，预算 %s", transport, len(cands), o.Budget))

	deadline := time.Now().Add(o.Budget)
	var lastErr error
	// 最后一次**拨号**失败的原因。它和 lastErr（拨号成功但数据面不通）是两回事，
	// 分开留着，最终报错才能说清卡在哪一步。
	var lastDialErr error
	attempts := 0
	for _, endpoint := range cands {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if time.Now().After(deadline) {
			if lastErr != nil {
				return nil, fmt.Errorf("MASQUE 在 %s 内没有可用端点（最后一次：%v）", o.Budget, lastErr)
			}
			if lastDialErr != nil {
				return nil, fmt.Errorf("MASQUE 在 %s 内没有可用端点（试了 %d 次，最后拨号错误：%v）", o.Budget, attempts, lastDialErr)
			}
			return nil, fmt.Errorf("MASQUE 在 %s 内没有可用端点（试了 %d 次）", o.Budget, attempts)
		}
		attempts++
		// 每个失败都要说话：MASQUE 的失败模式（UDP 被拦、凭据没登记、端点不答）
		// 只能靠错误文本区分，一句「42 次都没建起」等于没有信息。
		if dialErr := tn.handshakeErr(ctx, endpoint, o.HandshakeTimeout); dialErr != nil {
			lastDialErr = dialErr
			o.report(fmt.Sprintf("%s 拨号失败：%v", endpoint, dialErr))
			continue
		}
		o.report(fmt.Sprintf("%s 已建立 CONNECT-IP，验证出口…", endpoint))
		start := time.Now()
		body, passed := egressProbe(ctx, tn, o.VerifyTimeout)
		if !passed {
			lastErr = fmt.Errorf("%s 连上但数据面不通", endpoint)
			o.report(fmt.Sprintf("%s 数据面不通，换下一个", endpoint))
			continue
		}
		s := &Session{tn: tn, Endpoint: endpoint, RTT: time.Since(start)}
		s.applyMeta(body)
		if s.ExitIP == "" && s.Country == "" {
			o.report("警告：出口信息解析为空（/meta 被拒），但数据面已验证")
		} else {
			o.report(fmt.Sprintf("出口 %s (%s)", s.ExitIP, s.Country))
		}
		if o.SocksListen != "" && tn.stack() != nil {
			s.startSocks(ctx, o.SocksListen, o.report)
		}
		ok = true
		return s, nil
	}
	if lastErr != nil {
		return nil, fmt.Errorf("MASQUE 没有可用端点（%d 次尝试，最后一个错误：%v）", attempts, lastErr)
	}
	if lastDialErr != nil {
		return nil, fmt.Errorf("MASQUE 没有可用端点：%d 次尝试都没建起 CONNECT-IP（最后拨号错误：%v）", attempts, lastDialErr)
	}
	return nil, fmt.Errorf("MASQUE 没有可用端点：%d 次尝试都没建起 CONNECT-IP", attempts)
}

// hasPrefix 判断地址字符串是否落在给定前缀里（这里用于比对 172.16.0.2）。
func hasPrefix(addr, want string) bool {
	if addr == want {
		return true
	}
	ip, err := netip.ParseAddr(addr)
	if err != nil {
		return false
	}
	return ip.String() == want
}

func randInt(n int) int {
	if n <= 1 {
		return 0
	}
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0
	}
	return int(v.Int64())
}

// addOffset 在地址上加上一个主机位偏移。
func addOffset(base netip.Addr, off uint64) netip.Addr {
	if base.Is4() {
		b := base.As4()
		v := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
		v += uint32(off)
		return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
	}
	b := base.As16()
	var v [16]byte
	copy(v[:], b[:])
	for i := 15; i >= 0 && off > 0; i-- {
		sum := uint64(v[i]) + (off & 0xff)
		v[i] = byte(sum)
		off >>= 8
		if sum <= 0xff {
			break
		}
	}
	return netip.AddrFrom16(v)
}
