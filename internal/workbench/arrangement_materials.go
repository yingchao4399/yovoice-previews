package workbench

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"slices"
	"strings"
	"yovoice/internal/schema"
)

// arrangementUnit is one planned instrument in one phrase. The score remains the
// editable source of notes; this list is the audio render manifest.
type arrangementUnit struct {
	Section      string
	Phrase       string
	SectionIndex int
	PhraseIndex  int
	PartIndex    int
	Part         schema.ArrangementPart
	Track        schema.ScoreTrack
	Start        float64
	End          float64
}

var arrangementAliases = map[string]string{
	"pipa": "琵琶", "xiao": "箫", "guzheng": "古筝", "erhu": "二胡",
	"dizi": "笛子", "竹笛": "笛子", "横笛": "笛子", "洞箫": "箫", "guqin": "古琴", "suona": "唢呐", "ruan": "阮",
	"violin": "小提琴", "violins": "小提琴", "fiddle": "小提琴",
	"piano": "钢琴", "guitar": "吉他", "acousticguitar": "原声吉他",
}

func arrangementInstrumentKey(name string) string {
	plain := strings.ToLower(strings.NewReplacer(" ", "", "-", "", "_", "", "（", "", "）", "", "(", "", ")", "").Replace(name))
	if alias := arrangementAliases[plain]; alias != "" {
		return alias
	}
	return plain
}

func arrangementSameInstrument(a, b string) bool {
	a, b = arrangementInstrumentKey(a), arrangementInstrumentKey(b)
	return a == b || (len([]rune(a)) >= 2 && strings.Contains(b, a)) || (len([]rune(b)) >= 2 && strings.Contains(a, b))
}

func arrangementUnits(plan schema.ArrangementPlan, music schema.Score) ([]arrangementUnit, error) {
	if issue := schema.ArrangementIssue(&plan); issue != "" || !plan.Approved {
		return nil, fmt.Errorf("编曲计划尚未审核或无效：%s", issue)
	}
	if err := schema.ValidateScore(&music); err != nil {
		return nil, err
	}
	if math.Abs(music.Tempo-plan.Tempo) > 0.01 || len(music.TimeSignature) != 2 || music.TimeSignature[0] != plan.TimeSignature[0] || music.TimeSignature[1] != plan.TimeSignature[1] {
		return nil, fmt.Errorf("曲谱速度或拍号与编曲计划不一致，请先修改曲谱")
	}
	var units []arrangementUnit
	assigned := make(map[string]int)
	knownParts := make(map[string]bool)
	for _, section := range plan.Sections {
		for _, phrase := range section.Phrases {
			for _, part := range phrase.Parts {
				if part.ID != "" {
					knownParts[part.ID] = true
				}
			}
		}
	}
	for _, track := range music.Tracks {
		for _, partID := range track.PartIDs {
			if !knownParts[partID] {
				return nil, fmt.Errorf("声部 %s 引用了不在编曲计划中的 partId %s", track.Name, partID)
			}
		}
	}
	for sectionIndex, section := range plan.Sections {
		for phraseIndex, phrase := range section.Phrases {
			start, end := music.Seconds(phrase.StartBar, 1), music.Seconds(phrase.EndBar+1, 1)
			expected := float64(phrase.EndBar-phrase.StartBar+1) * music.BeatsPerBar() * 60 / plan.Tempo
			if math.Abs((end-start)-expected) > .1 {
				return nil, fmt.Errorf("%s 的曲谱变速与 AI 乐句规划时长不一致，请重新生成或修改曲谱", phrase.Name)
			}
			for partIndex, part := range phrase.Parts {
				if part.Source != "real" && part.Source != "ai" {
					return nil, fmt.Errorf("%s · %s 尚未决定使用真实或 AI 音源", phrase.Name, part.Instrument)
				}
				matched := 0
				for _, track := range music.Tracks {
					// 新计划必须按稳定身份绑定。旧工程没有 part ID 时才按乐器名称兼容匹配。
					bound := part.ID != "" && slices.Contains(track.PartIDs, part.ID)
					if track.Mute || (part.ID != "" && !bound) || (part.ID == "" && !arrangementSameInstrument(part.Instrument, track.Name)) || (track.Role != "" && part.Role != "" && track.Role != part.Role) {
						continue
					}
					copyTrack := track
					copyTrack.Notes = nil
					for noteIndex, note := range track.Notes {
						if note.Bar >= phrase.StartBar && note.Bar <= phrase.EndBar {
							copyTrack.Notes = append(copyTrack.Notes, note)
							assigned[fmt.Sprintf("%s:%d", track.ID, noteIndex)]++
						}
					}
					if len(copyTrack.Notes) > 0 {
						matched++
						units = append(units, arrangementUnit{Section: section.Name, Phrase: phrase.Name, SectionIndex: sectionIndex, PhraseIndex: phraseIndex, PartIndex: partIndex, Part: part, Track: copyTrack, Start: start, End: end})
					}
				}
				if matched == 0 {
					return nil, fmt.Errorf("%s · %s 在曲谱里没有对应音符或声部 ID，请先补全曲谱", phrase.Name, part.Instrument)
				}
				if matched > 1 && (part.Source == "ai" || part.ID != "") {
					return nil, fmt.Errorf("%s · %s 对应多个曲谱声部，请拆分计划或修正声部 ID", phrase.Name, part.Instrument)
				}
			}
		}
	}
	if len(units) == 0 {
		return nil, fmt.Errorf("曲谱中的声部没有与编曲计划匹配，请检查乐器名称与角色")
	}
	for _, track := range music.Tracks {
		if track.Mute {
			continue
		}
		for noteIndex, note := range track.Notes {
			count := assigned[fmt.Sprintf("%s:%d", track.ID, noteIndex)]
			if count != 1 {
				return nil, fmt.Errorf("声部 %s 第 %d 小节的音符匹配到 %d 个计划声部，请修改计划或曲谱", track.Name, note.Bar, count)
			}
		}
	}
	return units, nil
}

