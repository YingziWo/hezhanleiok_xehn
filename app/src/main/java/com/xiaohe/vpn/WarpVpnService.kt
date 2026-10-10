package com.xiaohe.vpn

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Intent
import android.content.pm.ServiceInfo
import android.net.VpnService
import android.os.Build
import androidx.core.app.NotificationCompat

/**
 * 常驻服务：系统 VPN 与会话的主人。
 *
 * 桌面版把「守护」做成 detach 的子进程（health.go 的 spawnHealthGuard 再 spawn
 * 自己）。Android 上 os.Executable() 指向 apk 内部不可执行的路径，那套方案必须
 * 整体丢掉 —— 换成「前台 Service 持有 Engine + VpnService 建 tun」。
 *
 * 这一版的核心变化：**连接 = 真的建立系统 VPN**。之前只有进程内的 gVisor 隧道，
 * 系统 VPN 根本没起来，手机上其它应用一个字节都不走它 —— 界面显示「已连接」而
 * 状态栏没有 VPN 钥匙，就是那个状态。现在顺序是：
 *
 *   装 socket protector → VpnService.Builder.establish() → 把 fd 交给引擎
 *   → 引擎用 OS tun 做数据面 → 隧道内验证出口
 *
 * 任何一步失败都直接报错收摊，绝不留下「显示已连接但没路由」的假成功。
 */
class WarpVpnService : VpnService() {

    companion object {
        const val ACTION_CONNECT = "com.xiaohe.vpn.action.CONNECT"
        const val ACTION_DISCONNECT = "com.xiaohe.vpn.action.DISCONNECT"
        const val EXTRA_PROTO = "proto"
        const val EXTRA_ENDPOINT = "endpoint"
        const val EXTRA_PSIPHON = "psiphon"

        private const val NOTIF_ID = 1001
        private const val CHANNEL_ID = "vpn-state"

        /** 隧道里用的设备地址（Cloudflare WARP 会给设备一个 172.16.0.2/32）。 */
        private const val TUN_ADDR_V4 = "172.16.0.2"
        private const val TUN_ADDR_V6 = "fd00:1:fd00:1:fd00:1:fd00:1"

        /**
         * Psiphon packet tunnel 的 MTU（tunnel-core 的 DEFAULT_MTU）。
         *
         * Psiphon 会往 tun 写最多 1500 字节的包，而内核会按接口 MTU 丢弃过大的包 ——
         * 接口 MTU 设 1280 时表现为「连上了、小请求通、稍微一大就断」。所以走
         * Psiphon 出口时，系统 tun 必须用它的 MTU；基础隧道在 netstack 里，
         * 不受这个值影响。
         */
        private const val PSIPHON_TUN_MTU = 1500
    }

