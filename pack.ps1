# pack.ps1 — assemble the portable warp-client ZIP (v2rayN-style layout).
#
#   .\pack.ps1 [-MihomoPath <path>] [-PsiphonExePath <path>] [-PsiphonConfigPath <path>]
#              [-Version <tag>] [-SkipBuild]
#
# Layout produced in dist\:
#   warp-client\
#   ├── warp-client-gui.exe   ← double-click to start (no console window)
#   ├── warp-client.exe       ← same binary, console build, for CLI use
#   ├── core\
#   │   ├── mihomo.exe        ← the Clash core, updatable independently
#   │   ├── psiphon.exe       ← optional: the Psiphon exit sidecar (GPL-3.0)
#   │   ├── client.config     ← optional: official Psiphon credentials (needed with psiphon.exe)
#   │   └── server_list.dat   ← optional: initial Psiphon server list
#   ├── licenses\
#   │   └── GPL-3.0.txt       ← license text for the bundled mihomo + Psiphon
#   ├── THIRD_PARTY_NOTICES.md
#   └── README.txt
#
# Everything else (account, scan cache, state, config.yaml, logs) is created at
# runtime next to the exe thanks to portable-mode auto-anchoring.
param(
    [string]$MihomoPath = "",
    [string]$PsiphonExePath = "",
    [string]$PsiphonConfigPath = "",
    [string]$Version = "",
    [switch]$SkipBuild
)

$ErrorActionPreference = 'Stop'
$here = Split-Path -Parent $MyInvocation.MyCommand.Path
$dist = Join-Path $here 'dist'
$root = Join-Path $dist 'warp-client'
$stamp = Get-Date -Format 'yyyyMMddHHmmss'
if (-not $Version) { $Version = "0.3.0-$stamp" }

if (-not $SkipBuild) {
    # Environment: keep Go caches inside the workspace when unset outside.
    if (-not $env:GOMODCACHE) { $env:GOMODCACHE = 'E:\xiaohe\xhwarp\.gomod' }
    if (-not $env:GOCACHE)    { $env:GOCACHE    = 'E:\xiaohe\xhwarp\.gocache' }
    if (-not $env:GOPATH)     { $env:GOPATH     = 'E:\xiaohe\xhwarp\.gopath' }
    if (-not $env:GOPROXY)    { $env:GOPROXY    = 'https://goproxy.cn,direct' }
    $env:GOOS = 'windows'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'

    Push-Location $here

    # Regenerate the logo artifacts from the source art: the embedded PNG the
    # in-app brand + window/taskbar icon render from, and the multi-size .ico
    # rsrc bakes into the exe as the program icon. The source lives one level up
    # (the workspace root); a missing source leaves the committed artifacts.
    $logoSrc = Join-Path $here '..\logo.png'
    if (Test-Path $logoSrc) {
        go run ./tools/mklogo -src $logoSrc -png assets/logo.png -ico logo.ico
        if ($LASTEXITCODE -ne 0) { Pop-Location; throw "mklogo failed to build the logo artifacts" }
    }

    # Embed the Win32 manifest (comctl32 v6 + PerMonitorV2 DPI) and the app icon
    # (.ico -> the exe's program/taskbar-group icon). walk's native widgets
    # require themed common controls or NewMainWindow fails with
    # "TTM_ADDTOOL failed" at startup. rsrc is installed via
    #   go install github.com/akavel/rsrc@latest
    $rsrc = Join-Path $env:GOPATH 'bin\rsrc.exe'
    $syso = Join-Path $here 'rsrc_windows_amd64.syso'
    $icoArgs = @()
    if (Test-Path (Join-Path $here 'logo.ico')) { $icoArgs = @('-ico', (Join-Path $here 'logo.ico')) }
    if (Test-Path $rsrc) {
        & $rsrc -manifest (Join-Path $here 'app.manifest') @icoArgs -o $syso -arch amd64
        if ($LASTEXITCODE -ne 0) { Pop-Location; throw "rsrc failed to embed manifest/icon" }
    } elseif (-not (Test-Path $syso)) {
        Pop-Location; throw "rsrc_windows_amd64.syso missing AND rsrc.exe not installed — install via: go install github.com/akavel/rsrc@latest"
    }

    go build -trimpath -ldflags "-s -w -X main.version=$Version" -o warp-client.exe .
    if ($LASTEXITCODE -ne 0) { Pop-Location; throw "console build failed" }
    go build -trimpath -ldflags "-s -w -H windowsgui -X main.version=$Version -X main.windowsGUIBuild=1" -o warp-client-gui.exe .
    if ($LASTEXITCODE -ne 0) { Pop-Location; throw "windowsgui build failed" }
    Pop-Location
    Write-Host "built warp-client.exe + warp-client-gui.exe ($Version)"
}

