//go:build windows

package main

import (
	"github.com/lxn/walk"

	"github.com/xhwarp/warp-client/internal/core"
)

var ruleTabIDs = []string{"global", "split", "route"}
var ruleTabLabels = []string{"全局代理", "分流规则", "路由设置"}

func (u *natUI) paintRules(c *walk.Canvas) {
	x, y, w := shellContentX, shellBodyY, 1080-shellPadX-shellContentX
	u.subtabs(c, x, y, "ruletab:", ruleTabIDs, ruleTabLabels, u.m.ruleTab)
	top := y + 46
	switch u.m.ruleTab {
	case "split":
		u.paintRulesSplit(c, x, top, w)
	case "route":
		u.paintRulesRoute(c, x, top, w)
	default:
		u.paintRulesGlobal(c, x, top, w)
	}
	if u.m.setDirty {
		u.textC(c, tr("有改动：重新连接后生效"), u.f.f9, th.warn, u.r(x, 668, w, 20))
	}
}

// ---------------------------------------------------------------------------
// Global proxy
// ---------------------------------------------------------------------------

func (u *natUI) paintRulesGlobal(c *walk.Canvas, x, y, w float64) {
	cy := u.card(c, x, y, w, 232, tr("全局代理"))
	u.txt(c, x+16, cy, w-32, 18, tr("开启后，所有流量将通过代理转发"), u.f.f8, th.dim)
	on := u.m.set.ProxyMode == modeGlobal
	u.switchAt(c, "mode:global", x+w-16-44, cy+2, on, true)
	u.addSpot("mode:global", u.r(x+w-72, cy-4, 72, 30), true)

	u.hline(c, x+16, y+88, w-32, th.line)
	u.rowChips(c, x, y+100, w, "mode:", tr("代理模式"),
		[]string{modeGlobal, modeRule, modeDirect},
		[]string{"全局", "规则", "直连"}, u.m.set.ProxyMode)
	u.hline(c, x+16, y+142, w-32, th.line)
	u.rowEdit(c, x, y+152, w, "set:picknode", tr("默认节点"),
		orEmpty(u.m.pin, u.m.status.Endpoint), tr("自动（延迟最低）"))
	u.txt(c, x+16, y+192, w-32, 18, tr("选择节点后点击「连接」生效"), u.f.f8, th.faint)

	// A compact picker: the fastest cached endpoints, one click to pin. Five
	// rows is what the remaining height holds with the hint line under them.
	listY := y + 250
	listH := 700 - 14 - listY
	u.cardPlain(c, x, listY, w, listH)
	u.txt(c, x+16, listY+12, w-260, 20, tr("节点列表"), u.f.f11b, th.text)
	u.txt(c, x+w-240, listY+14, 224, 18, tr("点击一行设为默认节点"), u.f.f8, th.faint)
	rows := u.visibleNodeRows()
	if len(rows) == 0 {
		u.textC(c, tr("缓存中暂无节点"), u.f.f9, th.faint, u.r(x+16, listY+60, w-32, 20))
		return
	}
	if maxRows := int((listH - 50) / 44); len(rows) > maxRows {
		rows = rows[:maxRows]
	}
	for i, ce := range rows {
		ry := listY + 44 + float64(i)*44
		r := u.r(x+8, ry, w-16, 40)
		if u.m.pin == ce.Endpoint {
			u.fillRound(c, th.accentSoft, r, 8)
		} else if u.m.hover == "row:"+ce.Endpoint {
			u.fillRound(c, th.cardHover, r, 8)
		}
		flag := ce.NodeISO
		if flag == "" {
			flag = ce.Country
		}
		u.flagChip(c, x+18, ry+11, 24, 18, flag)
		u.txt(c, x+52, ry+10, w-220, 20, ce.Endpoint, u.f.f10, th.text)
		ms := ce.TunPingMS
		if ms == 0 {
			ms = ce.EpPingMS
		}
		u.textR(c, latencyText(ms), u.f.f10, th.dim, u.r(x+w-110, ry+10, 90, 20))
		u.addSpot("row:"+ce.Endpoint, r, true)
	}
}

// ---------------------------------------------------------------------------
// Split rules
// ---------------------------------------------------------------------------

