package workbench

import (
	"fmt"
	"strings"
	"time"
	"yovoice/internal/msg"
	"yovoice/internal/schema"
)

type BandPresetInput struct {
	Draft     schema.Draft `json:"draft"`
	AIModelID string       `json:"aiModelId,omitempty"`
}

// SaveArrangementBand fixes reusable instrumentation and source choices. AI
// phrase audio belongs to its original composition; a preset keeps the model
// choice rather than copying those phrases into an unrelated song.
func (w *Workbench) SaveArrangementBand(in BandPresetInput) (schema.BandPreset, error) {
	d := in.Draft
	if d.Kind != "score" || d.Arrangement == nil || !d.Arrangement.Approved || schema.ArrangementIssue(d.Arrangement) != "" {
		return schema.BandPreset{}, msg.Err(msg.ErrRequestInvalid, nil)
	}
	w.edit.Lock()
	defer w.edit.Unlock()
	check, err := w.InspectArrangement(ArrangementInspectInput{Draft: schema.Draft{Arrangement: d.Arrangement}})
	if err != nil {
		return schema.BandPreset{}, err
	}
	for _, issue := range check.Issues {
		if issue.Code != "SOURCE_MISSING" {
			return schema.BandPreset{}, fmt.Errorf("%s", issue.Message)
		}
	}
	state := w.Store.Read()
	parts := make([]schema.BandPart, 0)
	seen := make(map[string]bool)
	for _, section := range d.Arrangement.Sections {
		for _, phrase := range section.Phrases {
			for _, part := range phrase.Parts {
				key := arrangementInstrumentKey(part.Instrument) + "|" + part.Role + "|" + part.Source + "|" + part.Technique + "|" + part.SourceID
				if seen[key] {
					continue
				}
				seen[key] = true
				item := schema.BandPart{Instrument: strings.TrimSpace(part.Instrument), Role: part.Role, Technique: part.Technique, Source: part.Source, SourceID: part.SourceID, AllowSubstitution: part.AllowSubstitution}
				for _, source := range state.InstrumentSources {
					if (item.SourceID == "" || source.ID == item.SourceID) && sourceIdentityIssue(part, source) == "" && w.sourceFilesIssue(source) == "" {
						item.SourceID = source.ID
						break
					}
				}
				parts = append(parts, item)
			}
		}
	}
	if len(parts) == 0 || len(parts) > 32 {
		return schema.BandPreset{}, fmt.Errorf("乐队组合需要 1–32 个不同的乐器与音源搭配")
	}
	name := strings.TrimSpace(d.Arrangement.Title)
	if name == "" {
		name = d.Title
	}
	if schema.TextLen(name) > 100 {
		name = string([]rune(name)[:100])
	}
	band := schema.BandPreset{ID: schema.NewID(), Name: name + " · 乐队组合", Parts: parts, AIModelID: in.AIModelID, CreatedAt: time.Now().UTC()}
	if err := w.Store.Update(func(s *schema.State) { s.BandPresets = append([]schema.BandPreset{band}, s.BandPresets...) }, true); err != nil {
		return schema.BandPreset{}, err
	}
	return band, nil
}
