package workbench

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
	"yovoice/internal/catalog"
	"yovoice/internal/engine"
	"yovoice/internal/msg"
	"yovoice/internal/schema"
	"yovoice/internal/score"
)

type arrangementGenerationTask struct {
	sampler  bool
	job      arrangementAIJob
	contract schema.ArrangementGenerationContract
	music    schema.Draft
}

// PrepareArrangementAI 和持久备料、助手调用共用；候选生成后必须经过独立采用操作。
func (w *Workbench) PrepareArrangementAI(in ArrangementAIPrepareInput) error {
	w.edit.Lock()
	defer w.edit.Unlock()
	d := in.Draft
	if d.Kind != "score" || d.Arrangement == nil {
		return msg.Err(msg.ErrRequestInvalid, nil)
	}
	if in.ModelID == "" {
		in.ModelID = d.Arrangement.AIModelID
	}
	jobs, err := arrangementAIJobs(*d.Arrangement, in.Target)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if err := w.repairConfirmedAISamples(job.part); err != nil {
			return err
		}
	}
	if err := w.recoverInterruptedGeneration(d, jobs, in.ModelID); err != nil {
		return err
	}
	state := w.Store.Read()
	inspection, err := w.InspectArrangement(ArrangementInspectInput{Draft: d, Stage: "score", AIModelID: in.ModelID})
	if err != nil {
		return err
	}
	var tasks []arrangementGenerationTask
	for _, job := range jobs {
		// 健康且覆盖本句的已绑定 AI 音色无需模型或联网。
		if !in.Retry && generationSettings(job.part).AssetKind == "instrument" && slices.ContainsFunc(inspection.Parts, func(p ArrangementPartInspection) bool { return p.Ref == job.ref && p.SourceID != "" && p.Ready }) {
			continue
		}
		pre, err := w.ArrangementGenerationPreflight(ArrangementAIPrepareInput{Draft: d, ModelID: in.ModelID, Target: &job.ref})
		if err != nil {
			return err
		}
		if len(pre.Issues) > 0 {
			return fmt.Errorf("%s · %s：%s", job.phrase.Name, job.part.Instrument, strings.Join(pre.Issues, "；"))
		}
		if pre.Settings.Method == "direct" {
			found := false
			for _, m := range state.ArrangementMaterials {
				if m.ProjectID == d.ID && arrangementMaterialMatches(m, d, job.ref) && m.Generation != nil && m.Generation.Pitch == nil && m.ModelID == pre.Contract.ModelID && w.generationCandidateRetained(m) {
					found = true
				}
			}
			if found && !in.Retry {
				continue
			}
			music, e := arrangementModelDraft(d, pre.Contract.ModelID, job)
			if e != nil {
				return e
			}
			if pre.Settings.Mode == "score" && !pre.Capability.ScoreConditioning {
				return fmt.Errorf("当前 ACE-Step 适配器不接收逐音符曲谱；请选择音色按谱演奏或自由模式")
			}
			tasks = append(tasks, arrangementGenerationTask{job: job, contract: pre.Contract, music: music})
			continue
		}
		if len(pre.Cells) > 0 && pre.Reused == len(pre.Cells) {
			if err := w.ensureGeneratedSampleMap(job.part, pre.Cells); err != nil {
				return err
			}
			if pre.Settings.AssetKind == "phrase" {
				tasks = append(tasks, arrangementGenerationTask{sampler: true, job: job, contract: pre.Contract})
			}
			continue
		}
		pilotReady := map[string]bool{}
		for _, c := range pre.Cells {
			if c.State == "reused" {
				pilotReady[c.Articulation] = true
			}
		}
		for _, cell := range pre.Cells {
			if in.RetryCell != "" && cell.Key != in.RetryCell {
				continue
			}
			if !in.Retry && (cell.State == "reused" || cell.MaterialID != "" && (cell.State == "needs_review" || cell.State == "needs_fit" || cell.State == "confirmed")) {
				continue
			}
			// 每种奏法先生成一项，用户确认后才展开同组剩余音高与力度层。
			if !in.Retry && !pilotReady[cell.Articulation] && !cell.Pilot {
				continue
			}
			c := pre.Contract
			c.Pitch = new(int)
			*c.Pitch = cell.Pitch
			c.Articulation = cell.Articulation
			c.VelocityLow = cell.VelocityLow
			c.VelocityHigh = cell.VelocityHigh
			c.CoreSeconds = cell.SustainSeconds
			c.EffectiveSeconds = cell.SustainSeconds
			c.ExpectedOnsetSeconds = new(float64)
			setGenerationBudget(&c, pre.Capability)
			music, e := arrangementSampleDraft(d, job, c)
			if e != nil {
				return e
			}
			tasks = append(tasks, arrangementGenerationTask{job: job, contract: c, music: music})
		}
	}
	if len(tasks) == 0 {
		return nil
	}
	for _, task := range tasks {
		if task.sampler {
			continue
		}
		if _, err = engine.BuildArrangementRequest(task.music, task.contract); err != nil {
			return err
		}
		if !slices.ContainsFunc(state.Models, func(m schema.InstalledModel) bool { return m.ID == task.contract.ModelID }) {
			return msg.Err(msg.ErrModelRequired, nil)
		}
		if err = modelEngineReady(state, task.contract.ModelID); err != nil {
			return err
		}
	}
	if slices.ContainsFunc(tasks, func(t arrangementGenerationTask) bool { return !t.sampler }) && !RuntimeReady(state) {
		return msg.Err(msg.ErrRuntimeRequired, nil)
	}
	if err = w.saveDraft(d); err != nil {
		return err
	}
	_, err = w.start("generate", msg.ActivityGenerate, nil, nil, func(ctx context.Context) error {
		var failures []string
		for i, task := range tasks {
			if e := w.runArrangementGenerationTask(ctx, d, task, in, i, len(tasks)); e != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				failures = append(failures, e.Error())
			}
			// 当前下一项完成后优先处理已提交的试听反馈，再继续原批次。
			if e := w.drainArrangementRetries(ctx, d, func(task arrangementGenerationTask, retry ArrangementAIPrepareInput) error {
				return w.runArrangementGenerationTask(ctx, d, task, retry, i, len(tasks))
			}); e != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				failures = append(failures, e.Error())
			}
		}
		if len(failures) > 0 {
			return fmt.Errorf("%s；此前成功原片和候选均已保留", strings.Join(failures, "；"))
		}
		return nil
	}, "", d.ID)
	return err
}

