package workbench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
	"yovoice/internal/assistant"
	"yovoice/internal/assistant/fakeocho"
	"yovoice/internal/schema"
)

func TestArrangementFailureAutomaticallyDiagnosesOnceInSeparateSession(t *testing.T) {
	w := assistantWorkbench(t, fakeocho.Plan{})
	d, ref := recoveryDraft(t, w)
	m := recoveryCandidate(t, w, d, ref, "failed")
	m.GenerationResult.RepairRound = 3
	m.GenerationResult.Checks = []schema.GenerationCheck{{Code: "PITCH", Status: "fail", Message: "目标音高不匹配"}}
	must(t, w.persistGenerationMaterial(m))
	original, err := w.NewAssistantSession()
	must(t, err)
	must(t, w.Store.Update(func(s *schema.State) {
		assistantSessionByID(*s, original.ID).Messages = []schema.AssistantMessage{{ID: schema.NewID(), Role: "assistant", Text: "其他作品的对话", Status: "completed"}}
	}, true))
	plan := fakeocho.Plan{Turns: [][]fakeocho.Step{{
		{Call: "get_generation_status", Args: mustJSON(map[string]any{"id": d.ID})},
		{Call: "arrangement_generation_preflight", Args: mustJSON(map[string]any{"projectId": d.ID, "ref": ref, "modelId": m.ModelID})},
		{Call: "retry_arrangement_material", Args: mustJSON(map[string]any{"projectId": d.ID, "materialId": m.ID})},
		{Result: "success"},
	}}}
	data, err := json.Marshal(plan)
	must(t, err)
	path := filepath.Join(t.TempDir(), "auto-repair.json")
	must(t, os.WriteFile(path, data, 0600))
	w.assistant = assistant.New(w.Store.Root, assistant.Options{Tools: assistantTools, NewServer: w.newAssistantServer, ExtraEnv: []string{fakeocho.PlanEnv + "=" + path}})
	started, err := w.requestArrangementRecovery(d.ID)
	must(t, err)
	if !started {
		t.Fatal("失败没有主动进入助手诊断")
	}
	started, err = w.requestArrangementRecovery(d.ID)
	must(t, err)
	if started {
		t.Fatal("同一失败重复入队")
	}
	waitChatIdle(t, w)
	s := w.Store.Read()
	if s.AssistantSessions.CurrentID != original.ID {
		t.Fatal("自动诊断切走了用户的原会话")
	}
	recoveryID := s.ArrangementMaterials[0].GenerationResult.RecoverySession
	q := sessionQuestion(s, recoveryID, "")
	if q == nil || q.State != "pending" || q.WorkID != d.ID {
		t.Fatal("三轮失败后未在作品上下文创建第四轮问答卡")
	}
	if s.ArrangementMaterials[0].GenerationResult.State != "failed" {
		t.Fatal("自动诊断伪造了生成成功")
	}
	if messages := assistantSessionByID(s, recoveryID).Messages; len(messages) == 0 || !strings.Contains(messages[0].Text, "后台失败事件") {
		t.Fatal("自动事件没有明确标记来源")
	}
}

func TestArrangementRecoveryIgnoresSupersededAndStaleFailures(t *testing.T) {
	w := assistantWorkbench(t, fakeocho.Plan{})
	d, ref := recoveryDraft(t, w)
	old := recoveryCandidate(t, w, d, ref, "failed")
	healthy := old
	healthy.ID, healthy.CreatedAt = schema.NewID(), old.CreatedAt.Add(time.Second)
	r := *old.GenerationResult
	r.State = "needs_review"
	healthy.GenerationResult = &r
	must(t, w.persistGenerationMaterial(healthy))
	if started, err := w.requestArrangementRecovery(d.ID); err != nil || started {
		t.Fatalf("健康的新候选仍被旧失败触发：%v %v", started, err)
	}
	d.Score.Tracks[1].Notes[0].Pitch++
	must(t, w.SaveDraft(d))
	if started, err := w.requestArrangementRecovery(d.ID); err != nil || started {
		t.Fatalf("旧曲谱失败仍触发诊断：%v %v", started, err)
	}
	d.Arrangement.Sections = nil
	must(t, w.Store.Update(func(s *schema.State) {
		for i := range s.Drafts {
			if s.Drafts[i].ID == d.ID {
				s.Drafts[i] = d
			}
		}
	}, true))
	if started, err := w.requestArrangementRecovery(d.ID); err != nil || started {
		t.Fatalf("删掉乐句后旧候选仍触发诊断：%v %v", started, err)
	}
	_ = arrangementDiagnosticMaterials(w.Store.Read(), d)
}