    private var tunFd = -1

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_DISCONNECT -> {
                shutdown()
                return START_NOT_STICKY
            }
            ACTION_CONNECT -> {
                startForegroundSafely(getString(R.string.notif_connecting))
                startConnect(intent)
                return START_NOT_STICKY
            }
            else -> {
                // 没有任何指令就进来了（例如系统重建服务）：绝不擅自建立 VPN。
                // 建 VPN 必须是用户点出来的动作，这也是 START_NOT_STICKY 的理由。
                EngineBridge.note("[vpn] 收到空指令，未做任何事")
                return START_NOT_STICKY
            }
        }
    }

    private fun startConnect(intent: Intent?) {
        val e = EngineBridge.engineOrCreate(this)
        val s = SettingsStore.load(this)

        // 坑：getBooleanExtra 返回的是**非空** Boolean —— intent 没带这个 extra 时
        // 它给的是默认值 false，`?: s.psiphon` 这两条分支永远不会走到 Elvis 后面，
        // 结果就是「Psiphon 出口」开关开了也一直是 false（开关看起来完全无效）。
        // 必须用 hasExtra 区分「intent 没带」（退回本地设置）与「intent 明确说是
        // false」（用户刚关掉）。proto 用 getStringExtra 是可空的，本没这个坑，
        // 但保持同一种写法，免得以后再踩。
        val proto = if (intent?.hasExtra(EXTRA_PROTO) == true) {
            intent.getStringExtra(EXTRA_PROTO) ?: s.proto
        } else {
            s.proto
        }
        val psiphon = if (intent?.hasExtra(EXTRA_PSIPHON) == true) {
            intent.getBooleanExtra(EXTRA_PSIPHON, false)
        } else {
            s.psiphon
        }
        EngineBridge.note("[vpn] 本次连接：协议=$proto，Psiphon 出口=$psiphon")

        // 1) 先装 socket 排除器，必须在 establish 之前：系统 VPN 一建立，
        //    隧道自己发往 Cloudflare 的 UDP 就会被自己的 VPN 抓回去，
        //    握手包出不去，表现就是「已连接但打不开网页」。
        EngineBridge.installProtector(this, this)

        // 2) 建系统 VPN。失败就明确报错，不进入连接流程。
        val fd = establishTun(s, psiphon)
        if (fd < 0) {
            EngineBridge.note("[vpn] 系统 VPN 建立失败（未授权或被系统拒绝），已中止连接")
            e.setVPNState(false)
            EngineBridge.refresh(e)
            stopForegroundCompat()
            stopSelf()
            return
        }
        tunFd = fd
        e.setTunFD(fd.toLong())
        e.setVPNState(true)
        e.setTimeout(s.timeoutSec.toLong())
        EngineBridge.note(
            "[vpn] 系统 VPN 已启用（tun fd=$fd, MTU=${if (psiphon) PSIPHON_TUN_MTU else s.mtu}）"
        )
        EngineBridge.refresh(e)

        // 3) 拨号。引擎会用这个 fd 当数据面，出口验证走真实系统网络 ——
        //    所以「连接成功」本身就证明了系统 VPN 真的在路由。
        e.connect(
            proto,
            intent?.getStringExtra(EXTRA_ENDPOINT) ?: "",
            psiphon,
            s.ipv6,
            s.mtu.toLong(),
        )
        EngineBridge.refresh(e)
        updateNotification()
    }

    /**
     * 建立系统 VPN 并交出 fd 的所有权。
     *
     * detachFd() 而不是 fd：fd 的生命周期交给 Go 侧的 tun 设备，断开时由它关闭，
     * 系统 VPN 随之消失。用 pfd.fd 的话，Java 侧对象被回收时会关掉同一个 fd，
     * 隧道就会在毫无征兆的情况下断掉。
     */
    private fun establishTun(s: AppSettings, psiphon: Boolean): Int = try {
        val mtu = if (psiphon) PSIPHON_TUN_MTU else s.mtu
        val b = Builder()
            .setSession(getString(R.string.app_name))
            .setMtu(mtu)
            .addAddress(TUN_ADDR_V4, 32)
            .addRoute("0.0.0.0", 0)
            // 阻塞模式：wireguard 在独立 goroutine 上读设备，这正是它期望的语义。
            .setBlocking(true)

        if (s.ipv6) {
            // 不给 ::/0 配地址就路由 v6 会把 v6 流量丢进黑洞。
            b.addAddress(TUN_ADDR_V6, 128)
            b.addRoute("::", 0)
        }
        if (!s.dnsOff) {
            val dns = s.dnsList().ifEmpty { listOf("1.1.1.1", "223.5.5.5") }
            dns.forEach { runCatching { b.addDnsServer(it) } }
        }
        applySplit(b, s)

        val pfd = b.establish() ?: return -1
        pfd.detachFd()
    } catch (t: Throwable) {
        EngineBridge.note("[vpn] establish 异常: ${t.message}")
        -1
    }

    /**
     * 分应用：直接用 VpnService 的原生白/黑名单，等价于电脑版「分应用」三档，
     * 而且内核层生效，没有 mihomo 规则匹配的开销。
     */
    private fun applySplit(b: Builder, s: AppSettings) {
        val apps = s.splitAppList().filter { it != packageName }
        if (s.splitMode == "off" || apps.isEmpty()) return
        var applied = 0
        for (pkg in apps) {
            val ok = runCatching {
                when (s.splitMode) {
                    "proxy" -> b.addAllowedApplication(pkg)
                    "direct" -> b.addDisallowedApplication(pkg)
                }
            }.isSuccess
            if (ok) applied++
        }
        if (applied == 0) {
            // 全部包名都无效时，加白名单一个都没成功 = 变成「全部应用走 VPN」，
            // 与用户选的意图相反，必须说出来。
            EngineBridge.note("[vpn] 分应用列表里没有一个有效的包名，本次按全局模式处理")
        } else {
            EngineBridge.note("[vpn] 分应用已生效：$applied 个应用（模式 ${s.splitMode}）")
        }
    }

    private fun shutdown() {
        EngineBridge.peek()?.let { engine ->
            // 引擎关掉 tun 设备时 fd 随之关闭，系统 VPN 接口就消失了。
            engine.disconnect()
            engine.setVPNState(false)
            EngineBridge.refresh(engine)
        }
        tunFd = -1
        stopForegroundCompat()
        stopSelf()
    }

    /** 用户在系统设置里撤销授权，或启用了别的 VPN：必须收干净。 */
    override fun onRevoke() {
        EngineBridge.note("[vpn] 系统撤销了 VPN 授权，正在断开")
        shutdown()
        super.onRevoke()
    }

    override fun onDestroy() {
        shutdown()
        super.onDestroy()
    }

    // -----------------------------------------------------------------------
    // 通知
    // -----------------------------------------------------------------------

    private fun ensureChannel() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
        val nm = getSystemService(NotificationManager::class.java) ?: return
        if (nm.getNotificationChannel(CHANNEL_ID) != null) return
        val ch = NotificationChannel(
            CHANNEL_ID,
            getString(R.string.channel_name),
            NotificationManager.IMPORTANCE_LOW,
        )
        ch.setShowBadge(false)
        nm.createNotificationChannel(ch)
    }

    private fun startForegroundSafely(text: String) {
        ensureChannel()
        val notif = buildNotification(text)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            // specialUse 类型：与 manifest 的 foregroundServiceType 必须一致。
            startForeground(NOTIF_ID, notif, ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE)
        } else {
            startForeground(NOTIF_ID, notif)
        }
    }

    private fun updateNotification() {
        val nm = getSystemService(NotificationManager::class.java) ?: return
        val s = EngineBridge.state.value
        val text = when {
            s.isConnected && s.exitIp.isNotEmpty() -> "${getString(R.string.notif_connected)} · ${s.exitIp}"
            s.isConnected -> getString(R.string.notif_connected)
            s.isBusy -> getString(R.string.notif_connecting)
            else -> getString(R.string.notif_disconnected)
        }
        nm.notify(NOTIF_ID, buildNotification(text))
    }

    private fun stopForegroundCompat() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.N) {
            stopForeground(STOP_FOREGROUND_REMOVE)
        } else {
            @Suppress("DEPRECATION")
            stopForeground(true)
        }
    }

    private fun buildNotification(text: String): Notification {
        val open = PendingIntent.getActivity(
            this,
            0,
            Intent(this, MainActivity::class.java),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        val disconnect = PendingIntent.getService(
            this,
            1,
            Intent(this, WarpVpnService::class.java).setAction(ACTION_DISCONNECT),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        return NotificationCompat.Builder(this, CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_vpn_notification)
            .setContentTitle(getString(R.string.app_name))
            .setContentText(text)
            .setContentIntent(open)
            .addAction(0, getString(R.string.action_disconnect), disconnect)
            .setOngoing(true)
            .setPriority(NotificationCompat.PRIORITY_LOW)
            .build()
    }
}
