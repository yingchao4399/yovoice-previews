package workbench

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"yovoice/internal/msg"
	"yovoice/internal/schema"
	"yovoice/internal/testkit"
)

func recoveryDraft(t *testing.T, w *Workbench) (schema.Draft, ArrangementPartRef) {
	t.Helper()
	d := workflowDraft()
	d.Arrangement.Sections[0].Phrases[1].Parts[0].Generation = generationTestSettings()
	_, err := migrateLegacyArrangementIDs(&d)
	must(t, err)
	must(t, w.SaveDraft(d))
	must(t, w.Store.Update(func(s *schema.State) { s.RuntimeVersion = ptr("v0.9.1") }, true))
	return d, ArrangementPartRef{0, 1, 0}
}

func recoveryCandidate(t *testing.T, w *Workbench, d schema.Draft, ref ArrangementPartRef, status string) schema.ArrangementMaterial {
	t.Helper()
	pre, err := w.ArrangementGenerationPreflight(ArrangementAIPrepareInput{Draft: d, ModelID: "ace-step-1.5-turbo-q8", Target: &ref})
	must(t, err)
	id := schema.NewID()
	part := d.Arrangement.Sections[ref.SectionIndex].Phrases[ref.PhraseIndex].Parts[ref.PartIndex]
	m := schema.ArrangementMaterial{ID: id, ProjectID: d.ID, PartID: part.ID, Instrument: part.Instrument, ModelID: pre.Contract.ModelID, FileName: "import-" + id + ".wav", PlanHash: arrangementJobHash(*d.Arrangement, ref), ScoreHash: arrangementConditionHash(d, ref), SectionIndex: ref.SectionIndex, PhraseIndex: ref.PhraseIndex, PartIndex: ref.PartIndex, ReviewStatus: "needs_review", Generation: &pre.Contract}
	path := filepath.Join(w.Store.Root, "outputs", m.FileName)
	must(t, os.WriteFile(path, testkit.WAVSeconds(2), 0600))
	digest, err := arrangementAudioDigest(path)
	must(t, err)
	m.GenerationResult = &schema.ArrangementGenerationResult{State: status, ActualSeconds: 2, RawSHA256: digest, TruncationKnown: true}
	must(t, w.persistGenerationMaterial(m))
	return m
}

func TestInterruptedSavedOriginalResumesChecksWithoutModel(t *testing.T) {
	root := t.TempDir()
	w, err := New(root)
	must(t, err)
	d, ref := recoveryDraft(t, w)
	m := recoveryCandidate(t, w, d, ref, "raw_saved")
	w.Close()
	w, err = New(root)
	must(t, err)
	defer w.Close()
	if w.Store.Read().ArrangementMaterials[0].GenerationResultState() != "interrupted" {
		t.Fatal("重启没有保留中断状态")
	}
	// 不登记推理模型：如果没有复用完整原片，这里会错误地要求重新安装模型。
	must(t, w.PrepareArrangementAI(ArrangementAIPrepareInput{Draft: d, ModelID: m.ModelID, Target: &ref}))
	state := w.Store.Read()
	if len(state.ArrangementMaterials) != 1 || state.ArrangementMaterials[0].ID != m.ID || state.ArrangementMaterials[0].GenerationResultState() != "needs_review" || state.ArrangementMaterials[0].ReviewStatus == "confirmed" {
		t.Fatalf("中断原片应只恢复检查，仍待试听：%+v", state.ArrangementMaterials)
	}
	if state.ArrangementMaterials[0].GenerationResult.RawSHA256 != m.GenerationResult.RawSHA256 || state.Activity != nil {
		t.Fatal("恢复检查不应修改原片或调用模型")
	}
}

func TestFailedOrCorruptPhraseDoesNotBlockFreshPreparation(t *testing.T) {
	for _, status := range []string{"failed", "corrupt", "interrupted_missing"} {
		t.Run(status, func(t *testing.T) {
			w, err := New(t.TempDir())
			must(t, err)
			defer w.Close()
			d, ref := recoveryDraft(t, w)
			state := status
			if status == "corrupt" {
				state = "needs_review"
			}
			if status == "interrupted_missing" {
				state = "interrupted"
			}
			m := recoveryCandidate(t, w, d, ref, state)
			path := filepath.Join(w.Store.Root, "outputs", m.FileName)
			if status == "corrupt" {
				must(t, os.WriteFile(path, []byte("changed"), 0600))
			}
			if status == "interrupted_missing" {
				must(t, os.Remove(path))
			}
			var callErr *msg.CallError
			err = w.PrepareArrangementAI(ArrangementAIPrepareInput{Draft: d, ModelID: m.ModelID, Target: &ref})
			if !errors.As(err, &callErr) || callErr.Code != msg.ErrModelRequired {
				t.Fatalf("失败或损坏原片应重新备料，不能按文件存在跳过：%v", err)
			}
			if status == "interrupted_missing" && w.Store.Read().ArrangementMaterials[0].GenerationResultState() != "failed" {
				t.Fatal("无原片的中断项没有保存明确失败原因")
			}
		})
	}
}

