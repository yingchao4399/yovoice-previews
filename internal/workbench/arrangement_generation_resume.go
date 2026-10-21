package workbench

import (
	"context"
	"time"
	"yovoice/internal/audio"
	"yovoice/internal/schema"
)

// 只有健康且正在等待适配、试听或已经确认的候选才能阻止重复生成。
// 失败、中断和仅创建了文件的记录不能成为“备料已完成”的依据。
func (w *Workbench) generationCandidateRetained(m schema.ArrangementMaterial) bool {
	switch m.GenerationResultState() {
	case "needs_fit":
		_, err := w.verifyGenerationOriginalIntegrity(m)
		return err == nil
	case "needs_review", "confirmed":
		_, err := w.verifyGenerationMaterialIntegrity(m)
		return err == nil
	default:
		return false
	}
}

// 重启后先检查已经完整保存的原片，不把中断状态当成已完成，也不重复调用模型。
// 只恢复当前修订的选中声部；原片不完整的项留有失败原因，随后允许单项重新生成。
func (w *Workbench) recoverInterruptedGeneration(d schema.Draft, jobs []arrangementAIJob, modelID string) error {
	w.generationReview.Lock()
	defer w.generationReview.Unlock()
	for _, m := range w.Store.Read().ArrangementMaterials {
		if m.ProjectID != d.ID || m.Generation == nil || m.Generation.Version != arrangementAdapterVersion || m.GenerationResultState() != "interrupted" {
			continue
		}
		matched := false
		for _, job := range jobs {
			selected := modelID
			if job.part.Generation != nil && job.part.Generation.ModelID != "" {
				selected = job.part.Generation.ModelID
			}
			if m.ModelID == selected && arrangementMaterialMatches(m, d, job.ref) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		path, err := w.verifyGenerationOriginalIntegrity(m)
		if err == nil {
			m.Duration, err = audio.Duration(path)
		}
		if err == nil {
			m.GenerationResult.ActualSeconds = m.Duration
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			m, err = w.checkGenerationMaterial(ctx, m, false)
			cancel()
		}
		if err != nil {
			m.GenerationResult.State = "failed"
			m.GenerationResult.Checks = append(m.GenerationResult.Checks, schema.GenerationCheck{Code: "RECOVERY", Status: "fail", Message: "中断候选无法从原片恢复：" + err.Error()})
		}
		if err := w.persistGenerationMaterial(m); err != nil {
			return err
		}
	}
	return nil
}
