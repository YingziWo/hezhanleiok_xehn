# 从 logo.png 生成 Android 图标资源。
#
# 两个必须处理的点：
#   1. 原图是「圆角方形卡片 + 四角纯白」的画布。直接当图标用，四角的白会露出来。
#      所以先把纯白边框裁掉，只留卡片本身。
#   2. 自适应图标（API 26+）由启动器按自己的形状（圆/方圆）裁剪 foreground，
#      只要卡片铺得比遮罩大，卡片自己的白色圆角就落在遮罩外 —— 白角自然消失。
#      这里把卡片按画布 90% 摆放：卡片圆角在遮罩外被裁掉，底部 XEHN 字样
#      仍落在遮罩内（按 108dp 画布 / 54dp 半径算过）。
#
# 同时生成 legacy 图标（API 25 及以下、以及忽略自适应图标的启动器）：
# 方形的铺背景色，圆的逐像素抠成圆并做边缘抗锯齿。
#
# 注意：不用 Graphics.SetClip(GraphicsPath, CombineMode)。PowerShell 在
# 重载解析上会挑错 SetClip(Rectangle) 并报一个看不懂的转换错误，逐像素更稳。

Add-Type -AssemblyName System.Drawing

$root = 'D:\xhwarp-android'
$src = Join-Path $root 'logo.png'
$res = Join-Path $root 'app\src\main\res'

# 卡片外圈的纯白；阈值放宽到 250，否则卡片自身的极浅渐变会被当成白边。
$whiteCut = 250

Write-Host "读取 $src"
$bmp = [System.Drawing.Bitmap]::FromFile($src)
$w = $bmp.Width
$h = $bmp.Height
Write-Host "  尺寸 ${w}x${h}"