func (w *Workbench) runArrangementGenerationTask(ctx context.Context, d schema.Draft, task arrangementGenerationTask, in ArrangementAIPrepareInput, i, total int) error {
	state := w.Store.Read()
	if !task.sampler && (!RuntimeReady(state) || state.RuntimePath == nil) {
		return msg.Err(msg.ErrRuntimeRequired, nil)
	}
	var failures []string
	if err := ctx.Err(); err != nil {
		return err
	}
	if task.sampler {
		if err := w.renderGeneratedSamplePhrase(ctx, d, task); err != nil {
			return err
		}
		return nil
	}
	w.progress(msg.ActivityRendering, msg.Params{"stage": "ai", "done": i, "total": total}, int64(i), int64(total))
	modelIndex := slices.IndexFunc(state.Models, func(m schema.InstalledModel) bool { return m.ID == task.contract.ModelID })
	if modelIndex < 0 {
		return msg.Err(msg.ErrModelRequired, nil)
	}
	for attempt := 1; attempt <= 3; attempt++ {
		music := task.music
		if in.RetryFeedback != "" {
			music.Text += " User audition feedback for this retry: " + in.RetryFeedback + ". Preserve the requested instrument, technique, source policy, score and phrase length; address only this feedback."
		}
		m := schema.ArrangementMaterial{ID: schema.NewID(), ProjectID: d.ID, PartID: task.job.part.ID, PlanHash: arrangementJobHash(*d.Arrangement, task.job.ref), ScoreHash: arrangementConditionHash(d, task.job.ref), SectionIndex: task.job.ref.SectionIndex, PhraseIndex: task.job.ref.PhraseIndex, PartIndex: task.job.ref.PartIndex, Instrument: task.job.part.Instrument, Role: task.job.part.Role, Technique: task.job.part.Technique, Phrase: task.job.phrase.Name, Prompt: music.Text, UserFeedback: in.RetryFeedback, Tempo: d.Arrangement.Tempo, Key: d.Arrangement.Key, ModelID: task.contract.ModelID, ReviewStatus: "needs_review", CreatedAt: time.Now().UTC()}
		c := task.contract
		// 每一轮助手修复都改变采样随机种子，避免重复同一候选；请求合同仍保持不变。
		seed := 1234 + in.RepairRound*17 + attempt - 1
		c.Seed = &seed
		music.Seed = &seed
		m.Generation = &c
		m.Condition = "free_audio"
		if c.Settings.Mode == "score" {
			m.Condition = "score_conditioned"
		}
		if c.Pitch != nil {
			m.Condition = "sample_candidate"
		}
		m.FileName = "import-" + m.ID + ".wav"
		m.GenerationResult = &schema.ArrangementGenerationResult{State: "generating", Attempt: attempt, RepairRound: in.RepairRound}
		if e := w.persistGenerationMaterial(m); e != nil {
			return e
		}
		path, e := w.Store.MediaPath("outputs", m.FileName)
		if e != nil {
			return e
		}
		result, e := w.engine.GenerateArrangement(ctx, *state.RuntimePath, state.Models[modelIndex], state.Preferences.Backend, music, c, path, func(code msg.Code, params msg.Params) { w.progress(code, params, int64(i), int64(total)) })
		result.Attempt = attempt
		result.RepairRound = in.RepairRound
		m.GenerationResult = &result
		m.Duration = result.ActualSeconds
		if e != nil {
			result.State = "failed"
			result.Checks = append(result.Checks, schema.GenerationCheck{Code: "ENGINE", Status: "fail", Message: e.Error()})
			m.GenerationResult = &result
			if persistErr := w.persistGenerationMaterial(m); persistErr != nil {
				return persistErr
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			lower := strings.ToLower(e.Error())
			if strings.Contains(lower, "out of memory") || strings.Contains(lower, "allocation failed") {
				failures = append(failures, e.Error()+"；资源不足，已暂停该项，未自动改用其他模型或音源")
				break
			}
			if attempt == 3 {
				failures = append(failures, e.Error())
			}
			continue
		}
		// 原片先落库，检查/取消失败也不删除已返回内容。
		if e = w.persistGenerationMaterial(m); e != nil {
			return e
		}
		m, e = w.checkAndPersistGeneration(ctx, m)
		if e != nil {
			return e
		}
		if m.GenerationResult.State != "failed" {
			break
		}
		if attempt == 3 {
			failures = append(failures, task.job.phrase.Name+" · "+task.job.part.Instrument+" 三次候选均未通过本地检查")
		}
	}

	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "；"))
	}
	return nil
}

