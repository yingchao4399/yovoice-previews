package main

import (
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ebitengine/purego"
)

// GTK 只能在主线程调用：ui 把函数排进 GLib 主循环，uiWait 在后台 goroutine 中等它返回。
var uiQueue struct {
	sync.Mutex
	fns []func()
}

var idleCallback = purego.NewCallback(func(data uintptr) int32 {
	uiQueue.Lock()
	fns := uiQueue.fns
	uiQueue.fns = nil
	uiQueue.Unlock()
	for _, fn := range fns {
		fn()
	}
	return 0 // G_SOURCE_REMOVE
})

func ui(fn func()) {
	uiQueue.Lock()
	uiQueue.fns = append(uiQueue.fns, fn)
	uiQueue.Unlock()
	gIdleAddFull(0, idleCallback, 0, 0) // G_PRIORITY_DEFAULT
}

// uiWait 只能在后台 goroutine 中调用，在主线程上调用会死锁。
func uiWait[T any](fn func() T) T {
	result := make(chan T, 1)
	ui(func() { result <- fn() })
	return <-result
}

// alert 显示模态提示并返回所选按钮的序号；关闭对话框返回 -1。
func alert(parent uintptr, title, message string, buttons ...string) int {
	dialog := gtkMessageDialogNew(parent, 1|2, 1, 0, "%s", title) // MODAL|DESTROY_WITH_PARENT, WARNING, BUTTONS_NONE
	if message != "" {
		gtkMessageDialogFormatSecondaryText(dialog, "%s", message)
	}
	gtkWindowSetTitle(dialog, "yovoice")
	if len(buttons) == 0 {
		buttons = []string{T("好", "OK")}
	}
	for i, label := range buttons {
		gtkDialogAddButton(dialog, label, int32(i))
	}
	gtkDialogSetDefaultResponse(dialog, 0)
	response := gtkDialogRun(dialog)
	gtkWidgetDestroy(dialog)
	if response < 0 {
		return -1
	}
	return int(response)
}

type fileFilter struct {
	name       string
	extensions []string
}

const (
	chooseOpen   = 0
	chooseSave   = 1
	chooseFolder = 2
)

// choose 显示系统文件选择器（沙箱环境中自动走 xdg-desktop-portal），取消时返回空字符串。
// 保存时补齐扩展名：GTK 不像 Windows 对话框那样按筛选器自动追加。
func choose(parent uintptr, action int32, title, name string, filters ...fileFilter) string {
	accept := T("打开", "_Open")
	switch action {
	case chooseSave:
		accept = T("保存", "_Save")
	case chooseFolder:
		accept = T("选择", "_Select")
	}
	chooser := gtkFileChooserNativeNew(title, parent, action, accept, T("取消", "_Cancel"))
	defer gObjectUnref(chooser)
	if action == chooseSave {
		gtkFileChooserSetDoOverwriteConfirm(chooser, true)
		if name != "" {
			gtkFileChooserSetCurrentName(chooser, name)
		}
	}
	for _, f := range filters {
		filter := gtkFileFilterNew()
		gtkFileFilterSetName(filter, f.name)
		for _, extension := range f.extensions {
			gtkFileFilterAddPattern(filter, "*."+extension)
			gtkFileFilterAddPattern(filter, "*."+strings.ToUpper(extension))
		}
		gtkFileChooserAddFilter(chooser, filter)
	}
	if gtkNativeDialogRun(chooser) != -3 { // GTK_RESPONSE_ACCEPT
		return ""
	}
	path := takeString(gtkFileChooserGetFilename(chooser))
	if action == chooseSave && len(filters) > 0 && len(filters[0].extensions) > 0 {
		extension := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
		matched := false
		for _, e := range filters[0].extensions {
			matched = matched || extension == e
		}
		if !matched {
			path += "." + filters[0].extensions[0]
		}
	}
	return path
}

// openPath 用默认程序（通常是文件管理器）打开目录或网址。
func openPath(target string) error {
	cmd := exec.Command("xdg-open", target)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// reveal 在文件管理器中选中文件；文件管理器不支持 FileManager1 接口时打开所在目录。
func reveal(path string) error {
	uri := (&url.URL{Scheme: "file", Path: path}).String()
	if exec.Command("dbus-send", "--session", "--print-reply", "--dest=org.freedesktop.FileManager1", "--type=method_call",
		"/org/freedesktop/FileManager1", "org.freedesktop.FileManager1.ShowItems", "array:string:"+uri, "string:").Run() == nil {
		return nil
	}
	return openPath(filepath.Dir(path))
}

// writeAtomic 先写临时文件再替换，避免导出到一半时留下损坏的目标文件。
func writeAtomic(path string, data []byte) error {
	temporary := path + "." + newID() + ".tmp"
	if err := os.WriteFile(temporary, data, 0644); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}
