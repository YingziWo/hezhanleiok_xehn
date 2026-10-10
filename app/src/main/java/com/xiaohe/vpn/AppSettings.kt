package com.xiaohe.vpn

import android.content.Context

/**
 * 用户可配置项。字段与默认值对齐电脑版 `settings.go`，去掉了桌面专有的部分
 * （开机自启写注册表、系统代理、mihomo 的混合端口/API 端口、外部内核路径）。
 *
 * Android 特有的两处差异：
 *  - 「分应用」在电脑版是给 mihomo 生成规则，这里直接落到 VpnService 的
 *    addAllowedApplication / addDisallowedApplication —— 系统原生能力，更可靠。
 *  - 主题默认浅色（用户明确要求），电脑版默认也是 light。
 */
data class AppSettings(
    val proto: String = "awg",
    val psiphon: Boolean = false,
    val ipv6: Boolean = false,
    val mtu: Int = 1280,
    val dns: String = "1.1.1.1, 223.5.5.5",
    val dnsOff: Boolean = false,
    val autoConnect: Boolean = false,
    val rescan: Boolean = false,
    val timeoutSec: Int = 3,
    /** off / proxy / direct */
    val splitMode: String = "off",
    val splitApps: String = "",
    val theme: String = "light",
    val lang: String = "zh",
    val starred: String = "",
    val logAutoScroll: Boolean = true,
) {
    fun dnsList(): List<String> =
        dns.split(',', ';', ' ', '\n').map { it.trim() }.filter { it.isNotEmpty() }

    fun splitAppList(): List<String> =
        splitApps.split(',', ';', ' ', '\n').map { it.trim() }.filter { it.isNotEmpty() }

    fun starSet(): Set<String> =
        starred.split(',').map { it.trim() }.filter { it.isNotEmpty() }.toSet()

    /** 协议下拉里显示的名字。 */
    fun protoLabel(): String = when (proto) {
        "wg" -> "WireGuard"
        "awg" -> "AmneziaWG"
        else -> proto.uppercase()
    }
}

/** SharedPreferences 持久化。读写都很小，不需要额外的仓库层。 */
object SettingsStore {
    private const val FILE = "xiaohe-settings"

    fun load(ctx: Context): AppSettings {
        val p = ctx.getSharedPreferences(FILE, Context.MODE_PRIVATE)
        val d = AppSettings()
        return AppSettings(
            proto = p.getString("proto", d.proto) ?: d.proto,
            psiphon = p.getBoolean("psiphon", d.psiphon),
            ipv6 = p.getBoolean("ipv6", d.ipv6),
            mtu = p.getInt("mtu", d.mtu),
            dns = p.getString("dns", d.dns) ?: d.dns,
            dnsOff = p.getBoolean("dnsOff", d.dnsOff),
            autoConnect = p.getBoolean("autoConnect", d.autoConnect),
            rescan = p.getBoolean("rescan", d.rescan),
            timeoutSec = p.getInt("timeoutSec", d.timeoutSec),
            splitMode = p.getString("splitMode", d.splitMode) ?: d.splitMode,
            splitApps = p.getString("splitApps", d.splitApps) ?: d.splitApps,
            theme = p.getString("theme", d.theme) ?: d.theme,
            lang = p.getString("lang", d.lang) ?: d.lang,
            starred = p.getString("starred", d.starred) ?: d.starred,
            logAutoScroll = p.getBoolean("logAutoScroll", d.logAutoScroll),
        )
    }

    fun save(ctx: Context, s: AppSettings) {
        ctx.getSharedPreferences(FILE, Context.MODE_PRIVATE).edit().apply {
            putString("proto", s.proto)
            putBoolean("psiphon", s.psiphon)
            putBoolean("ipv6", s.ipv6)
            putInt("mtu", s.mtu)
            putString("dns", s.dns)
            putBoolean("dnsOff", s.dnsOff)
            putBoolean("autoConnect", s.autoConnect)
            putBoolean("rescan", s.rescan)
            putInt("timeoutSec", s.timeoutSec)
            putString("splitMode", s.splitMode)
            putString("splitApps", s.splitApps)
            putString("theme", s.theme)
            putString("lang", s.lang)
            putString("starred", s.starred)
            putBoolean("logAutoScroll", s.logAutoScroll)
        }.apply()
    }
}
