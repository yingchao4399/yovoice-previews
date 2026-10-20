package workbench

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"yovoice/internal/schema"
)

func TestResourceClipPreservesAudioAndSurvivesOriginalDeletion(t *testing.T) {
	w, e := New(t.TempDir())
	must(t, e)
	defer w.Close()
	d := schema.DefaultDraft()
	d.Kind, d.ModelID = "music", "yue2-3b-q8"
	must(t, w.SaveDraft(d))
	id := schema.NewID()
	file := id + ".wav"
	must(t, os.WriteFile(filepath.Join(w.Store.Root, "outputs", file), wav(), 0600))
	must(t, w.Store.Update(func(s *schema.State) {
		s.History = []schema.Generation{{ID: id, Title: "已生成音乐", FileName: file, Duration: 1, Settings: d}}
	}, true))
	fixed, e := w.AddResourceClip(context.Background(), ResourceClipInput{Draft: d, ResourceID: "generation:" + id, Start: 4, Offset: .2, Duration: .5})
	must(t, e)
	if len(fixed.Timeline.Assets) != 1 || len(fixed.Timeline.Tracks) != 1 || fixed.Timeline.Tracks[0].Clips[0].Start != 4 {
		t.Fatal("没有按指定位置放置可编辑素材")
	}
	asset := fixed.Timeline.Assets[0]
	if asset.FileName == file {
		t.Fatal("应保留原音频，并独立登记工程引用")
	}
	must(t, w.DeleteMedia("outputs", id))
	_, e = w.CleanupResiduals()
	must(t, e)
	data, e := os.ReadFile(filepath.Join(w.Store.Root, "outputs", asset.FileName))
	must(t, e)
	if !bytes.Equal(data, wav()) || !w.Store.Read().MediaRefs()["outputs"][asset.FileName] {
		t.Fatal("原记录删除或清理损坏了工程素材")
	}
	resource, e := w.ResolveResource("asset:" + d.ID + "/" + asset.ID)
	must(t, e)
	if resource.Origin != "ai" || asset.SourceResourceID != "generation:"+id {
		t.Fatal("复制后删除原历史仍须保留AI来源和原身份", resource, asset)
	}
}

func TestResourceReferenceCopiesOneSampleAndRetainsLicenseWithoutAddingTracks(t *testing.T) {
	w, e := New(t.TempDir())
	must(t, e)
	defer w.Close()
	source := inspectionSource(t, w, "箫", 72)
	data := wav()
	must(t, os.WriteFile(filepath.Join(w.Store.Root, "instruments", source.Samples[0].FileName), data, 0600))
	hash := sha256.Sum256(data)
	source.Samples[0].SHA256, source.Samples[0].Duration = hex.EncodeToString(hash[:]), 1
	source.Verification, source.License, source.Author = "user", "CC0-1.0", "sample author"
	must(t, w.Store.Update(func(s *schema.State) { s.InstrumentSources = []schema.InstrumentSource{source} }, true))
	id := "sample:" + source.ID + "/" + source.Samples[0].ID
	before := len(w.Store.Read().Drafts)
	asset, e := w.ImportResourceAudio(context.Background(), id)
	must(t, e)
	if asset.Origin != "real" || asset.SourceResourceID != id || asset.License != source.License || asset.Author != source.Author {
		t.Fatal("单样本来源丢失", asset)
	}
	if len(w.Store.Read().Drafts) != before {
		t.Fatal("选择参考不能自动创建作品或音轨")
	}
	if asset.Duration != source.Samples[0].Duration {
		t.Fatal("未读取完整采样")
	}
}

func TestResourceClipRejectsUnknownPathsAndRealOnlyAI(t *testing.T) {
	w, e := New(t.TempDir())
	must(t, e)
	defer w.Close()
	d := workflowDraft()
	d.Arrangement.SourceMode = "real"
	id := schema.NewID()
	file := id + ".wav"
	must(t, os.WriteFile(filepath.Join(w.Store.Root, "outputs", file), wav(), 0600))
	must(t, w.Store.Update(func(s *schema.State) {
		s.History = []schema.Generation{{ID: id, Title: "AI音乐", FileName: file, Duration: 1, Settings: schema.Draft{SynthesisSettings: schema.SynthesisSettings{ModelID: "yue2-3b-q8"}, Kind: "music"}}}
	}, true))
	if _, e = w.AddResourceClip(context.Background(), ResourceClipInput{Draft: d, ResourceID: "generation:" + id}); e == nil {
		t.Fatal("仅真实模式不能混入AI音频")
	}
	for _, id := range []string{"../../private.wav", "https://example.com/audio.wav", "generation:nonexistent"} {
		if _, e = w.ResolveResource(id); e == nil {
			t.Fatal("不能让模型构造任意文件路径")
		}
	}
}
