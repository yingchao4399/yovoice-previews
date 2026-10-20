package llm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"yovoice/internal/msg"
)

// 按会话 ID 从 CODEX_HOME 取回图片，复制到目标路径后清理 CLI 留下的文件。
func TestCodexImageCopiesGeneratedFile(t *testing.T) {
	dir := withFakeCodex(t, "image")
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	path, err := CodexImage(context.Background(), "月亮上的橘猫", ImageLandscape, filepath.Join(dir, "cover"))
	if err != nil || path != filepath.Join(dir, "cover.png") {
		t.Fatal(path, err)
	}
	if b, _ := os.ReadFile(path); string(b) != "PNGDATA" {
		t.Fatalf("图片内容: %q", b)
	}
	if _, err := os.Stat(filepath.Join(home, "generated_images", "t-1")); !os.IsNotExist(err) {
		t.Fatal("应删除 CLI 留下的图片目录", err)
	}
	args, _ := os.ReadFile(filepath.Join(dir, "args.txt"))
	for _, flag := range []string{"--ignore-user-config", "--ephemeral", "--sandbox read-only", "--json"} {
		if !strings.Contains(string(args), flag) {
			t.Fatalf("缺少参数 %s: %s", flag, args)
		}
	}
	prompt, _ := os.ReadFile(filepath.Join(dir, "prompt.txt"))
	if !strings.Contains(string(prompt), "月亮上的橘猫") || !strings.Contains(string(prompt), "横版") {
		t.Fatalf("提示词应包含画面描述与比例: %s", prompt)
	}
}

func TestCodexImageErrors(t *testing.T) {
	withFakeCodex(t, "noimage")
	t.Setenv("CODEX_HOME", t.TempDir())
	var ce *msg.CallError
	_, err := CodexImage(context.Background(), "x", ImageSquare, filepath.Join(t.TempDir(), "a"))
	if !errors.As(err, &ce) || ce.Code != msg.ErrCodexNoImage || ce.Params["detail"] != "图像生成工具不可用" {
		t.Fatal("没有图片时带上模型的说明", err)
	}
	if _, err = CodexImage(context.Background(), "x", "huge", "a"); !errors.As(err, &ce) || ce.Code != msg.ErrRequestInvalid {
		t.Fatal("不支持的比例", err)
	}
	withFakeCodex(t, "limit")
	if _, err = CodexImage(context.Background(), "x", ImageSquare, "a"); !errors.As(err, &ce) || ce.Code != msg.ErrCodexLimit {
		t.Fatal("额度用完沿用文字请求的错误", err)
	}
}

func TestCodexThreadRejectsPaths(t *testing.T) {
	for line, want := range map[string]string{
		`{"type":"thread.started","thread_id":"abc"}`:  "abc",
		`{"type":"thread.started","thread_id":"../x"}`: "",
		`{"type":"thread.started","thread_id":".."}`:   "",
		`{"type":"turn.started"}`:                      "",
	} {
		if got := codexThread([]byte(line)); got != want {
			t.Fatalf("%s: 得到 %q", line, got)
		}
	}
}
