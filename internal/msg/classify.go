package msg

import (
	"errors"
	"runtime"
	"syscall"
)

// Classify 把常见的系统错误翻译成可读的错误码；其余错误原样返回。
// 磁盘写满时系统原文（如 "no space left on device"）对用户没有帮助，需要告诉他该做什么。
func Classify(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*CallError); ok {
		return err
	}
	if diskFull(err) {
		return Err(ErrDiskFull, nil)
	}
	return err
}

func diskFull(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	// Windows：ERROR_HANDLE_DISK_FULL(39)、ERROR_DISK_FULL(112)。
	return errno == syscall.ENOSPC || (runtime.GOOS == "windows" && (errno == 39 || errno == 112))
}
