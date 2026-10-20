package workbench

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"yovoice/internal/audio"
	"yovoice/internal/schema"
)

func TestResourceRegionExportAndTemporaryCleanupPreserveOriginal(t *testing.T) {
	w, e := New(t.TempDir())
	must(t, e)
	defer w.Close()
	id := schema.NewID()
	file := id + ".wav"
	path := filepath.Join(w.Store.Root, "outputs", file)
	original := wav()
	must(t, os.WriteFile(path, original, 0600))
	d := schema.DefaultDraft()
	d.Kind, d.ModelID = "music", "yue2-3b-q8"
	must(t, w.Store.Update(func(s *schema.State) {
		s.History = []schema.Generation{{ID: id, FileName: file, Title: "保留原曲", Duration: 1, Settings: d}}
	}, true))
	asset, e := w.ResourceRegion(context.Background(), ResourceRegionInput{ID: "generation:" + id, Region: audio.RegionOptions{Seconds: .5, FadeOut: .04}})
	must(t, e)
	if asset.Duration != .5 || asset.Origin != "ai" {
		t.Fatal("未按实际音频生成定长副本", asset)
	}
	must(t, w.DiscardAudioAsset(asset.ID))
	if _, e = os.Stat(filepath.Join(w.Store.Root, "outputs", asset.FileName)); !os.IsNotExist(e) {
		t.Fatal("导出临时文件没有回收")
	}
	data, e := os.ReadFile(path)
	must(t, e)
	if !bytes.Equal(data, original) {
		t.Fatal("导出或清理改坏原曲")
	}
	asset, e = w.ResourceRegion(context.Background(), ResourceRegionInput{ID: "generation:" + id, Region: audio.RegionOptions{Seconds: 2, Pad: true}})
	must(t, e)
	d.Timeline = &schema.AudioTimeline{Assets: []schema.AudioAsset{asset}, Tracks: []schema.AudioLane{}}
	must(t, w.SaveDraft(d))
	if w.DiscardAudioAsset(asset.ID) == nil {
		t.Fatal("已存工程素材不能误删为临时文件")
	}
}
