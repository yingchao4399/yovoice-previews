package workbench

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"yovoice/internal/schema"
)

var arrangementRecoveryOptions = []string{"只重新规划并生成这个声部，保留其他已完成部分", "重新规划整首编曲", "先查看失败原因和可选方案，暂不重规划"}

// closeAnsweredArrangementRecovery 结束不需要重规划的诊断会话。问答卡回答“保留现状”或
// “暂不重规划”后，失败素材仍保留在历史中，但不再带着 RecoveryQueued 锁住后续助手对话。
func (w *Workbench) closeAnsweredArrangementRecovery(sessionID string) error {
	if sessionID == "" {
		return nil
	}
	return w.Store.Update(func(s *schema.State) {
		for i := range s.ArrangementMaterials {
			m := &s.ArrangementMaterials[i]
			if m.GenerationResult != nil && m.GenerationResult.RecoverySession == sessionID {
				m.GenerationResult.RecoveryQueued = false
				m.GenerationResult.RecoverySession = ""
			}
		}
		for i := range s.ArrangementJobs {
			for j := range s.ArrangementJobs[i].Steps {
				step := &s.ArrangementJobs[i].Steps[j]
				if step.RecoverySession == sessionID {
					step.RecoveryQueued = false
					step.RecoverySession = ""
				}
			}
		}
	}, true)
}

// reconcileAnsweredArrangementRecoveries 修复旧版本留下的“问答已回答但失败诊断仍锁定”状态。
// 只处理明确选择结束/暂不重规划的问答；选择重新规划整首仍保留会话约束。
func (w *Workbench) reconcileAnsweredArrangementRecoveries() {
	s := w.Store.Read()
	closeSessions := map[string]bool{}
	if s.AssistantSessions == nil {
		return
	}
	for _, session := range s.AssistantSessions.Sessions {
		for _, message := range session.Messages {
			for _, block := range message.Blocks {
				q := block.Question
				if q == nil || q.State != "answered" || len(q.Selected) != 1 || (q.Selected[0] != 0 && q.Selected[0] != 2) {
					continue
				}
				closeSessions[session.ID] = true
			}
		}
	}
	for sessionID := range closeSessions {
		_ = w.closeAnsweredArrangementRecovery(sessionID)
	}
}

// 后台诊断回合的写操作受限；不能把“主动修复”当成改写整曲或替换来源的许可。
func arrangementRecoveryToolAllowed(s schema.State, sessionID, workID, tool string) bool {
	if sessionID == "" {
		return true
	}
	isRecovery := slices.ContainsFunc(s.ArrangementMaterials, func(m schema.ArrangementMaterial) bool {
		return m.GenerationResult != nil && m.GenerationResult.RecoverySession == sessionID
	})
	for _, job := range s.ArrangementJobs {
		isRecovery = isRecovery || slices.ContainsFunc(job.Steps, func(step schema.ArrangementJobStep) bool { return step.RecoverySession == sessionID })
	}
	if !isRecovery {
		return true
	}
	if slices.Contains([]string{"ask_user", "get_work", "list_works", "status", "get_app_status", "get_recent_errors", "get_generation_status", "arrangement_jobs", "arrangement_generation_preflight", "inspect_arrangement", "arrangement_inventory", "search_resources", "list_models", "list_voices", "list_characters", "resource_downloads", "retry_arrangement_material"}, tool) {
		return true
	}
	if tool != "plan_arrangement" && tool != "replan_arrangement_generation" {
		return false
	}
	session := assistantSessionByID(s, sessionID)
	if session == nil {
		return false
	}
	for _, d := range s.Drafts {
		if d.ID != workID {
			continue
		}
		for _, m := range session.Messages {
			for _, b := range m.Blocks {
				q := b.Question
				selection := 1
				if tool == "replan_arrangement_generation" {
					selection = 0
				}
				if q != nil && q.WorkID == workID && q.State == "answered" && q.Revision == questionRevision(d) && slices.Equal(q.Options, arrangementRecoveryOptions) && slices.Equal(q.Selected, []int{selection}) && q.Custom == "" {
					return true
				}
			}
		}
	}
	return false
}

