package workbench

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"yovoice/internal/msg"
)

// 假的 codex 放在 PATH 上：exec 时把图片写到 CODEX_HOME 下这次会话的目录。
const fakeImageCodex = `#!/bin/sh
cat > /dev/null
echo '{"type":"thread.started","thread_id":"t-img"}'
mkdir -p "$CODEX_HOME/generated_images/t-img"
printf 'PNGDATA' > "$CODEX_HOME/generated_images/t-img/exec-1.png"
`

// 生图不依赖当前写作服务，图片存到数据目录的 images/，路径写回任务结果。
func TestStartImageSavesIntoDataRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("假 codex 用 shell 脚本实现")
	}
	bin := t.TempDir()
	must(t, os.WriteFile(filepath.Join(bin, "codex"), []byte(fakeImageCodex), 0700))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CODEX_HOME", t.TempDir())
	w, err := New(t.TempDir())
	must(t, err)
	defer w.Close()
	var ce *msg.CallError
	if _, err = w.StartImage(ImageBrief{Prompt: "  "}); !errors.As(err, &ce) || ce.Code != msg.ErrTextRequired {
		t.Fatal("空描述应拒绝", err)
	}
	if _, err = w.StartImage(ImageBrief{Prompt: "猫", Size: "wide"}); !errors.As(err, &ce) || ce.Code != msg.ErrRequestInvalid {
		t.Fatal("不支持的比例应拒绝", err)
	}
	task, err := w.StartImage(ImageBrief{ProjectID: "p1", Prompt: "月亮上的橘猫"})
	must(t, err)
	if task.Kind != "image" || task.Status != "running" {
		t.Fatalf("任务: %+v", task)
	}
	done := waitAI(t, w)
	if done.Status != "completed" {
		t.Fatalf("任务应完成: %+v", done)
	}
	var result ImageResult
	must(t, json.Unmarshal(done.Result, &result))
	if result.Path != filepath.Join(w.Store.Root, "images", result.ID+".png") {
		t.Fatalf("图片路径: %+v", result)
	}
	if b, _ := os.ReadFile(result.Path); string(b) != "PNGDATA" {
		t.Fatalf("图片内容: %q", b)
	}
}
