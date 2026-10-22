package workbench

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// 导出的音频写成临时文件，只保留最近一份；不是 WAV 的内容不落盘。
func TestSaveExport(t *testing.T) {
	w, err := New(t.TempDir())
	must(t, err)
	defer w.Close()
	wav := append([]byte("RIFF\x00\x00\x00\x00WAVE"), bytes.Repeat([]byte{1}, 64<<20)...)
	first, err := w.SaveExport(bytes.NewReader(wav))
	must(t, err)
	second, err := w.SaveExport(bytes.NewReader(wav))
	must(t, err)
	if _, err = w.ExportPath(first); err == nil {
		t.Fatal("上一份导出文件应在新导出时删除")
	}
	path, err := w.ExportPath(second)
	must(t, err)
	if info, e := os.Stat(path); e != nil || info.Size() != int64(len(wav)) {
		t.Fatalf("导出文件不完整：%v %v", info, e)
	}
	if _, err = w.SaveExport(strings.NewReader("not a wave file")); err == nil {
		t.Fatal("非 WAV 内容应被拒绝")
	}
	if _, err = w.ExportPath("../" + second); err == nil {
		t.Fatal("无效 ID 应被拒绝")
	}
}
