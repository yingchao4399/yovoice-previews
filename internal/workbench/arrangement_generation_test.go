package workbench

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"yovoice/internal/schema"
	"yovoice/internal/score"
	"yovoice/internal/testkit"
)

func generationTestSettings() *schema.ArrangementGenerationSettings {
	return &schema.ArrangementGenerationSettings{AssetKind: "phrase", Mode: "free", Method: "direct", LengthMode: "phrase", SoundType: "instrument", TailSeconds: 0}
}

func TestSemanticBudgetDoesNotAddAnExtraFloatingPointFrame(t *testing.T) {
	c := schema.ArrangementGenerationContract{CoreSeconds: 1.730769230769, TailSeconds: .5}
	setGenerationBudget(&c, ArrangementGenerationCapability{Family: "yue2", MinDuration: .04, DurationGranularity: .04})
	if c.SemanticMax != 56 || math.Abs(c.RequestSeconds-2.24) > .000001 {
		t.Fatalf("浮点预算错误：%+v", c)
	}
}

func TestAIGenerationCacheTracksOnlyRelevantScoreTempo(t *testing.T) {
	d := workflowDraft()
	ref := ArrangementPartRef{0, 1, 0}
	before := arrangementConditionHash(d, ref)
	d.Score.Sections = []schema.ScoreSection{{Name: "其他句", Start: 1, End: 1, Tempo: 60}}
	if arrangementConditionHash(d, ref) != before {
		t.Fatal("其他句变速不应使本句候选失效")
	}
	d.Score.Sections = append(d.Score.Sections, schema.ScoreSection{Name: "本句", Start: 2, End: 2, Tempo: 90})
	if arrangementConditionHash(d, ref) == before {
		t.Fatal("本句速度变化必须使旧候选失效")
	}
}

func TestAIGenerationPreflightPreservesScoreAndGroupsOnlyRequiredCells(t *testing.T) {
	w, e := New(t.TempDir())
	must(t, e)
	defer w.Close()
	must(t, w.Store.Update(func(s *schema.State) { s.RuntimeVersion = ptr("v0.9.1") }, true))
	d := workflowDraft()
	ref := ArrangementPartRef{0, 1, 0}
	part := &d.Arrangement.Sections[0].Phrases[1].Parts[0]
	part.Generation = nil
	d.Score.Tracks[1].Notes = []schema.ScoreNote{{Bar: 2, Beat: 1, Pitch: 62, Length: 1, Velocity: 30, Articulation: "pluck"}, {Bar: 2, Beat: 2, Pitch: 62, Length: 2, Velocity: 32, Articulation: "pluck"}, {Bar: 2, Beat: 4, Pitch: 65, Length: 1, Velocity: 100, Articulation: "tremolo"}}
	r, e := w.ArrangementGenerationPreflight(ArrangementAIPrepareInput{Draft: d, ModelID: "yue2-3b-q8", Target: &ref})
	must(t, e)
	if r.Settings.Method != "sampler" || r.Settings.Mode != "score" || len(r.Cells) != 2 || r.Cells[0].SustainSeconds != 1 || r.Cells[0].VelocityLow != 1 || r.Cells[1].VelocityLow != 86 || !r.Cells[0].Pilot || !r.Cells[1].Pilot {
		t.Fatalf("覆盖任务不正确：%+v", r)
	}
	before := arrangementConditionHash(d, ref)
	part.Generation = &schema.ArrangementGenerationSettings{AssetKind: "instrument", Mode: "score", Method: "sampler", LengthMode: "seconds", Seconds: .1, SoundType: "instrument", TailSeconds: .5}
	r, e = w.ArrangementGenerationPreflight(ArrangementAIPrepareInput{Draft: d, ModelID: "yue2-3b-q8", Target: &ref})
	must(t, e)
	if len(r.Issues) == 0 || len(d.Score.Tracks[1].Notes) != 3 || d.Score.Tracks[1].Notes[1].Length != 2 {
		t.Fatal("自定义长度未报告冲突或修改了原谱")
	}
	if arrangementConditionHash(d, ref) == before {
		t.Fatal("生成设置没有进入缓存指纹")
	}
}

