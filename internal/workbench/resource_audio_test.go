package workbench

import (
	"bytes"
	"context"
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
