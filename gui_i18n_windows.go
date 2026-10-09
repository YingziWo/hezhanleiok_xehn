//go:build windows

package main

import "fmt"

// The UI is authored in Chinese: tr() takes the Chinese string and answers with
// the English one when the language is en. A missing entry falls back to the
// key, so an untranslated string shows up as Chinese rather than as a blank or
// a raw identifier — visible, and never broken.
//
// Format strings use the same mechanism via trf: the format itself is the key,
// so "%d 个可用节点" has its own English twin.
var enStrings = map[string]string{
	// Shell / navigation.
	"首页":  "Home",
	"节点":  "Nodes",
	"规则":  "Rules",
	"设置":  "Settings",
	"日志":  "Logs",
	"关于":  "About",
	"连接":  "Connect",
	"断开":  "Disconnect",
	"连接中…": "Connecting…",
	"断开中…": "Disconnecting…",
	"未连接":  "Disconnected",
	"已连接":  "Connected",
	"快速概览 · 连接状态 · 节点信息":  "Overview · Status · Node",
	"节点列表 · 选择节点 · 延迟测试":  "List · Select · Latency test",
	"全局代理 · 分流规则 · 路由设置":  "Global · Split rules · Routing",
	"常规设置 · 启动设置 · 代理设置 · DNS 设置 · 关于": "General · Startup · Proxy · DNS · About",
	"运行日志 · 连接记录 · 调试信息":            "Runtime log · Sessions · Diagnostics",

	// Theme / language.
	"浅色":    "Light",
	"深色":    "Dark",
	"跟随系统":  "System",
	"语言":    "Language",
	"主题":    "Theme",
	"中文":    "中文",
	"中文（默认）": "Chinese",
	"English": "English",

	// Home.
	"当前出口":        "Current exit",
	"上传":          "Upload",
	"下载":          "Download",
	"连接时长":        "Duration",
	"点击连接":        "Click to connect",
	"协议":          "Protocol",
	"可用":          "Ready",
	"不可用":         "Unavailable",
	"未探测":         "Unknown",
	"节点信息":        "Node details",
	"节点名称":        "Node",
	"延迟":          "Latency",
	"出口":          "Exit IP",
	"国家/地区":       "Country",
	"先连接后查看节点信息":  "Connect to see node details",
	"未选择节点":       "No node selected",
	"Psiphon 出口":   "Psiphon exit",
	"分流":          "Split",
	"直连":          "Direct",
	"代理":          "Proxy",
	"全局":          "Global",
	"规则模式":        "Rule",
	"关闭":          "Off",
	"自动":          "Auto",

	// Nodes.
	"搜索节点名称、国家/地区": "Search node, country",
	"全部":            "All",
	"国家":            "Country",
	"延迟排序":          "By latency",
	"名称排序":          "By name",
	"刷新":            "Refresh",
	"收藏":            "Star",
	"取消收藏":          "Unstar",
	"使用中":           "In use",
	"选中":            "Select",
	"无匹配节点":         "No matching node",
	"缓存中暂无节点":       "No nodes cached yet",
	"点击「刷新」重新扫描可用节点": "Press refresh to scan for nodes",
	"扫描中…":          "Scanning…",
	"共 %d 个节点":      "%d nodes",
	"%d 个已收藏":       "%d starred",

	// Rules.
	"开启后，所有流量将通过代理转发": "When on, all traffic goes through the proxy",
	"代理模式":              "Proxy mode",
	"默认节点":              "Default node",
	"分流规则":              "Split rules",
	"管理规则":              "Manage",
	"分应用":               "Per-app",
	"自定义域名":             "Custom domains",
	"直连域名":              "Direct domains",
	"代理域名":              "Proxy domains",
	"地区分流":              "Region split",
	"国内网站":              "Domestic sites",
	"国外网站":              "Foreign sites",
	"其它流量":              "Everything else",
	"需要 geosite.dat / geoip.metadb（放入内核目录后可用）": "Needs geosite.dat / geoip.metadb (drop them into the core folder)",
	"路由设置":    "Routing",
	"TUN 全局模式": "TUN mode",
	"设置系统代理":   "System proxy",
	"混合端口":     "Mixed port",
	"API 端口":    "API port",
	"进程列表为空，请在下方填写进程名": "No apps listed — add process names below",
	"规则顺序：代理域名 → 直连域名 → 分应用 → 地区分流 → 其它流量": "Rule order: proxy domains → direct domains → per-app → region → everything else",

	// Settings.
	"常规":     "General",
	"启动":     "Startup",
	"网络":     "Network",
	"记住上次页面": "Remember last page",
	"开机自动启动": "Launch at login",
	"启动后自动连接": "Connect on launch",
	"连接前重新扫描": "Rescan before connecting",
	"自动修复":    "Auto repair",
	"自动修复（健康守护）": "Auto repair (health guard)",
	"会话断开后由后台守护自动重建": "A background guard rebuilds a broken session",
	"连接超时（秒）":        "Connect timeout (s)",
	"自定义 DNS":      "Custom DNS",
	"常用":           "Presets",
	"不下发 DNS":       "No DNS from tunnel",
	"分应用分流":         "Per-app split",
	"模式":            "Mode",
	"应用进程":          "Processes",
	"仅代理所选":         "Only listed",
	"所选直连":          "Listed bypass",
	"地址族":           "Address family",
	"恢复默认设置":        "Reset to defaults",
	"恢复默认设置？":       "Reset to defaults?",
	"所有设置将回到初始值，需要重新连接才生效。": "Every setting returns to its initial value; reconnect to apply.",
	"设置已保存":          "Saved",
	"有改动：重新连接后生效":    "Changed: reconnect to apply",
	"重新连接以应用":        "Reconnect to apply",
	"配置已保存，下次连接时生效":  "Saved; applied on the next connect",

	// About / update.
	"本程序":        "This application",
	"名称":         "Name",
	"版本":         "Version",
	"构建":         "Build",
	"数据目录":       "Data folder",
	"核心文件":       "Core file",
	"核心版本":       "Core version",
	"核心大小":       "Core size",
	"开源与许可":      "Open source & licences",
	"内核":         "Core",
	"内核许可":       "Core licence",
	"声明文件":       "Notices",
	"许可全文":       "Licence text",
	"更新源":        "Release source",
	"使用说明":       "How to use",
	"免责声明":       "Disclaimer",
	"检查更新":       "Check for updates",
	"打开发布页":      "Open releases",
	"内核 (mihomo)": "Core (mihomo)",
	"状态":         "Status",
	"路径":         "Path",
	"检查/下载内核":    "Check / download core",
	"重新探测内核":     "Re-probe core",
	"打开数据目录":     "Open data folder",
	"复制诊断信息":     "Copy diagnostics",
	"打开第三方声明":    "Open third-party notices",
	"当前版本":       "Installed",
	"最新版本":       "Latest",
	"上次检查":       "Checked",
	"尚未检查":       "Not checked",
	"本次运行尚未检查":   "Not checked yet",
	"点击下方按钮检查更新": "Press the button below to check",
	"正在检查…":      "Checking…",
	"检查失败：发布页不可达（网络受限或 GitHub 被拦截）": "Check failed: releases unreachable (network restricted or GitHub blocked)",
	"发现新版本，可到发布页下载":                  "A newer version is available on the releases page",
	"已是最新版本":                         "Up to date",
	"已就绪":                            "Ready",
	"未安装":                            "Not installed",
	"未知":                             "Unknown",
	"开发版":                            "dev build",
	"更新说明":                           "About updates",
	"客户端更新：本项目通过 GitHub Releases 分发。检查按钮会直接访问发布页接口，结果不做缓存，失败时会明确告知是网络不可达，而不是假装「已是最新」。": "Client updates ship through GitHub Releases. The check hits the releases API directly and is never answered from cache; a failure says the network is unreachable instead of pretending to be up to date.",
	"内核更新：mihomo 是独立进程，可以重新下载覆盖 core 目录下的 mihomo.exe，也可以手动替换后点「重新探测内核」。": "The core is a separate process: download it again to replace core\\mihomo.exe, or swap the file and press re-probe.",
	"便携模式：程序目录可以整体复制走，配置与缓存就在 exe 同级目录。": "Portable: copy the whole program folder; settings and cache live next to the exe.",

	// Logs.
	"运行日志":   "Runtime",
	"连接记录":   "Sessions",
	"调试信息":   "Diagnostics",
	"清空日志":   "Clear",
	"复制":     "Copy",
	"自动滚动":   "Auto scroll",
	"暂无日志":   "No log yet",
	"暂无连接记录": "No sessions yet",
	"连接事件会显示在这里": "Connect and disconnect events show up here",
	"已复制到剪贴板":     "Copied to clipboard",
	"复制失败":        "Copy failed",

	// Dialogs / hints.
	"确定":                  "OK",
	"取消":                  "Cancel",
	"搜索节点":                "Search nodes",
	"进程名，如 chrome.exe, steam.exe": "Process names, e.g. chrome.exe, steam.exe",
	"域名后缀，如 example.com":     "Domain suffixes, e.g. example.com",
	"逗号或空格分隔":             "Separate with commas or spaces",
	"重置后需要重新连接":           "Reconnect after resetting",
	"选择节点后点击「连接」生效":       "Picked nodes apply on the next connect",
	"打开输入框失败":             "Could not open the input dialog",
	"输入框布局失败":             "Could not lay out the input dialog",
	"输入框创建失败":             "Could not create the input dialog",
	"打开对话框失败":             "Could not open the dialog",
	"当前 Core 不支持 %s 协议，请升级 Core（Mihomo 官方最新版）": "This core cannot parse %s; update mihomo",
	"诊断信息已复制到剪贴板":                          "Diagnostics copied to clipboard",
	"设置：启动后自动连接":                            "Setting: connect on launch",
	"日志已复制到剪贴板":                            "Log copied to clipboard",
	"复制失败: ":                                "Copy failed: ",
	"设置保存失败: ":                              "Could not save settings: ",

	// Home and shell details.
	"点击圆钮连接或断开":     "Click the circle to connect or disconnect",
	"会话：":          "Session: ",
	"实时流量":         "Live traffic",
	"连接后开始采集":      "Sampling starts after connecting",
	"先连基础协议，再叠加 Psiphon 出口": "Connect the base protocol first, then add the Psiphon exit",

	// Rules page details.
	"全局代理":     "Global proxy",
	"国内直连 / 国外代理": "Domestic direct / foreign proxied",
	"外层":       "Outer",
	"内层":       "Inner",
	"节点列表":     "Node list",
	"点击一行设为默认节点": "Click a row to make it the default node",
	"自动（延迟最低）":   "Automatic (lowest latency)",
	"其它流量按「全局代理」页的代理模式决定（默认全走代理）": "Everything else follows the proxy mode on the Global proxy tab (default: all through the proxy)",
	"需要管理员权限；开启后接管全部流量":              "Needs administrator rights; takes over all traffic when on",
	"连接时写入系统代理，断开时还原":                "Set the system proxy on connect, restore it on disconnect",
	"端口与 MTU 在下次连接时读取":                "Ports and MTU are read on the next connect",

	// Settings page details.
	"客户端更新":                "Client updates",
	"协议引擎":                 "Protocol engines",
	"写入当前用户的启动项":           "Adds a startup entry for the current user",
	"写入启动项失败":              "Could not write the startup entry",
	"程序启动即连接上次的协议":         "Connect with the last protocol as soon as it starts",
	"连接时重新扫描端点，耗时更长但更新":    "Rescan endpoints on connect: slower, but fresher",
	"连接超时用于扫描与握手，过小会漏掉慢端点": "The timeout covers scanning and handshakes; too small misses slow endpoints",
	"端口、地址族与 MTU 在下次连接时读取":  "Ports, address family and MTU are read on the next connect",
	"关闭后每次都从首页开始":          "When off, every launch starts on Home",
	"不向隧道提供解析器，全部用系统 DNS":   "Hand the tunnel no resolver; use the system DNS",
	"留空表示不指定，由内核自己决定解析方式。": "Leave empty to let the core decide how to resolve.",
	"解析器会写进 mihomo 配置的 dns.nameserver，并下发到 tunnel。": "The resolvers go into mihomo's dns.nameserver and are handed to the tunnel.",
	"自定义解析器必须能解析域名，否则连接验证会失败。": "A custom resolver must resolve names, or the connect verification fails.",
	"本程序只负责在你自己的设备上配置一条加密隧道，不提供、不代理任何服务器资源。": "This program only configures an encrypted tunnel on your own device; it provides and proxies no server resources.",
	"隧道出口由 Cloudflare WARP 提供；内核与协议引擎均为独立的 GPL-3.0 作品，以单独的可执行文件分发。": "The exit is Cloudflare WARP; the core and protocol engines are separate GPL-3.0 works, shipped as standalone executables.",
	"程序按「现状」提供，不对可用性、速度或适用性作任何承诺。": "Provided as is, with no promise of availability, speed or fitness.",

	// Dialog hints and field placeholders.
	"名称、国家、城市或 IP":            "Name, country, city or IP",
	"逗号分隔，如 223.5.5.5, 1.1.1.1": "Comma separated, e.g. 223.5.5.5, 1.1.1.1",
	"端点，如 188.114.97.198:4500（留空 = 自动）": "Endpoint, e.g. 188.114.97.198:4500 (empty = automatic)",
	"1-65535":        "1-65535",
	"1-60 秒":         "1-60 s",
	"0 = 自动":         "0 = auto",
	"MTU":            "MTU",
	"DNS":            "DNS",
	"个":             "",
	"已选":            "Pinned",
	"重新探测内核…":       "Re-probing the core…",
}