# --- 1) 找非白内容的外框 -------------------------------------------------
$rect = New-Object System.Drawing.Rectangle(0, 0, $w, $h)
$data = $bmp.LockBits($rect, [System.Drawing.Imaging.ImageLockMode]::ReadOnly, [System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
$bytes = New-Object byte[] ($data.Stride * $h)
[System.Runtime.InteropServices.Marshal]::Copy($data.Scan0, $bytes, 0, $bytes.Length)
$bmp.UnlockBits($data)

$minX = $w; $minY = $h; $maxX = -1; $maxY = -1
for ($y = 0; $y -lt $h; $y++) {
    $row = $y * $data.Stride
    for ($x = 0; $x -lt $w; $x++) {
        $i = $row + $x * 4
        # 内存里是 BGRA
        if ($bytes[$i] -lt $whiteCut -or $bytes[$i + 1] -lt $whiteCut -or $bytes[$i + 2] -lt $whiteCut) {
            if ($x -lt $minX) { $minX = $x }
            if ($x -gt $maxX) { $maxX = $x }
            if ($y -lt $minY) { $minY = $y }
            if ($y -gt $maxY) { $maxY = $y }
        }
    }
}
if ($maxX -lt 0) {
    Write-Error '整张图都是白的？裁边失败'
    exit 1
}
$cardW = $maxX - $minX + 1
$cardH = $maxY - $minY + 1
Write-Host "  裁掉白边 -> 卡片 ${cardW}x${cardH} @ ($minX,$minY)"

# 卡片接近正方形；不是的话以中心取正方，避免被拉变形。
$side = [Math]::Min($cardW, $cardH)
$sq = New-Object System.Drawing.Rectangle(
    ($minX + [int](($cardW - $side) / 2)),
    ($minY + [int](($cardH - $side) / 2)),
    $side,
    $side)

$fill = 0.90                                                  # 卡片占画布的比例
$bg = [System.Drawing.Color]::FromArgb(255, 241, 246, 255)    # #F1F6FF

function Save-Png {
    param($Bitmap, [string]$Path)
    $dir = Split-Path $Path -Parent
    if (-not (Test-Path $dir)) { New-Item -ItemType Directory -Force $dir | Out-Null }
    $Bitmap.Save($Path, [System.Drawing.Imaging.ImageFormat]::Png)
}

function New-Icon {
    param(
        [int]$Size,
        [string]$Path,
        [bool]$Round = $false,
        [bool]$WithBg = $false,
        [double]$Fill = 0.90
    )
    $out = [System.Drawing.Bitmap]::new($Size, $Size, [System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
    $g = [System.Drawing.Graphics]::FromImage($out)
    $g.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
    $g.PixelOffsetMode = [System.Drawing.Drawing2D.PixelOffsetMode]::HighQuality
    $g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
    if ($WithBg) { $g.Clear($bg) } else { $g.Clear([System.Drawing.Color]::Transparent) }

    $art = [int]($Size * $Fill)
    $off = [int](($Size - $art) / 2)
    # 用 ::new 而不是 New-Object Type(...)：后者在参数带括号时的解析很容易出岔子，
    # 出岔子时 PowerShell 又默认不终止，会静默生成一张不对的图。
    $dest = [System.Drawing.Rectangle]::new($off, $off, $art, $art)
    $g.DrawImage($bmp, $dest, $sq, [System.Drawing.GraphicsUnit]::Pixel)
    $g.Dispose()

    if ($Round) {
        # 逐像素抠圆 + 边缘抗锯齿（1px 过渡），避免 GetPixel 之外的锯齿圈。
        $r = $Size / 2.0
        $cx = $r
        $cy = $r
        for ($y = 0; $y -lt $Size; $y++) {
            $dy = $y + 0.5 - $cy
            for ($x = 0; $x -lt $Size; $x++) {
                $dx = $x + 0.5 - $cx
                $dist = [Math]::Sqrt($dx * $dx + $dy * $dy)
                if ($dist -le $r - 1) { continue }
                if ($dist -ge $r) {
                    $out.SetPixel($x, $y, [System.Drawing.Color]::Transparent)
                } else {
                    $p = $out.GetPixel($x, $y)
                    $a = [int](255 * ($r - $dist))
                    if ($a -lt 0) { $a = 0 }
                    if ($a -gt 255) { $a = 255 }
                    $out.SetPixel($x, $y, [System.Drawing.Color]::FromArgb($a, $p.R, $p.G, $p.B))
                }
            }
        }
    }

    Save-Png -Bitmap $out -Path $Path
    $out.Dispose()
}

$densities = @(
    @{ name = 'mdpi';    legacy = 48;  fg = 108 },
    @{ name = 'hdpi';    legacy = 72;  fg = 162 },
    @{ name = 'xhdpi';   legacy = 96;  fg = 216 },
    @{ name = 'xxhdpi';  legacy = 144; fg = 324 },
    @{ name = 'xxxhdpi'; legacy = 192; fg = 432 }
)

foreach ($d in $densities) {
    $dir = Join-Path $res ("mipmap-" + $d.name)
    New-Icon -Size $d.legacy -Path (Join-Path $dir 'ic_launcher.png') -WithBg $true
    New-Icon -Size $d.legacy -Path (Join-Path $dir 'ic_launcher_round.png') -Round $true -WithBg $true
    New-Icon -Size $d.fg -Path (Join-Path $dir 'ic_launcher_foreground.png')
    Write-Host ("  mipmap-{0}: legacy={1} foreground={2}" -f $d.name, $d.legacy, $d.fg)
}

# --- 2) 自适应图标描述 ---------------------------------------------------
$anydpi = Join-Path $res 'mipmap-anydpi-v26'
if (-not (Test-Path $anydpi)) { New-Item -ItemType Directory -Force $anydpi | Out-Null }

$adaptive = @'
<?xml version="1.0" encoding="utf-8"?>
<!--
    自适应图标：foreground 是裁掉白边后的卡片，按画布 90% 摆放。
    启动器按自己的形状裁剪，卡片自身那圈白色圆角落在遮罩之外，所以不会有
    四角白边；background 用与卡片边缘同色系的浅蓝白，遮罩边缘自然衔接。
-->
<adaptive-icon xmlns:android="http://schemas.android.com/apk/res/android">
    <background android:drawable="@color/ic_launcher_background" />
    <foreground android:drawable="@mipmap/ic_launcher_foreground" />
</adaptive-icon>
'@
Set-Content -Path (Join-Path $anydpi 'ic_launcher.xml') -Value $adaptive -Encoding UTF8
Set-Content -Path (Join-Path $anydpi 'ic_launcher_round.xml') -Value $adaptive -Encoding UTF8

$colors = @'
<?xml version="1.0" encoding="utf-8"?>
<resources>
    <!-- 自适应图标底板色：取自 logo 卡片边缘的浅蓝白 -->
    <color name="ic_launcher_background">#F1F6FF</color>
</resources>
'@
Set-Content -Path (Join-Path $res 'values\colors.xml') -Value $colors -Encoding UTF8

# --- 3) App 内部用的品牌图（纯 PNG，不带 anydpi） -------------------------
#
# 为什么必须单独出一张：设置页要显示 logo，但 R.mipmap.ic_launcher 在 API 26+
# 会解析到 <adaptive-icon> XML，而 Compose 的 painterResource 不支持这个类型 ——
# 会直接抛异常，表现是「点设置就闪退」。所以内部显示必须用不带 anydpi 变体的
# 纯 PNG 资源，并且放在 drawable-nodpi 里，避免任何密度匹配歧义。
$brand = Join-Path $res 'drawable-nodpi\brand_logo.png'
New-Icon -Size 256 -Path $brand -Fill 1.0
Write-Host "  drawable-nodpi/brand_logo.png (256, 卡片铺满)"

# 源位图到这里才释放：上面几段都还在读它。提前 Dispose 会让 DrawImage 抛
# "Parameter is not valid"，而 PowerShell 默认不终止脚本 —— 结果是静默生成
# 一张不对的图，很难查。
$bmp.Dispose()

Write-Host '完成：各密度 mipmap + anydpi-v26 自适应图标 + drawable-nodpi/brand_logo.png + colors.xml'