// 只给助手实际的合同、测量和检查状态，不携带原片路径、原始请求或密钥。
func arrangementDiagnosticMaterials(s schema.State, d schema.Draft) []map[string]any {
	var out []map[string]any
	for _, m := range s.ArrangementMaterials {
		if m.ProjectID != d.ID || m.Generation == nil || m.GenerationResult == nil {
			continue
		}
		ref := ArrangementPartRef{m.SectionIndex, m.PhraseIndex, m.PartIndex}
		r := m.GenerationResult
		out = append(out, map[string]any{
			"id": m.ID, "partId": m.PartID, "ref": ref, "instrument": m.Instrument,
			"condition": m.Condition, "currentRevision": arrangementMaterialMatches(m, d, ref),
			"modelId": m.ModelID, "state": r.State, "checks": r.Checks,
			"actualSeconds": r.ActualSeconds, "coreSeconds": m.Generation.CoreSeconds,
			"requestSeconds": m.Generation.RequestSeconds, "tailSeconds": m.Generation.TailSeconds,
			"pitch": m.Generation.Pitch, "articulation": m.Generation.Articulation,
			"detectedPitch": r.DetectedPitch, "truncated": r.Truncated,
			"repairRound": r.RepairRound, "retry": m.RetryRequest,
		})
	}
	// 返回最新候选及其前序诊断，避免长工程无限增大工具返回。
	if len(out) > 60 {
		out = out[len(out)-60:]
	}
	return out
}

func recoveryCell(m schema.ArrangementMaterial) string {
	return fmt.Sprintf("%d:%d:%d:%s:%s:%v:%d:%d:%s", m.SectionIndex, m.PhraseIndex, m.PartIndex,
		m.PlanHash, m.ScoreHash, pitchValue(m.Generation.Pitch), m.Generation.VelocityLow,
		m.Generation.VelocityHigh, m.Generation.Articulation)
}

func pitchValue(p *int) any {
	if p == nil {
		return "phrase"
	}
	return *p
}

// 生成和外层备料批次都释放资源后才通知，避免助手重试与下一声部争抢引擎。
func (w *Workbench) scheduleArrangementRecovery(projectID string) {
	done := w.Done()
	go func() {
		<-done
		w.mu.Lock()
		batch := w.arrangementDone
		running := w.arrangementCancel != nil
		w.mu.Unlock()
		if running && batch != nil {
			<-batch
		}
		_, _ = w.requestArrangementRecovery(projectID)
	}()
}

