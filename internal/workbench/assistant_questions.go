package workbench

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"yovoice/internal/schema"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type AssistantQuestionInput struct {
	Prompt   string   `json:"prompt" jsonschema:"简洁且可以独立理解的问题"`
	Options  []string `json:"options" jsonschema:"2至6个具体选项；界面会自动附加自定义回答框"`
	Multiple bool     `json:"multiple,omitempty"`
}

type AssistantAnswerInput struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	Action    string `json:"action"`
	Selected  []int  `json:"selected,omitempty"`
	Custom    string `json:"custom,omitempty"`
}

func questionRevision(d schema.Draft) string {
	b, _ := json.Marshal([]any{d.Text, d.SynthesisSettings, d.Score, d.Arrangement, d.Timeline})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func sessionQuestion(s schema.State, sessionID, id string) *schema.AssistantQuestion {
	session := assistantSessionByID(s, sessionID)
	if session == nil {
		return nil
	}
	for _, message := range session.Messages {
		for _, block := range message.Blocks {
			if q := block.Question; q != nil && (id == "" && q.State == "pending" || id != "" && q.ID == id) {
				return q
			}
		}
	}
	return nil
}

func (w *Workbench) registerQuestionTool(server *mcp.Server) {
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method == "tools/call" {
				w.chat.mu.Lock()
				turn := w.chat.turn
				waiting := turn != nil && turn.waiting
				sessionID := ""
				if turn != nil {
					sessionID = turn.sessionID
				}
				w.chat.mu.Unlock()
				if waiting {
					return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "awaiting_user: 本轮已提交问题，请结束回复，等待用户在问答卡回答。"}}}, nil
				}
				if sessionQuestion(w.Store.Read(), sessionID, "") != nil {
					name := ""
					switch p := req.GetParams().(type) {
					case *mcp.CallToolParamsRaw:
						name = p.Name
					case *mcp.CallToolParams:
						name = p.Name
					}
					if !slices.Contains([]string{"ask_user", "search_resources", "arrangement_inventory", "inspect_arrangement", "list_works", "get_work", "list_models", "list_voices", "list_characters", "status", "get_app_status", "get_recent_errors", "get_generation_status", "arrangement_jobs"}, name) {
						return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "awaiting_user: 当前会话还有待回答的问题，请先请用户回答或取消问答卡，不能代用户作决定。"}}}, nil
					}
				}
			}
			return next(ctx, method, req)
		}
	})
	mcp.AddTool(server, &mcp.Tool{Name: "ask_user", Description: "向用户显示可点选问答卡，自动提供自定义回答框；支持单选/多选。只问缺少的信息或偏好，不能用普通选项代替下载、删除或上传确认。调用后本轮结束，用户回答会开启原会话的新一轮；不要继续依赖答案执行动作。"}, func(ctx context.Context, req *mcp.CallToolRequest, in AssistantQuestionInput) (*mcp.CallToolResult, any, error) {
		q, err := w.AskAssistantQuestion(in)
		return nil, map[string]any{"state": "awaiting_user", "question": q}, err
	})
}

