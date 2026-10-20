package schema

import "time"

// ArrangementJob 保存用户发起的备料批次，重启后可从成功的乐句继续。
type ArrangementJob struct {
	ID        string               `json:"id"`
	ProjectID string               `json:"projectId"`
	Revision  string               `json:"revision"`
	Mode      string               `json:"mode"`
	ModelID   string               `json:"modelId,omitempty"`
	Status    string               `json:"status"`
	Error     string               `json:"error,omitempty"`
	Steps     []ArrangementJobStep `json:"steps"`
	Draft     Draft                `json:"draft"`
	CreatedAt time.Time            `json:"createdAt"`
	UpdatedAt time.Time            `json:"updatedAt"`
}

type ArrangementJobStep struct {
	PartID       string `json:"partId,omitempty"`
	SectionIndex int    `json:"sectionIndex"`
	PhraseIndex  int    `json:"phraseIndex"`
	PartIndex    int    `json:"partIndex"`
	Instrument   string `json:"instrument"`
	Source       string `json:"source"`
	Status       string `json:"status"`
	Error        string `json:"error,omitempty"`
}

// ArrangementScorePhrase 是已通过本句校验的写谱进度，不覆盖用户当前乐谱。
type ArrangementScorePhrase struct {
	ID            string    `json:"id"`
	ProjectID     string    `json:"projectId"`
	ConditionHash string    `json:"conditionHash"`
	PhraseID      string    `json:"phraseId"`
	Score         Score     `json:"score"`
	UpdatedAt     time.Time `json:"updatedAt"`
}
