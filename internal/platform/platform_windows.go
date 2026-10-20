package platform

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func Lock(path string) (*os.File, error) {
	p, e := syscall.UTF16PtrFromString(path)
	if e != nil {
		return nil, e
	}
	h, e := syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if e != nil {
		return nil, e
	}
	return os.NewFile(uintptr(h), path), nil
}
func ConfigureProcess(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
func KillProcess(c *exec.Cmd) {
	if c.Process != nil {
		p := exec.Command("taskkill", "/PID", strconv.Itoa(c.Process.Pid), "/T", "/F")
		ConfigureProcess(p)
		_ = p.Run()
		_ = c.Process.Kill()
	}
}
func FreeSpace(path string) (uint64, error) {
	p, e := syscall.UTF16PtrFromString(path)
	if e != nil {
		return 0, e
	}
	var available uint64
	r, _, e := syscall.NewLazyDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW").Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&available)), 0, 0)
	if r == 0 {
		return 0, e
	}
	return available, nil
}

// serviceJob 是设置了“关闭即结束”的作业对象：本进程退出（包括崩溃或被任务管理器结束）时，
// 系统关闭最后一个句柄并结束作业内的推理进程，不会残留占用显存和锁住内核文件。
var serviceJob = sync.OnceValues(func() (windows.Handle, error) {
	h, e := windows.CreateJobObject(nil, nil)
	if e != nil {
		return 0, e
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE}}
	if _, e = windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); e != nil {
		_ = windows.CloseHandle(h)
		return 0, e
	}
	return h, nil
})

// TieToParent 把已启动的子进程加入 serviceJob。
func TieToParent(c *exec.Cmd) error {
	if c.Process == nil {
		return errors.New("process not started")
	}
	job, e := serviceJob()
	if e != nil {
		return e
	}
	p, e := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(c.Process.Pid))
	if e != nil {
		return e
	}
	defer windows.CloseHandle(p)
	return windows.AssignProcessToJobObject(job, p)
}

// ProcessCommand 在 Windows 上不读取命令行：推理进程已由作业对象随服务结束。
// ProcessCommand 通过 WMI 读取进程命令行，只在启动时回收残留进程用，几秒的开销可以接受。
func ProcessCommand(pid int) (string, error) {
	query := "(Get-CimInstance Win32_Process -Filter 'ProcessId=" + strconv.Itoa(pid) + "').CommandLine"
	c := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", query)
	ConfigureProcess(c)
	out, err := c.Output()
	return strings.TrimSpace(string(out)), err
}

func KillGroup(pid int) {
	p := exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F")
	ConfigureProcess(p)
	_ = p.Run()
}
