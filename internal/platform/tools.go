// Package platform 封装文件锁、子进程控制、磁盘空间与随包工具定位等平台差异。
package platform

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

// ErrToolMissing 表示安装包中缺少随附工具，调用方转换为各自的用户错误。
var ErrToolMissing = errors.New("packaged tool missing")

// ErrOchoMissing 表示 ocho 运行时不可用：安装包未随附，且未通过 YOVOICE_OCHO_PATH 指定。
var ErrOchoMissing = errors.New("ocho runtime unavailable")

// PackagedTool 查找随安装包分发的工具，不依赖系统 PATH。
func PackagedTool(name string) (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return "", err
	}
	directory := filepath.Dir(executable)
	// CLI 的 tools 与可执行文件同级；桌面服务位于 service 子目录。
	bases := []string{directory}
	if filepath.Base(directory) == "service" {
		bases = append(bases, filepath.Dir(directory))
	}
	for _, base := range bases {
		path := filepath.Join(base, "tools", name)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path, nil
		}
	}
	return "", ErrToolMissing
}

// OchoExecutable 定位随包分发的 ocho 运行时可执行文件；环境变量 YOVOICE_OCHO_PATH
// 优先（指向的文件不存在时报错，不静默回退），用于开发时指向本地构建产物。
func OchoExecutable() (string, error) {
	if path := os.Getenv("YOVOICE_OCHO_PATH"); path != "" {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path, nil
		}
		return "", ErrOchoMissing
	}
	name := "ocho/ocho"
	if runtime.GOOS == "windows" {
		name = "ocho/ocho.exe"
	}
	path, err := PackagedTool(name)
	if errors.Is(err, ErrToolMissing) {
		return "", ErrOchoMissing
	}
	return path, err
}