// 失败事件持久去重，使用专属会话，不能把诊断消息插进用户正在进行的其他对话。
func (w *Workbench) requestArrangementRecovery(projectID string) (bool, error) {
	w.generationRetry.Lock()
	defer w.generationRetry.Unlock()
	w.mu.Lock()
	closed := w.closed
	w.mu.Unlock()
	if closed {
		return false, nil
	}
	s := w.Store.Read()
	if s.Activity != nil && s.Activity.ProjectID == projectID && s.Activity.Status == "cancelled" {
		return false, nil
	}
	di := slices.IndexFunc(s.Drafts, func(d schema.Draft) bool { return d.ID == projectID })
	if di < 0 || s.Drafts[di].Arrangement == nil {
		return false, nil
	}
	d := s.Drafts[di]
	latest := map[string]schema.ArrangementMaterial{}
	ordered := slices.Clone(s.ArrangementMaterials)
	slices.SortStableFunc(ordered, func(a, b schema.ArrangementMaterial) int { return a.CreatedAt.Compare(b.CreatedAt) })
	sessionID := ""
	for _, m := range ordered {
		if m.ProjectID != projectID || m.Generation == nil || m.GenerationResult == nil {
			continue
		}
		ref := ArrangementPartRef{m.SectionIndex, m.PhraseIndex, m.PartIndex}
		if !arrangementMaterialMatches(m, d, ref) {
			continue
		}
		latest[recoveryCell(m)] = m
		if m.GenerationResult.RecoverySession != "" {
			sessionID = m.GenerationResult.RecoverySession
		}
	}
	var ids []string
	for _, m := range latest {
		if m.GenerationResult.State == "failed" && !m.GenerationResult.RecoveryQueued &&
			(m.RetryRequest == nil || m.RetryRequest.Status == "failed") {
			ids = append(ids, m.ID)
		}
	}
	type failedStep struct {
		jobID  string
		index  int
		partID string
		error  string
	}
	var steps []failedStep
	for _, job := range s.ArrangementJobs {
		if job.ProjectID != projectID || job.Revision != arrangementRevision(d) || job.Status != "failed" && job.Status != "interrupted" {
			continue
		}
		for i, step := range job.Steps {
			if step.Source != "ai" || step.Status != "failed" {
				continue
			}
			if step.RecoverySession != "" {
				sessionID = step.RecoverySession
			}
			// 已有当前候选时由候选检查驱动，不能为同一错误重复派发。
			if step.RecoveryQueued || slices.ContainsFunc(ordered, func(m schema.ArrangementMaterial) bool {
				return m.ProjectID == projectID && m.Generation != nil && m.GenerationResult != nil && m.SectionIndex == step.SectionIndex && m.PhraseIndex == step.PhraseIndex && m.PartIndex == step.PartIndex && arrangementMaterialMatches(m, d, ArrangementPartRef{step.SectionIndex, step.PhraseIndex, step.PartIndex})
			}) {
				continue
			}
			steps = append(steps, failedStep{job.ID, i, step.PartID, step.Error})
		}
	}
	if len(ids) == 0 && len(steps) == 0 {
		return false, nil
	}
	slices.Sort(ids)
	if sessionID == "" || assistantSessionByID(s, sessionID) == nil {
		sessionID = schema.NewID()
	}
	// 不可用的助手保持明确的失败候选，不创建假对话或循环重试。
	if _, err := w.assistantConfig(sessionID); err != nil {
		_ = w.Store.Update(func(s *schema.State) {
			for i := range s.ArrangementMaterials {
				if slices.Contains(ids, s.ArrangementMaterials[i].ID) {
					s.ArrangementMaterials[i].GenerationResult.RecoveryError = "Ocho 自动诊断暂不可用，请检查线上 AI 服务配置和助手运行时；原候选和失败原因已保留，可继续单项处理。"
				}
			}
			for j := range s.ArrangementJobs {
				for _, failed := range steps {
					if s.ArrangementJobs[j].ID == failed.jobID {
						s.ArrangementJobs[j].Steps[failed.index].RecoveryError = "Ocho 自动诊断不可用，请检查线上 AI 配置；失败任务已保留。"
					}
				}
			}
		}, true)
		return false, err
	}
	now := time.Now().UTC()
	err := w.Store.Update(func(s *schema.State) {
		if s.AssistantSessions == nil {
			s.AssistantSessions = &schema.AssistantSessions{}
		}
		if assistantSessionByID(*s, sessionID) == nil {
			s.AssistantSessions.Sessions = append(s.AssistantSessions.Sessions, schema.AssistantSession{
				ID: sessionID, Title: "编曲自动诊断 · " + d.Title, UpdatedAt: now, Messages: []schema.AssistantMessage{},
			})
			if s.AssistantSessions.CurrentID == "" {
				s.AssistantSessions.CurrentID = sessionID
			}
		}
		for i := range s.ArrangementMaterials {
			m := &s.ArrangementMaterials[i]
			if slices.Contains(ids, m.ID) {
				m.GenerationResult.RecoveryQueued = true
				m.GenerationResult.RecoverySession = sessionID
				m.GenerationResult.RecoveryError = ""
			}
		}
		for j := range s.ArrangementJobs {
			for _, failed := range steps {
				if s.ArrangementJobs[j].ID == failed.jobID {
					step := &s.ArrangementJobs[j].Steps[failed.index]
					step.RecoveryQueued = true
					step.RecoverySession = sessionID
					step.RecoveryError = ""
				}
			}
		}
	}, true)
	if err != nil {
		return false, err
	}
	text := fmt.Sprintf("【智能编曲后台失败事件】作品ID=%s，候选ID=%v；预检/启动失败声部=%v。用户已要求编曲生成失败时主动诊断并局部修复。先读取get_work、arrangement_jobs和get_generation_status中的arrangementMaterials检查结果，再调用arrangement_generation_preflight；不要将事件当作新的音乐需求。只修复失败项，保留已完成素材、曲谱、乐器和来源限制。随机生成失败可用retry_arrangement_material，最多三轮；确定性合同/长度/能力问题不要原样重试。需要改变生成方案时先ask_user，选项必须依次为%v；用户明确选第一项后可用replan_arrangement_generation仅修改所选声部的生成设置，并重新预检、启动该声部；不允许改乐器、来源和原谱。自定义意见存在时先澄清其含义。第三轮仍失败时第四轮必须先征求用户同意。重试返回started不代表成功，请查询实际状态；待试听时由编曲页试听卡交给用户确认，禁止代为确认。", projectID, ids, steps, arrangementRecoveryOptions)
	if err = w.sendAssistantToSession(sessionID, text, AssistantContext{Page: "arrangement", WorkID: projectID, WorkTitle: d.Title, WorkKind: "score", Locale: s.Preferences.UiLocale}); err != nil {
		_ = w.Store.Update(func(s *schema.State) {
			for i := range s.ArrangementMaterials {
				if slices.Contains(ids, s.ArrangementMaterials[i].ID) {
					s.ArrangementMaterials[i].GenerationResult.RecoveryQueued = false
				}
			}
			for j := range s.ArrangementJobs {
				for _, failed := range steps {
					if s.ArrangementJobs[j].ID == failed.jobID {
						s.ArrangementJobs[j].Steps[failed.index].RecoveryQueued = false
					}
				}
			}
		}, true)
		return false, err
	}
	return true, nil
}

