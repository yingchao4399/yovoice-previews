package desktop

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"yovoice/internal/schema"
	"yovoice/internal/workbench"
)

// 助手的瞬时状态不能反复传送整库；真实窗口由同一事件流接收这些更新。
func TestAssistantStreamDoesNotResendUnchangedLibrary(t *testing.T) {
	w, err := workbench.New(t.TempDir())
	must(t, err)
	defer w.Close()
	must(t, w.Store.Update(func(s *schema.State) { s.Drafts[0].Text = strings.Repeat("large-project-data ", 60000) }, false))
	secret := strings.Repeat("b", 64)
	server := httptest.NewServer(&Server{Workbench: w, Assets: t.TempDir(), Secret: secret})
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/state-events", nil)
	must(t, err)
	req.AddCookie(&http.Cookie{Name: "vw-" + secret[:12], Value: secret})
	res, err := server.Client().Do(req)
	must(t, err)
	defer res.Body.Close()
	reader := bufio.NewReader(res.Body)
	first, err := reader.ReadString('\n')
	must(t, err)
	if len(first) < 1<<20 {
		t.Fatal("没有建立具有整库负载的基线")
	}
	_, err = reader.ReadString('\n')
	must(t, err)
	must(t, w.Store.Update(func(s *schema.State) { s.AssistantLive = &schema.AssistantLive{Queued: 1} }, false))
	next, err := reader.ReadString('\n')
	must(t, err)
	if len(next) > 4096 || strings.Contains(next, `"drafts"`) {
		t.Fatalf("助手队列更新重发了未变整库：%d bytes，期望仅传瞬时变化字段", len(next))
	}
	t.Logf("完整状态 %d bytes；助手单次增量 %d bytes", len(first), len(next))
}
