package workbench

import (
	"os"
	"path/filepath"
	"testing"
	"yovoice/internal/schema"
)

func TestResourcesDistinguishSamplesClipsAndMissingFiles(t *testing.T) {
	w, e := New(t.TempDir())
	must(t, e)
	defer w.Close()
	source := inspectionSource(t, w, "琵琶", 60)
	source.Verification = "user"
	ai := inspectionSource(t, w, "箫", 72)
	ai.Origin = "ai"
	ai.Verification = "user"
	file := "test-generated.wav"
	must(t, os.WriteFile(filepath.Join(w.Store.Root, "outputs", file), []byte("audio fixture"), 0600))
	must(t, w.Store.Update(func(s *schema.State) {
		s.InstrumentSources = []schema.InstrumentSource{source, ai}
		s.History = []schema.Generation{{ID: schema.NewID(), Title: "已有歌曲", FileName: file, Duration: 90}}
	}, true))
	page, e := w.SearchResources(ResourceQuery{Kind: "sample", ParentID: "source:" + source.ID})
	must(t, e)
	if page.Total != 1 || page.Items[0].SampleID != source.Samples[0].ID || page.Items[0].Usage != "notes" || page.Items[0].Status != "ready" {
		t.Fatalf("未返回真实单音采样：%+v", page)
	}
	page, e = w.SearchResources(ResourceQuery{Kind: "sample", Origin: "ai"})
	must(t, e)
	if page.Total != 1 || page.Items[0].SourceID != ai.ID {
		t.Fatal("已生成AI采样不能丢失来源")
	}
	page, e = w.SearchResources(ResourceQuery{Kind: "audio"})
	must(t, e)
	if page.Total != 1 || page.Items[0].Usage != "clip" || page.Items[0].Duration != 90 {
		t.Fatal("整曲音频不能冒充单音采样")
	}
	must(t, os.Remove(filepath.Join(w.Store.Root, "instruments", source.Samples[0].FileName)))
	page, e = w.SearchResources(ResourceQuery{Kind: "sample", ParentID: "source:" + source.ID})
	must(t, e)
	if page.Items[0].Status != "repair" || page.Items[0].Issue != "SOURCE_FILE_MISSING" {
		t.Fatalf("丢失文件不能报已下载可用：%+v", page)
	}
}

func TestResourceCatalogIsPagedAndDoesNotInstall(t *testing.T) {
	w, e := New(t.TempDir())
	must(t, e)
	defer w.Close()
	page, e := w.SearchResources(ResourceQuery{Kind: "catalog", Limit: 2})
	must(t, e)
	if len(page.Items) != 2 || page.Total < 10 || page.NextOffset == nil {
		t.Fatalf("目录未分页：%+v", page)
	}
	for _, item := range page.Items {
		if item.Status != "download" || item.License != "CC0-1.0" || item.Manifest != xsxbLibrarySHA256 || item.Bytes < 1 {
			t.Fatalf("候选许可或大小不准确：%+v", item)
		}
	}
	second, e := w.SearchResources(ResourceQuery{Kind: "catalog", Limit: 2, Offset: *page.NextOffset})
	must(t, e)
	if second.Items[0].ID == page.Items[0].ID || len(w.Store.Read().InstrumentSources) != 0 || w.Store.Read().Activity != nil {
		t.Fatal("查询不应下载或篡改库存")
	}
}
