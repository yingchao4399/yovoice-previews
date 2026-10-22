// yovoice 的 Linux 桌面宿主：GTK 3 窗口 + WebKitGTK 网页视图，经 purego 动态加载，不依赖 cgo。
// 与 Windows、macOS 宿主一样只负责窗口、文件对话框和本地服务的生命周期，业务全部在 Go 服务中。
package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
)

// GTK 只能在创建它的线程上调用，主 goroutine 固定在主线程。
func init() { runtime.LockOSThread() }

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// fatal 在 GTK 不可用时也尽量让双击启动的用户看到原因。
func fatal(err error) error {
	_ = exec.Command("zenity", "--error", "--no-wrap", "--title=yovoice", "--text="+err.Error()).Run()
	return err
}

func run() error {
	// WebKitGTK 的 DMA-BUF 渲染在 NVIDIA 驱动、虚拟机和部分容器中会显示空白窗口，默认关闭。
	if os.Getenv("WEBKIT_DISABLE_DMABUF_RENDERER") == "" {
		_ = os.Setenv("WEBKIT_DISABLE_DMABUF_RENDERER", "1")
	}
	if err := loadLibraries(); err != nil {
		return fatal(err)
	}
	// 窗口类名与 .desktop 文件的 StartupWMClass 一致，任务栏才能把窗口归到应用图标下。
	gSetPrgname("yovoice")
	if !gtkInitCheck(0, 0) {
		return fmt.Errorf("%s", T("无法打开显示（请确认 DISPLAY 或 WAYLAND_DISPLAY 已设置）。", "Cannot open a display (is DISPLAY or WAYLAND_DISPLAY set?)."))
	}
	lock, err := singleInstance()
	if err != nil {
		alert(0, T("yovoice 已在运行", "yovoice is already running"), T("请切换到已打开的 yovoice 窗口。", "Switch to the yovoice window that is already open."))
		return nil
	}
	defer lock.Close()
	root, ok := dataRoot()
	if !ok {
		return nil
	}
	loadLocale(root)
	a := newApp(root)
	a.build()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		for range signals {
			go a.requestExit()
		}
	}()
	go a.start()
	gtkMain()
	if !a.finished() {
		a.svc.Stop()
	}
	return nil
}

// singleInstance 以文件锁保证每个用户只运行一个实例；进程退出时锁自动释放。
func singleInstance() (*os.File, error) {
	directory := os.Getenv("XDG_RUNTIME_DIR")
	if directory == "" {
		directory = os.TempDir()
	}
	file, err := os.OpenFile(filepath.Join(directory, "yovoice-"+strconv.Itoa(os.Getuid())+".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

// dataRoot 解析数据目录：环境变量优先，其次是“设置 › 存储”写入的重定向配置，最后回退到 ~/.yovoice。
// 自定义目录不存在时（外置盘未挂载、目录被移动或删除）不能静默新建空目录，由用户选择重试、新建或退出。
func dataRoot() (string, bool) {
	if path := os.Getenv("WORKBENCH_DATA"); path != "" {
		absolute, err := filepath.Abs(path)
		return absolute, err == nil
	}
	configured, err := configuredDataRoot()
	if err != nil {
		alert(0, T("存储配置无法读取", "Storage Settings Can't Be Read"), err.Error(), T("退出", "Quit"))
		return "", false
	}
	if configured == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			alert(0, T("找不到用户目录", "Home Folder Not Found"), err.Error(), T("退出", "Quit"))
			return "", false
		}
		return filepath.Join(home, ".yovoice"), true
	}
	for {
		if info, err := os.Stat(configured); err == nil && info.IsDir() {
			return configured, true
		}
		switch alert(0, T("找不到数据目录", "Data Folder Not Found"), T(configured+" 不存在。可能是外置磁盘未连接，或目录已被移动、删除。\n\n连接磁盘后点“重试”；确认不再需要原数据时，可以在原位置新建空的数据目录。", configured+" does not exist. The external drive may be disconnected, or the folder was moved or deleted.\n\nReconnect the drive and click Retry. If you no longer need the original data, you can create an empty data folder at the same location."), T("重试", "Retry"), T("退出", "Quit"), T("新建空目录", "Create Empty Folder")) {
		case 0:
			continue
		case 2:
			if err := os.MkdirAll(configured, 0700); err != nil {
				alert(0, T("无法创建数据目录", "Could Not Create Data Folder"), err.Error())
				continue
			}
			return configured, true
		default:
			return "", false
		}
	}
}
