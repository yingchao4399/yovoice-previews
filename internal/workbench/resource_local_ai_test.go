package workbench

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"yovoice/internal/audio"
	"yovoice/internal/schema"
	"yovoice/internal/score"
)

func TestResourceLocalAISamplePlanPrepareRenderWithoutModelDownload(t *testing.T) {
	w, err := New(t.TempDir())
	must(t, err)
	defer w.Close()
	d := workflowDraft()
	d.Arrangement.SourceMode = "ai"
	d.Arrangement.Sections[0].Phrases = d.Arrangement.Sections[0].Phrases[1:]
	phrase := &d.Arrangement.Sections[0].Phrases[0]
	phrase.StartBar, phrase.EndBar = 1, 2
	d.Score.Tracks = d.Score.Tracks[1:]
	d.Score.Tracks[0].Notes[0].Bar = 1
	source := inspectionSource(t, w, "箫", 67)
	source.Origin, source.Verification = "ai", "user"
	samples := make([]float32, score.SampleRate)
	for i := range samples {
		samples[i] = float32(.2 * math.Sin(2*math.Pi*392*float64(i)/score.SampleRate))
	}
	path := filepath.Join(w.Store.Root, "instruments", source.Samples[0].FileName)
	must(t, score.WriteWAV(path, samples, samples))
	data, err := os.ReadFile(path)
	must(t, err)
	hash := sha256.Sum256(data)
	source.Samples[0].SHA256, source.Samples[0].Duration = hex.EncodeToString(hash[:]), 1
	must(t, w.Store.Update(func(s *schema.State) { s.InstrumentSources = []schema.InstrumentSource{source} }, true))
	if issue := bindPlannedRealSources(ArrangementBrief{Source: "ai"}, d.Arrangement, w.availableArrangementSources("")); issue != "" {
		t.Fatal(issue)
	}
	if phrase.Parts[0].SourceID != source.ID {
		t.Fatal("规划没有复用适用的本地AI单音采样")
	}
	// 没有任何音频生成模型或运行时；本地单音采样不能触发生成或下载。
	must(t, w.PrepareArrangementAI(ArrangementAIPrepareInput{Draft: d}))
	if w.Store.Read().Activity != nil || len(w.Store.Read().Models) != 0 {
		t.Fatal("健康本地AI采样不应触发新生成或模型下载")
	}
	must(t, w.RenderArrangement(ArrangementRenderInput{Draft: d}))
	<-w.Done()
	s := w.Store.Read()
	if s.Activity == nil || s.Activity.Status != "completed" {
		t.Fatalf("实际采样渲染失败：%+v", s.Activity)
	}
	i := slices.IndexFunc(s.Drafts, func(old schema.Draft) bool { return old.ID == d.ID })
	if i < 0 || s.Drafts[i].Timeline == nil || len(s.Drafts[i].Timeline.Assets) != 1 {
		t.Fatal("没有生成可编辑采样轨")
	}
	asset := s.Drafts[i].Timeline.Assets[0]
	if asset.Origin != "ai" || asset.SourceResourceID != "source:"+source.ID {
		t.Fatal("渲染后丢失来源", asset)
	}
	quality, err := audio.InspectSignal(filepath.Join(w.Store.Root, "outputs", asset.FileName))
	must(t, err)
	if quality.Peak < .01 {
		t.Fatal("不能以静音冒充AI采样演奏")
	}
}

func TestResourceBandAppliesOnlyMatchingUnlockedPartsAndPreservesNotes(t *testing.T) {
	w, err := New(t.TempDir())
	must(t, err)
	defer w.Close()
	d := workflowDraft()
	erhu := inspectionSource(t, w, "二胡", 62)
	xiao := inspectionSource(t, w, "箫", 67)
	xiao.Origin = "ai"
	band := schema.BandPreset{ID: schema.NewID(), Name: "本地乐队", Parts: []schema.BandPart{
		{Instrument: "二胡", Role: "melody", Source: "real", SourceID: erhu.ID},
		{Instrument: "箫", Role: "melody", Source: "ai", SourceID: xiao.ID},
	}}
	must(t, w.Store.Update(func(s *schema.State) {
		s.InstrumentSources = []schema.InstrumentSource{erhu, xiao}
		s.BandPresets = []schema.BandPreset{band}
	}, true))
	must(t, w.SaveDraft(d))
	d, err = w.assistantWork(d.ID)
	must(t, err)
	locked := &d.Arrangement.Sections[0].Phrases[0].Parts[0]
	locked.SourceID, locked.Locked = erhu.ID, true
	must(t, w.SaveDraft(d))
	fixed, err := w.ApplyResourceBand(ResourceBandInput{Draft: d, ResourceID: "band:" + band.ID})
	must(t, err)
	if fixed.Arrangement.Sections[0].Phrases[0].Parts[0] != *locked || fixed.Arrangement.Sections[0].Phrases[1].Parts[0].SourceID != xiao.ID {
		t.Fatal("未保留锁定或匹配组合音源")
	}
	beforeNote, afterNote := d.Score.Tracks[1].Notes[0], fixed.Score.Tracks[1].Notes[0]
	// 未指定奏法可由绑定结果补成同义的默认 sustain，旋律、节奏和力度必须保持。
	afterNote.Articulation = beforeNote.Articulation
	if afterNote != beforeNote {
		t.Fatal("套用乐队不能改旋律")
	}
	page, err := w.SearchResources(ResourceQuery{Kind: "band"})
	must(t, err)
	if len(page.Items) != 1 || len(page.Items[0].BandParts) != 2 {
		t.Fatal("助手需要完整乐队成员及预设信息")
	}
}
