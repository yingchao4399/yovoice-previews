package workbench

import (
	"context"
	"strings"
	"testing"
	"time"
	"yovoice/internal/schema"
)

func pendingDownload(t *testing.T, count int) (*Workbench, schema.AssistantProposal) {
	t.Helper()
	w, e := New(t.TempDir())
	must(t, e)
	t.Cleanup(w.Close)
	session, e := w.NewAssistantSession()
	must(t, e)
	w.chat.mu.Lock()
	w.chat.turn = &chatTurn{id: schema.NewID(), sessionID: session.ID}
	w.chat.mu.Unlock()
	page, e := w.SearchResources(ResourceQuery{Kind: "catalog", Limit: count})
	must(t, e)
	ids := []string{}
	for _, item := range page.Items {
		ids = append(ids, item.CatalogID)
	}
	p, e := w.ProposeResourceDownloads(ResourceDownloadInput{CatalogIDs: ids, Reason: "补齐任务所需音源"})
	must(t, e)
	w.chat.mu.Lock()
	w.chat.turn = nil
	w.chat.mu.Unlock()
	return w, p
}

func TestResourceDownloadProposalRequiresExactManifestAndConfirmation(t *testing.T) {
	w, p := pendingDownload(t, 1)
	if p.State != "pending" || p.Downloads.State != "awaiting_approval" || p.Downloads.Items[0].Manifest != xsxbLibrarySHA256 || len(w.Store.Read().InstrumentSources) != 0 || w.Store.Read().Activity != nil {
		t.Fatal("提议不能开始联网或宣称已下载")
	}
	if e := w.ControlResourceDownload(p.ID, "resume"); e == nil {
		t.Fatal("不能通过重试绕过首次确认")
	}
	tampered := cloneDownloadProposal(p)
	tampered.Downloads.Items[0].Manifest = strings.Repeat("0", 64)
	must(t, w.storeProposal(tampered))
	if e := w.ResolveAssistantProposal(p.ID, true); e == nil {
		t.Fatal("旧/篡改清单不能获准下载")
	}
	if w.Store.Read().Activity != nil {
		t.Fatal("许可校验失败时不能启动下载")
	}
	must(t, w.ResolveAssistantProposal(p.ID, false))
	if w.findProposal(p.ID).Downloads.State != "cancelled" {
		t.Fatal("忽略提议没有保存")
	}
}

func TestResourceDownloadResumeKeepsEarlierCompletedItems(t *testing.T) {
	w, p := pendingDownload(t, 4)
	var installed []schema.InstrumentSource
	for i := 0; i < 3; i++ {
		s := inspectionSource(t, w, "已完成录音", 60+i)
		s.CatalogID = p.Downloads.Items[i].CatalogID
		installed = append(installed, s)
		p.Downloads.Items[i].State = "completed"
	}
	must(t, w.Store.Update(func(s *schema.State) { s.InstrumentSources = installed }, true))
	p.State = "confirmed"
	p.Downloads.State = "paused"
	p.Downloads.Items[3].Manifest = strings.Repeat("0", 64)
	must(t, w.storeProposal(p))
	if e := w.runResourceDownloadBatch(context.Background(), p.ID); e == nil {
		t.Fatal("剩余错误清单应停止")
	}
	after := w.findProposal(p.ID)
	for i := 0; i < 3; i++ {
		if after.Downloads.Items[i].State != "completed" {
			t.Fatal("失败回滚了已完成条目")
		}
	}
	if len(w.Store.Read().InstrumentSources) != 3 || after.Downloads.State != "failed" {
		t.Fatal("没有保留已完成成果并标记失败")
	}
}

func TestResourceDownloadRestartReusesHealthyLocalFiles(t *testing.T) {
	w, p := pendingDownload(t, 1)
	source := inspectionSource(t, w, "本地录音", 60)
	source.CatalogID = p.Downloads.Items[0].CatalogID
	must(t, w.Store.Update(func(s *schema.State) { s.InstrumentSources = []schema.InstrumentSource{source} }, true))
	p.State = "confirmed"
	p.Downloads.State = "queued"
	must(t, w.storeProposal(p))
	root := w.Store.Root
	w.Close()
	other, e := New(root)
	must(t, e)
	defer other.Close()
	changes, stop := other.Store.Subscribe()
	defer stop()
	deadline := time.After(10 * time.Second)
	for {
		current := other.findProposal(p.ID)
		if current != nil && current.Downloads.State == "completed" {
			break
		}
		select {
		case <-changes:
		case <-deadline:
			t.Fatal("重开后没有恢复已确认批次")
		}
	}
	if len(other.Store.Read().InstrumentSources) != 1 || other.Store.Read().InstrumentSources[0].ID != source.ID {
		t.Fatal("健康本地录音被重复下载/替换")
	}
}

func TestAssistantPreparationRequiresLocalSourcesBeforeAnyDownload(t *testing.T) {
	w, e := New(t.TempDir())
	must(t, e)
	defer w.Close()
	d := workflowDraft()
	if e = w.requireLocalPreparationSources(d, nil); e == nil || !strings.Contains(e.Error(), "propose_resource_download") {
		t.Fatal("助手备料应先请求明确下载确认")
	}
	if len(w.Store.Read().InstrumentSources) != 0 || w.Store.Read().Activity != nil {
		t.Fatal("预检不能偷偷下载")
	}
}
