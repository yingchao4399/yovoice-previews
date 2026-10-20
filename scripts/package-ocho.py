"""把固定版本的 ocho 运行时解包进安装包；版本与校验值取自 internal/catalog/ocho.json。

环境变量：
- YOVOICE_OCHO_SKIP=1：跳过 ocho（源码构建者没有闭源运行时时使用），打印提示。
- YOVOICE_OCHO_ARCHIVE：指定本地压缩包，跳过下载但仍校验摘要。
"""
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import sys
import tarfile
import time
import urllib.request
import zipfile

ROOT = Path(__file__).resolve().parents[1]
RELEASES = 'https://github.com/leemysw/ocho-release/releases/download'


def current_platform():
    goos = os.environ.get('GOOS', {'Darwin': 'darwin', 'Windows': 'windows', 'Linux': 'linux'}.get(platform.system(), platform.system().lower()))
    goarch = os.environ.get('GOARCH', 'arm64' if platform.machine().lower() in ('arm64', 'aarch64') else 'amd64')
    return goos, goarch


def asset_name(goos, goarch, version):
    suffix = {('darwin', 'arm64'): 'darwin-arm64.tar.gz', ('windows', 'amd64'): 'windows-amd64.zip', ('linux', 'amd64'): 'linux-amd64.tar.gz'}.get((goos, goarch))
    if suffix is None:
        raise SystemExit('ocho 仅支持 macOS ARM64、Windows x64 和 Linux x64')
    return f'ocho-{version}-{suffix}'


def download(url, target):
    """下载到 .part 后原子改名，失败重试三次。"""
    partial = target.with_suffix(target.suffix + '.part')
    for attempt in range(3):
        try:
            with urllib.request.urlopen(url) as response, partial.open('wb') as output:
                shutil.copyfileobj(response, output)
            partial.replace(target)
            return
        except Exception:
            partial.unlink(missing_ok=True)
            if attempt == 2:
                raise
            time.sleep(2 ** attempt)


def extract(archive, binary, destination):
    """只解压压缩包根目录的 ocho 可执行文件与 LICENSE.txt，其他内容不进入安装包。"""
    found = set()
    if archive.suffix == '.zip':
        with zipfile.ZipFile(archive) as bundle:
            for entry in bundle.infolist():
                name = entry.filename.removeprefix('./')
                if entry.is_dir() or name not in (binary, 'LICENSE.txt'):
                    continue
                with bundle.open(entry) as source, (destination / name).open('wb') as output:
                    shutil.copyfileobj(source, output)
                found.add(name)
    else:
        with tarfile.open(archive) as bundle:
            for member in bundle.getmembers():
                name = member.name.removeprefix('./')
                if not member.isfile() or name not in (binary, 'LICENSE.txt'):
                    continue
                with bundle.extractfile(member) as source, (destination / name).open('wb') as output:
                    shutil.copyfileobj(source, output)
                found.add(name)
    missing = {binary, 'LICENSE.txt'} - found
    if missing:
        raise SystemExit(f'ocho 压缩包缺少文件：{", ".join(sorted(missing))}')
    if binary != 'ocho.exe':
        (destination / binary).chmod(0o755)


def package(destination):
    if os.environ.get('YOVOICE_OCHO_SKIP') == '1':
        print('跳过 ocho 运行时（YOVOICE_OCHO_SKIP=1），安装包不含 AI 编曲运行时。')
        return
    goos, goarch = current_platform()
    manifest = json.loads((ROOT / 'internal/catalog/ocho.json').read_text())
    name = asset_name(goos, goarch, manifest['version'])
    digest = manifest['archives'].get(name)
    if not digest:
        raise SystemExit(f'ocho 清单缺少 {name}')
    local = os.environ.get('YOVOICE_OCHO_ARCHIVE')
    if local:
        archive = Path(local)
        if not archive.is_file():
            raise SystemExit(f'YOVOICE_OCHO_ARCHIVE 不存在：{local}')
    else:
        archive = ROOT / 'artifacts/downloads' / name
        archive.parent.mkdir(parents=True, exist_ok=True)
        if not archive.exists():
            download(f'{RELEASES}/{manifest["version"]}/{name}', archive)
    if hashlib.sha256(archive.read_bytes()).hexdigest() != digest:
        raise SystemExit(f'ocho 压缩包校验失败：{archive}；请删除后重试或更新 internal/catalog/ocho.json')
    destination.mkdir(parents=True, exist_ok=True)
    extract(archive, 'ocho.exe' if goos == 'windows' else 'ocho', destination)


if __name__ == '__main__':
    package(Path(sys.argv[1]))
