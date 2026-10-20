package schema

import (
	"encoding/json"
	"time"
	"yovoice/internal/msg"
)

// AIProvider 是用户配置的大模型服务（可选功能）。密钥不写入状态，单独保存在数据目录的 secrets.json。
type AIProvider struct {
	ID         string   `json:"id"`
	Preset     string   `json:"preset"`
	Name       string   `json:"name"`
	Format     string   `json:"format"`
	BaseURL    string   `json:"baseURL"`
	ModelsPath string   `json:"modelsPath"`
	Model      string   `json:"model"`
	Models     []string `json:"models,omitempty"`
	// KeyMask 只保留密钥首尾，用于界面提示已填写。
	KeyMask  string  `json:"keyMask,omitempty"`
	LastTest *AITest `json:"lastTest,omitempty"`
}

// AITest 记录最近一次连通测试：成功时 Code 为空。
type AITest struct {
	OK     bool       `json:"ok"`
	Code   msg.Code   `json:"code,omitempty"`
	Params msg.Params `json:"params,omitempty"`
	At     time.Time  `json:"at"`
}

const AIProviderLimit = 16

// AI 请求（写作任务与助手对话一轮）的默认与允许的超时（秒），每次请求单独计时。
const (
	AITimeoutDefault = 600
	AITimeoutMin     = 30
	AITimeoutMax     = 1800
)

// AITask 是后台运行的 AI 写作任务：关闭对话框后仍在进行，结束后结果留在状态里等界面取用。
// 只在内存中有效，重启后清除。Status 为 running、completed、failed 或 cancelled。
type AITask struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	ProjectID string `json:"projectId"`
	Brief     string `json:"brief"`
	Status    string `json:"status"`
	Stage     string `json:"stage"`
	// Done 与 Total 是分段任务（如整本书分角色）的进度，单次请求的任务为 0。
	Done        int             `json:"done,omitempty"`
	Total       int             `json:"total,omitempty"`
	Provider    string          `json:"provider"`
	Model       string          `json:"model"`
	Timeout     int             `json:"timeout"`
	StartedAt   time.Time       `json:"startedAt"`
	FinishedAt  *time.Time      `json:"finishedAt,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
	ErrorCode   msg.Code        `json:"errorCode,omitempty"`
	ErrorParams msg.Params      `json:"errorParams,omitempty"`
}