func TestAIChoirPromptDoesNotForbidVocalsOrIncludeOtherParts(t *testing.T) {
	d := workflowDraft()
	d.Arrangement.Brief = "整首含琵琶、大鼓、人声和笛子"
	p := &d.Arrangement.Sections[0].Phrases[1].Parts[0]
	p.Instrument = "合唱"
	p.Generation = generationTestSettings()
	p.Generation.SoundType = "wordless"
	jobs, e := arrangementAIJobs(*d.Arrangement, &ArrangementPartRef{0, 1, 0})
	must(t, e)
	m := arrangementMusicDraft(d, "yue2-3b-q8", jobs[0])
	if m.Instrumental || strings.Contains(strings.ToLower(m.Text), "no vocals") || strings.Contains(m.Text, "琵琶") || !strings.Contains(m.Text, "Wordless") {
		t.Fatal(m.Text)
	}
	if m.ModelOptions["yue2"]["cot"] != "full" {
		t.Fatal("隔离声部不应沿用自由伴奏的默认条件模式")
	}
}

func TestNewChoirPartDefaultsToReviewedWordlessPhrase(t *testing.T) {
	w, e := New(t.TempDir())
	must(t, e)
	defer w.Close()
	must(t, w.Store.Update(func(s *schema.State) { s.RuntimeVersion = ptr("v0.9.1") }, true))
	d := workflowDraft()
	p := &d.Arrangement.Sections[0].Phrases[1].Parts[0]
	p.Instrument = "合唱"
	p.Role = "pad"
	p.Generation = nil
	d.Score.Tracks[1].Name = "合唱"
	d.Score.Tracks[1].Role = "pad"
	ref := ArrangementPartRef{0, 1, 0}
	r, e := w.ArrangementGenerationPreflight(ArrangementAIPrepareInput{Draft: d, ModelID: "yue2-3b-q8", Target: &ref})
	must(t, e)
	if r.Settings.AssetKind != "phrase" || r.Settings.Mode != "score" || r.Settings.Method != "direct" || r.Settings.SoundType != "wordless" {
		t.Fatalf("合唱默认设置不符合独立无词乐句：%+v", r.Settings)
	}
	if len(r.Issues) != 0 {
		t.Fatalf("默认合唱设置不应有预检错误：%v", r.Issues)
	}
}

func TestAICandidateCannotBeAdoptedByFileExistenceOrTruncation(t *testing.T) {
	w, e := New(t.TempDir())
	must(t, e)
	defer w.Close()
	d := workflowDraft()
	must(t, w.SaveDraft(d))
	m := schema.ArrangementMaterial{ID: schema.NewID(), ProjectID: d.ID, Generation: &schema.ArrangementGenerationContract{Version: arrangementAdapterVersion}, GenerationResult: &schema.ArrangementGenerationResult{State: "needs_fit", Truncated: true}, ReviewStatus: "needs_review"}
	must(t, w.persistGenerationMaterial(m))
	if generationMaterialUsable(m) {
		t.Fatal("未适配素材被当成可用")
	}
	if e = w.ReviewArrangementMaterial(m.ID, true); e == nil {
		t.Fatal("截断候选可直接确认")
	}
	m.GenerationResult.State = "needs_review"
	m.GenerationResult.Checks = []schema.GenerationCheck{{Code: "PITCH", Status: "fail", Message: "错误根音"}}
	must(t, w.persistGenerationMaterial(m))
	if e = w.ReviewArrangementMaterial(m.ID, true); e == nil {
		t.Fatal("错误根音可确认")
	}
}

func TestGenerationMaterialPersistenceOwnsSnapshotAndCheckFailureIsTerminal(t *testing.T) {
	w, e := New(t.TempDir())
	must(t, e)
	defer w.Close()
	d := workflowDraft()
	must(t, w.SaveDraft(d))
	pitch := 69
	m := schema.ArrangementMaterial{ID: schema.NewID(), ProjectID: d.ID, FileName: "import-" + schema.NewID() + ".wav", Generation: &schema.ArrangementGenerationContract{Version: arrangementAdapterVersion, CoreSeconds: 1, EffectiveSeconds: 1, Pitch: &pitch}, GenerationResult: &schema.ArrangementGenerationResult{State: "raw_saved", ActualSeconds: 2, Checks: []schema.GenerationCheck{{Code: "ORIGINAL"}}}}
	must(t, w.persistGenerationMaterial(m))
	m.Generation.CoreSeconds = 99
	m.GenerationResult.Checks[0].Code = "MODIFIED"
	saved := w.Store.Read().ArrangementMaterials[0]
	if saved.Generation.CoreSeconds != 1 || saved.GenerationResult.Checks[0].Code != "ORIGINAL" {
		t.Fatal("检查候选期间修改了 Store 持有的对象")
	}
	_, e = w.checkAndPersistGeneration(context.Background(), saved)
	if e == nil {
		t.Fatal("不存在的原片不应通过提取")
	}
	if got := w.Store.Read().ArrangementMaterials[0].GenerationResult.State; got != "failed" {
		t.Fatalf("检查异常后状态没有收尾：%s", got)
	}
}

