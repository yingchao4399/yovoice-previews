package workbench

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"slices"
	"yovoice/internal/schema"
)

// 旧版正常乐句只读兼容；有新生成设置、音符已改动或明显超长时绝不借旧缓存放行。
func legacyArrangementMaterialMatches(m schema.ArrangementMaterial, d schema.Draft, ref ArrangementPartRef) bool {
	if m.Generation != nil || d.Arrangement == nil || d.Score == nil {
		return false
	}
	plan := d.Arrangement
	phrase := plan.Sections[ref.SectionIndex].Phrases[ref.PhraseIndex]
	part := phrase.Parts[ref.PartIndex]
	if part.Generation != nil {
		return false
	}
	seconds := d.Score.Seconds(phrase.EndBar+1, 1) - d.Score.Seconds(phrase.StartBar, 1)
	if m.Duration < seconds-.1 || m.Duration > seconds+math.Max(.5, seconds*.15) {
		return false
	}
	input := struct {
		Brief         string
		Tempo         float64
		TimeSignature []int
		Key           string
		PhraseName    string
		StartBar      int
		EndBar        int
		Instrument    string
		Role          string
		Technique     string
	}{plan.Brief, plan.Tempo, plan.TimeSignature, plan.Key, phrase.Name, phrase.StartBar, phrase.EndBar, part.Instrument, part.Role, part.Technique}
	b, _ := json.Marshal(input)
	h := sha256.Sum256(b)
	hash := hex.EncodeToString(h[:])
	if m.PlanHash != hash {
		return false
	}
	var notes []schema.ScoreNote
	for _, track := range d.Score.Tracks {
		if part.ID != "" && slices.Contains(track.PartIDs, part.ID) || part.ID == "" && arrangementSameInstrument(track.Name, part.Instrument) {
			for _, n := range track.Notes {
				if n.Bar >= phrase.StartBar && n.Bar <= phrase.EndBar {
					notes = append(notes, n)
				}
			}
		}
	}
	b, _ = json.Marshal([]any{"arrangement-condition-v1", hash, notes})
	h = sha256.Sum256(b)
	return m.ScoreHash != "" && m.ScoreHash == hex.EncodeToString(h[:])
}
