//go:build !windows

package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
	"yovoice/internal/platform"
)

// 服务异常退出后残留的推理进程在下次启动时结束；PID 被其他程序复用时不能误杀。
func TestReapOrphan(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"runtime", "logs"} {
		must(t, os.MkdirAll(filepath.Join(root, dir), 0700))
	}
	start := func(args ...string) *exec.Cmd {
		c := exec.Command("sh", append([]string{"-c", "sleep 30; true"}, args...)...)
		platform.ConfigureProcess(c)
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { platform.KillProcess(c); _ = c.Wait() })
		return c
	}
	exited := func(c *exec.Cmd) bool {
		done := make(chan struct{})
		go func() { _ = c.Wait(); close(done) }()
		select {
		case <-done:
			return true
		case <-time.After(3 * time.Second):
			return false
		}
	}
	other := start("unrelated")
	must(t, os.WriteFile(pidPath(root), []byte(strconv.Itoa(other.Process.Pid)), 0600))
	ReapOrphan(root)
	if err := other.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatal("不应结束无关进程", err)
	}
	orphan := start(filepath.Join(root, "runtime", "server.json"))
	must(t, os.WriteFile(pidPath(root), []byte(strconv.Itoa(orphan.Process.Pid)), 0600))
	ReapOrphan(root)
	if !exited(orphan) {
		t.Fatal("残留的推理进程应被结束")
	}
	if _, err := os.Stat(pidPath(root)); !os.IsNotExist(err) {
		t.Fatal("清理后应删除 PID 文件")
	}
}