// trfKeys holds the parameterised strings. fmt verbs are part of the key, so
// the translation can also reorder them (English may need the number first).
var enFormats = map[string]string{
	"%s 连接中…":         "Connecting %s…",
	"%s 已连接":          "%s connected",
	"%d 个节点":          "%d nodes",
	"共 %d 个节点":        "%d nodes",
	"%d 个已收藏":         "%d starred",
	"%d 个可用节点":        "%d nodes available",
	"%d 个协议可用":        "%d protocols ready",
	"%s 出口":           "%s exit",
	"%s 可用":           "%s ready",
	"扫描于 %s":          "scanned %s",
	"延迟 %s":           "latency %s",
	"已连接 %s":          "connected %s",
	"未知 · core v%s":   "unknown · core v%s",
	"确定":              "OK",
}

// tr translates a UI string. Chinese is the source language, so zh needs no
// table at all.
func tr(s string) string {
	if uiLang == langEN {
		if v, ok := enStrings[s]; ok {
			return v
		}
	}
	return s
}

// trf translates and formats. The format string is the lookup key.
func trf(format string, args ...any) string {
	if uiLang == langEN {
		if v, ok := enFormats[format]; ok {
			format = v
		}
	}
	return fmt.Sprintf(format, args...)
}

// uiLang is the active UI language. applyLang is its only writer, so every
// paint site can read it without plumbing it through.
var uiLang = langZH

func (u *natUI) applyLang() {
	uiLang = u.m.set.Lang
	if uiLang != langEN {
		uiLang = langZH
	}
	u.invalidate()
}
