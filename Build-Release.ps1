# Build-Release.ps1：一键打包前后端为一个发布物（Windows amd64）。
# 用法：.\Build-Release.ps1 [-Version v1.0.0]
#   -Version 缺省时取 git describe（无 tag 则 dev + 短 commit）
# 产物：dist/memable-$Version-windows-amd64.zip（含 SHA256 校验文件）
# 代码注释使用中文
param(
    [string]$Version = ""
)

# 注意：不使用 $ErrorActionPreference="Stop"——PowerShell 5.1 会把外部命令
# （go/flutter/git，尤其是 .bat 包装的 flutter）的 stderr 甚至部分 stdout
# 包装成 NativeCommandError 并在 Stop 模式下终止脚本。改为外部命令显式检查
# $LASTEXITCODE，cmdlet 用 -ErrorAction Stop。
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $root

# ===== 1. 版本号 =====
if (-not $Version) {
    # git describe 无 tag 时返回非零（stderr 输出），用 LASTEXITCODE 判断成败
    $tag = ""
    $commit = ""
    $describe = git describe --tags --abbrev=0 2>&1
    if ($LASTEXITCODE -eq 0) { $tag = "$describe" }
    $rev = git rev-parse --short HEAD 2>&1
    if ($LASTEXITCODE -eq 0) { $commit = "$rev" }
    if ($tag) { $Version = $tag }
    elseif ($commit) { $Version = "dev-$commit" }
    else { $Version = "dev" }
}
Write-Host "==> 版本: $Version"

# ===== 2. 前置检查 =====
if (-not (Get-Command go -ErrorAction SilentlyContinue)) { throw "未找到 go，请先安装 Go 并加入 PATH" }
if (-not (Get-Command flutter -ErrorAction SilentlyContinue)) { throw "未找到 flutter，请先安装 Flutter SDK 并加入 PATH" }
$flutterExe = Get-Command flutter

# ===== 3. 构建后端（强制 amd64，避免 32 位进程地址空间限制）=====
Write-Host "==> 构建后端 server.exe ..."
$prevGoarch = $env:GOARCH
try {
    $env:GOARCH = "amd64"
    # 输出到 dist/server.exe（顶层），避免与 dist/memable 合并目录的清理互相干扰
    go build -trimpath -ldflags "-s -w -X main.version=$Version" -o dist/server.exe ./cmd/server
    if ($LASTEXITCODE -ne 0) { throw "go build 失败" }
} finally {
    $env:GOARCH = $prevGoarch
}

# ===== 4. 构建前端（Flutter Windows Release）=====
Write-Host "==> 构建前端 flutter build windows ..."
Push-Location flutter_app
try {
    & $flutterExe build windows --release
    if ($LASTEXITCODE -ne 0) { throw "flutter build 失败" }
} finally {
    Pop-Location
}
$flutterRelease = "flutter_app/build/windows/x64/runner/Release"

# ===== 5. 合并产物 =====
Write-Host "==> 合并产物到 dist/memable/ ..."
$out = "dist/memable"
if (Test-Path $out) { Remove-Item -Recurse -Force $out -ErrorAction Stop }
New-Item -ItemType Directory -Force -Path $out | Out-Null

# 复制整个 Release 目录为 app/（保留 data/ 子目录结构——PS 5.1 的
# Copy-Item "src\*" "dest\" 在目标不存在时会摊平子目录，导致 data/app.so 丢失、
# Flutter 引擎无法加载 AOT 数据而崩溃，因此先建目录再整目录复制）。
Copy-Item -Recurse -Force "$flutterRelease" "$out\app" -ErrorAction Stop
if (-not (Test-Path "$out\app\data\app.so")) { throw "前端产物缺少 data/app.so，打包结构异常" }
# server.exe 放入 app/ 与前端 exe 同目录（前端自动拉起逻辑按自身目录查找 server.exe）
Copy-Item "dist\server.exe" "$out\app\server.exe" -Force -ErrorAction Stop
# 示例配置放入 app/：前端以相对路径 -config config.yaml 拉起 server，
# 双击启动时工作目录即 app/，必须与 server.exe 同目录。
Copy-Item "config.yaml" "$out\app\config.yaml" -Force -ErrorAction Stop
# 版本信息
"$Version" | Out-File -FilePath "$out\version.txt" -Encoding utf8 -ErrorAction Stop

# ===== 6. 压缩 =====
Write-Host "==> 打包 zip ..."
$zipName = "memable-$Version-windows-amd64.zip"
if (Test-Path "dist/$zipName") { Remove-Item -Force "dist/$zipName" -ErrorAction Stop }
Compress-Archive -Path "$out\*" -DestinationPath "dist/$zipName" -ErrorAction Stop

# ===== 7. SHA256 =====
$sha = Get-FileHash "dist/$zipName" -Algorithm SHA256
"$($sha.Hash)  $zipName" | Out-File "dist/$zipName.sha256" -Encoding utf8
Write-Host ""
Write-Host "===== 打包完成 ====="
Write-Host "产物: dist/$zipName"
Write-Host "SHA256: $($sha.Hash)"
Write-Host "解压后运行 app\memable.exe（首次启动自动拉起同目录 server.exe，端口默认 12358，占用自动避让）"
