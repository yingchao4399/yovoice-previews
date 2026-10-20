package platform

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// 测试二进制目录旁不存在 tools/ocho，覆盖"未随包分发"的场景。
func TestOchoExecutable(t *testing.T) {
	if _, err := OchoExecutable(); !errors.Is(err, ErrOchoMissing) {
		t.Fatalf("未随包分发时应返回 ErrOchoMissing，得到 %v", err)
	}
	t.Setenv("YOVOICE_OCHO_PATH", filepath.Join(t.TempDir(), "ocho"))
	if _, err := OchoExecutable(); !errors.Is(err, ErrOchoMissing) {
		t.Fatalf("环境变量指向缺失文件时应返回 ErrOchoMissing，得到 %v", err)
	}
	local := filepath.Join(t.TempDir(), "ocho")
	if err := os.WriteFile(local, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YOVOICE_OCHO_PATH", local)
	if path, err := OchoExecutable(); err != nil || path != local {
		t.Fatalf("环境变量应优先于包内文件，期望 %q，得到 %q、%v", local, path, err)
	}
}
