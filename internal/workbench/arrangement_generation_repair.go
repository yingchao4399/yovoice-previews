package workbench

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"yovoice/internal/engine"
	"yovoice/internal/schema"
)

// 局部重规划只调整执行合同，不能借此重写已经审核的曲谱和音乐意图。
type ArrangementGenerationRepairInput struct {
	ProjectID  string                               `json:"projectId"`
	QuestionID string                               `json:"questionId"`
	Ref        ArrangementPartRef                   `json:"ref"`
	Settings   schema.ArrangementGenerationSettings `json:"settings"`
	sessionID  string
}

func (w *Workbench) ReplanArrangementGeneration(in ArrangementGenerationRepairInput) (schema.ArrangementJob, error) {
	var empty schema.ArrangementJob
	d, err := w.assistantWork(in.ProjectID)
	if err != nil {
		return empty, err
	}
	if d.Arrangement == nil {
		return empty, fmt.Errorf("当前作品没有编曲方案")
	}
	q := sessionQuestion(w.Store.Read(), in.sessionID, in.QuestionID)
	if in.QuestionID == "" || q == nil || q.State != "answered" || q.WorkID != d.ID || q.Revision != questionRevision(d) || !slices.Equal(q.Options, arrangementRecoveryOptions) || !slices.Equal(q.Selected, []int{0}) || strings.TrimSpace(q.Custom) != "" {
		return empty, fmt.Errorf("请先通过本修订的故障问答卡明确确认局部重新规划；自定义意见需要先澄清确认")
	}
	jobs, err := arrangementAIJobs(*d.Arrangement, &in.Ref)
	if err != nil || len(jobs) != 1 {
		return empty, fmt.Errorf("无法定位当前AI声部")
	}
	job := jobs[0]
	s := w.Store.Read()
	failed := slices.ContainsFunc(s.ArrangementMaterials, func(m schema.ArrangementMaterial) bool {
		return m.ProjectID == d.ID && m.GenerationResult != nil && m.GenerationResult.State == "failed" && m.GenerationResult.RecoverySession == in.sessionID && m.SectionIndex == in.Ref.SectionIndex && m.PhraseIndex == in.Ref.PhraseIndex && m.PartIndex == in.Ref.PartIndex && arrangementMaterialMatches(m, d, in.Ref)
	})
	for _, batch := range s.ArrangementJobs {
		if batch.ProjectID != d.ID || batch.Revision != arrangementRevision(d) {
			continue
		}
		failed = failed || slices.ContainsFunc(batch.Steps, func(step schema.ArrangementJobStep) bool {
			return step.Status == "failed" && step.RecoverySession == in.sessionID && step.PartID == job.part.ID
		})
	}
	if !failed {
		return empty, fmt.Errorf("此声部不是当前诊断会话中的失败项")
	}
	before := generationSettings(job.part)
	if in.Settings.SoundType != before.SoundType || in.Settings.Mode != before.Mode {
		return empty, fmt.Errorf("局部自动修复不能改变声音类别或按谱/自由创作意图，请在声部设置中明确修改")
	}
	if err = validateGenerationSettings(in.Settings); err != nil {
		return empty, err
	}
	if !slices.ContainsFunc(s.Models, func(m schema.InstalledModel) bool { return m.ID == in.Settings.ModelID }) {
		return empty, fmt.Errorf("请选择实际已安装的音频模型")
	}
	if err = modelEngineReady(s, in.Settings.ModelID); err != nil {
		return empty, err
	}
	if !RuntimeReady(s) {
		return empty, fmt.Errorf("音频运行时未就绪，暂不修改当前方案")
	}
	a, _ := json.Marshal(before)
	b, _ := json.Marshal(in.Settings)
	if string(a) == string(b) {
		return empty, fmt.Errorf("生成方案没有变化，请勿用原样重试代替重新规划")
	}
	base := arrangementRevision(d)
	d.Arrangement.Sections[in.Ref.SectionIndex].Phrases[in.Ref.PhraseIndex].Parts[in.Ref.PartIndex].Generation = &in.Settings
	pre, err := w.ArrangementGenerationPreflight(ArrangementAIPrepareInput{Draft: d, ModelID: in.Settings.ModelID, Target: &in.Ref})
	if err != nil {
		return empty, err
	}
	if len(pre.Issues) > 0 {
		return empty, fmt.Errorf("局部新方案仍不可执行：%s", strings.Join(pre.Issues, "；"))
	}
	modelDraft, err := arrangementModelDraft(d, pre.Contract.ModelID, job)
	if err != nil {
		return empty, err
	}
	if _, err = engine.BuildArrangementRequest(modelDraft, pre.Contract); err != nil {
		return empty, err
	}
	w.mu.Lock()
	busy := w.cancel != nil || w.arrangementCancel != nil
	w.mu.Unlock()
	if busy {
		return empty, fmt.Errorf("请等待当前生成批次结束后再调整失败声部")
	}
	if err = w.saveAssistantArrangement(d, base); err != nil {
		return empty, err
	}
	return w.PrepareArrangement(ArrangementPrepareInput{Draft: d, Mode: "ai", AIModelID: in.Settings.ModelID, Target: &in.Ref})
}
