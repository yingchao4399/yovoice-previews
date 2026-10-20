package llm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"yovoice/internal/msg"
)

// 假的 codex：按子命令输出固定内容；exec 记录参数和输入，按 FAKE_CODEX 模拟成功、生图、未出图、未登录、额度用完或卡住。
const fakeCodex = `#!/bin/sh
case "$1" in
--version) echo "codex-cli 9.9.9"; exit 0 ;;
login) if [ "$2" = "status" ]; then echo "Logged in using ChatGPT"; exit 0; fi; exit 0 ;;
debug) echo '{"models":[{"slug":"hidden","visibility":"hide"},{"slug":"gpt-a","visibility":"list"},{"slug":"gpt-b","visibility":"list"}]}'; exit 0 ;;
exec)
  echo "$@" > "$FAKE_DIR/args.txt"
  cat > "$FAKE_DIR/prompt.txt"
  out=""
  while [ $# -gt 0 ]; do if [ "$1" = "--output-last-message" ]; then out="$2"; fi; shift; done
  case "$FAKE_CODEX" in
  ok) echo '{"type":"turn.started"}'; printf '{"answer":"好"}' > "$out"; exit 0 ;;
  loggedout) echo '{"type":"error","message":"Not logged in. Run codex login."}'; exit 1 ;;
  limit) echo '{"type":"turn.failed","error":{"message":"You have hit your usage limit."}}'; exit 1 ;;
  image) echo '{"type":"thread.started","thread_id":"t-1"}'; mkdir -p "$CODEX_HOME/generated_images/t-1"; printf 'PNGDATA' > "$CODEX_HOME/generated_images/t-1/exec-1.png"; exit 0 ;;
  noimage) echo '{"type":"thread.started","thread_id":"t-2"}'; printf '图像生成工具不可用' > "$out"; exit 0 ;;
  hang) sleep 30 ;;
  esac
  echo "boom" >&2; exit 2 ;;
esac
`

func withFakeCodex(t *testing.T, mode string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("假 codex 用 shell 脚本实现")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "codex")
	if err := os.WriteFile(path, []byte(fakeCodex), 0700); err != nil {
		t.Fatal(err)
	}
	saved := codexPath
	codexPath = func() string { return path }
	t.Cleanup(func() { codexPath = saved })
	t.Setenv("FAKE_DIR", dir)
	t.Setenv("FAKE_CODEX", mode)
	return dir
}

func TestCodexStatusAndModels(t *testing.T) {
	withFakeCodex(t, "ok")
	info := CodexStatus(context.Background())
	if info.Version != "9.9.9" || info.Login != "chatgpt" || info.Path == "" {
		t.Fatalf("检测结果: %+v", info)
	}
	models, err := Models(context.Background(), nil, Config{Format: FormatCodex})
	if err != nil || strings.Join(models, ",") != "gpt-a,gpt-b" {
		t.Fatal("只列出可见模型", models, err)
	}
	saved := codexPath
	codexPath = func() string { return "" }
	defer func() { codexPath = saved }()
	if info := CodexStatus(context.Background()); info.Path != "" {
		t.Fatal("未安装时返回空结果")
	}
	var ce *msg.CallError
	if _, err := Complete(context.Background(), nil, Config{Format: FormatCodex}, Request{Messages: []Message{{Role: "user", Content: "x"}}}); !errors.As(err, &ce) || ce.Code != msg.ErrCodexMissing {
		t.Fatal("未安装时提示安装", err)
	}
}

func TestCodexCompleteRunsIsolated(t *testing.T) {
	dir := withFakeCodex(t, "ok")
	result, err := Complete(context.Background(), nil, Config{Format: FormatCodex, Model: "gpt-a"}, Request{System: "写一句歌词", Messages: []Message{{Role: "user", Content: "夏天"}, {Role: "assistant", Content: "旧回答"}, {Role: "user", Content: "改短"}}})
	if err != nil || result.Text != `{"answer":"好"}` {
		t.Fatal(result, err)
	}
	args, _ := os.ReadFile(filepath.Join(dir, "args.txt"))
	for _, flag := range []string{"--ignore-user-config", "--ignore-rules", "--ephemeral", "--sandbox read-only", "--model gpt-a", "--json"} {
		if !strings.Contains(string(args), flag) {
			t.Fatalf("缺少参数 %s: %s", flag, args)
		}
	}
	prompt, _ := os.ReadFile(filepath.Join(dir, "prompt.txt"))
	if !strings.Contains(string(prompt), "写一句歌词") || !strings.Contains(string(prompt), "旧回答") || strings.Index(string(prompt), "夏天") > strings.Index(string(prompt), "改短") {
		t.Fatalf("提示词应按顺序包含系统要求与多轮消息: %s", prompt)
	}
}

func TestCodexErrors(t *testing.T) {
	request := Request{Messages: []Message{{Role: "user", Content: "x"}}}
	for mode, want := range map[string]msg.Code{"loggedout": msg.ErrCodexLoggedOut, "limit": msg.ErrCodexLimit, "crash": msg.ErrCodexFailed} {
		t.Run(mode, func(t *testing.T) {
			withFakeCodex(t, mode)
			var ce *msg.CallError
			_, err := Complete(context.Background(), nil, Config{Format: FormatCodex}, request)
			if !errors.As(err, &ce) || ce.Code != want {
				t.Fatalf("得到 %v，应为 %s", err, want)
			}
			if mode == "crash" && ce.Params["detail"] != "boom" {
				t.Fatalf("应带上 CLI 的错误输出: %v", ce.Params)
			}
		})
	}
	withFakeCodex(t, "hang")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := Complete(ctx, nil, Config{Format: FormatCodex}, request); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("超时应结束 CLI 进程", err)
	}
	if time.Since(started) > 10*time.Second {
		t.Fatal("取消后没有及时结束")
	}
}