# Fresh dist tree
if (Test-Path $root) { Remove-Item $root -Recurse -Force }
New-Item -ItemType Directory -Force -Path "$root\core" | Out-Null

Copy-Item (Join-Path $here 'warp-client-gui.exe') $root
Copy-Item (Join-Path $here 'warp-client.exe')     $root

# Core: bundled when provided, otherwise the GUI downloads it on first run.
if ($MihomoPath -and (Test-Path $MihomoPath)) {
    Copy-Item $MihomoPath (Join-Path $root 'core\mihomo.exe') -Force
    Write-Host "bundled core: $([math]::Round((Get-Item $MihomoPath).Length/1MB,1)) MB"
} else {
    Write-Host "no -MihomoPath given: core/ stays empty; the GUI offers one-click download"
}

# Psiphon exit sidecar (V1): bundled when present. Both the executable and the
# official client config are required for `connect --psiphon` — the config carries
# the SponsorId / PropagationChannelId / server-list URLs without which Psiphon
# discovers zero servers. Defaults look in the source core\ dir so a dev tree
# that already has them packs them automatically.
if (-not $PsiphonExePath)    { $PsiphonExePath    = Join-Path $here 'core\psiphon.exe' }
if (-not $PsiphonConfigPath) { $PsiphonConfigPath = Join-Path $here 'core\client.config' }
if (Test-Path $PsiphonExePath) {
    Copy-Item $PsiphonExePath (Join-Path $root 'core\psiphon.exe') -Force
    Write-Host "bundled psiphon sidecar: $([math]::Round((Get-Item $PsiphonExePath).Length/1MB,1)) MB"
} else {
    Write-Host "no psiphon.exe: core\psiphon.exe absent; connect --psiphon will fail until it is placed"
}
if (Test-Path $PsiphonConfigPath) {
    Copy-Item $PsiphonConfigPath (Join-Path $root 'core\client.config') -Force
    Write-Host "bundled psiphon config: $PsiphonConfigPath"
    # The initial server list travels with the config: Psiphon uses it before it
    # has a downloaded list of its own.
    $serverList = Join-Path (Split-Path -Parent $PsiphonConfigPath) 'server_list.dat'
    if (Test-Path $serverList) {
        Copy-Item $serverList (Join-Path $root 'core\server_list.dat') -Force
        Write-Host "bundled psiphon server list: $([math]::Round((Get-Item $serverList).Length/1KB,0)) KB"
    }
} else {
    Write-Host "no client.config: core\client.config absent; connect --psiphon will fail until it is placed"
}

Copy-Item (Join-Path $here 'README-portable.txt') (Join-Path $root 'README.txt')

# Third-party license inventory + the GPL-3.0 text. The ZIP bundles GPL-3.0
# binaries (mihomo, Psiphon), so redistributing it must carry both.
if (Test-Path (Join-Path $here 'THIRD_PARTY_NOTICES.md')) {
    Copy-Item (Join-Path $here 'THIRD_PARTY_NOTICES.md') (Join-Path $root 'THIRD_PARTY_NOTICES.md')
}
$gpl = Join-Path $here 'third_party\GPL-3.0.txt'
if (Test-Path $gpl) {
    New-Item -ItemType Directory -Force -Path (Join-Path $root 'licenses') | Out-Null
    Copy-Item $gpl (Join-Path $root 'licenses\GPL-3.0.txt')
    Write-Host 'bundled licenses\GPL-3.0.txt'
} else {
    Write-Host 'WARNING: third_party\GPL-3.0.txt missing — the ZIP bundles GPL-3.0 binaries and must ship the license'
}

# Zip it
$zip = Join-Path $dist "warp-client-$Version.zip"
if (Test-Path $zip) { Remove-Item $zip -Force }
Compress-Archive -Path $root -DestinationPath $zip -CompressionLevel Optimal
Write-Host ""
Write-Host "ZIP ready: $zip ($([math]::Round((Get-Item $zip).Length/1MB,1)) MB)"
Write-Host "Layout:"
Get-ChildItem $root -Recurse -File | ForEach-Object {
    Write-Host ("  " + $_.FullName.Substring($root.Length + 1) + "  ($([math]::Round($_.Length/1MB,2)) MB)")
}