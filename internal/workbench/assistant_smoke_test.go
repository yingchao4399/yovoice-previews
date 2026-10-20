package workbench

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"yovoice/internal/schema"
)

// 真实冒烟（本机可选）：设置 ANTHROPIC_BASE_URL、ANTHROPIC_API_KEY、YOVOICE_SMOKE_MODEL 与
// YOVOICE_OCHO_PATH 后运行，用临时数据目录跑一轮“新建编曲作品并写四小节钢琴旋律”的对话，
// 断言作品被创建且乐谱非空；未设置时跳过。
func TestSmokeAssistantChat(t *testing.T) {
	model := os.Getenv("YOVOICE_SMOKE_MODEL")
	baseURL := os.Getenv("ANTHROPIC_BASE_URL")
	key := os.Getenv("ANTHROPIC_API_KEY")
	executable := os.Getenv("YOVOICE_OCHO_PATH")
	if model == "" || baseURL == "" || key == "" || executable == "" {
		t.Skip("未设置 ANTHROPIC_BASE_URL/ANTHROPIC_API_KEY/YOVOICE_SMOKE_MODEL/YOVOICE_OCHO_PATH，跳过真实冒烟")
	}
	if _, err := os.Stat(executable); err != nil {
		t.Skipf("ocho 不可用：%v", err)
	}
	root, err := os.MkdirTemp("", "yovoice-smoke-*")
	must(t, err)
	t.Cleanup(func() { os.RemoveAll(root) })
	w, err := New(root)
	must(t, err)
	t.Cleanup(w.Close)
	_, err = w.SaveAIProvider(AIProviderInput{Preset: "custom", Name: "冒烟", Format: "anthropic_messages", BaseURL: baseURL, Key: &key, Model: model})
	must(t, err)
	t.Setenv("YOVOICE_OCHO_PATH", executable)

	must(t, w.SendAssistant("新建一个编曲作品，写四小节的钢琴旋律，直接写进作品里。", AssistantContext{Page: "score", Locale: schema.UiLocaleZhCN}))
	deadline := time.After(10 * time.Minute)
	for {
		sessions := w.Store.Read().AssistantSessions
		var last schema.AssistantMessage
		if sessions != nil {
			if session := assistantSessionByID(w.Store.Read(), sessions.CurrentID); session != nil && len(session.Messages) > 0 {
				last = session.Messages[len(session.Messages)-1]
			}
		}
		if last.Status != "" && last.Role == "assistant" {
			break
		}
		select {
		case <-time.After(500 * time.Millisecond):
		case <-deadline:
			t.Fatal("冒烟对话超时未结束")
		}
	}
	sessions := w.Store.Read().AssistantSessions
	messages := assistantSessionByID(w.Store.Read(), sessions.CurrentID).Messages
	last := messages[len(messages)-1]
	if last.Status != "completed" {
		t.Fatalf("对话失败：%+v", last)
	}
	var scoreDraft *schema.Draft
	notes := 0
	for _, d := range w.Store.Read().Drafts {
		if d.Kind == "score" && d.Score != nil && strings.Contains(d.Title, "钢琴") || d.Kind == "score" && d.Score != nil {
			for _, track := range d.Score.Tracks {
				notes += len(track.Notes)
			}
			if notes > 0 && scoreDraft == nil {
				scoreDraft = &d
			}
		}
	}
	if scoreDraft == nil || notes == 0 {
		t.Fatalf("作品未被创建或乐谱为空：notes=%d drafts=%d", notes, len(w.Store.Read().Drafts))
	}
	t.Logf("冒烟完成：作品 %q tempo=%g %d 声部 %d 音符，回复 %q", scoreDraft.Title, scoreDraft.Score.Tempo, len(scoreDraft.Score.Tracks), notes, clipRunes(last.Text, 120))
	for _, block := range last.Blocks {
		if block.Card != nil {
			t.Logf("工具卡片：%s %s %s", block.Card.Tool, block.Card.Summary, block.Card.State)
		} else {
			t.Logf("%s：%s", block.Kind, clipRunes(block.Text, 80))
		}
	}

	// 冒烟结束后保留日志便于排查：打印助手子进程日志的尾部。
	if log, err := os.ReadFile(filepath.Join(root, "logs", "assistant.log")); err == nil {
		tail := string(log)
		if r := []rune(tail); len(r) > 1500 {
			tail = string(r[len(r)-1500:])
		}
		t.Logf("assistant.log 尾部：\n%s", tail)
	}
}

func clipRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