func TestArrangementDiagnosticContainsActualChecksWithoutRawRequest(t *testing.T) {
	w, err := New(t.TempDir())
	must(t, err)
	defer w.Close()
	d, ref := recoveryDraft(t, w)
	m := recoveryCandidate(t, w, d, ref, "failed")
	m.GenerationResult.Request = map[string]any{"private": "raw-request-must-not-be-returned"}
	m.GenerationResult.Checks = []schema.GenerationCheck{{Code: "PITCH", Status: "fail", Message: "wrong pitch"}}
	must(t, w.persistGenerationMaterial(m))
	data, err := json.Marshal(arrangementDiagnosticMaterials(w.Store.Read(), d))
	must(t, err)
	if !strings.Contains(string(data), "PITCH") || !strings.Contains(string(data), m.ID) || strings.Contains(string(data), "raw-request") || strings.Contains(string(data), m.FileName) {
		t.Fatalf("诊断没有隔离原始请求/文件名或缺少实际检查：%s", data)
	}
}

func TestAutomaticRecoveryCannotRewriteWholePlanWithoutSpecificConsent(t *testing.T) {
	w, err := New(t.TempDir())
	must(t, err)
	defer w.Close()
	d, ref := recoveryDraft(t, w)
	m := recoveryCandidate(t, w, d, ref, "failed")
	m.GenerationResult.RecoverySession = "recovery"
	must(t, w.persistGenerationMaterial(m))
	s := w.Store.Read()
	if arrangementRecoveryToolAllowed(s, "recovery", d.ID, "plan_arrangement") || arrangementRecoveryToolAllowed(s, "recovery", d.ID, "update_work") {
		t.Fatal("自动诊断可绕过同意改写作品")
	}
	if !arrangementRecoveryToolAllowed(s, "recovery", d.ID, "retry_arrangement_material") || !arrangementRecoveryToolAllowed(s, "", d.ID, "update_work") {
		t.Fatal("限定重试或原有手动操作被误封锁")
	}
	q := schema.AssistantQuestion{ID: "choice", WorkID: d.ID, Revision: questionRevision(d), Options: arrangementRecoveryOptions, State: "answered", Selected: []int{0}}
	s.AssistantSessions = &schema.AssistantSessions{Sessions: []schema.AssistantSession{{ID: "recovery", Messages: []schema.AssistantMessage{{Blocks: []schema.AssistantBlock{{Kind: "question", Question: &q}}}}}}}
	if arrangementRecoveryToolAllowed(s, "recovery", d.ID, "plan_arrangement") {
		t.Fatal("局部同意被扩大成整曲同意")
	}
	if !arrangementRecoveryToolAllowed(s, "recovery", d.ID, "replan_arrangement_generation") {
		t.Fatal("明确的局部授权未开放受限修复工具")
	}
	q.Selected = []int{1}
	if !arrangementRecoveryToolAllowed(s, "recovery", d.ID, "plan_arrangement") {
		t.Fatal("明确的整曲规划同意未生效")
	}
	for i := range s.Drafts {
		if s.Drafts[i].ID == d.ID {
			s.Drafts[i].Score.Tracks[1].Notes[0].Pitch++
		}
	}
	if arrangementRecoveryToolAllowed(s, "recovery", d.ID, "plan_arrangement") {
		t.Fatal("旧修订的同意被用于新曲谱")
	}
}

func TestPreflightFailureWithoutCandidateAutomaticallyDiagnoses(t *testing.T) {
	w := assistantWorkbench(t, fakeocho.Plan{})
	original, err := w.NewAssistantSession()
	must(t, err)
	must(t, w.Store.Update(func(s *schema.State) { s.AssistantSessions.CurrentID = original.ID }, true))
	d, ref := recoveryDraft(t, w)
	part := d.Arrangement.Sections[ref.SectionIndex].Phrases[ref.PhraseIndex].Parts[ref.PartIndex]
	must(t, w.Store.Update(func(s *schema.State) {
		s.ArrangementJobs = []schema.ArrangementJob{{ID: "failed-job", ProjectID: d.ID, Revision: arrangementRevision(d), Status: "failed", Steps: []schema.ArrangementJobStep{{PartID: part.ID, SectionIndex: ref.SectionIndex, PhraseIndex: ref.PhraseIndex, PartIndex: ref.PartIndex, Source: "ai", Status: "failed", Error: "自定义长度不能覆盖曲谱"}}}}
	}, true))
	started, err := w.requestArrangementRecovery(d.ID)
	must(t, err)
	if !started {
		t.Fatal("预检失败没有自动进入诊断")
	}
	started, err = w.requestArrangementRecovery(d.ID)
	must(t, err)
	if started {
		t.Fatal("预检失败被重复派发")
	}
	// 自动诊断不切走原会话；测试只把等待目标指向后台诊断会话。
	s := w.Store.Read()
	step := s.ArrangementJobs[0].Steps[0]
	if !step.RecoveryQueued || step.RecoverySession == "" {
		t.Fatal("失败事件未持久登记")
	}
	if s.AssistantSessions.CurrentID != original.ID {
		t.Fatal("无候选诊断切走了用户当前会话")
	}
	if session := assistantSessionByID(s, step.RecoverySession); session == nil || len(session.Messages) != 1 || !strings.Contains(session.Messages[0].Text, "预检/启动失败") {
		t.Fatal("无候选失败没有持久投递诊断事件")
	}
	if arrangementRecoveryToolAllowed(s, step.RecoverySession, d.ID, "update_work") {
		t.Fatal("无候选诊断绕过写入保护")
	}
}

