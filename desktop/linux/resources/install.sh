#!/bin/sh
# 为当前用户安装 yovoice 桌面应用：复制到用户数据目录，登记应用菜单与图标，不需要 root。
# 用法：./install.sh 安装或升级；./install.sh --uninstall 卸载（作品与模型保存在 ~/.yovoice，不会删除）。
set -eu
source=$(cd "$(dirname "$0")" && pwd)
data=${XDG_DATA_HOME:-$HOME/.local/share}
target="$data/yovoice-desktop"
entry="$data/applications/yovoice.desktop"
icon="$data/icons/hicolor/256x256/apps/yovoice.png"

refresh() {
    update-desktop-database "$data/applications" >/dev/null 2>&1 || true
    gtk-update-icon-cache -q -t "$data/icons/hicolor" >/dev/null 2>&1 || true
}

if [ "${1:-}" = "--uninstall" ]; then
    rm -rf "$target" "$entry" "$icon"
    refresh
    echo "yovoice 已卸载 / yovoice has been uninstalled."
    exit 0
fi

if [ "$source" != "$target" ]; then
    rm -rf "$target.new"
    cp -a "$source" "$target.new"
    rm -rf "$target"
    mv "$target.new" "$target"
fi
mkdir -p "$(dirname "$entry")" "$(dirname "$icon")"
cp "$target/resources/yovoice.png" "$icon"
# 路径可能含空格，按桌面条目规范给 Exec 加引号。
sed "s|@EXEC@|\"$target/yovoice-desktop\"|" "$target/resources/yovoice.desktop" > "$entry"
refresh

if ! { ldconfig -p 2>/dev/null || /sbin/ldconfig -p 2>/dev/null; } | grep -q -e 'libwebkit2gtk-4.1.so.0' -e 'libwebkit2gtk-4.0.so.37'; then
    echo "需要 WebKitGTK / WebKitGTK is required:"
    echo "  Debian/Ubuntu: sudo apt install libwebkit2gtk-4.1-0"
    echo "  Fedora:        sudo dnf install webkit2gtk4.1"
    echo "  Arch:          sudo pacman -S webkit2gtk-4.1"
fi
echo "yovoice 已安装，可从应用菜单打开 / yovoice is installed; open it from the application menu."