func (w *Workbench) recordArrangementRecoveryError(sessionID, message string) {
	_ = w.Store.Update(func(s *schema.State) {
		for i := range s.ArrangementMaterials {
			m := &s.ArrangementMaterials[i]
			if m.GenerationResult != nil && m.GenerationResult.RecoverySession == sessionID {
				m.GenerationResult.RecoveryError = message
			}
		}
		for j := range s.ArrangementJobs {
			for i := range s.ArrangementJobs[j].Steps {
				if s.ArrangementJobs[j].Steps[i].RecoverySession == sessionID {
					s.ArrangementJobs[j].Steps[i].RecoveryError = message
				}
			}
		}
	}, true)
}

// 应用重启后，继续处理已经持久化但尚未收到助手回复的失败事件；不重复追加用户消息。
func (w *Workbench) resumePendingArrangementRecoveries() {
	s := w.Store.Read()
	var sessions []string
	for _, m := range s.ArrangementMaterials {
		if m.GenerationResult != nil && m.GenerationResult.RecoveryQueued && m.GenerationResult.RecoverySession != "" {
			sessions = append(sessions, m.GenerationResult.RecoverySession)
		}
	}
	for _, job := range s.ArrangementJobs {
		for _, step := range job.Steps {
			if step.RecoveryQueued && step.RecoverySession != "" {
				sessions = append(sessions, step.RecoverySession)
			}
		}
	}
	slices.Sort(sessions)
	sessions = slices.Compact(sessions)
	for _, id := range sessions {
		session := assistantSessionByID(s, id)
		if session == nil || len(session.Messages) == 0 {
			continue
		}
		last := session.Messages[len(session.Messages)-1]
		if last.Role != "user" || !strings.Contains(last.Text, "【智能编曲后台失败事件】") {
			continue
		}
		if _, err := w.assistantConfig(id); err != nil {
			w.recordArrangementRecoveryError(id, "Ocho 自动诊断暂不可用，请检查线上 AI 服务配置和助手运行时；原候选与失败原因已保留。")
			continue
		}
		_ = w.enqueueAssistantTurn(id, last.Text, AssistantContext{Page: "arrangement", WorkID: sessionRecoveryWorkID(s, id), WorkTitle: "编曲自动诊断", WorkKind: "score", Locale: s.Preferences.UiLocale})
	}
}

func sessionRecoveryWorkID(s schema.State, id string) string {
	for _, m := range s.ArrangementMaterials {
		if m.GenerationResult != nil && m.GenerationResult.RecoverySession == id {
			return m.ProjectID
		}
	}
	for _, job := range s.ArrangementJobs {
		for _, step := range job.Steps {
			if step.RecoverySession == id {
				return job.ProjectID
			}
		}
	}
	return ""
}
