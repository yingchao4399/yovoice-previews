package schema

import (
	"fmt"
	"strings"
)

type ArrangementInput struct {
	Brief               string `json:"brief"`
	Seconds             int    `json:"seconds"`
	Source              string `json:"source"`
	ReferenceAssetID    string `json:"referenceAssetId,omitempty"`
	ReferenceABC        string `json:"referenceAbc,omitempty"`
	ReferenceAnalysisAt int64  `json:"referenceAnalysisAt,omitempty"`
}

// ArrangementPlan 是可审核的整曲结构规划；音源选择按乐句中的声部分配。
type ArrangementPlan struct {
	Brief         string               `json:"brief,omitempty"`
	AIModelID     string               `json:"aiModelId,omitempty"`
	SourceMode    string               `json:"sourceMode,omitempty"`
	ReferenceABC  string               `json:"referenceAbc,omitempty"`
	Title         string               `json:"title"`
	Tempo         float64              `json:"tempo"`
	TimeSignature []int                `json:"timeSignature"`
	Key           string               `json:"key,omitempty"`
	Seconds       int                  `json:"seconds"`
	Sections      []ArrangementSection `json:"sections"`
	Approved      bool                 `json:"approved,omitempty"`
}

type ArrangementSection struct {
	ID       string              `json:"id,omitempty"`
	Name     string              `json:"name"`
	StartBar int                 `json:"startBar"`
	EndBar   int                 `json:"endBar"`
	Phrases  []ArrangementPhrase `json:"phrases"`
}

type ArrangementPhrase struct {
	ID       string            `json:"id,omitempty"`
	Name     string            `json:"name"`
	StartBar int               `json:"startBar"`
	EndBar   int               `json:"endBar"`
	Parts    []ArrangementPart `json:"parts"`
}

type ArrangementPart struct {
	ID         string `json:"id,omitempty"`
	Instrument string `json:"instrument"`
	Role       string `json:"role"`
	Technique  string `json:"technique,omitempty"`
	Source     string `json:"source"`
	SourceID   string `json:"sourceId,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Locked     bool   `json:"locked,omitempty"`
	// AllowSubstitution 仅由用户明确选择替代音色后设置，固定 ID 本身不代表允许换乐器。
	AllowSubstitution bool                           `json:"allowSubstitution,omitempty"`
	Generation        *ArrangementGenerationSettings `json:"generation,omitempty"`
}

// ArrangementIssue 限制模型输出大小与区间，避免不完整规划进入工程。
func ArrangementIssue(p *ArrangementPlan) string {
	if p == nil || len(p.ReferenceABC) > 64<<10 || len(p.AIModelID) > 120 || strings.TrimSpace(p.Title) == "" || TextLen(p.Title) > 120 || p.Tempo < 30 || p.Tempo > 240 || p.Seconds < 1 || p.Seconds > 600 || len(p.TimeSignature) != 2 || p.TimeSignature[0] < 1 || p.TimeSignature[0] > 12 || (p.TimeSignature[1] != 2 && p.TimeSignature[1] != 4 && p.TimeSignature[1] != 8 && p.TimeSignature[1] != 16) || len(p.Sections) == 0 || len(p.Sections) > 10 {
		return "invalid arrangement overview"
	}
	previous := 0
	ids := make(map[string]bool)
	checkID := func(id string) bool {
		if id == "" {
			return true // 旧工程尚未迁移，仍可按原字段读取。
		}
		if !ValidID(id) || ids[id] {
			return false
		}
		ids[id] = true
		return true
	}
	for _, section := range p.Sections {
		if !checkID(section.ID) || strings.TrimSpace(section.Name) == "" || section.StartBar != previous+1 || section.EndBar < section.StartBar || section.EndBar > ScoreMaxBars || len(section.Phrases) == 0 || len(section.Phrases) > 8 {
			return fmt.Sprintf("invalid section at bar %d", section.StartBar)
		}
		phraseEnd := section.StartBar - 1
		for _, phrase := range section.Phrases {
			if !checkID(phrase.ID) || strings.TrimSpace(phrase.Name) == "" || phrase.StartBar != phraseEnd+1 || phrase.EndBar < phrase.StartBar || phrase.EndBar > section.EndBar || len(phrase.Parts) == 0 || len(phrase.Parts) > 8 {
				return fmt.Sprintf("invalid phrase at bar %d", phrase.StartBar)
			}
			for _, part := range phrase.Parts {
				if !checkID(part.ID) || strings.TrimSpace(part.Instrument) == "" || strings.TrimSpace(part.Role) == "" || (part.Source != "real" && part.Source != "ai" && part.Source != "auto") || (p.Approved && part.Source == "auto") || (part.SourceID != "" && !ValidID(part.SourceID)) {
					return fmt.Sprintf("invalid instrument/source in phrase %q", phrase.Name)
				}
			}
			phraseEnd = phrase.EndBar
		}
		if phraseEnd != section.EndBar {
			return fmt.Sprintf("phrases do not cover section %q", section.Name)
		}
		previous = section.EndBar
	}
	return ""
}

// EnsureArrangementIDs 由服务端分配身份；忽略模型返回的任意 ID，避免伪造或冲突。
// 已保存的旧工程在读取时不强制重写，只在重新规划时获得 ID。
func EnsureArrangementIDs(p *ArrangementPlan) {
	for si := range p.Sections {
		section := &p.Sections[si]
		section.ID = NewID()
		for pi := range section.Phrases {
			phrase := &section.Phrases[pi]
			phrase.ID = NewID()
			for ai := range phrase.Parts {
				phrase.Parts[ai].ID = NewID()
			}
		}
	}
}

// 音源替换记录只在执行输入仍相同时撤销，避免覆盖后续人工改谱。
type ArrangementBindingEdit struct {
	PartID               string `json:"partId"`
	PreviousSourceID     string `json:"previousSourceId,omitempty"`
	PreviousSubstitution bool   `json:"previousSubstitution,omitempty"`
	PreviousScore        *Score `json:"previousScore,omitempty"`
	AppliedRevision      string `json:"appliedRevision"`
}
