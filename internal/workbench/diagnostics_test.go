package workbench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"yovoice/internal/diag"
	"yovoice/internal/schema"
)

func TestSaveDiagnostics(t *testing.T) {
	w, err := New(t.TempDir())
	must(t, err)
	defer w.Close()
	d := schema.DefaultDraft()
	d.Text = "不应出现在日志中的正文"
	d.ReferenceText = "不应出现在日志中的参考文本"
	must(t, w.SaveDraft(d))
	broken := d
	broken.Kind = "invalid"
	if err = w.SaveDraft(broken); err == nil {
		t.Fatal("应拒绝无效作品")
	}
	// 模拟回滚日志被目录占用，保留具体的数据库错误。
	must(t, os.Mkdir(filepath.Join(w.Store.Root, "yovoice.db-journal"), 0700))
	// 内容未变的保存不写盘，需带上改动才会触发写入。
	d.Title = "改动后的标题"
	if err = w.SaveDraft(d); err == nil {
		t.Fatal("应报告写入失败")
	}
	b, err := os.ReadFile(filepath.Join(w.Store.Root, "logs", "service.log"))
	must(t, err)
	for _, marker := range []string{"draft.save", "validate_kind", "state.write_failed", "yovoice.db", "detail", d.ID} {
		if !strings.Contains(string(b), marker) {
			t.Fatal("缺少诊断字段", marker)
		}
	}
	if strings.Contains(string(b), d.Text) || strings.Contains(string(b), d.ReferenceText) {
		t.Fatal("日志泄露正文")
	}
}

func TestDiagnosticRotationAndConcurrency(t *testing.T) {
	root := t.TempDir()
	must(t, os.Mkdir(filepath.Join(root, "logs"), 0700))
	path := filepath.Join(root, "logs", "service.log")
	must(t, os.WriteFile(path, make([]byte, 5<<20), 0600))
	var tasks sync.WaitGroup
	for i := range 20 {
		tasks.Add(1)
		go func() { defer tasks.Done(); diag.Log(root, "check", "index", i) }()
	}
	tasks.Wait()
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	must(t, err)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 20 {
		t.Fatal("并发写入丢失")
	}
	for _, line := range lines {
		if !json.Valid([]byte(line)) {
			t.Fatal("日志记录交错")
		}
	}
	// 日志目录不可写不影响业务结果。
	w, err := New(t.TempDir())
	must(t, err)
	defer w.Close()
	must(t, os.RemoveAll(filepath.Join(w.Store.Root, "logs")))
	must(t, os.WriteFile(filepath.Join(w.Store.Root, "logs"), nil, 0600))
	must(t, w.SaveDraft(schema.DefaultDraft()))
}

// 无法使用的 AI 输出只记形态（长度、行数、是否代码块或 JSON），不记内容，避免歌词进入日志。
func TestInvalidAIOutputLogsShapeOnly(t *testing.T) {
	w, err := New(t.TempDir())
	must(t, err)
	defer w.Close()
	w.logAIOutput("lyrics", "missing style", "```\n[verse]\n不该出现在日志里的歌词\n```")
	b, err := os.ReadFile(filepath.Join(w.Store.Root, "logs", "service.log"))
	must(t, err)
	if strings.Contains(string(b), "不该出现在日志里的歌词") {
		t.Fatal("日志泄露 AI 输出内容")
	}
	for _, field := range []string{`"msg":"ai.output_invalid"`, `"issue":"missing style"`, `"lines":4`, `"fenced":true`, `"json":false`} {
		if !strings.Contains(string(b), field) {
			t.Fatal("缺少字段", field, string(b))
		}
	}
}