func TestPendingAutomaticRecoveryResumesAfterRestartWithoutDuplicateEvent(t *testing.T) {
	w := assistantWorkbench(t, fakeocho.Plan{})
	d, ref := recoveryDraft(t, w)
	m := recoveryCandidate(t, w, d, ref, "failed")
	sessionID := schema.NewID()
	text := "【智能编曲后台失败事件】作品ID=" + d.ID + "候选ID=" + m.ID
	m.GenerationResult.RecoveryQueued, m.GenerationResult.RecoverySession = true, sessionID
	must(t, w.persistGenerationMaterial(m))
	must(t, w.Store.Update(func(s *schema.State) {
		if s.AssistantSessions == nil {
			s.AssistantSessions = &schema.AssistantSessions{}
		}
		s.AssistantSessions.Sessions = append(s.AssistantSessions.Sessions, schema.AssistantSession{ID: sessionID, Title: "编曲自动诊断", Messages: []schema.AssistantMessage{{ID: schema.NewID(), Role: "user", Text: text}}})
		s.AssistantSessions.CurrentID = sessionID
	}, true))
	// 模拟助手队列正在执行其他消息，确保恢复任务入队但不启动子进程。
	w.chat.mu.Lock()
	w.chat.running = true
	w.chat.mu.Unlock()
	w.resumePendingArrangementRecoveries()
	w.chat.mu.Lock()
	queue := slices.Clone(w.chat.queue)
	w.chat.queue, w.chat.running = nil, false
	w.chat.mu.Unlock()
	saved := assistantSessionByID(w.Store.Read(), sessionID)
	if len(queue) != 1 || queue[0].sessionID != sessionID || queue[0].text != text || len(saved.Messages) != 1 {
		t.Fatalf("重启后未从持久事件续接，或重复追加了事件：queue=%+v messages=%+v", queue, saved.Messages)
	}
}

func TestLocalGenerationReplanPreservesScoreAndOtherParts(t *testing.T) {
	w, err := New(t.TempDir())
	must(t, err)
	defer w.Close()
	d, ref := recoveryDraft(t, w)
	m := recoveryCandidate(t, w, d, ref, "failed")
	m.GenerationResult.RecoverySession = "repair"
	must(t, w.persistGenerationMaterial(m))
	executable, err := os.Executable()
	must(t, err)
	model := filepath.Join(w.Store.Root, "models", "fake.gguf")
	must(t, os.WriteFile(model, []byte("test"), 0600))
	q := schema.AssistantQuestion{ID: "consent", SessionID: "repair", WorkID: d.ID, Revision: questionRevision(d), State: "answered", Options: arrangementRecoveryOptions, Selected: []int{0}}
	must(t, w.Store.Update(func(s *schema.State) {
		s.Models = []schema.InstalledModel{{ID: "ace-step-1.5-turbo-q8", Path: model}}
		s.RuntimePath = &executable
		s.RuntimeBackend = ptr("cpu")
		s.Preferences.Backend = "cpu"
		s.AssistantSessions = &schema.AssistantSessions{Sessions: []schema.AssistantSession{{ID: "repair", Messages: []schema.AssistantMessage{{Blocks: []schema.AssistantBlock{{Kind: "question", Question: &q}}}}}}}
	}, true))
	settings := generationSettings(d.Arrangement.Sections[ref.SectionIndex].Phrases[ref.PhraseIndex].Parts[ref.PartIndex])
	settings.ModelID = m.ModelID
	settings.TailSeconds = 1
	input := ArrangementGenerationRepairInput{ProjectID: d.ID, QuestionID: q.ID, Ref: ref, Settings: settings, sessionID: "repair"}
	bad := input
	bad.Settings.Mode = "score"
	if _, err = w.ReplanArrangementGeneration(bad); err == nil {
		t.Fatal("局部修复改变了创作意图")
	}
	batch, err := w.ReplanArrangementGeneration(input)
	must(t, err)
	if len(batch.Steps) != 1 || batch.Steps[0].PartID != m.PartID {
		t.Fatal("局部修复启动了其他声部")
	}
	saved, err := w.assistantWork(d.ID)
	must(t, err)
	if !reflect.DeepEqual(saved.Score, d.Score) || !reflect.DeepEqual(saved.Arrangement.Sections[0].Phrases[0], d.Arrangement.Sections[0].Phrases[0]) {
		t.Fatal("局部修复修改原谱或其他乐句")
	}
	_ = awaitPreparation(t, w, batch.ID)
	if _, err = w.ReplanArrangementGeneration(input); err == nil {
		t.Fatal("旧修订授权被重复使用")
	}
}
