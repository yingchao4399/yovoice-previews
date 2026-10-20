# 使用 Authenticode 证书签名 Windows 可执行文件。未配置 WINDOWS_SIGN_PFX_BASE64 时跳过（开发与 PR 构建），
# 配置后签名失败即中止打包。证书为 PFX 的 Base64，密码放在 WINDOWS_SIGN_PASSWORD。
param([Parameter(Mandatory = $true)][string[]]$Path)
$ErrorActionPreference = 'Stop'
if (!$env:WINDOWS_SIGN_PFX_BASE64) { Write-Host '未配置 Windows 签名证书，跳过签名。'; return }
if (!$env:WINDOWS_SIGN_PASSWORD) { throw '缺少 WINDOWS_SIGN_PASSWORD。' }
$signtool = Get-Command signtool.exe -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Source -First 1
if (!$signtool) {
    $signtool = Get-ChildItem "${env:ProgramFiles(x86)}\Windows Kits\10\bin\*\x64\signtool.exe" -ErrorAction SilentlyContinue |
        Sort-Object FullName -Descending | Select-Object -ExpandProperty FullName -First 1
}
if (!$signtool) { throw '未找到 signtool.exe，请安装 Windows SDK。' }
$timestamp = if ($env:WINDOWS_SIGN_TIMESTAMP_URL) { $env:WINDOWS_SIGN_TIMESTAMP_URL } else { 'http://timestamp.digicert.com' }
$certificate = Join-Path ([IO.Path]::GetTempPath()) ('yovoice-sign-' + [Guid]::NewGuid().ToString('N') + '.pfx')
try {
    [IO.File]::WriteAllBytes($certificate, [Convert]::FromBase64String($env:WINDOWS_SIGN_PFX_BASE64))
    foreach ($file in $Path) {
        if (!(Test-Path -LiteralPath $file)) { throw "待签名文件不存在：$file" }
        & $signtool sign /fd SHA256 /td SHA256 /tr $timestamp /f $certificate /p $env:WINDOWS_SIGN_PASSWORD $file
        if ($LASTEXITCODE -ne 0) { throw "签名失败：$file" }
        if ((Get-AuthenticodeSignature -LiteralPath $file).Status -ne 'Valid') { throw "签名校验失败：$file" }
    }
} finally {
    Remove-Item -LiteralPath $certificate -Force -ErrorAction SilentlyContinue
}
