package schema

import (
	"time"
	"yovoice/internal/msg"
)

// 助手对话状态：会话与消息持久化（面板重开可见），进行中的一轮与界面事件只随状态广播、不入库。

// AssistantMessage 是助手对话中的一条消息：用户消息只有 Text；助手消息含按顺序的内容块，
// Text 为其中文本块的拼接（复制用）。Tools 只出现在旧版本记录里，新消息的工具卡片在 Blocks 中。
type AssistantMessage struct {
	ID          string              `json:"id"`
	Role        string              `json:"role"` // user 或 assistant
	Text        string              `json:"text,omitempty"`
	Blocks      []AssistantBlock    `json:"blocks,omitempty"`
	Tools       []AssistantToolCard `json:"tools,omitempty"`
	Status      string              `json:"status,omitempty"` // 助手消息的结束状态：completed、cancelled、failed
	ErrorCode   msg.Code            `json:"errorCode,omitempty"`
	ErrorParams msg.Params          `json:"errorParams,omitempty"`
	CreatedAt   time.Time           `json:"createdAt"`
}

// AssistantToolCard 是消息内一次工具调用的摘要；Args 为截断后的参数 JSON，点开卡片时查看。
type AssistantToolCard struct {
	Tool    string `json:"tool"`
	Summary string `json:"summary,omitempty"` // 动作摘要，如“修改了《示例小步舞曲》的乐谱”
	Args    string `json:"args,omitempty"`
	State   string `json:"state,omitempty"` // running、done 或 error
	// Proposal 是助手提议、等用户确认的操作；只出现在 propose_action 的卡片上。
	Proposal *AssistantProposal `json:"proposal,omitempty"`
}

// AssistantProposal 是助手提议的一次写操作（删除、安装模型、改设置等）：界面显示确认卡片，
// 用户确认后才执行，执行仍走与界面相同的应用服务。
type AssistantProposal struct {
	ID     string `json:"id"`
	Action string `json:"action"`
	// Target 是操作对象的 ID（作品、生成记录、角色或模型），Value 是设置的新值；Label 是给用户看的对象名。
	Target      string     `json:"target,omitempty"`
	Value       string     `json:"value,omitempty"`
	Label       string     `json:"label,omitempty"`
	Reason      string     `json:"reason,omitempty"`
	State       string     `json:"state"` // pending、confirmed、dismissed 或 failed
	ErrorCode   msg.Code   `json:"errorCode,omitempty"`
	ErrorParams msg.Params `json:"errorParams,omitempty"`
}

// AssistantMemo 是助手记住的一条用户偏好（常用音色、风格、语速等），每轮对话随上下文带上；设置里可查看与删除。
type AssistantMemo struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"createdAt"`
}

// AssistantMemoLimit 是偏好条数上限，超出时丢弃最旧的一条。
const AssistantMemoLimit = 30

// AssistantBlock 是助手消息里按出现顺序排列的一块：文本、思考，或一次工具调用（Card）。
type AssistantBlock struct {
	Kind string             `json:"kind"` // text、thinking 或 tool
	Text string             `json:"text,omitempty"`
	Card *AssistantToolCard `json:"card,omitempty"`
}

// AssistantSession 是一段助手对话：标题取首条用户消息，消息数量有上限；
// 每个会话对应独立的助手子进程时间线（数据目录按会话区分）。
type AssistantSession struct {
	ID        string             `json:"id"`
	Title     string             `json:"title"`
	Messages  []AssistantMessage `json:"messages"`
	UpdatedAt time.Time          `json:"updatedAt"`
}

// AssistantSessions 是助手会话集合，CurrentID 指向面板当前打开的会话。
type AssistantSessions struct {
	CurrentID string             `json:"currentId"`
	Sessions  []AssistantSession `json:"sessions"`
}

const (
	// AssistantSessionLimit 是保留的会话数上限，超出时丢弃最旧的会话。
	AssistantSessionLimit = 50
	// AssistantChatLimit 是单个会话的消息上限，超出时丢弃最旧的消息。
	AssistantChatLimit = 200
)

// AssistantTurn 是正在运行的一轮对话的实时状态：按顺序的内容块（含尚未定稿的流式增量）。
type AssistantTurn struct {
	Blocks    []AssistantBlock `json:"blocks,omitempty"`
	Model     string           `json:"model,omitempty"`
	StartedAt time.Time        `json:"startedAt"`
}

// AssistantNav 是助手要求界面跳转的一次事件，ID 变化即为新事件；WorkID 与 Page 二选一。
type AssistantNav struct {
	ID     string `json:"id"`
	WorkID string `json:"workId,omitempty"`
	Page   string `json:"page,omitempty"`
}

// AssistantUpdate 是助手最近一次保存的作品修改事件，界面据此刷新正打开的编辑器。
type AssistantUpdate struct {
	ID     string `json:"id"`
	WorkID string `json:"workId"`
}

// AssistantLive 是只随状态广播的助手实时信息：进行中的一轮、排队条数与界面事件。
type AssistantLive struct {
	Turn   *AssistantTurn   `json:"turn,omitempty"`
	Queued int              `json:"queued,omitempty"`
	Nav    *AssistantNav    `json:"nav,omitempty"`
	Update *AssistantUpdate `json:"update,omitempty"`
}
