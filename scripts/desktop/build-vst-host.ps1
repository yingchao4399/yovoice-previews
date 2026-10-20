param([Parameter(Mandatory=$true)][string]$Destination)
$ErrorActionPreference = 'Stop'
$root = Split-Path (Split-Path $PSScriptRoot -Parent) -Parent
$venv = Join-Path $root 'artifacts/vst-host-venv'
$python = Join-Path $venv 'Scripts/python.exe'
$uv = Get-Command uv -CommandType Application -ErrorAction SilentlyContinue
if (!$uv) { throw '构建 VST3 宿主需要 uv。' }
if (!(Test-Path -LiteralPath $python)) {
    & $uv.Source venv --python 3.12 $venv
    if ($LASTEXITCODE -ne 0) { throw 'VST3 宿主 Python 环境创建失败' }
}
& $uv.Source pip install --python $python 'pedalboard==0.9.26' 'mido==1.3.3' 'pyinstaller==6.22.3'
if ($LASTEXITCODE -ne 0) { throw 'VST3 宿主依赖安装失败' }
& $python -m PyInstaller --noconfirm --onefile --hidden-import pedalboard.midi_utils `
    --name yovoice-vst-host --distpath (Join-Path $root 'artifacts/vst-host-dist') `
    --workpath (Join-Path $root 'artifacts/vst-host-build') `
    --specpath (Join-Path $root 'artifacts/vst-host-build') `
    (Join-Path $root 'scripts/vst-host/main.py')
if ($LASTEXITCODE -ne 0) { throw 'VST3 宿主打包失败' }
New-Item -ItemType Directory -Force -Path $Destination | Out-Null
Copy-Item -LiteralPath (Join-Path $root 'artifacts/vst-host-dist/yovoice-vst-host.exe') -Destination (Join-Path $Destination 'yovoice-vst-host.exe') -Force
Copy-Item -LiteralPath (Join-Path $root 'scripts/vst-host/main.py') -Destination (Join-Path $Destination 'yovoice-vst-host-source.py') -Force
$sourceZip = Join-Path $root 'artifacts/downloads/pedalboard-v0.9.26-source.zip'
$checkout = Join-Path $root 'artifacts/pedalboard-source'
if (!(Test-Path -LiteralPath $checkout)) {
    git clone --depth 1 --branch v0.9.26 https://github.com/spotify/pedalboard.git $checkout
    if ($LASTEXITCODE -ne 0) { throw '无法取得 pedalboard 0.9.26 对应源码' }
}
if (!(Test-Path -LiteralPath $sourceZip)) {
    git -C $checkout archive --format=zip --output=$sourceZip HEAD
    if ($LASTEXITCODE -ne 0) { throw 'pedalboard 源码归档失败' }
}
Copy-Item -LiteralPath $sourceZip -Destination (Join-Path $Destination 'pedalboard-v0.9.26-source.zip') -Force
Copy-Item -LiteralPath (Join-Path $root 'artifacts/pedalboard-source/LICENSE') -Destination (Join-Path $Destination 'pedalboard-LICENSE.txt') -Force
