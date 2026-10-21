package workbench

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"yovoice/internal/msg"
	"yovoice/internal/schema"
)

// 请求写入素材记录，生成工作线程在下一个单元完成时认领。
// 取消时保留未执行请求；再次点击重试或继续备料即可恢复。
func (w *Workbench) drainArrangementRetries(ctx context.Context, d schema.Draft, execute func(arrangementGenerationTask, ArrangementAIPrepareInput) error) error {
	var failures []string
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		w.generationRetry.Lock()
		state := w.Store.Read()
		i := slices.IndexFunc(state.ArrangementMaterials, func(m schema.ArrangementMaterial) bool {
			return m.ProjectID == d.ID && m.RetryRequest != nil && m.RetryRequest.Status == "queued"
		})
		if i < 0 {
			w.generationRetry.Unlock()
			break
		}
		m := state.ArrangementMaterials[i]
		m.RetryRequest.Status = "running"
		err := w.persistGenerationMaterial(m)
		w.generationRetry.Unlock()
		if err != nil {
			return err
		}
		ref := ArrangementPartRef{m.SectionIndex, m.PhraseIndex, m.PartIndex}
		current := slices.IndexFunc(state.Drafts, func(current schema.Draft) bool { return current.ID == d.ID })
		var tasks []arrangementAIJob
		if current < 0 || !arrangementMaterialMatches(m, d, ref) || !arrangementMaterialMatches(m, state.Drafts[current], ref) {
			err = fmt.Errorf("曲谱或生成设置已变更，已保留旧候选，请对当前声部重新备料")
		} else {
			tasks, err = arrangementAIJobs(*d.Arrangement, &ref)
		}
		if err == nil && (len(tasks) != 1 || m.Generation == nil) {
			err = fmt.Errorf("无法定位需要重新生成的声部")
		}
		if err == nil {
			c := *m.Generation
			var music schema.Draft
			if c.Pitch != nil {
				music, err = arrangementSampleDraft(d, tasks[0], c)
			} else {
				music, err = arrangementModelDraft(d, m.ModelID, tasks[0])
			}
			if err == nil {
				in := ArrangementAIPrepareInput{Draft: d, ModelID: m.ModelID, Target: &ref, Retry: true, RepairRound: m.RetryRequest.Round, RetryFeedback: m.RetryRequest.Feedback}
				err = execute(arrangementGenerationTask{job: tasks[0], contract: c, music: music}, in)
			}
		}
		m.RetryRequest.Status = "completed"
		if err != nil {
			m.RetryRequest.Status, m.RetryRequest.Error = "failed", err.Error()
			failures = append(failures, err.Error())
		} else {
			m.GenerationResult.State = "rejected"
		}
		if e := w.persistGenerationMaterial(m); e != nil {
			return e
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "；"))
	}
	return nil
}

// 处理恰好在批次末尾提交的请求；正常情况下已由当前工作线程完成。
func (w *Workbench) resumeArrangementRetriesAfterOperation(projectID string, done <-chan struct{}) {
	for {
		<-done
		w.mu.Lock()
		closed := w.closed
		batchDone := w.arrangementDone
		batchRunning := w.arrangementCancel != nil
		w.mu.Unlock()
		if closed {
			return
		}
		// 外层备料任务可能正切换下一声部，不能抢走它的引擎执行槽。
		if batchRunning && batchDone != nil {
			<-batchDone
			continue
		}
		state := w.Store.Read()
		if state.Activity != nil && state.Activity.Status == "cancelled" {
			return
		}
		if !slices.ContainsFunc(state.ArrangementMaterials, func(m schema.ArrangementMaterial) bool {
			return m.ProjectID == projectID && m.RetryRequest != nil && m.RetryRequest.Status == "queued"
		}) {
			return
		}
		i := slices.IndexFunc(state.Drafts, func(d schema.Draft) bool { return d.ID == projectID })
		if i < 0 {
			return
		}
		d := state.Drafts[i]
		_, err := w.start("generate", msg.ActivityGenerate, nil, nil, func(ctx context.Context) error {
			return w.drainArrangementRetries(ctx, d, func(task arrangementGenerationTask, in ArrangementAIPrepareInput) error {
				return w.runArrangementGenerationTask(ctx, d, task, in, 0, 1)
			})
		}, "", projectID)
		if err == nil {
			return
		}
		// 若外层备料批次抢先开始下一声部，就等该操作到达边界。
		w.mu.Lock()
		busy := w.cancel != nil
		done = w.done
		w.mu.Unlock()
		if !busy || done == nil {
			return
		}
	}
}
