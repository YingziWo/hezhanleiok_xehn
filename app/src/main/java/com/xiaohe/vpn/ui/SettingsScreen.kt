package com.xiaohe.vpn.ui

import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ArrowBack
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.xiaohe.vpn.AppSettings
import com.xiaohe.vpn.PROTOCOLS
import com.xiaohe.vpn.ProtoOption
import com.xiaohe.vpn.R
import com.xiaohe.vpn.VpnState

/**
 * 设置页。
 *
 * 收录了电脑版 `settings.go` 里在手机上有意义的全部条目；桌面专有的（开机自启、
 * 系统代理、mihomo 的混合/API 端口、外部内核路径、绑定网卡）不出现，
 * 因为它们在 Android 上不存在对应物。
 *
 * 还没搬到安卓的条目**照实显示并说明原因**，不做成能打开却什么也不做的假开关：
 * 一个骗人的开关比一个明确的「暂未实现」更浪费时间。
 */
@Composable
fun SettingsScreen(
    state: VpnState,
    settings: AppSettings,
    onChange: (AppSettings) -> Unit,
    onClearLogs: () -> Unit,
    onPickApps: () -> Unit,
    onBack: () -> Unit,
) {
    val ctx = LocalContext.current

    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(MaterialTheme.colorScheme.background)
            // 与首页同样处理：insets 在滚动之前，返回箭头不会被系统状态栏压住。
            .statusBarsPadding()
            .navigationBarsPadding()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 16.dp),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(top = 6.dp, bottom = 6.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            IconButton(onClick = onBack, modifier = Modifier.size(42.dp)) {
                Icon(Icons.Filled.ArrowBack, contentDescription = "返回", tint = MaterialTheme.colorScheme.primary)
            }
            Text(
                "设置",
                style = MaterialTheme.typography.titleLarge,
                fontWeight = FontWeight.SemiBold,
                modifier = Modifier.weight(1f),
            )
        }

        // 品牌块：logo + 软件名 + 版本。图标资源就是从根目录的 logo.png 生成的。
        BrandHeader()

        // ---------------- 连接 ----------------
        Section("连接")
        SettingDropdown(
            title = "默认协议",
            subtitle = "连接时使用的传输方式；连接中不可切换",
            current = settings.proto,
            options = PROTOCOLS,
            onChange = { onChange(settings.copy(proto = it)) },
        )
        SettingSwitch(
            title = "启动后自动连接",
            subtitle = "打开 App 时自动建立隧道",
            checked = settings.autoConnect,
            onChange = { onChange(settings.copy(autoConnect = it)) },
        )
        SettingNumber(
            title = "连接超时（秒）",
            subtitle = "单个端点握手等待时间，1-15 秒；越小切端点越快",
            value = settings.timeoutSec.toString(),
            onChange = { v ->
                v.toIntOrNull()?.let { onChange(settings.copy(timeoutSec = it.coerceIn(1, 15))) }
            },
        )
        SettingSwitch(
            title = "连接前重新扫描端点",
            subtitle = "安卓版每次连接都会重新从端点池抽样，等效于常开",
            checked = true,
            enabled = false,
            onChange = {},
        )

        // ---------------- 出口 ----------------
        Section("出口")
        SettingSwitch(
            title = "Psiphon 出口",
            subtitle = "最终出口走 Psiphon（进程内跑官方 tunnel-core）；" +
                "基础协议（建议 AWG）只作它的上游载体，出口 IP/国家按 Psiphon 实测显示",
            checked = settings.psiphon,
            enabled = true,
            onChange = { onChange(settings.copy(psiphon = it)) },
        )
        SettingChips(
            title = "分应用",
            subtitle = "用系统原生白/黑名单实现，内核层生效",
            options = listOf("off" to "关闭", "proxy" to "仅代理所选", "direct" to "所选直连"),
            selected = settings.splitMode,
            onSelect = { onChange(settings.copy(splitMode = it)) },
        )
        // 勾选而不是手输包名：包名写错时列表是空的，表现出来就是「开了没效果」。
        ActionRow(
            "选择应用",
            "已选 ${parsePackageList(settings.splitApps).size} 个 —— 从已安装应用里勾选",
        ) { onPickApps() }
        if (settings.splitMode != "off" && parsePackageList(settings.splitApps).isEmpty()) {
            SettingCard {
                Text(
                    "分应用已开启但一个应用都没选：现在等于不生效。点上面「选择应用」勾选。",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.error,
                )
            }
        }

        // ---------------- 网络 ----------------
        Section("网络")
        SettingSwitch(
            title = "IPv6",
            subtitle = "同时路由 ::/0；只在你的网络确实有 v6 时才打开",
            checked = settings.ipv6,
            onChange = { onChange(settings.copy(ipv6 = it)) },
        )
        SettingNumber(
            title = "MTU",
            subtitle = "与 VpnService 的 tun 设备共用同一个值，1280 最稳",
            value = settings.mtu.toString(),
            onChange = { v -> v.toIntOrNull()?.let { onChange(settings.copy(mtu = it.coerceIn(576, 1500))) } },
        )
        SettingText(
            title = "自定义 DNS",
            subtitle = "逗号分隔，会下发给 VPN 接口",
            value = settings.dns,
            onChange = { onChange(settings.copy(dns = it)) },
        )
        SettingSwitch(
            title = "不下发 DNS",
            subtitle = "让系统继续用当前网络的解析器",
            checked = settings.dnsOff,
            onChange = { onChange(settings.copy(dnsOff = it)) },
        )

        // ---------------- 界面 ----------------
        Section("界面")
        SettingChips(
            title = "主题",
            subtitle = "默认浅色",
            options = listOf("light" to "浅色", "dark" to "深色", "auto" to "跟随系统"),
            selected = settings.theme,
            onSelect = { onChange(settings.copy(theme = it)) },
        )
        SettingSwitch(
            title = "日志自动滚动",
            subtitle = "关闭后日志停在当前内容，方便复制",
            checked = settings.logAutoScroll,
            onChange = { onChange(settings.copy(logAutoScroll = it)) },
        )

        // ---------------- 关于 ----------------
        Section("关于")
        InfoRow("App 版本", appVersionName(ctx))
        InfoRow("引擎", state.version.ifBlank { "—" })
        InfoRow("内置账号", if (state.accountBuiltIn) "已导入（assets/account.json）" else "未导入（连接时需联网注册）")
        InfoRow("数据目录", ctx.filesDir.absolutePath + "/engine")
        InfoRow("系统 VPN", if (state.vpnUp) "已启用" else "未启用")
        Spacer(Modifier.height(8.dp))
        ActionRow("清空日志", "只清界面上的日志，不影响连接") { onClearLogs() }

        // ---------------- 尚未搬运 ----------------
        Section("电脑版有、安卓版还没搬的功能")
        NotYet("分流规则（直连域名 / 代理域名 / 地区分流）", "需要域名级规则引擎，VpnService 只能按应用和 IP 分流")
        NotYet("全局 / 规则 / 直连 三档模式", "「直连」等价于断开；「规则」依赖上面的规则引擎")
        NotYet("导出配置（.conf / mihomo YAML）", "电脑版只有命令行能用；安卓版要另做分享入口")
        NotYet("界面语言中英切换", "安卓界面目前只有中文，i18n 表还没建")
        NotYet("健康守护自动修复", "电脑版靠子进程守护；安卓会改成 Service 内的看门狗")

        Spacer(Modifier.height(24.dp))
    }
}

