package platform

import (
	"os"
	"path/filepath"
	"runtime"
)

// WriteFileAtomic 先在同一目录写临时文件并落盘，再改名替换目标：中途失败或断电时，
// 读取方只会看到旧文件或完整的新文件，不会看到写到一半的内容。临时文件名随机，并发写同一路径也不会互相覆盖。
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	f, e := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(data); e == nil {
		e = f.Chmod(perm)
	}
	if e == nil {
		e = f.Sync()
	}
	if ce := f.Close(); e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	if e = os.Rename(f.Name(), path); e != nil {
		return e
	}
	SyncDir(filepath.Dir(path))
	return nil
}

// SyncDir 把目录项（新建、改名）落盘；Windows 不支持打开目录同步，NTFS 的元数据日志已保证改名持久。
func SyncDir(dir string) {
	if runtime.GOOS == "windows" {
		return
	}
	if f, e := os.Open(dir); e == nil {
		_ = f.Sync()
		_ = f.Close()
	}
}
