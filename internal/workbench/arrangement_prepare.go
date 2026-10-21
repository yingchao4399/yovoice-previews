package workbench

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"
	"yovoice/internal/schema"
)

type ArrangementPrepareInput struct {
	Draft     schema.Draft        `json:"draft"`
	Mode      string              `json:"mode"`
	AIModelID string              `json:"aiModelId,omitempty"`
	PartIDs   []string            `json:"partIds,omitempty"`
	Target    *ArrangementPartRef `json:"target,omitempty"`
	ResumeID  string              `json:"resumeId,omitempty"`
}

func (w *Workbench) ArrangementJobs(projectID string) []schema.ArrangementJob {
	return slices.DeleteFunc(w.Store.Read().ArrangementJobs, func(j schema.ArrangementJob) bool { return projectID != "" && j.ProjectID != projectID })
}

// PrepareArrangement 批次不依赖页面轮询生命周期；成功子任务在数据库中保留。
func (w *Workbench) PrepareArrangement(in ArrangementPrepareInput) (schema.ArrangementJob, error) {
	if in.Mode != "real" && in.Mode != "ai" && in.Mode != "both" {
		return schema.ArrangementJob{}, fmt.Errorf("备料模式必须为 real、ai 或 both")
	}
	var d schema.Draft
	b, _ := json.Marshal(in.Draft)
	_ = json.Unmarshal(b, &d)
	if _, e := migrateLegacyArrangementIDs(&d); e != nil {
		return schema.ArrangementJob{}, e
	}
	if d.Arrangement == nil || !d.Arrangement.Approved {
		return schema.ArrangementJob{}, fmt.Errorf("请先审核编曲方案")
	}
	if e := schema.ArrangementIssue(d.Arrangement); e != "" {
		return schema.ArrangementJob{}, fmt.Errorf("编曲计划无效：%s", e)
	}
	if in.AIModelID == "" {
		in.AIModelID = d.Arrangement.AIModelID
	}
	job := schema.ArrangementJob{ID: schema.NewID(), ProjectID: d.ID, Revision: arrangementRevision(d), Mode: in.Mode, ModelID: in.AIModelID, Status: "queued", Draft: d, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	for si, s := range d.Arrangement.Sections {
		for pi, p := range s.Phrases {
			for ai, a := range p.Parts {
				if (in.Mode != "both" && a.Source != in.Mode) || (len(in.PartIDs) > 0 && !slices.Contains(in.PartIDs, a.ID)) || (in.Target != nil && *in.Target != (ArrangementPartRef{si, pi, ai})) {
					continue
				}
				job.Steps = append(job.Steps, schema.ArrangementJobStep{PartID: a.ID, SectionIndex: si, PhraseIndex: pi, PartIndex: ai, Instrument: a.Instrument, Source: a.Source, Status: "queued"})
			}
		}
	}
	if len(job.Steps) == 0 {
		return job, fmt.Errorf("没有需要准备的声部")
	}
	resumeFound := in.ResumeID == ""
	for _, old := range w.ArrangementJobs(d.ID) {
		if old.Revision == job.Revision && old.Mode == job.Mode && old.ModelID == job.ModelID && slices.EqualFunc(old.Steps, job.Steps, func(a, b schema.ArrangementJobStep) bool {
			return a.PartID == b.PartID && a.SectionIndex == b.SectionIndex && a.PhraseIndex == b.PhraseIndex && a.PartIndex == b.PartIndex
		}) && (old.Status == "running" || old.Status == "queued") {
			return old, nil
		}
		if in.ResumeID == old.ID {
			resumeFound = true
			if old.Revision != job.Revision {
				return job, fmt.Errorf("计划或曲谱已更改，请对新修订创建任务")
			}
			job = old
			job.Status = "queued"
			job.Error = ""
			for i := range job.Steps {
				if job.Steps[i].Status != "ready" {
					job.Steps[i].Status = "queued"
					job.Steps[i].Error = ""
				}
			}
		}
	}
	if !resumeFound {
		return job, fmt.Errorf("找不到需要恢复的备料任务")
	}
	w.mu.Lock()
	if w.closed || w.arrangementCancel != nil {
		w.mu.Unlock()
		return job, fmt.Errorf("已有编曲备料任务运行中")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	w.arrangementCancel = cancel
	w.arrangementDone = done
	w.mu.Unlock()
	if e := w.SaveDraft(d); e != nil {
		cancel()
		w.mu.Lock()
		w.arrangementCancel = nil
		close(done)
		w.mu.Unlock()
		return job, e
	}
	if e := w.Store.Update(func(s *schema.State) {
		snapshot := cloneArrangementJob(job)
		i := slices.IndexFunc(s.ArrangementJobs, func(j schema.ArrangementJob) bool { return j.ID == job.ID })
		if i >= 0 {
			s.ArrangementJobs[i] = snapshot
		} else {
			s.ArrangementJobs = append(s.ArrangementJobs, snapshot)
		}
	}, true); e != nil {
		cancel()
		w.mu.Lock()
		w.arrangementCancel = nil
		close(done)
		w.mu.Unlock()
		return job, e
	}
	workerJob := cloneArrangementJob(job)
	go func() {
		defer func() { cancel(); w.mu.Lock(); w.arrangementCancel = nil; close(done); w.mu.Unlock() }()
		w.runArrangementJob(ctx, workerJob)
	}()
	return job, nil
}

// 工作线程、持久状态和调用方分别持有快照，避免步骤切片被跨线程改写。
func cloneArrangementJob(job schema.ArrangementJob) schema.ArrangementJob {
	b, _ := json.Marshal(job)
	var copy schema.ArrangementJob
	_ = json.Unmarshal(b, &copy)
	return copy
}

func (w *Workbench) updateArrangementJob(job schema.ArrangementJob) error {
	job.UpdatedAt = time.Now().UTC()
	snapshot := cloneArrangementJob(job)
	return w.Store.Update(func(s *schema.State) {
		for i := range s.ArrangementJobs {
			if s.ArrangementJobs[i].ID == job.ID {
				s.ArrangementJobs[i] = snapshot
				break
			}
		}
	}, true)
}

func (w *Workbench) runArrangementJob(ctx context.Context, job schema.ArrangementJob) {
	job.Status = "running"
	if w.updateArrangementJob(job) != nil {
		return
	}
	for i := range job.Steps {
		if job.Steps[i].Status == "ready" {
			continue
		}
		step := &job.Steps[i]
		step.Status = "running"
		if w.updateArrangementJob(job) != nil {
			return
		}
		err := ctx.Err()
		if err == nil {
			current := w.Store.Read()
			idx := slices.IndexFunc(current.Drafts, func(d schema.Draft) bool { return d.ID == job.ProjectID })
			if idx < 0 || arrangementRevision(current.Drafts[idx]) != job.Revision {
				err = fmt.Errorf("作品已变更，已完成素材保留；请为新修订准备")
			}
		}
		if err == nil {
			if step.Source == "ai" {
				ref := ArrangementPartRef{step.SectionIndex, step.PhraseIndex, step.PartIndex}
				before := w.Store.Read().Activity
				err = w.PrepareArrangementAI(ArrangementAIPrepareInput{Draft: job.Draft, ModelID: job.ModelID, Target: &ref})
				if err == nil {
					after := w.Store.Read().Activity
					if after != nil && (before == nil || !after.StartedAt.Equal(before.StartedAt)) {
						err = w.waitArrangementOperation(ctx, false)
					}
				}
			} else {
				err = w.prepareRealArrangementStep(ctx, job.Draft, *step)
			}
		}
		if err != nil {
			step.Status = "failed"
			step.Error = err.Error()
			job.Status = "failed"
			job.Error = err.Error()
			if ctx.Err() != nil {
				job.Status = "interrupted"
				step.Status = "queued"
			}
			_ = w.updateArrangementJob(job)
			return
		}
		step.Status = "ready"
		step.Error = ""
		if step.Source == "ai" {
			ref := ArrangementPartRef{step.SectionIndex, step.PhraseIndex, step.PartIndex}
			inspection, e := w.InspectArrangement(ArrangementInspectInput{Draft: job.Draft, AIModelID: job.ModelID, Stage: "render"})
			ready := e == nil && slices.ContainsFunc(inspection.Parts, func(p ArrangementPartInspection) bool { return p.Ref == ref && p.Ready })
			setArrangementAIStepReview(step, ready, e)
		}
		if w.updateArrangementJob(job) != nil {
			return
		}
	}
	job.Status = "ready"
	if slices.ContainsFunc(job.Steps, func(s schema.ArrangementJobStep) bool { return s.Status != "ready" }) {
		job.Status = "needs_review"
	}
	job.Error = ""
	_ = w.updateArrangementJob(job)
}

func setArrangementAIStepReview(step *schema.ArrangementJobStep, ready bool, inspectErr error) {
	step.Status = "ready"
	step.Error = ""
	if inspectErr != nil {
		step.Status = "needs_review"
		step.Error = inspectErr.Error()
	} else if !ready {
		step.Status = "needs_review"
		step.Error = "AI 素材已生成，但本句尚未通过可演奏检查或试听确认"
	}
}

func (w *Workbench) waitArrangementOperation(ctx context.Context, download bool) error {
	var done <-chan struct{}
	if download {
		done = w.ModelDone()
	} else {
		done = w.Done()
	}
	select {
	case <-done:
	case <-ctx.Done():
		if download {
			w.CancelModel()
		} else {
			w.cancelOperation(done)
		}
		<-done
		return ctx.Err()
	}
	s := w.Store.Read()
	a := s.Activity
	if download {
		a = s.ModelActivity
	}
	if a == nil || a.Status != "completed" {
		if a != nil && a.ErrorParams != nil {
			return fmt.Errorf("%v", a.ErrorParams["detail"])
		}
		return fmt.Errorf("素材准备未完成，请查看任务日志")
	}
	return nil
}

func (w *Workbench) prepareRealArrangementStep(ctx context.Context, d schema.Draft, step schema.ArrangementJobStep) error {
	part := d.Arrangement.Sections[step.SectionIndex].Phrases[step.PhraseIndex].Parts[step.PartIndex]
	ready := func() (bool, error) {
		r, e := w.InspectArrangement(ArrangementInspectInput{Draft: d})
		if e != nil {
			return false, e
		}
		for _, p := range r.Parts {
			if p.Ref == (ArrangementPartRef{step.SectionIndex, step.PhraseIndex, step.PartIndex}) {
				if p.Ready {
					return true, nil
				}
				if part.SourceID != "" && len(p.Issues) > 0 {
					code := p.Issues[0].Code
					if code == "SOURCE_FILE_MISSING" || code == "SOURCE_FILE_CORRUPT" {
						for _, source := range w.Store.Read().InstrumentSources {
							if source.ID == part.SourceID && source.CatalogID != "" {
								return false, nil
							}
						}
					}
					return false, fmt.Errorf("%s", p.Issues[0].Message)
				}
			}
		}
		return false, nil
	}
	if ok, e := ready(); ok || e != nil {
		return e
	}
	if part.SourceID != "" {
		for _, source := range w.Store.Read().InstrumentSources {
			if source.ID == part.SourceID && source.CatalogID != "" {
				if e := w.DownloadCatalogInstrument(source.CatalogID); e != nil {
					return e
				}
				if e := w.waitArrangementOperation(ctx, true); e != nil {
					return e
				}
				if ok, e := ready(); ok || e != nil {
					return e
				}
				return fmt.Errorf("修复后的音源仍不能覆盖本句，请检查奏法与音域")
			}
		}
	}
	catalogIDs := map[string]string{"笛子": "dizi", "二胡": "erhu", "古筝": "guzheng"}
	if id := catalogIDs[arrangementInstrumentKey(part.Instrument)]; id != "" {
		if e := w.DownloadCatalogInstrument(id); e != nil {
			return e
		}
		if e := w.waitArrangementOperation(ctx, true); e != nil {
			return e
		}
		if ok, e := ready(); ok || e != nil {
			return e
		}
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	for _, source := range w.Store.Read().InstrumentSources {
		if source.Verification == "metadata" && arrangementSameInstrument(part.Instrument, source.Instrument) && w.sourceFilesIssue(source) == "" {
			return fmt.Errorf("%s 已找到在线采样 %s，请在音源库试听并确认后继续", part.Instrument, source.Name)
		}
	}
	if e := w.AutoFindInstrument(part.Instrument); e != nil {
		return e
	}
	if e := w.waitArrangementOperation(ctx, true); e != nil {
		return e
	}
	if ok, e := ready(); ok || e != nil {
		return e
	}
	for _, source := range w.Store.Read().InstrumentSources {
		if source.Verification == "metadata" && arrangementSameInstrument(part.Instrument, source.Instrument) {
			return fmt.Errorf("%s 已下载到音源库，仍需试听确认其乐器身份后继续", source.Name)
		}
	}
	return fmt.Errorf("%s 已完成查找，但采样不能覆盖本句；请更换音源或局部修改曲谱", part.Instrument)
}

func (w *Workbench) CancelArrangementJob() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.arrangementCancel != nil {
		w.arrangementCancel()
	}
}
