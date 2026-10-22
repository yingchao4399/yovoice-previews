package workbench

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"yovoice/internal/audio"
	"yovoice/internal/schema"
	"yovoice/internal/score"
)

// 此用例只证明候选检查、人工确认、采样映射、复用与工程技术链路，不证明模型音色质量。
func TestAISampleConfirmPlayReuseAndArchive(t *testing.T) {
	w, e := New(t.TempDir())
	must(t, e)
	defer w.Close()
	d := workflowDraft()
	d.Arrangement.Sections[0].Phrases = d.Arrangement.Sections[0].Phrases[1:]
	p := &d.Arrangement.Sections[0].Phrases[0]
	p.StartBar = 1
	p.EndBar = 1
	d.Arrangement.Sections[0].EndBar = 1
	d.Arrangement.Seconds = 2
	d.Score.Tracks = d.Score.Tracks[1:]
	d.Score.Tracks[0].Notes = []schema.ScoreNote{{Bar: 1, Beat: 1, Pitch: 69, Length: 1, Velocity: 30}, {Bar: 1, Beat: 2, Pitch: 72, Length: 1, Velocity: 100, Articulation: "tremolo"}}
	_, e = migrateLegacyArrangementIDs(&d)
	must(t, e)
	part := &p.Parts[0]
	settings := generationSettings(*part)
	settings.SoundType = "instrument"
	part.Generation = &settings
	must(t, w.SaveDraft(d))
	must(t, w.Store.Update(func(s *schema.State) { s.RuntimeVersion = ptr("v0.9.1") }, true))
	ref := ArrangementPartRef{0, 0, 0}
	for i, n := range d.Score.Tracks[0].Notes {
		id := schema.NewID()
		file := "import-" + id + ".wav"
		path := filepath.Join(w.Store.Root, "outputs", file)
		pcm := make([]float32, score.SampleRate*2)
		for j := range pcm {
			amp := 1.0
			if n.Articulation == "tremolo" {
				amp = .5 + .5*math.Sin(2*math.Pi*8*float64(j)/score.SampleRate)
			}
			pcm[j] = float32(.2 * amp * math.Sin(2*math.Pi*440*math.Pow(2, float64(n.Pitch-69)/12)*float64(j)/score.SampleRate))
		}
		must(t, score.WriteWAV(path, pcm, pcm))
		rawHash, hashErr := arrangementAudioDigest(path)
		must(t, hashErr)
		low, high := velocityLayer(n.Velocity)
		pitch := n.Pitch
		c := schema.ArrangementGenerationContract{Version: arrangementAdapterVersion, Settings: settings, PartID: part.ID, ScoreHash: arrangementConditionHash(d, ref), CoreSeconds: .5, RequestSeconds: 2, EffectiveSeconds: .5, TailSeconds: .5, Pitch: &pitch, VelocityLow: low, VelocityHigh: high, Articulation: sampleTechnique(*part, n, schema.InstrumentSource{}), ModelID: "ace-step-1.5-turbo-q8", EngineVersion: "v0.9.1"}
		m := schema.ArrangementMaterial{ID: id, ProjectID: d.ID, PartID: part.ID, PlanHash: arrangementJobHash(*d.Arrangement, ref), ScoreHash: c.ScoreHash, Instrument: part.Instrument, ModelID: c.ModelID, FileName: file, Duration: 2, ReviewStatus: "needs_review", Generation: &c, GenerationResult: &schema.ArrangementGenerationResult{State: "needs_fit", ActualSeconds: 2, RawSHA256: rawHash}}
		must(t, w.persistGenerationMaterial(m))
		fitted, e := w.FitArrangementMaterial(ArrangementMaterialFitInput{ID: id, Seconds: .5, TailSeconds: .5})
		must(t, e)
		if fitted.GenerationResult.DetectedPitch == nil || *fitted.GenerationResult.DetectedPitch != pitch {
			t.Fatalf("单音核验 %d：%+v", i, fitted.GenerationResult)
		}
		// 特殊奏法测量结果单独交给试听；不能靠跳过音高检查登记。
		if fitted.GenerationResult.State != "needs_review" {
			t.Fatalf("检查结果：%+v", fitted.GenerationResult)
		}
		// 派生文件损坏后仍可从摘要一致的原片重新适配，不应被旧派生摘要阻断修复。
		must(t, os.Remove(filepath.Join(w.Store.Root, "outputs", fitted.GenerationResult.DerivedFile)))
		fitted, e = w.FitArrangementMaterial(ArrangementMaterialFitInput{ID: id, Seconds: .5, TailSeconds: .5})
		must(t, e)
		if fitted.GenerationResult.State != "needs_review" {
			t.Fatal("原片健康时无法修复丢失的派生文件")
		}
		if i == 0 {
			// 用户确认不依赖播放事件或稳定根音识别；检查仍可处于 checking，
			// 只要原片和适配片完整，确认动作应保留用户决定。
			fitted.GenerationResult.State = "checking"
			fitted.GenerationResult.DetectedPitch = nil
			must(t, w.persistGenerationMaterial(fitted))
		}
		must(t, w.ReviewArrangementMaterial(id, true))
	}
	pre, e := w.ArrangementGenerationPreflight(ArrangementAIPrepareInput{Draft: d, ModelID: "ace-step-1.5-turbo-q8", Target: &ref})
	must(t, e)
	if pre.Reused != 2 {
		t.Fatalf("未复用精确音高/力度/奏法：%+v", pre.Cells)
	}
	inspection, e := w.InspectArrangement(ArrangementInspectInput{Draft: d, Stage: "render"})
	must(t, e)
	if !inspection.Ready {
		t.Fatalf("不能演奏：%+v", inspection.Issues)
	}
	must(t, w.RenderArrangement(ArrangementRenderInput{Draft: d}))
	<-w.Done()
	state := w.Store.Read()
	if state.Activity.Status != "completed" {
		t.Fatalf("按谱演奏失败：%+v", state.Activity)
	}
	file := state.Drafts[0].Timeline.Assets[0].FileName
	q, e := audio.InspectSignal(filepath.Join(w.Store.Root, "outputs", file))
	must(t, e)
	if q.Peak < .003 {
		t.Fatal("渲染静音")
	}
	archive := filepath.Join(t.TempDir(), "ai-samples.yovoice")
	must(t, w.ExportProject(d.ID, archive))
	other, e := New(t.TempDir())
	must(t, e)
	defer other.Close()
	opened, e := other.ImportProject(archive)
	must(t, e)
	if len(other.Store.Read().ArrangementMaterials) != 2 || opened.Score == nil {
		t.Fatal("原片/派生文件/可编辑曲谱未保留")
	}
	refs := other.Store.Read().MediaRefs()
	for _, m := range other.Store.Read().ArrangementMaterials {
		if !refs["outputs"][m.GenerationResult.DerivedFile] {
			t.Fatal("适配文件未纳入引用")
		}
	}
	second := d
	second.ID = schema.NewID()
	pre, e = w.ArrangementGenerationPreflight(ArrangementAIPrepareInput{Draft: second, ModelID: "ace-step-1.5-turbo-q8", Target: &ref})
	must(t, e)
	if pre.Reused != 2 {
		t.Fatal("另一作品不能复用")
	}
}
