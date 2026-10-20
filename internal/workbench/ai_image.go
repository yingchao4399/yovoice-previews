package workbench

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
	"yovoice/internal/diag"
	"yovoice/internal/llm"
	"yovoice/internal/msg"
	"yovoice/internal/schema"
)

// 生图固定经 Codex CLI 使用 ChatGPT 订阅，与当前选用的写作服务无关；图片存到数据目录的 images/，
// 结果路径写回 AITask，界面取用后可设为作品封面等。

// imagePromptMax 限制画面描述长度，避免把整篇正文当作提示词。
const imagePromptMax = 2000

// ImageBrief 是生图请求：Size 为 square、landscape 或 portrait，空值按方形。
type ImageBrief struct {
	ProjectID string `json:"projectId"`
	Prompt    string `json:"prompt"`
	Size      string `json:"size"`
}

// ImageResult 是生成的图片：ID 即文件名主干，Path 为本机绝对路径。
type ImageResult struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

// StartImage 在后台生成一张图片，与写作任务共用同一个 AI 任务槽，可用 CancelAI 取消。
func (w *Workbench) StartImage(in ImageBrief) (schema.AITask, error) {
	in.Prompt = strings.TrimSpace(in.Prompt)
	if in.Size == "" {
		in.Size = llm.ImageSquare
	}
	if in.Prompt == "" {
		return schema.AITask{}, msg.Err(msg.ErrTextRequired, nil)
	}
	if utf8.RuneCountInString(in.Prompt) > imagePromptMax || !llm.ValidImageSize(in.Size) {
		return schema.AITask{}, msg.Err(msg.ErrRequestInvalid, nil)
	}
	task := schema.AITask{Kind: "image", ProjectID: in.ProjectID, Brief: in.Prompt, Stage: "writing", Provider: "ChatGPT (Codex)"}
	return w.runAI(task, "", func(ctx context.Context) (any, error) {
		timeout := aiTimeout(w.Store.Read().Preferences)
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		dir := filepath.Join(w.Store.Root, "images")
		if e := os.MkdirAll(dir, 0700); e != nil {
			return nil, e
		}
		id := schema.NewID()
		started := time.Now()
		path, e := llm.CodexImage(ctx, in.Prompt, in.Size, filepath.Join(dir, id))
		if e != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
			e = msg.Err(msg.ErrAITimeout, msg.Params{"seconds": int(timeout.Seconds())})
		}
		diag.Log(w.Store.Root, "ai.image", "size", in.Size, "chars", len(in.Prompt), "elapsed_ms", time.Since(started).Milliseconds(), "error", diag.Error(e))
		if e != nil {
			return nil, e
		}
		return ImageResult{ID: id, Path: path}, nil
	})
}
