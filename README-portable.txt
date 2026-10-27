══════════════════════════════════════════════════════════════
  WARP Client — 便携版
  Cloudflare WARP 一键客户端 (AmneziaWG / WireGuard / MASQUE)
══════════════════════════════════════════════════════════════

【使用须知】

  · 本软件开源免费，仅供学习交流使用，禁止售卖！
  · 项目主页(免费下载/反馈): https://github.com/hezhanleiok/xehn
  · 每台设备首次运行会生成一个控制码(「关于」页可见)；
    若该控制码被远程停用，客户端将无法连接。
  · 程序只在本机配置一条加密隧道，不提供、不代理任何服务器资源。

【快速开始】

  1. 解压到任意可写目录
  2. 双击 xehn.exe
     → 弹出原生图形窗口(深色 Proton 风格,非网页)
     → 首次点击「连接」会自动注册本设备的 WARP 账号 + 扫描最优端点
  3. 窗口内即可看到:出口 IP、国家、延迟、Colo 节点,以及实时延迟图表

【图形界面说明】

  · 左上「协议」:选择 AWG / WG / MASQUE / MASQUE H2
  · 「连接选项」:
      - TUN 全局模式        整机流量走隧道(需管理员权限)
      - 连接前重新扫描      每次连接都重扫端点,取当时最优
      - WARP in WARP(链式)  内层隧道穿过外层 WARP,规避深度检测;
                             开启后可再选内层协议 WG/AWG
  · 「国家 / Colo 过滤」:点选只允许的国家/机房,连接时自动择优
  · 中央大圆:连接/断开;下方显示 出口IP/国家/延迟/Colo
  · 「实时延迟(ms)」:连接后每 2 秒采样一次的延迟曲线图
  · 「缓存端点」:扫描结果列表(端点/国家/COLO/延迟/丢包),
     点击某行即可锁定该端点(固定连接)
  · 底栏「核心」:未安装时可一键下载 Clash 核心

  TUN 全局模式:右键 xehn.exe → 以管理员身份运行

【目录说明】

  xehn.exe       唯一的程序文件:双击启动原生图形窗口;
                        带子命令运行时作为命令行工具(输出到当前终端)
  core\
    mihomo.exe          Clash 核心 — 独立文件,可单独替换更新
    psiphon.exe         (可选) Psiphon 出口组件 — 独立可执行文件 (GPL-3.0)
    client.config       (可选) Psiphon 官方凭据 — 与 psiphon.exe 成对必需
    server_list.dat     (可选) Psiphon 初始服务器列表
  licenses\
    GPL-3.0.txt         GPL-3.0 许可证全文(mihomo 与 Psiphon 均为 GPL-3.0)
  THIRD_PARTY_NOTICES.md  第三方组件清单与再分发义务(再分发前请阅读)

  运行时自动生成(都在本目录内,删除即重置):
    warpscout-account.json   本设备的 WARP 账号(首次连接/扫描时自动注册)
    cache.json               扫描结果缓存(6 小时有效)
    state.json               连接状态
    core\config.yaml         mihomo 配置
    xehn.log          日志

【分发到新设备】

  ZIP 内不含任何账号数据。把解压后的目录拷到另一台电脑时,首次连接或扫描
  会自动为那台设备注册新的 WARP 账号。
  不要把本机的 warpscout-account.json 一起复制过去 — 它代表当前设备的
  WARP 身份,带过去会沿用旧账号而不是重新注册。

【核心更新(核心分离设计)】

  方式一(推荐):面板底栏点「核心」一键下载最新 mihomo
  方式二:手动替换 core\mihomo.exe 为新版即可,GUI 程序无需重新编译
  方式三:下载任意 mihomo-windows-amd64 版本,改名为 mihomo.exe
         放入 core\ 目录(https://github.com/MetaCubeX/mihomo/releases)

【命令行用法】

  与 GUI 是同一个 xehn.exe,在终端带子命令运行即可。
  注意:该 exe 为窗口子系统,cmd 不会等待命令结束(提示符会先返回);
  注册/连接仍在后台继续,输出显示在当前终端。

  xehn.exe register              注册 WARP 账号
  xehn.exe connect -p awg        连接(awg/wg/masque/masque-h2)
  xehn.exe connect -p awg --psiphon
                                        基础协议 + Psiphon 出口(最终出口 IP 变为 Psiphon 的)
  xehn.exe connect --tun         TUN 全局模式(需管理员)
  xehn.exe status                查看出口 IP/延迟/国家
  xehn.exe list                  查看缓存的端点
  xehn.exe disconnect            断开并还原系统代理
  xehn.exe gui                   启动原生图形窗口
  xehn.exe gui --web             旧网页面板(浏览器模式)
  xehn.exe scan -best -p awg     只扫描输出最优端点

【常见问题】

  Q: 连接后没网?
  A: 检查 xehn.log;先 disconnect 再重连;或换协议
     (AWG 抗封锁能力最强,推荐优先使用)

  Q: 想完全重置?
  A: 删除目录内生成的文件(保留 exe 和 core\)后重新连接

  Q: Psiphon 出口怎么用?
  A: 把官方 psiphon.exe 与 client.config 放进 core\ ,然后
     xehn.exe connect -p awg --psiphon
     基础协议(WG/AWG/MASQUE)负责内层承载,最终公网出口 IP 变为 Psiphon 的;
     status 会显示组合态,如 session=AWG-Psiphon。
     也可用环境变量指定路径:PSIPHON_EXE、PSIPHON_CONFIG。
     注意:Psiphon 组件为 GPL-3.0,以独立进程 + 本地 SOCKS 方式集成,不并入主程序。

  Q: 默认代理端口?
  A: 混合端口 7890 (HTTP+SOCKS),系统代理自动设置;
     REST 控制器 9090

  Q: 与其他 Clash 工具冲突?
  A: 确保没有其他程序占用 7890/9090 端口

──────────────────────────────────────────────────────────────
  基于 github.com/vernette/warpscout 扫描引擎 + mihomo 核心
  可选:Psiphon 出口组件(psiphon-tunnel-core, GPL-3.0, 独立进程)
──────────────────────────────────────────────────────────────