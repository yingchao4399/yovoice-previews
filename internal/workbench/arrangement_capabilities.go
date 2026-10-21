package workbench

import (
	"encoding/json"
	"fmt"
	"yovoice/internal/catalog"
	"yovoice/internal/schema"
)

// 规划、写谱和预检共用音高规则；已生成的音色不能靠移调冒充未验证的音高。
func arrangementSamplePitchBounds(s schema.InstrumentSample) (int, int) {
	if s.ExactPitch || s.GenerationID != "" {
		return s.RootPitch, s.RootPitch
	}
	return max(0, s.RootPitch-7), min(127, s.RootPitch+7)
}

type arrangementSampleCapability struct {
	Root              int      `json:"root"`
	Low               int      `json:"low"`
	High              int      `json:"high"`
	VelocityLow       int      `json:"velocityLow"`
	VelocityHigh      int      `json:"velocityHigh"`
	MaxSustainSeconds *float64 `json:"maxSustainSeconds,omitempty"`
}

// 保留逐采样的奏法、力度和持续时长，不能用总音域填平中间缺口。
func arrangementSampleCapabilities(source schema.InstrumentSource) map[string][]arrangementSampleCapability {
	result := map[string][]arrangementSampleCapability{}
	seen := map[string]bool{}
	for _, s := range source.Samples {
		low, high := arrangementSamplePitchBounds(s)
		item := arrangementSampleCapability{Root: s.RootPitch, Low: low, High: high, VelocityLow: s.VelocityLow, VelocityHigh: s.VelocityHigh}
		if s.ExactPitch || s.GenerationID != "" {
			seconds := s.SustainSeconds
			item.MaxSustainSeconds = &seconds
		}
		encoded, _ := json.Marshal([]any{s.Articulation, item})
		if seen[string(encoded)] {
			continue
		}
		seen[string(encoded)] = true
		result[s.Articulation] = append(result[s.Articulation], item)
	}
	return result
}

func arrangementPartModel(plan schema.ArrangementPlan, part schema.ArrangementPart) string {
	if part.Generation != nil && part.Generation.ModelID != "" {
		return part.Generation.ModelID
	}
	return plan.AIModelID
}

// 同一模型生成的可扩充音色允许先写所需音符，再按缺口备料；仍不允许移调旧采样冒充覆盖。
func arrangementCanExtendAISource(plan schema.ArrangementPlan, part schema.ArrangementPart, source schema.InstrumentSource) bool {
	modelID := arrangementPartModel(plan, part)
	return part.Source == "ai" && generationSettings(part).Method == "sampler" && source.Origin == "ai" && source.Plugin == nil && modelID != "" && source.Provenance == "generated:"+modelID && sourceIdentityIssue(part, source) == ""
}

func arrangementDirectScoreFamily(plan schema.ArrangementPlan, part schema.ArrangementPart) string {
	settings := generationSettings(part)
	if part.Source != "ai" || settings.Method != "direct" || settings.Mode != "score" {
		return ""
	}
	if model, err := catalog.Lookup(arrangementPartModel(plan, part)); err == nil {
		return model.Family
	}
	return ""
}

func arrangementGenerationPlanIssue(plan schema.ArrangementPlan, part schema.ArrangementPart) error {
	if part.Source != "ai" {
		return nil
	}
	if err := validateGenerationSettings(generationSettings(part)); err != nil {
		return err
	}
	if modelID := arrangementPartModel(plan, part); modelID != "" {
		model, err := catalog.Lookup(modelID)
		if err != nil || model.Family != "yue2" && model.Family != "ace_step" {
			return fmt.Errorf("请选择 YuE2 或 ACE-Step 音频模型；线上语言模型只负责规划和写谱")
		}
	}
	if arrangementDirectScoreFamily(plan, part) == "ace_step" {
		return fmt.Errorf("当前 ACE-Step 适配器不接收逐音符曲谱；请在生成设置选择支持谱条件的模型或音色按谱演奏。不会自动改成自由生成")
	}
	return nil
}

// 这里只提供能力与证据，不阻止尚未安装音频模型的用户先规划和写谱。
func (w *Workbench) arrangementPlanningModels() string {
	type item struct {
		Capability ArrangementGenerationCapability `json:"capability"`
		Issue      string                          `json:"issue,omitempty"`
	}
	state := w.Store.Read()
	models := []item{}
	for _, model := range state.Models {
		definition, err := catalog.Lookup(model.ID)
		if err != nil || (definition.Family != "yue2" && definition.Family != "ace_step") {
			continue
		}
		capability, err := generationCapability(model.ID, value(state.RuntimeVersion))
		entry := item{Capability: capability}
		if err != nil {
			entry.Issue = err.Error()
		}
		models = append(models, entry)
	}
	data, _ := json.Marshal(models)
	return string(data)
}
