package msg

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"testing"
)

func TestClassifyDiskFull(t *testing.T) {
	err := &os.PathError{Op: "write", Path: "state.json", Err: syscall.ENOSPC}
	if ce, ok := Classify(fmt.Errorf("保存失败：%w", err)).(*CallError); !ok || ce.Code != ErrDiskFull {
		t.Fatal("磁盘已满应翻译为 ErrDiskFull", Classify(err))
	}
	if Encode(err)["code"] != ErrDiskFull {
		t.Fatal(Encode(err))
	}
	other := errors.New("boom")
	if Classify(other) != other || Encode(other)["code"] != ErrUnknown {
		t.Fatal("其他错误应保持原样")
	}
}
