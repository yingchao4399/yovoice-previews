package diag

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenProcessLogAppendsAndRolls(t *testing.T) {
	path := filepath.Join(t.TempDir(), "engine.log")
	for i := 0; i < 2; i++ {
		f, err := OpenProcessLog(path, "engine")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.WriteString("line\n")
		_ = f.Close()
	}
	data, _ := os.ReadFile(path)
	if strings.Count(string(data), "line\n") != 2 {
		t.Fatalf("重复启动应追加到同一文件: %q", data)
	}
	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Fatal("未超过上限时不应滚动")
	}
	if err := os.WriteFile(path, make([]byte, processLogLimit), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := OpenProcessLog(path, "engine")
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if info, err := os.Stat(path + ".1"); err != nil || info.Size() != processLogLimit {
		t.Fatal("超过上限时应滚动为 .1")
	}
}
