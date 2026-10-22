package workbench

import (
	"os"
	"path/filepath"
	"testing"
	"yovoice/internal/msg"
	"yovoice/internal/schema"
)

func TestPrepareSynthesisResolvesNativeMusicReference(t *testing.T) {
	w, err := New(t.TempDir())
	must(t, err)
	defer w.Close()

	assetID := schema.NewID()
	assetFile := assetID + ".wav"
	path, err := w.Store.MediaPath("outputs", assetFile)
	must(t, err)
	must(t, os.WriteFile(path, []byte("reference audio"), 0600))

	d := schema.DefaultDraft()
	d.Kind, d.ModelID, d.Text, d.Instrumental = "music", "ace-step-1.5-turbo-bf16", "soft folk", true
	d.Music = &schema.MusicCreationOptions{Duration: schema.DurationIntent{Mode: "auto"}, AccompanimentAssetID: assetID, AccompanimentDescription: "soft and spacious"}
	d.Timeline = &schema.AudioTimeline{Assets: []schema.AudioAsset{{ID: assetID, Name: "reference", FileName: assetFile}}}
	state := w.Store.Read()
	state.Models = []schema.InstalledModel{{ID: d.ModelID, Path: filepath.Join(w.Store.Root, "models", "ace.gguf")}}
	parts, err := w.prepareSynthesis(d, state)
	must(t, err)
	if len(parts) != 1 || parts[0].musicReference != path {
		t.Fatalf("应从作品素材解析 ACE-Step 参考音频路径，got %#v", parts)
	}

	state.Models[0].ID = "yue2-3b-q8"
	d.ModelID = state.Models[0].ID
	if _, err = w.prepareSynthesis(d, state); err == nil {
		t.Fatal("YuE2 不支持参考音频时应在准备阶段明确失败")
	}
}

func TestPrepareSynthesisRejectsMissingMusicReferenceFile(t *testing.T) {
	w, err := New(t.TempDir())
	must(t, err)
	defer w.Close()

	assetID := schema.NewID()
	d := schema.DefaultDraft()
	d.Kind, d.ModelID, d.Text, d.Instrumental = "music", "ace-step-1.5-turbo-bf16", "soft folk", true
	d.Music = &schema.MusicCreationOptions{Duration: schema.DurationIntent{Mode: "auto"}, AccompanimentAssetID: assetID}
	d.Timeline = &schema.AudioTimeline{Assets: []schema.AudioAsset{{ID: assetID, Name: "missing reference", FileName: assetID + ".wav"}}}
	state := w.Store.Read()
	state.Models = []schema.InstalledModel{{ID: d.ModelID, Path: filepath.Join(w.Store.Root, "models", "ace.gguf")}}
	_, err = w.prepareSynthesis(d, state)
	if !isCode(err, msg.ErrAudioMissing) {
		t.Fatalf("素材文件缺失时应在生成前明确报错，实际: %v", err)
	}
}
