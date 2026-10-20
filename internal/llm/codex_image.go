package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"yovoice/internal/msg"
)

// ChatGPT 订阅生图同样经 Codex CLI：codex exec 调用内置的 image_gen 工具，图片由 CLI 存到
// $CODEX_HOME/generated_images/<thread_id>/，JSONL 事件里不含图片。按 thread.started 的会话 ID
// 找到这次生成的图片，复制到调用方目录后删除 CLI 留下的那一份。

// 图片比例：内置工具按比例出图（实测方形约 1254×1254、横版 1536×1024），不保证精确像素。
const (
	ImageSquare    = "square"
	ImageLandscape = "landscape"
	ImagePortrait  = "portrait"
)

var imageAspects = map[string]string{
	ImageSquare:    "方形（1:1）",
	ImageLandscape: "横版（3:2）",
	ImagePortrait:  "竖版（2:3）",
}

// ValidImageSize 判断比例是否受支持。
func ValidImageSize(size string) bool {
	_, ok := imageAspects[size]
	return ok
}

var imageExts = []string{".png", ".jpg", ".jpeg", ".webp"}

// codexHome 是 CLI 的数据目录，与 CLI 自己的规则一致：CODEX_HOME 优先，否则为 ~/.codex。
func codexHome() string {
	if home := os.Getenv("CODEX_HOME"); home != "" {
		return home
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex")
}

func codexImagePrompt(prompt, size string) string {
	return "使用内置的图像生成工具（image_gen）生成一张" + imageAspects[size] + "图片。" +
		"不要运行命令、读取或修改文件，也不要改用脚本或 API；只生成一张，生成后不需要文字说明。" +
		"如果无法生成，用一句话说明原因。\n\n## 画面描述\n" + prompt + "\n"
}

// CodexImage 用 ChatGPT 订阅生成一张图片，存为 dest 加图片扩展名，返回最终路径。
func CodexImage(ctx context.Context, prompt, size, dest string) (string, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" || !ValidImageSize(size) {
		return "", msg.Err(msg.ErrRequestInvalid, nil)
	}
	path := codexPath()
	if path == "" {
		return "", msg.Err(msg.ErrCodexMissing, nil)
	}
	dir, e := os.MkdirTemp("", "yovoice-codex-")
	if e != nil {
		return "", e
	}
	defer os.RemoveAll(dir)
	last := filepath.Join(dir, "last-message.txt")
	cmd := codexCommand(ctx, path, append(codexExecArgs(dir, last), "-")...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(codexImagePrompt(prompt, size))
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	e = cmd.Run()
	thread := codexThread(stdout.Bytes())
	images := ""
	if thread != "" {
		images = filepath.Join(codexHome(), "generated_images", thread)
		// 生成的图片只为这次请求所用，复制后删除，不在 CLI 目录里积累。
		defer os.RemoveAll(images)
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if e != nil {
		return "", codexError(stdout.Bytes(), stderr.Bytes(), e)
	}
	source := newestImage(images)
	if source == "" {
		text, _ := os.ReadFile(last)
		return "", msg.Err(msg.ErrCodexNoImage, msg.Params{"detail": Sanitize(string(text))})
	}
	target := dest + strings.ToLower(filepath.Ext(source))
	if e = copyFile(source, target); e != nil {
		return "", e
	}
	return target, nil
}

// codexExecArgs 是每次 codex exec 共用的隔离参数：空目录、只读沙箱、忽略用户配置与规则、不保存会话。
func codexExecArgs(dir, last string) []string {
	return []string{"exec", "--ignore-user-config", "--ignore-rules", "--skip-git-repo-check", "--ephemeral", "--sandbox", "read-only", "--cd", dir, "--output-last-message", last, "--json", "--color", "never"}
}

// codexThread 取出 thread.started 事件的会话 ID；ID 带路径分隔符时视为无效。
func codexThread(stdout []byte) string {
	scanner := bufio.NewScanner(bytes.NewReader(stdout))
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	for scanner.Scan() {
		var event struct {
			Type     string `json:"type"`
			ThreadID string `json:"thread_id"`
		}
		if json.Unmarshal(scanner.Bytes(), &event) == nil && event.Type == "thread.started" {
			if id := event.ThreadID; id != "" && id != "." && id != ".." && filepath.Base(id) == id {
				return id
			}
			return ""
		}
	}
	return ""
}

// newestImage 返回目录里最新的图片文件，没有时返回空串。
func newestImage(dir string) string {
	if dir == "" {
		return ""
	}
	entries, _ := os.ReadDir(dir)
	best, bestTime := "", time.Time{}
	for _, entry := range entries {
		if entry.IsDir() || !slices.Contains(imageExts, strings.ToLower(filepath.Ext(entry.Name()))) {
			continue
		}
		if info, e := entry.Info(); e == nil && info.Size() > 0 && !info.ModTime().Before(bestTime) {
			best, bestTime = filepath.Join(dir, entry.Name()), info.ModTime()
		}
	}
	return best
}

func copyFile(source, target string) error {
	in, e := os.Open(source)
	if e != nil {
		return e
	}
	defer in.Close()
	out, e := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if e != nil {
		return e
	}
	if _, e = io.Copy(out, in); e != nil {
		out.Close()
		os.Remove(target)
		return e
	}
	return out.Close()
}