func (w *Workbench) checkAndPersistGeneration(ctx context.Context, m schema.ArrangementMaterial) (schema.ArrangementMaterial, error) {
	w.generationReview.Lock()
	defer w.generationReview.Unlock()
	for _, current := range w.Store.Read().ArrangementMaterials {
		if current.ID == m.ID && current.GenerationResultState() != "raw_saved" {
			return current, nil
		}
	}
	m, e := w.checkGenerationMaterial(ctx, m, false)
	if e != nil {
		m.GenerationResult.State = "failed"
		m.GenerationResult.Checks = append(m.GenerationResult.Checks, schema.GenerationCheck{Code: "CHECK", Status: "fail", Message: e.Error()})
		if persistErr := w.persistGenerationMaterial(m); persistErr != nil {
			return m, persistErr
		}
		return m, e
	}
	return m, w.persistGenerationMaterial(m)
}

func arrangementSampleDraft(d schema.Draft, job arrangementAIJob, c schema.ArrangementGenerationContract) (schema.Draft, error) {
	m := arrangementMusicDraft(d, c.ModelID, job)
	velocity := "medium"
	if c.VelocityHigh <= 42 {
		velocity = "soft"
	}
	if c.VelocityLow >= 86 {
		velocity = "strong"
	}
	m.Text = fmt.Sprintf("Isolated %s, %s articulation, %s dynamic, one sustained MIDI %d note for %.2f seconds with a natural release. No melody changes, no accompaniment, no other instruments.", aiInstrumentName(job.part.Instrument), c.Articulation, velocity, *c.Pitch, c.CoreSeconds)
	names := []string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}
	m.Text += fmt.Sprintf(" Target %s%d, %.1f Hz.", names[*c.Pitch%12], *c.Pitch/12-1, 440*math.Pow(2, float64(*c.Pitch-69)/12))
	if c.Settings.SoundType == "wordless" {
		m.Text += " Unison wordless vowel ah, all voices at the requested pitch, no harmony, no words or instruments."
	} else {
		m.Text += " Instrumental, no vocals or speech."
	}
	def, _ := catalog.Lookup(c.ModelID)
	if def.Family == "yue2" {
		var err error
		m.Abc, err = score.SampleABC(*c.Pitch, (c.VelocityLow+c.VelocityHigh)/2, c.CoreSeconds, c.NormalizedKey, job.part.Instrument)
		if err != nil {
			return m, err
		}
		if c.Settings.SoundType == "wordless" {
			m.Abc, err = score.PhraseVocalABC(m.Abc)
			if err != nil {
				return m, err
			}
		}
		m.Abc, err = score.PhraseReleaseABC(m.Abc, c.TailSeconds)
		if err != nil {
			return m, err
		}
	}
	return m, nil
}

func (w *Workbench) persistGenerationMaterial(m schema.ArrangementMaterial) error {
	// Store.Update 的回调保存对象所有权；不能把仍将继续检查的调用方指针交给状态。
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	var snapshot schema.ArrangementMaterial
	if err = json.Unmarshal(b, &snapshot); err != nil {
		return err
	}
	foundProject := false
	err = w.Store.Update(func(s *schema.State) {
		if !slices.ContainsFunc(s.Drafts, func(d schema.Draft) bool { return d.ID == m.ProjectID }) {
			return
		}
		foundProject = true
		i := slices.IndexFunc(s.ArrangementMaterials, func(old schema.ArrangementMaterial) bool { return old.ID == m.ID })
		if i < 0 {
			s.ArrangementMaterials = append(s.ArrangementMaterials, snapshot)
		} else {
			s.ArrangementMaterials[i] = snapshot
		}
	}, true)
	if err != nil {
		return err
	}
	if !foundProject {
		return fmt.Errorf("作品已经删除，未恢复已删除的生成记录")
	}
	return nil
}