func TestNewCandidateDoesNotHideConfirmedAIPlayablePhrase(t *testing.T) {
	w, e := New(t.TempDir())
	must(t, e)
	defer w.Close()
	d := workflowDraft()
	ref := ArrangementPartRef{0, 1, 0}
	part := &d.Arrangement.Sections[0].Phrases[1].Parts[0]
	part.Generation = generationTestSettings()
	must(t, w.SaveDraft(d))
	id := schema.NewID()
	file := "import-" + id + ".wav"
	path := filepath.Join(w.Store.Root, "outputs", file)
	must(t, os.WriteFile(path, testkit.WAV(), 0600))
	digest, e := arrangementAudioDigest(path)
	must(t, e)
	m := schema.ArrangementMaterial{ID: id, ProjectID: d.ID, SectionIndex: 0, PhraseIndex: 1, PartIndex: 0, PlanHash: arrangementJobHash(*d.Arrangement, ref), ScoreHash: arrangementConditionHash(d, ref), FileName: file, Duration: 2, ReviewStatus: "confirmed", Generation: &schema.ArrangementGenerationContract{Version: arrangementAdapterVersion, Settings: *part.Generation, CoreSeconds: 2, EffectiveSeconds: 2}, GenerationResult: &schema.ArrangementGenerationResult{State: "confirmed", RawSHA256: digest}}
	must(t, w.persistGenerationMaterial(m))
	m.ID = schema.NewID()
	m.ReviewStatus = "needs_review"
	m.GenerationResult.State = "needs_review"
	must(t, w.persistGenerationMaterial(m))
	r, e := w.InspectArrangement(ArrangementInspectInput{Draft: d, Stage: "render"})
	must(t, e)
	if !r.Parts[1].Ready || r.Parts[1].MaterialID != id {
		t.Fatalf("新候选遮蔽已采用乐句：%+v", r.Parts[1])
	}
	must(t, os.WriteFile(path, []byte("changed"), 0600))
	r, e = w.InspectArrangement(ArrangementInspectInput{Draft: d, Stage: "render"})
	must(t, e)
	if r.Parts[1].Ready || r.Parts[1].Issues[0].Code != "AI_MATERIAL_CORRUPT" {
		t.Fatalf("损坏素材未在预检拦截：%+v", r.Parts[1])
	}
}

func TestWordlessPhraseMustRetainAudibleRelease(t *testing.T) {
	for _, released := range []bool{false, true} {
		w, e := New(t.TempDir())
		must(t, e)
		d := workflowDraft()
		must(t, w.SaveDraft(d))
		id := schema.NewID()
		file := "import-" + id + ".wav"
		path := filepath.Join(w.Store.Root, "outputs", file)
		pcm := make([]float32, score.SampleRate*2)
		for i := range pcm {
			seconds := float64(i) / score.SampleRate
			gain := 1.0
			if released && seconds > 1 {
				gain = math.Pow(math.Max(0, 1-(seconds-1)/.5), 2)
			}
			pcm[i] = float32(.2 * gain * math.Sin(2*math.Pi*440*seconds))
		}
		must(t, score.WriteWAV(path, pcm, pcm))
		digest, e := arrangementAudioDigest(path)
		must(t, e)
		m := schema.ArrangementMaterial{ID: id, ProjectID: d.ID, FileName: file, Duration: 2, Generation: &schema.ArrangementGenerationContract{Version: arrangementAdapterVersion, CoreSeconds: 1, EffectiveSeconds: 1, TailSeconds: .5, Settings: schema.ArrangementGenerationSettings{SoundType: "wordless", Mode: "score"}}, GenerationResult: &schema.ArrangementGenerationResult{State: "raw_saved", ActualSeconds: 2, RawSHA256: digest}}
		must(t, w.persistGenerationMaterial(m))
		fitted, e := w.FitArrangementMaterial(ArrangementMaterialFitInput{ID: id, Seconds: 1, TailSeconds: .5})
		must(t, e)
		if released && fitted.GenerationResult.State != "needs_review" || !released && fitted.GenerationResult.State != "needs_fit" {
			t.Fatalf("释音检测错误 released=%v: %+v", released, fitted.GenerationResult)
		}
		if !released && w.ReviewArrangementMaterial(id, true) == nil {
			t.Fatal("仍在明显发声的合唱结尾不能直接采用")
		}
		w.Close()
	}
}
