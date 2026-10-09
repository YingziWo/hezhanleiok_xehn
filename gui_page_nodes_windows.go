//go:build windows

package main

import (
	"sort"
	"strings"

	"github.com/lxn/walk"
)

// Node list: search, filter, sort, and the cached endpoints themselves. The
// list is the scan cache, so a row is a real measured endpoint, not a catalogue
// entry — every number shown here was observed on this machine.
func (u *natUI) paintNodes(c *walk.Canvas) {
	x, y := shellContentX, shellBodyY
	w := 1080 - shellPadX - x

	// Toolbar.
	u.inputBox(c, x, y, 288, 38, "nodesearch", u.m.nodeSearch, tr("搜索节点名称、国家/地区"))
	u.icon(c, "search", x+258, y+11, 16, th.faint)
	if u.m.nodeSearch != "" {
		u.icon(c, "close", x+278, y+12, 14, th.dim)
		u.addSpot("nodesearch:clear", u.r(x+270, y+6, 26, 26), true)
	}
	cx := x + 288 + 14
	for _, f := range nodeFilters {
		cw := chipWidth(tr(f.label))
		u.chipAt(c, "nodefilter:"+f.id, tr(f.label), cx, y+6, u.m.nodeFilter == f.id, true)
		cx += cw + 6
	}
	cx += 8
	for _, s := range nodeSorts {
		cw := chipWidth(tr(s.label))
		u.chipAt(c, "nodesort:"+s.id, tr(s.label), cx, y+6, u.m.nodeSort == s.id, true)
		cx += cw + 6
	}
	u.iconButton(c, x+w-40, y+1, 36, "rescannow", "refresh", !u.m.working && !u.m.connecting)

	// List.
	listY := y + 52
	listH := 700 - listY - 14
	u.cardPlain(c, x, listY, w, listH)

	rows := u.visibleNodeRows()
	if len(rows) == 0 {
		msg := tr("无匹配节点")
		if len(u.m.endpoints) == 0 {
			if u.m.working && !u.m.connected {
				msg = tr("扫描中…")
			} else {
				msg = tr("缓存中暂无节点")
			}
		}
		u.textC(c, msg, u.f.f10, th.faint, u.r(x+16, listY+listH/2-20, w-32, 22))
		if len(u.m.endpoints) == 0 {
			u.textC(c, tr("点击「刷新」重新扫描可用节点"), u.f.f8, th.faint,
				u.r(x+16, listY+listH/2+4, w-32, 20))
		}
		return
	}

	// Header line: how many, how fresh, and how many favourites.
	u.txt(c, x+16, listY+10, 300, 18, trf("共 %d 个节点", len(rows)), u.f.f8, th.dim)
	if !u.m.scanAt.IsZero() {
		u.txt(c, x+130, listY+10, 240, 18, trf("扫描于 %s", u.m.scanAt.Format("15:04:05")), u.f.f8, th.faint)
	}
	if n := len(u.m.set.Starred); n > 0 {
		u.textR(c, trf("%d 个已收藏", n), u.f.f8, th.faint, u.r(x+w-56, listY+10, 40, 18))
	}
	u.hline(c, x+16, listY+32, w-32, th.line)

	// Row height is fixed; the wheel scrolls by whole rows.
	const rowH = 54.0
	gridY := listY + 38
	maxRows := int((listH - 44) / rowH)
	if u.m.scroll > len(rows)-maxRows {
		u.m.scroll = len(rows) - maxRows
	}
	if u.m.scroll < 0 {
		u.m.scroll = 0
	}
	end := u.m.scroll + maxRows
	if end > len(rows) {
		end = len(rows)
	}
	u.m.nodeVisibleRows = maxRows
	for i, ce := range rows[u.m.scroll:end] {
		u.nodeRow(c, x, gridY+float64(i)*rowH, w, rowH, ce)
	}
	if len(rows) > maxRows {
		u.textR(c, itoa(int64(u.m.scroll+1))+"-"+itoa(int64(end))+"/"+itoa(int64(len(rows))),
			u.f.f8, th.faint, u.r(x+w-140, listY+listH-24, 124, 18))
	}
}

type nodeFilterDef struct{ id, label string }

var nodeFilters = []nodeFilterDef{
	{"all", "全部"},
	{"working", "可用"},
	{"starred", "收藏"},
}