@Composable
private fun Section(title: String) {
    Spacer(Modifier.height(14.dp))
    Text(
        title,
        style = MaterialTheme.typography.labelLarge,
        color = MaterialTheme.colorScheme.primary,
        fontWeight = FontWeight.SemiBold,
        modifier = Modifier.padding(start = 4.dp, bottom = 6.dp),
    )
}

@Composable
private fun SettingCard(content: @Composable () -> Unit) {
    Card(
        modifier = Modifier
            .fillMaxWidth()
            .padding(bottom = 8.dp),
        shape = RoundedCornerShape(16.dp),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surface),
    ) {
        Column(Modifier.padding(14.dp)) { content() }
    }
}

@Composable
private fun SettingSwitch(
    title: String,
    subtitle: String,
    checked: Boolean,
    enabled: Boolean = true,
    onChange: (Boolean) -> Unit,
) {
    SettingCard {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text(
                    title,
                    style = MaterialTheme.typography.bodyLarge,
                    color = if (enabled) MaterialTheme.colorScheme.onSurface
                    else MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Text(
                    subtitle,
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Spacer(Modifier.width(12.dp))
            Switch(checked = checked, onCheckedChange = onChange, enabled = enabled)
        }
    }
}

@Composable
private fun SettingText(
    title: String,
    subtitle: String,
    value: String,
    onChange: (String) -> Unit,
) {
    SettingCard {
        Text(title, style = MaterialTheme.typography.bodyLarge)
        Text(
            subtitle,
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(Modifier.height(8.dp))
        OutlinedTextField(
            value = value,
            onValueChange = onChange,
            modifier = Modifier.fillMaxWidth(),
            singleLine = true,
            textStyle = MaterialTheme.typography.bodyMedium,
        )
    }
}

@Composable
private fun SettingNumber(
    title: String,
    subtitle: String,
    value: String,
    onChange: (String) -> Unit,
) {
    SettingText(title = title, subtitle = subtitle, value = value, onChange = onChange)
}

@Composable
private fun SettingChips(
    title: String,
    subtitle: String,
    options: List<Pair<String, String>>,
    selected: String,
    onSelect: (String) -> Unit,
) {
    SettingCard {
        Text(title, style = MaterialTheme.typography.bodyLarge)
        Text(
            subtitle,
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(Modifier.height(10.dp))
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            options.forEach { (id, label) ->
                val active = id == selected
                Box(
                    modifier = Modifier
                        .weight(1f)
                        .background(
                            if (active) MaterialTheme.colorScheme.primaryContainer
                            else MaterialTheme.colorScheme.surfaceVariant,
                            RoundedCornerShape(10.dp),
                        )
                        .clickable { onSelect(id) }
                        .padding(vertical = 10.dp),
                    contentAlignment = Alignment.Center,
                ) {
                    Text(
                        label,
                        style = MaterialTheme.typography.labelMedium,
                        color = if (active) MaterialTheme.colorScheme.onPrimaryContainer
                        else MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }
    }
}

@Composable
private fun SettingDropdown(
    title: String,
    subtitle: String,
    current: String,
    options: List<ProtoOption>,
    onChange: (String) -> Unit,
) {
    var open by remember { mutableStateOf(false) }
    SettingCard {
        Text(title, style = MaterialTheme.typography.bodyLarge)
        Text(
            subtitle,
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(Modifier.height(8.dp))
        Box {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .background(MaterialTheme.colorScheme.surfaceVariant, RoundedCornerShape(10.dp))
                    .clickable { open = true }
                    .padding(horizontal = 12.dp, vertical = 12.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    options.firstOrNull { it.id == current }?.label ?: current,
                    style = MaterialTheme.typography.bodyMedium,
                    modifier = Modifier.weight(1f),
                )
                Text("切换", style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.primary)
            }
            DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
                options.forEach { p ->
                    DropdownMenuItem(
                        enabled = p.ready,
                        text = {
                            Row(verticalAlignment = Alignment.CenterVertically) {
                                Text(p.label)
                                if (!p.ready) {
                                    Spacer(Modifier.width(8.dp))
                                    Text(
                                        "暂未实现",
                                        style = MaterialTheme.typography.labelSmall,
                                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                                    )
                                }
                            }
                        },
                        onClick = {
                            open = false
                            onChange(p.id)
                        },
                    )
                }
            }
        }
    }
}

/** 品牌块：应用图标（由根目录 logo.png 生成） + 软件名 + 版本。 */
@Composable
private fun BrandHeader() {
    val ctx = LocalContext.current
    SettingCard {
        Row(verticalAlignment = Alignment.CenterVertically) {
            // 必须用 drawable-nodpi/brand_logo.png，不能用 R.mipmap.ic_launcher：
            // 后者在 API 26+ 解析到 <adaptive-icon> XML，而 Compose 的
            // painterResource 不支持这个类型，会在合成设置页时抛异常 ——
            // 用户看到的就是「点三条杠 App 直接退出」。
            Image(
                painter = painterResource(R.drawable.brand_logo),
                contentDescription = "Xehn",
                modifier = Modifier.size(56.dp),
            )
            Spacer(Modifier.width(14.dp))
            Column {
                Text(
                    "Xehn",
                    style = MaterialTheme.typography.titleLarge,
                    fontWeight = FontWeight.Bold,
                )
                Text(
                    "版本 ${appVersionName(ctx)}",
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

/** 从 PackageManager 读版本名，避免和 build.gradle 里的 versionName 各写一份。 */
private fun appVersionName(ctx: Context): String = try {
    val info = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
        ctx.packageManager.getPackageInfo(ctx.packageName, PackageManager.PackageInfoFlags.of(0))
    } else {
        @Suppress("DEPRECATION")
        ctx.packageManager.getPackageInfo(ctx.packageName, 0)
    }
    info.versionName ?: "—"
} catch (_: Exception) {
    "—"
}

@Composable
private fun InfoRow(title: String, value: String) {
    SettingCard {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(title, style = MaterialTheme.typography.bodyMedium, modifier = Modifier.weight(1f))
            Text(
                value,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

@Composable
private fun ActionRow(title: String, subtitle: String, onClick: () -> Unit) {
    Card(
        modifier = Modifier
            .fillMaxWidth()
            .padding(bottom = 8.dp)
            .clickable { onClick() },
        shape = RoundedCornerShape(16.dp),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surface),
    ) {
        Column(Modifier.padding(14.dp)) {
            Text(title, style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.primary)
            Text(
                subtitle,
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

@Composable
private fun NotYet(title: String, why: String) {
    Card(
        modifier = Modifier
            .fillMaxWidth()
            .padding(bottom = 8.dp),
        shape = RoundedCornerShape(16.dp),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant),
    ) {
        Column(Modifier.padding(14.dp)) {
            Text(
                "· $title",
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Text(
                why,
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

/** 界面与首页保持一致：日志卡最多显示这么多行。 */
const val LOG_TAIL = 40