func (w *Workbench) AskAssistantQuestion(in AssistantQuestionInput) (schema.AssistantQuestion, error) {
	in.Prompt = strings.TrimSpace(in.Prompt)
	if in.Prompt == "" || schema.TextLen(in.Prompt) > 600 || len(in.Options) < 2 || len(in.Options) > 6 {
		return schema.AssistantQuestion{}, fmt.Errorf("问题需要2至6个选项，且不超过600字")
	}
	seen := map[string]bool{}
	for i, option := range in.Options {
		option = strings.TrimSpace(option)
		if option == "" || schema.TextLen(option) > 200 || seen[option] {
			return schema.AssistantQuestion{}, fmt.Errorf("选项不能为空、重复或超过200字")
		}
		seen[option] = true
		in.Options[i] = option
	}
	w.chat.mu.Lock()
	turn := w.chat.turn
	if turn == nil || turn.waiting {
		w.chat.mu.Unlock()
		return schema.AssistantQuestion{}, fmt.Errorf("没有可以提问的助手回合")
	}
	q := schema.AssistantQuestion{ID: schema.NewID(), SessionID: turn.sessionID, TurnID: turn.id, WorkID: turn.context.WorkID, Prompt: in.Prompt, Options: slices.Clone(in.Options), Multiple: in.Multiple, State: "pending"}
	w.chat.mu.Unlock()
	s := w.Store.Read()
	if existing := sessionQuestion(s, q.SessionID, ""); existing != nil {
		return *existing, nil
	}
	if q.WorkID != "" {
		for _, d := range s.Drafts {
			if d.ID == q.WorkID {
				q.Revision = questionRevision(d)
			}
		}
	}
	found := false
	err := w.Store.Update(func(s *schema.State) {
		session := assistantSessionByID(*s, q.SessionID)
		if session == nil {
			return
		}
		found = true
		session.Messages = append(session.Messages, schema.AssistantMessage{ID: schema.NewID(), Role: "assistant", Status: "completed", CreatedAt: time.Now().UTC(), Blocks: []schema.AssistantBlock{{Kind: "question", Question: &q}}})
	}, true)
	if err != nil {
		return schema.AssistantQuestion{}, err
	}
	if !found {
		return schema.AssistantQuestion{}, fmt.Errorf("原会话已关闭")
	}
	w.chat.mu.Lock()
	if w.chat.turn == turn {
		turn.waiting = true
	}
	w.chat.mu.Unlock()
	// 先让 MCP 返回问答身份，再中止等待中的模型；人的思考时间不计入模型超时。
	time.AfterFunc(350*time.Millisecond, func() {
		w.chat.mu.Lock()
		same := w.chat.turn == turn
		yield := turn.yield
		w.chat.mu.Unlock()
		if same && yield != nil {
			yield()
		}
	})
	return q, nil
}

func (w *Workbench) AnswerAssistantQuestion(in AssistantAnswerInput) error {
	if in.Action != "submit" && in.Action != "cancel" && in.Action != "later" {
		return fmt.Errorf("无效回答动作")
	}
	in.Custom = strings.TrimSpace(in.Custom)
	if schema.TextLen(in.Custom) > 2000 {
		return fmt.Errorf("自定义回答过长")
	}
	var next schema.AssistantQuestion
	var failure error
	accepted := false
	err := w.Store.Update(func(s *schema.State) {
		if s.AssistantSessions == nil || s.AssistantSessions.CurrentID != in.SessionID {
			failure = fmt.Errorf("请回到提问的会话再回答")
			return
		}
		q := sessionQuestion(*s, in.SessionID, in.ID)
		if q == nil {
			failure = fmt.Errorf("没有找到原问题")
			return
		}
		if q.State == "answered" && in.Action == "submit" && slices.Equal(q.Selected, in.Selected) && q.Custom == in.Custom {
			return
		}
		if q.State != "pending" && q.State != "deferred" {
			failure = fmt.Errorf("问题已处理，请勿重复提交")
			return
		}
		if in.Action == "later" {
			q.State = "deferred"
			return
		}
		if in.Action == "cancel" {
			q.State = "cancelled"
			return
		}
		if len(in.Selected) == 0 && in.Custom == "" || !q.Multiple && len(in.Selected) > 1 {
			failure = fmt.Errorf("请选择答案或填写自定义回答")
			return
		}
		seen := map[int]bool{}
		for _, i := range in.Selected {
			if i < 0 || i >= len(q.Options) || seen[i] {
				failure = fmt.Errorf("回答选项无效")
				return
			}
			seen[i] = true
		}
		if q.WorkID != "" {
			found := false
			for _, d := range s.Drafts {
				if d.ID == q.WorkID {
					found = true
					if questionRevision(d) != q.Revision {
						failure = fmt.Errorf("作品已改变，请让助手重新读取作品并提问")
						return
					}
				}
			}
			if !found {
				failure = fmt.Errorf("原作品已删除")
				return
			}
		}
		q.State = "answered"
		q.Selected = slices.Clone(in.Selected)
		q.Custom = in.Custom
		next = *q
		accepted = true
	}, true)
	if err != nil {
		return err
	}
	if failure != nil {
		return failure
	}
	if !accepted {
		return nil
	}
	answers := []string{}
	for _, i := range next.Selected {
		answers = append(answers, next.Options[i])
	}
	if next.Custom != "" {
		answers = append(answers, next.Custom)
	}
	text := fmt.Sprintf("问答卡回答（questionId=%s，turnId=%s）：\n问题：%s\n回答：%s\n请重新读取作品后继续。此回答不等于下载、删除或上传授权。", next.ID, next.TurnID, next.Prompt, strings.Join(answers, "；"))
	return w.sendAssistantToSession(next.SessionID, text, AssistantContext{WorkID: next.WorkID, Locale: w.Store.Read().Preferences.UiLocale})
}