var nodeSorts = []nodeFilterDef{
	{"latency", "延迟排序"},
	{"name", "名称排序"},
}

// nodeRow is one endpoint: identity on the left, quality on the right.
func (u *natUI) nodeRow(c *walk.Canvas, x, y, w, h float64, ce cachedEndpoint) {
	pinned := u.m.pin == ce.Endpoint
	active := pinned || (u.m.pin == "" && u.m.connected && ce.Endpoint == u.m.status.Endpoint)
	row := u.r(x+8, y+2, w-16, h-4)
	if active {
		u.fillRound(c, th.accentSoft, row, 9)
	} else if u.m.hover == "row:"+ce.Endpoint {
		u.fillRound(c, th.cardHover, row, 9)
	}

	flag := ce.NodeISO
	if flag == "" {
		flag = ce.Country
	}
	u.flagChip(c, x+20, y+16, 28, 20, flag)

	name := ce.Endpoint
	if ce.NodeCity != "" {
		name = ce.NodeCity + " · " + ce.Endpoint
	}
	u.txt(c, x+58, y+10, w-330, 20, name, u.f.f10b, th.text)
	sub := strings.TrimSpace(strings.Join([]string{
		orDash(strings.ToUpper(ce.Country)), ce.Colo, protoLabel(u.m.proto),
	}, " · "))
	u.txt(c, x+58, y+30, w-330, 18, sub, u.f.f8, th.dim)

	// Right side: the favourite marker, then latency and quality bars. The star
	// is a text glyph rather than a drawn shape: at 14px a hand-drawn star
	// reads as a plus sign, and both fonts in use carry U+2605/U+2606.
	starX := x + w - 58
	star, starCol := "☆", th.faint
	if u.m.set.starred(ce.Endpoint) {
		star, starCol = "★", th.warn
	}
	u.textC(c, star, u.f.f10, starCol, u.r(starX, y+8, 28, 28))
	u.addSpot("star:"+ce.Endpoint, u.r(starX, y+8, 28, 28), true)

	ms := ce.TunPingMS
	if ms == 0 {
		ms = ce.EpPingMS
	}
	u.textR(c, latencyText(ms), u.f.f10b, th.text, u.r(x+w-190, y+16, 76, 22))
	u.bars(c, x+w-104, y+18, ms, th.accent)

	if active {
		u.textR(c, tr("使用中"), u.f.f8, th.accent, u.r(x+w-190, y+36, 120, 16))
	} else if pinned {
		u.textR(c, tr("已选"), u.f.f8, th.dim, u.r(x+w-190, y+36, 120, 16))
	}
	u.addSpot("row:"+ce.Endpoint, u.r(x+8, y+2, w-90, h-4), true)
}

// visibleNodeRows applies the search box, the filter and the sort to the scan
// cache. Starred endpoints come first for their sort key, because a favourite
// that still needs scrolling for is not a favourite.
func (u *natUI) visibleNodeRows() []cachedEndpoint {
	out := make([]cachedEndpoint, 0, len(u.m.endpoints))
	needle := strings.ToLower(strings.TrimSpace(u.m.nodeSearch))
	for _, ce := range u.m.endpoints {
		switch u.m.nodeFilter {
		case "working":
			if !ce.Working {
				continue
			}
		case "starred":
			if !u.m.set.starred(ce.Endpoint) {
				continue
			}
		}
		if needle != "" {
			hay := strings.ToLower(strings.Join([]string{
				ce.Endpoint, ce.IP, ce.Country, ce.Colo, ce.NodeCity, ce.NodeISO,
			}, " "))
			if !strings.Contains(hay, needle) {
				continue
			}
		}
		out = append(out, ce)
	}
	less := func(i, j int) bool {
		a, b := out[i], out[j]
		sa, sb := u.m.set.starred(a.Endpoint), u.m.set.starred(b.Endpoint)
		if sa != sb {
			return sa
		}
		if u.m.nodeSort == "name" {
			return a.Endpoint < b.Endpoint
		}
		la, lb := a.TunPingMS, b.TunPingMS
		if la == 0 {
			la = a.EpPingMS
		}
		if lb == 0 {
			lb = b.EpPingMS
		}
		if la == 0 {
			la = 1 << 30
		}
		if lb == 0 {
			lb = 1 << 30
		}
		return la < lb
	}
	sort.SliceStable(out, less)
	return out
}