func (u *natUI) paintRulesSplit(c *walk.Canvas, x, y, w float64) {
	cy := u.card(c, x, y, w, 452, tr("分流规则"))

	// Per-app.
	u.txt(c, x+16, cy, w-32, 18, tr("分应用"), u.f.f10b, th.text)
	u.rowChips(c, x, cy+24, w, "set:split:",
		tr("模式"), []string{splitOff, splitProxy, splitDirect},
		[]string{"关闭", "仅代理所选", "所选直连"}, u.m.set.SplitMode)
	u.rowEdit(c, x, cy+58, w, "set:splitapps", tr("应用进程"),
		u.m.set.SplitApps, tr("进程名，如 chrome.exe, steam.exe"))
	u.hline(c, x+16, cy+104, w-32, th.line)

	// Custom domains: the two lists the engine can honour without geo data.
	u.txt(c, x+16, cy+116, w-32, 18, tr("自定义域名"), u.f.f10b, th.text)
	u.rowEdit(c, x, cy+140, w, "set:domdirect", tr("直连域名"),
		u.m.set.DomainDirect, tr("域名后缀，如 example.com"))
	u.rowEdit(c, x, cy+178, w, "set:domproxy", tr("代理域名"),
		u.m.set.DomainProxy, tr("域名后缀，如 example.com"))
	u.hline(c, x+16, cy+224, w-32, th.line)

	// Region split, which needs the core's geo data.
	u.txt(c, x+16, cy+236, w-32, 18, tr("地区分流"), u.f.f10b, th.text)
	geo := u.geoAvailable()
	u.rowToggle(c, x, cy+258, w, "set:geo", tr("国内直连 / 国外代理"),
		tr("需要 geosite.dat / geoip.metadb（放入内核目录后可用）"), u.m.set.GeoSplit && geo, geo)
	if geo && u.m.set.GeoSplit {
		u.chipAt(c, "", tr("国内网站"), x+16, cy+300, false, false)
		u.textL(c, tr("直连"), u.f.f9, th.dim, u.r(x+16+92, cy+300, 60, 26))
		u.chipAt(c, "", tr("国外网站"), x+180, cy+300, false, false)
		u.textL(c, tr("代理"), u.f.f9, th.dim, u.r(x+180+92, cy+300, 60, 26))
	}
	u.txt(c, x+16, cy+340, w-32, 18,
		tr("规则顺序：代理域名 → 直连域名 → 分应用 → 地区分流 → 其它流量"), u.f.f8, th.faint)
	u.txt(c, x+16, cy+362, w-32, 18,
		tr("其它流量按「全局代理」页的代理模式决定（默认全走代理）"), u.f.f8, th.faint)
}

// ---------------------------------------------------------------------------
// Routing
// ---------------------------------------------------------------------------

func (u *natUI) paintRulesRoute(c *walk.Canvas, x, y, w float64) {
	cy := u.card(c, x, y, w, 400, tr("路由设置"))
	s := u.m.set
	u.rowToggle(c, x, cy, w, "tun", tr("TUN 全局模式"),
		tr("需要管理员权限；开启后接管全部流量"), s.Tun, !u.m.connected)
	u.rowToggle(c, x, cy+40, w, "set:sysproxy", tr("设置系统代理"),
		tr("连接时写入系统代理，断开时还原"), s.SystemProxy, true)
	u.rowEdit(c, x, cy+84, w, "set:mixedport", tr("混合端口"),
		itoa(int64(s.MixedPort)), itoa(int64(core.DefaultMixedPort)))
	u.rowEdit(c, x, cy+122, w, "set:apiport", tr("API 端口"),
		itoa(int64(s.APIPort)), itoa(int64(core.DefaultAPIPort)))
	u.rowChips(c, x, cy+160, w, "set:ipfam:", tr("地址族"), []string{"v4", "v6"},
		[]string{"IPv4", "IPv6"}, currentFamily(s.IPv6))
	u.rowEdit(c, x, cy+196, w, "set:mtu", tr("MTU"),
		itoa(int64(s.MTU)), tr("0 = 自动"))
	u.rowEdit(c, x, cy+234, w, "set:timeout", tr("连接超时（秒）"),
		itoa(int64(s.TimeoutSec)), "2")
	u.hline(c, x+16, cy+280, w-32, th.line)
	u.txt(c, x+16, cy+292, w-32, 18, tr("端口与 MTU 在下次连接时读取"), u.f.f8, th.faint)
	u.button(c, x+16, cy+316, 150, 34, "set:connect", tr("重新连接以应用"), true, u.m.connected)
}

// geoAvailable reports whether the core has the geo data the region split
// needs. Without geosite.dat the core tries to download it and then refuses the
// whole config, so the switch stays off rather than producing a config that
// cannot start.
func (u *natUI) geoAvailable() bool { return geoDataReady() }