func TestFailedSampleCandidateCanBePreparedAgain(t *testing.T) {
	w, err := New(t.TempDir())
	must(t, err)
	defer w.Close()
	d, ref := recoveryDraft(t, w)
	part := &d.Arrangement.Sections[0].Phrases[1].Parts[0]
	part.Generation = nil
	must(t, w.SaveDraft(d))
	pre, err := w.ArrangementGenerationPreflight(ArrangementAIPrepareInput{Draft: d, ModelID: "ace-step-1.5-turbo-q8", Target: &ref})
	must(t, err)
	cell := pre.Cells[0]
	m := recoveryCandidate(t, w, d, ref, "failed")
	m.Generation.Pitch = &cell.Pitch
	m.Generation.CoreSeconds = cell.SustainSeconds
	m.Generation.Articulation = cell.Articulation
	m.Generation.VelocityLow, m.Generation.VelocityHigh = cell.VelocityLow, cell.VelocityHigh
	must(t, w.persistGenerationMaterial(m))
	var callErr *msg.CallError
	err = w.PrepareArrangementAI(ArrangementAIPrepareInput{Draft: d, ModelID: m.ModelID, Target: &ref})
	if !errors.As(err, &callErr) || callErr.Code != msg.ErrModelRequired {
		t.Fatalf("失败的代表采样不能阻止重新生成：%v", err)
	}
}

func TestAssistantRepairRoundsAreBoundedAndDeterministicFailuresNeedAPlan(t *testing.T) {
	w, err := New(t.TempDir())
	must(t, err)
	defer w.Close()
	d, ref := recoveryDraft(t, w)
	m := recoveryCandidate(t, w, d, ref, "failed")
	m.GenerationResult.RepairRound = 3
	must(t, w.persistGenerationMaterial(m))
	if round, err := w.RetryArrangementMaterialWithRound(m.ID, d); err == nil || round != 3 || !strings.Contains(err.Error(), "第四轮") {
		t.Fatalf("第四轮必须停止自主重试并交给问答卡：round=%d err=%v", round, err)
	}

	m.GenerationResult.RepairRound = 1
	m.GenerationResult.Checks = []schema.GenerationCheck{{Code: "DURATION", Status: "fail", Message: "有效区段缺少尾音"}}
	must(t, w.persistGenerationMaterial(m))
	if _, err := w.RetryArrangementMaterialWithRound(m.ID, d); err == nil || !strings.Contains(err.Error(), "禁止原样重试") {
		t.Fatalf("确定性时长问题不能原样重试：%v", err)
	}
}

func TestFailedNewSampleDoesNotHideHealthyCandidate(t *testing.T) {
	w, err := New(t.TempDir())
	must(t, err)
	defer w.Close()
	d, ref := recoveryDraft(t, w)
	part := &d.Arrangement.Sections[0].Phrases[1].Parts[0]
	part.Generation = nil
	must(t, w.SaveDraft(d))
	in := ArrangementAIPrepareInput{Draft: d, ModelID: "ace-step-1.5-turbo-q8", Target: &ref}
	pre, err := w.ArrangementGenerationPreflight(in)
	must(t, err)
	cell := pre.Cells[0]
	for _, status := range []string{"needs_fit", "failed"} {
		m := recoveryCandidate(t, w, d, ref, status)
		m.Generation.Pitch = &cell.Pitch
		m.Generation.CoreSeconds = cell.SustainSeconds
		m.Generation.Articulation = cell.Articulation
		m.Generation.VelocityLow, m.Generation.VelocityHigh = cell.VelocityLow, cell.VelocityHigh
		must(t, w.persistGenerationMaterial(m))
	}
	pre, err = w.ArrangementGenerationPreflight(in)
	must(t, err)
	if pre.Cells[0].State != "needs_fit" || pre.Cells[0].MaterialID != w.Store.Read().ArrangementMaterials[0].ID {
		t.Fatalf("新失败候选遮蔽了已有完整原片：%+v", pre.Cells[0])
	}
}