// migrateLegacyArrangementIDs binds a pre-ID project only when every planned
// phrase has exactly one unambiguous name/role score track and every note is
// covered exactly once. It never guesses through duplicate same-name tracks.
func migrateLegacyArrangementIDs(d *schema.Draft) (bool, error) {
	if d == nil || d.Kind != "score" || d.Arrangement == nil || d.Score == nil || !d.Arrangement.Approved {
		return false, nil
	}
	plan := d.Arrangement
	needsMigration := false
	missingPartIDs, existingPartIDs := false, false
	for _, section := range plan.Sections {
		if section.ID == "" {
			needsMigration = true
		}
		for _, phrase := range section.Phrases {
			if phrase.ID == "" {
				needsMigration = true
			}
			for _, part := range phrase.Parts {
				if part.ID == "" {
					needsMigration = true
					missingPartIDs = true
				} else {
					existingPartIDs = true
				}
			}
		}
	}
	if !needsMigration {
		return false, nil
	}
	if !missingPartIDs {
		for si := range plan.Sections {
			if plan.Sections[si].ID == "" {
				plan.Sections[si].ID = legacyArrangementID(d.ID, si, -1, -1)
			}
			for pi := range plan.Sections[si].Phrases {
				if plan.Sections[si].Phrases[pi].ID == "" {
					plan.Sections[si].Phrases[pi].ID = legacyArrangementID(d.ID, si, pi, -1)
				}
			}
		}
		if issue := schema.ArrangementIssue(plan); issue != "" {
			return false, fmt.Errorf("补齐编曲结构 ID 后计划无效：%s", issue)
		}
		return true, nil
	}
	if existingPartIDs {
		return false, fmt.Errorf("编曲计划只分配了部分声部 ID，暂不能自动迁移，请重新规划或人工检查曲谱")
	}

	// Use the same strict legacy name/role matching as arrangementUnits before
	// writing any new IDs, so a failed migration leaves the draft untouched.
	_, err := arrangementUnits(*plan, *d.Score)
	if err != nil {
		return false, err
	}
	trackByPart := make(map[string]string)
	coveredNotes := make(map[string]int)
	for sectionIndex, section := range plan.Sections {
		for phraseIndex, phrase := range section.Phrases {
			for partIndex, part := range phrase.Parts {
				var matches []schema.ScoreTrack
				for _, track := range d.Score.Tracks {
					if track.Mute || !arrangementSameInstrument(part.Instrument, track.Name) || (track.Role != "" && track.Role != part.Role) {
						continue
					}
					if slices.ContainsFunc(track.Notes, func(note schema.ScoreNote) bool { return note.Bar >= phrase.StartBar && note.Bar <= phrase.EndBar }) {
						matches = append(matches, track)
					}
				}
				if len(matches) != 1 {
					return false, fmt.Errorf("%s · %s 在旧曲谱中匹配到 %d 个声部，无法无歧义迁移，请人工检查", phrase.Name, part.Instrument, len(matches))
				}
				track := matches[0]
				trackByPart[fmt.Sprintf("%d:%d:%d", sectionIndex, phraseIndex, partIndex)] = track.ID
				for noteIndex, note := range track.Notes {
					if note.Bar >= phrase.StartBar && note.Bar <= phrase.EndBar {
						key := fmt.Sprintf("%s:%d", track.ID, noteIndex)
						coveredNotes[key]++
						if coveredNotes[key] > 1 {
							return false, fmt.Errorf("旧曲谱 %s 第 %d 小节的音符对应多个计划声部，无法无歧义迁移", track.Name, note.Bar)
						}
					}
				}
			}
		}
	}
	for _, track := range d.Score.Tracks {
		if track.Mute {
			continue
		}
		for noteIndex, note := range track.Notes {
			if coveredNotes[fmt.Sprintf("%s:%d", track.ID, noteIndex)] != 1 {
				return false, fmt.Errorf("旧曲谱 %s 第 %d 小节的音符未被计划唯一覆盖，无法无歧义迁移", track.Name, note.Bar)
			}
		}
	}
	for si := range plan.Sections {
		if plan.Sections[si].ID == "" {
			plan.Sections[si].ID = legacyArrangementID(d.ID, si, -1, -1)
		}
		for pi := range plan.Sections[si].Phrases {
			phrase := &plan.Sections[si].Phrases[pi]
			if phrase.ID == "" {
				phrase.ID = legacyArrangementID(d.ID, si, pi, -1)
			}
			for ai := range phrase.Parts {
				part := &phrase.Parts[ai]
				part.ID = legacyArrangementID(d.ID, si, pi, ai)
				for ti := range d.Score.Tracks {
					track := &d.Score.Tracks[ti]
					key := fmt.Sprintf("%d:%d:%d", si, pi, ai)
					if track.ID != trackByPart[key] {
						continue
					}
					track.PartIDs = append(track.PartIDs, part.ID)
				}
			}
		}
	}
	if issue := schema.ArrangementIssue(plan); issue != "" {
		return false, fmt.Errorf("迁移后的编曲计划无效：%s", issue)
	}
	return true, nil
}

func legacyArrangementID(project string, si, pi, ai int) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("legacy-arrangement-v1:%s:%d:%d:%d", project, si, pi, ai)))
	return hex.EncodeToString(h[:16])
}
