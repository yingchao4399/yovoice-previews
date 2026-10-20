package workbench

import (
	"encoding/json"
	"testing"
	"yovoice/internal/assistant/fakeocho"
	"yovoice/internal/schema"
)

func TestAssistantQuestionToolAndOriginalSessionContinuation(t *testing.T) {
	w := assistantWorkbench(t, fakeocho.Plan{Turns: [][]fakeocho.Step{
		{{Call: "ask_user", Args: json.RawMessage(`{"prompt":"选择怎样的编制？","options":["琵琶独奏","琵琶与箫"]}`)}, {Result: "success"}},
		{{Assistant: "收到，继续按你的选择编曲。"}, {Result: "success"}},
	}})
	must(t, w.SendAssistant("请先问我编制", AssistantContext{Locale: schema.UiLocaleZhCN}))
	waitChatIdle(t, w)
	s := w.Store.Read()
	sessionID := s.AssistantSessions.CurrentID
	q := sessionQuestion(s, sessionID, "")
	if q == nil {
		t.Fatal("工具没有显示持久问答卡")
	}
	input := AssistantAnswerInput{ID: q.ID, SessionID: sessionID, Action: "submit", Selected: []int{1}, Custom: "节奏自然"}
	must(t, w.AnswerAssistantQuestion(input))
	waitChatIdle(t, w)
	must(t, w.AnswerAssistantQuestion(input))
	s = w.Store.Read()
	if s.AssistantSessions.CurrentID != sessionID || sessionQuestion(s, sessionID, q.ID).State != "answered" {
		t.Fatal("没有在原会话完成回答")
	}
	answers := 0
	for _, m := range assistantSessionByID(s, sessionID).Messages {
		if m.Role == "user" {
			answers++
		}
	}
	if answers != 2 {
		t.Fatalf("重复回答产生了额外回合：%d", answers)
	}
}

func questionWorkbench(t *testing.T) (*Workbench, schema.AssistantQuestion) {
	t.Helper()
	w, e := New(t.TempDir())
	must(t, e)
	t.Cleanup(w.Close)
	session, e := w.NewAssistantSession()
	must(t, e)
	w.chat.turn = &chatTurn{id: schema.NewID(), sessionID: session.ID}
	q, e := w.AskAssistantQuestion(AssistantQuestionInput{Prompt: "希望怎样配器？", Options: []string{"琵琶独奏", "琵琶和箫"}})
	must(t, e)
	w.chat.turn = nil
	return w, q
}

func TestAssistantQuestionSurvivesRestartAndRejectsCrossSession(t *testing.T) {
	w, q := questionWorkbench(t)
	root := w.Store.Root
	w.Close()
	reopened, e := New(root)
	must(t, e)
	defer reopened.Close()
	if got := sessionQuestion(reopened.Store.Read(), q.SessionID, q.ID); got == nil || got.Prompt != q.Prompt || got.State != "pending" {
		t.Fatal("重开后问题丢失")
	}
	other, e := reopened.NewAssistantSession()
	must(t, e)
	if e = reopened.AnswerAssistantQuestion(AssistantAnswerInput{ID: q.ID, SessionID: other.ID, Action: "submit", Selected: []int{0}}); e == nil {
		t.Fatal("跨会话答案不应生效")
	}
}

func TestAssistantQuestionValidatesAnswersBeforeScheduling(t *testing.T) {
	w, q := questionWorkbench(t)
	for _, in := range []AssistantAnswerInput{
		{ID: q.ID, SessionID: q.SessionID, Action: "submit"},
		{ID: q.ID, SessionID: q.SessionID, Action: "submit", Selected: []int{2}},
		{ID: q.ID, SessionID: q.SessionID, Action: "submit", Selected: []int{0, 1}},
	} {
		if e := w.AnswerAssistantQuestion(in); e == nil {
			t.Fatal("无效回答被接受")
		}
	}
	must(t, w.AnswerAssistantQuestion(AssistantAnswerInput{ID: q.ID, SessionID: q.SessionID, Action: "later"}))
	if sessionQuestion(w.Store.Read(), q.SessionID, q.ID).State != "deferred" {
		t.Fatal("稍后回答没有保存")
	}
	must(t, w.AnswerAssistantQuestion(AssistantAnswerInput{ID: q.ID, SessionID: q.SessionID, Action: "cancel"}))
	if e := w.AnswerAssistantQuestion(AssistantAnswerInput{ID: q.ID, SessionID: q.SessionID, Action: "submit", Custom: "新的答案"}); e == nil {
		t.Fatal("取消后旧问题不应继续执行")
	}
}
