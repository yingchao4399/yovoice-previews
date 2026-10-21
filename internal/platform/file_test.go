package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomicReplacesWithoutLeftovers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.json")
	for _, content := range []string{"first", "second"} {
		if e := WriteFileAtomic(path, []byte(content), 0600); e != nil {
			t.Fatal(e)
		}
		if b, e := os.ReadFile(path); e != nil || string(b) != content {
			t.Fatal(string(b), e)
		}
	}
	entries, e := os.ReadDir(dir)
	if e != nil || len(entries) != 1 {
		t.Fatal(entries, e)
	}
	// 目标目录不存在时报错，不留下临时文件。
	if e := WriteFileAtomic(filepath.Join(dir, "missing", "b.json"), []byte("x"), 0600); e == nil {
		t.Fatal("expected error")
	}
}
