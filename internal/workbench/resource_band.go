package workbench

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"yovoice/internal/catalog"
	"yovoice/internal/schema"
)

type ResourceBandInput struct {
	Draft      schema.Draft `json:"draft"`
	ResourceID string       `json:"resourceId"`
}

// ApplyResourceBand 把组合的相同乐器、角色、来源绑定用于当前计划，锁定声部不变。
// 组合是可复用的配器与预设，不把旧曲子的AI乐句复制到新曲子。
func (w *Workbench) ApplyResourceBand(in ResourceBandInput) (schema.Draft, error) {
	var d schema.Draft
	b, err := json.Marshal(in.Draft)
	if err != nil {
		return d, err
	}
	if err = json.Unmarshal(b, &d); err != nil {
		return d, err
	}
	if d.Kind != "score" || d.Arrangement == nil || schema.ArrangementIssue(d.Arrangement) != "" {
		return d, fmt.Errorf("请先生成编曲计划")
	}
	state := w.Store.Read()
	idx := slices.IndexFunc(state.BandPresets, func(b schema.BandPreset) bool { return "band:"+b.ID == in.ResourceID })
	if idx < 0 {
		return d, fmt.Errorf("找不到乐队组合，请重新查询库存")
	}
	band := state.BandPresets[idx]
	matched := 0
	for si := range d.Arrangement.Sections {
		for pi := range d.Arrangement.Sections[si].Phrases {
			for ai := range d.Arrangement.Sections[si].Phrases[pi].Parts {
				part := &d.Arrangement.Sections[si].Phrases[pi].Parts[ai]
				if part.Locked {
					continue
				}
				var candidates []schema.BandPart
				for _, member := range band.Parts {
					if arrangementSameInstrument(part.Instrument, member.Instrument) && part.Role == member.Role && part.Source == member.Source && strings.EqualFold(strings.TrimSpace(part.Technique), strings.TrimSpace(member.Technique)) {
						candidates = append(candidates, member)
					}
				}
				// 多个同类组合成员不猜；缺省奏法只可用于唯一同类成员。
				if len(candidates) == 0 {
					for _, member := range band.Parts {
						if arrangementSameInstrument(part.Instrument, member.Instrument) && part.Role == member.Role && part.Source == member.Source && member.Technique == "" {
							candidates = append(candidates, member)
						}
					}
				}
				if len(candidates) != 1 {
					continue
				}
				member := candidates[0]
				if member.SourceID == "" {
					continue
				}
				sourceIndex := slices.IndexFunc(state.InstrumentSources, func(s schema.InstrumentSource) bool { return s.ID == member.SourceID })
				if sourceIndex < 0 {
					return d, fmt.Errorf("组合中的 %s 音源已缺失，请先修复", member.Instrument)
				}
				source := state.InstrumentSources[sourceIndex]
				if code := sourceIdentityIssue(*part, source); code != "" {
					return d, fmt.Errorf("组合中的 %s 不适用于当前声部：%s", member.Instrument, code)
				}
				if issue := w.sourceFilesIssue(source); issue != "" {
					return d, fmt.Errorf("组合中的 %s 文件不可用：%s", member.Instrument, issue)
				}
				if source.Verification == "metadata" {
					return d, fmt.Errorf("组合中的 %s 尚待试听核验", member.Instrument)
				}
				part.SourceID = source.ID
				matched++
			}
		}
	}
	if d.Arrangement.AIModelID == "" && band.AIModelID != "" {
		if model, e := catalog.Lookup(band.AIModelID); e == nil && catalog.IsMusic(model.Family) {
			d.Arrangement.AIModelID = band.AIModelID
			matched++
		}
	}
	if matched == 0 {
		return d, fmt.Errorf("组合没有与当前未锁定声部匹配的乐器、角色和来源")
	}
	inspection, err := w.InspectArrangement(ArrangementInspectInput{Draft: d})
	if err != nil {
		return d, err
	}
	for _, issue := range inspection.Issues {
		if issue.Code != "SOURCE_MISSING" {
			return d, fmt.Errorf("组合不能演奏当前曲谱：%s", issue.Message)
		}
	}
	if inspection.Score != nil {
		d.Score = inspection.Score
	}
	if err = w.saveAssistantArrangement(d, arrangementRevision(in.Draft)); err != nil {
		return d, err
	}
	return d, nil
}
