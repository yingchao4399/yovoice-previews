package workbench

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
	"yovoice/internal/catalog"
	"yovoice/internal/schema"
)

func TestArrangementAIStepKeepsInspectionOutcome(t *testing.T) {
	step := schema.ArrangementJobStep{Status: "running", Error: "stale"}
	setArrangementAIStepReview(&step, false, nil)
	if step.Status != "needs_review" || step.Error == "" {
		t.Fatalf("未通过检查的 AI 素材必须保留待确认状态和原因：%+v", step)
	}
	inspectErr := errors.New("音源文件缺失")
	setArrangementAIStepReview(&step, false, inspectErr)
	if step.Status != "needs_review" || step.Error != inspectErr.Error() {
		t.Fatalf("检查失败原因不能被后续状态清理：%+v", step)
	}
	setArrangementAIStepReview(&step, true, nil)
	if step.Status != "ready" || step.Error != "" {
		t.Fatalf("检查通过后应清除旧错误：%+v", step)
	}
}

func awaitPreparation(t *testing.T, w *Workbench, id string) schema.ArrangementJob {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		for _, j := range w.ArrangementJobs("") {
			if j.ID == id && j.Status != "queued" && j.Status != "running" {
				return j
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("备料任务超时")
	return schema.ArrangementJob{}
}
func TestPreparationKeepsSuccessfulStepsAcrossRestart(t *testing.T) {
	root := t.TempDir()
	w, e := New(root)
	must(t, e)
	d := workflowDraft()
	d.Arrangement.Seconds = 8
	sec := &d.Arrangement.Sections[0]
	sec.EndBar = 4
	sec.Phrases = nil
	d.Score.Tracks = d.Score.Tracks[:1]
	d.Score.Tracks[0].Notes = nil
	source := inspectionSource(t, w, "二胡", 62)
	wrong := inspectionSource(t, w, "低音提琴", 36)
	for i := 0; i < 4; i++ {
		part := schema.ArrangementPart{Instrument: "二胡", Role: "melody", Source: "real", SourceID: source.ID}
		if i == 3 {
			part.SourceID = wrong.ID
		}
		sec.Phrases = append(sec.Phrases, schema.ArrangementPhrase{Name: "乐句", StartBar: i + 1, EndBar: i + 1, Parts: []schema.ArrangementPart{part}})
		d.Score.Tracks[0].Notes = append(d.Score.Tracks[0].Notes, schema.ScoreNote{Bar: i + 1, Beat: 1, Pitch: 62, Length: 1, Velocity: 90})
	}
	must(t, w.Store.Update(func(s *schema.State) { s.InstrumentSources = []schema.InstrumentSource{source, wrong} }, true))
	job, e := w.PrepareArrangement(ArrangementPrepareInput{Draft: d, Mode: "real"})
	must(t, e)
	failed := awaitPreparation(t, w, job.ID)
	if failed.Status != "failed" || len(failed.Steps) != 4 || failed.Steps[0].Status != "ready" || failed.Steps[2].Status != "ready" || failed.Steps[3].Status != "failed" {
		t.Fatalf("第四句失败不应丢弃前三句：%+v", failed)
	}
	if w.Store.Read().ModelActivity != nil {
		t.Fatal("健康本地音源不应联网")
	}
	w.Close()
	w, e = New(root)
	must(t, e)
	defer w.Close()
	saved := w.Store.Read().Drafts[slices.IndexFunc(w.Store.Read().Drafts, func(v schema.Draft) bool { return v.ID == d.ID })]
	resumed, e := w.PrepareArrangement(ArrangementPrepareInput{Draft: saved, Mode: "real", ResumeID: job.ID})
	must(t, e)
	again := awaitPreparation(t, w, resumed.ID)
	if again.Steps[0].Status != "ready" || again.Steps[2].Status != "ready" {
		t.Fatal("重启丢失成功子任务")
	}
	// 等待工作线程完成清理再创建新修订任务。
	w.mu.Lock()
	done := w.arrangementDone
	w.mu.Unlock()
	if done != nil {
		<-done
	}
	saved.Arrangement.Sections[0].Phrases[3].Parts[0].SourceID = source.ID
	if _, e = w.PrepareArrangement(ArrangementPrepareInput{Draft: saved, Mode: "real", ResumeID: job.ID}); e == nil {
		t.Fatal("旧批次不能覆盖新修订")
	}
	fixed, e := w.PrepareArrangement(ArrangementPrepareInput{Draft: saved, Mode: "real"})
	must(t, e)
	if finished := awaitPreparation(t, w, fixed.ID); finished.Status != "ready" {
		t.Fatalf("修复绑定后本地备料失败：%+v", finished)
	}
}
func TestArrangementConditionInvalidatesOnlyChangedPhrase(t *testing.T) {
	d := workflowDraft()
	ref := ArrangementPartRef{0, 1, 0}
	before := arrangementConditionHash(d, ref)
	d.Score.Tracks[0].Notes[0].Pitch++
	if arrangementConditionHash(d, ref) != before {
		t.Fatal("其他乐句修改误使缓存失效")
	}
	d.Score.Tracks[1].Notes[0].Pitch++
	if arrangementConditionHash(d, ref) == before {
		t.Fatal("本句改音未使缓存失效")
	}
}

func TestAIPreparationCommitsEachPhraseBeforeLaterFailure(t *testing.T) {
	w, e := New(t.TempDir())
	must(t, e)
	defer w.Close()
	executable, e := os.Executable()
	must(t, e)
	model := filepath.Join(w.Store.Root, "models", "fake.gguf")
	must(t, os.WriteFile(model, []byte("test"), 0600))
	must(t, w.Store.Update(func(s *schema.State) {
		s.Models = []schema.InstalledModel{{ID: "ace-step-1.5-turbo-bf16", Path: model}}
		s.RuntimePath = &executable
		s.RuntimeBackend = ptr("cpu")
		s.RuntimeVersion = ptr(catalog.EngineVersion)
		s.Preferences.Backend = "cpu"
	}, true))
	d := workflowDraft()
	d.Arrangement.Seconds = 8
	sec := &d.Arrangement.Sections[0]
	sec.EndBar = 4
	sec.Phrases = nil
	d.Score.Tracks = d.Score.Tracks[:1]
	d.Score.Tracks[0].Notes = nil
	for i := 0; i < 4; i++ {
		p := schema.ArrangementPart{Instrument: "二胡", Role: "melody", Source: "ai", Generation: generationTestSettings()}
		if i == 3 {
			p.Technique = "模拟编曲失败"
		}
		sec.Phrases = append(sec.Phrases, schema.ArrangementPhrase{Name: "句", StartBar: i + 1, EndBar: i + 1, Parts: []schema.ArrangementPart{p}})
		d.Score.Tracks[0].Notes = append(d.Score.Tracks[0].Notes, schema.ScoreNote{Bar: i + 1, Beat: 1, Pitch: 62, Length: 1, Velocity: 90})
	}
	_, e = migrateLegacyArrangementIDs(&d)
	must(t, e)
	must(t, w.PrepareArrangementAI(ArrangementAIPrepareInput{Draft: d, ModelID: "ace-step-1.5-turbo-bf16"}))
	<-w.Done()
	state := w.Store.Read()
	if len(state.ArrangementMaterials) != 6 || state.Activity.Status != "failed" {
		t.Fatalf("后三句失败不应删除已完成素材：count=%d activity=%+v", len(state.ArrangementMaterials), state.Activity)
	}
	var ids []string
	for _, m := range state.ArrangementMaterials {
		ids = append(ids, m.ID)
		if m.Duration == 0 {
			continue
		}
		path, e := w.Store.MediaPath("outputs", m.FileName)
		must(t, e)
		if _, e = os.Stat(path); e != nil {
			t.Fatal("已登记文件被失败清理")
		}
	}
	d.Arrangement.Sections[0].Phrases[3].Parts[0].Technique = ""
	must(t, w.PrepareArrangementAI(ArrangementAIPrepareInput{Draft: d, ModelID: "ace-step-1.5-turbo-bf16"}))
	<-w.Done()
	state = w.Store.Read()
	if len(state.ArrangementMaterials) != 7 || state.Activity.Status != "completed" {
		t.Fatalf("修复后未仅生成最后一句：%+v", state.Activity)
	}
	for _, id := range ids {
		if !slices.ContainsFunc(state.ArrangementMaterials, func(m schema.ArrangementMaterial) bool { return m.ID == id }) {
			t.Fatal("成功的先前素材被替换")
		}
	}
}
