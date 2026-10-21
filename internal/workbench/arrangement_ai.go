package workbench

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"yovoice/internal/catalog"
	"yovoice/internal/schema"
	"yovoice/internal/score"
)

type ArrangementPartRef struct {
	SectionIndex int `json:"sectionIndex"`
	PhraseIndex  int `json:"phraseIndex"`
	PartIndex    int `json:"partIndex"`
}

type ArrangementAIPrepareInput struct {
	Draft     schema.Draft        `json:"draft"`
	ModelID   string              `json:"modelId"`
	Target    *ArrangementPartRef `json:"target,omitempty"`
	Retry     bool                `json:"-"`
	RetryCell string              `json:"-"`
}

func arrangementPlanHash(plan schema.ArrangementPlan) string {
	data, _ := json.Marshal(plan)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func arrangementJobHash(plan schema.ArrangementPlan, ref ArrangementPartRef) string {
	phrase := plan.Sections[ref.SectionIndex].Phrases[ref.PhraseIndex]
	part := phrase.Parts[ref.PartIndex]
	// Fingerprint only fields used by this phrase generation. An unrelated real
	// part may change without invalidating already generated AI audio.
	input := struct {
		BoundSourceID string
		Generation    schema.ArrangementGenerationSettings
		Tempo         float64
		TimeSignature []int
		Key           string
		PhraseName    string
		StartBar      int
		EndBar        int
		Instrument    string
		Role          string
		Technique     string
	}{part.SourceID, generationSettings(part), plan.Tempo, plan.TimeSignature, plan.Key, phrase.Name, phrase.StartBar, phrase.EndBar, part.Instrument, part.Role, part.Technique}
	data, _ := json.Marshal(input)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func ArrangementPartHashes(plan schema.ArrangementPlan) map[string]string {
	result := map[string]string{}
	if schema.ArrangementIssue(&plan) != "" {
		return result
	}
	for si, section := range plan.Sections {
		for pi, phrase := range section.Phrases {
			for ai, part := range phrase.Parts {
				if part.Source == "ai" {
					result[fmt.Sprintf("%d:%d:%d", si, pi, ai)] = arrangementJobHash(plan, ArrangementPartRef{si, pi, ai})
				}
			}
		}
	}
	return result
}

type arrangementAIJob struct {
	ref     ArrangementPartRef
	part    schema.ArrangementPart
	phrase  schema.ArrangementPhrase
	seconds float64
}

// 缓存条件包含本句实际音符；其他声部的修改不会使此句失效。
func arrangementConditionHash(d schema.Draft, ref ArrangementPartRef) string {
	if d.Arrangement == nil {
		return ""
	}
	phrase := d.Arrangement.Sections[ref.SectionIndex].Phrases[ref.PhraseIndex]
	part := phrase.Parts[ref.PartIndex]
	var notes []schema.ScoreNote
	var timing []float64
	var meter []int
	key := ""
	if d.Score != nil {
		for bar := phrase.StartBar; bar <= phrase.EndBar; bar++ {
			timing = append(timing, d.Score.BarTempo(bar))
		}
		meter, key = d.Score.TimeSignature, d.Score.Key
		for _, track := range d.Score.Tracks {
			if (part.ID != "" && slices.Contains(track.PartIDs, part.ID)) || (part.ID == "" && arrangementSameInstrument(track.Name, part.Instrument)) {
				for _, n := range track.Notes {
					if n.Bar >= phrase.StartBar && n.Bar <= phrase.EndBar {
						notes = append(notes, n)
					}
				}
			}
		}
	}
	b, _ := json.Marshal([]any{arrangementAdapterVersion, arrangementJobHash(*d.Arrangement, ref), notes, timing, meter, key})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func arrangementMaterialMatches(m schema.ArrangementMaterial, d schema.Draft, ref ArrangementPartRef) bool {
	if d.Arrangement == nil {
		return false
	}
	part := d.Arrangement.Sections[ref.SectionIndex].Phrases[ref.PhraseIndex].Parts[ref.PartIndex]
	if m.PartID != "" && m.PartID != part.ID {
		return false
	}
	if m.Generation == nil && d.Score != nil {
		phrase := d.Arrangement.Sections[ref.SectionIndex].Phrases[ref.PhraseIndex]
		seconds := d.Score.Seconds(phrase.EndBar+1, 1) - d.Score.Seconds(phrase.StartBar, 1)
		if m.Duration < seconds-.1 || m.Duration > seconds+math.Max(.5, seconds*.15) {
			return false
		}
	}
	if m.PlanHash == arrangementJobHash(*d.Arrangement, ref) && m.ScoreHash != "" && m.ScoreHash == arrangementConditionHash(d, ref) {
		return true
	}
	return legacyArrangementMaterialMatches(m, d, ref)
}

func arrangementAIJobs(plan schema.ArrangementPlan, target *ArrangementPartRef) ([]arrangementAIJob, error) {
	if issue := schema.ArrangementIssue(&plan); issue != "" || !plan.Approved {
		return nil, fmt.Errorf("编曲计划尚未审核或无效：%s", issue)
	}
	jobs := make([]arrangementAIJob, 0)
	for si, section := range plan.Sections {
		for pi, phrase := range section.Phrases {
			seconds := float64(phrase.EndBar-phrase.StartBar+1) * float64(plan.TimeSignature[0]) * 4 / float64(plan.TimeSignature[1]) * 60 / plan.Tempo
			for ai, part := range phrase.Parts {
				ref := ArrangementPartRef{si, pi, ai}
				if target != nil && *target != ref {
					continue
				}
				if part.Source != "ai" {
					if target != nil {
						return nil, fmt.Errorf("选择的声部不是 AI 音源")
					}
					continue
				}
				if seconds <= 0 || seconds > 300 {
					return nil, fmt.Errorf("%s 的 AI 乐句时长 %.1f 秒不在当前编曲支持范围", phrase.Name, seconds)
				}
				jobs = append(jobs, arrangementAIJob{ref: ref, part: part, phrase: phrase, seconds: seconds})
			}
		}
	}
	if len(jobs) == 0 {
		return nil, fmt.Errorf("计划中没有待生成的 AI 声部")
	}
	return jobs, nil
}

func arrangementMusicDraft(d schema.Draft, modelID string, job arrangementAIJob) schema.Draft {
	settings := generationSettings(job.part)
	prompt := fmt.Sprintf("Dry isolated %s only, %s technique. %g BPM, %s. Natural dynamics. No other instruments, no accompaniment.", aiInstrumentName(job.part.Instrument), job.part.Technique, d.Arrangement.Tempo, d.Arrangement.Key)
	instrumental := settings.SoundType != "wordless"
	lyrics := ""
	if instrumental {
		prompt += " Instrumental, no vocals, no speech."
	} else {
		prompt += " Wordless vocal ensemble, sustained vowel ah, no lyrics, no speech, no instruments."
		lyrics = "[Verse]\nah"
	}
	if settings.SoundType != "percussion" {
		prompt += " No drums."
	}
	if runes := []rune(prompt); len(runes) > 512 {
		prompt = string(runes[:512])
	}
	title := []rune(strings.TrimSpace(job.part.Instrument + " · " + job.phrase.Name))
	if len(title) > 120 {
		title = title[:120]
	}
	meter := "auto"
	if slices.Contains([]int{2, 3, 4, 6}, d.Arrangement.TimeSignature[0]) {
		meter = fmt.Sprint(d.Arrangement.TimeSignature[0])
	}
	keyscale := "auto"
	if key, err := score.NormalizeKey(d.Arrangement.Key); err == nil {
		key = strings.NewReplacer("Db", "C#", "D#", "Eb", "Gb", "F#", "G#", "Ab", "A#", "Bb", "Cb", "B", "E#", "F", "B#", "C", "Fb", "E").Replace(key)
		keyscale = key + " major"
		if strings.HasSuffix(key, "m") {
			keyscale = strings.TrimSuffix(key, "m") + " minor"
		}
	}
	return schema.Draft{ID: d.ID, Kind: "music", Title: string(title), Text: prompt, SynthesisSettings: schema.SynthesisSettings{ModelID: modelID, Instrumental: instrumental, Lyrics: lyrics, ModelOptions: map[string]map[string]any{"yue2": {"cot": "full"}, "ace_step": {"duration_seconds": math.Max(10, math.Ceil(job.seconds)), "bpm": d.Arrangement.Tempo, "keyscale": keyscale, "timesignature": meter}}}}
}

func aiInstrumentName(name string) string {
	names := map[string]string{"箫": "xiao bamboo flute", "笛子": "dizi bamboo flute", "琵琶": "Chinese pipa lute", "二胡": "erhu bowed strings", "古筝": "guzheng zither", "古琴": "guqin zither", "合唱": "unison wordless choir"}
	if english := names[arrangementInstrumentKey(name)]; english != "" {
		return name + " (" + english + ")"
	}
	return name
}

func arrangementModelDraft(d schema.Draft, modelID string, job arrangementAIJob) (schema.Draft, error) {
	music := arrangementMusicDraft(d, modelID, job)
	model, err := catalog.Lookup(modelID)
	if err != nil {
		return music, err
	}
	if model.Family == "yue2" && generationSettings(job.part).Mode == "score" {
		if d.Score == nil {
			return music, fmt.Errorf("YuE2 跟谱生成需要先完成曲谱")
		}
		units, err := arrangementUnits(*d.Arrangement, *d.Score)
		if err != nil {
			return music, err
		}
		for _, u := range units {
			if u.SectionIndex == job.ref.SectionIndex && u.PhraseIndex == job.ref.PhraseIndex && u.PartIndex == job.ref.PartIndex {
				music.Abc, err = score.PhraseABC(*d.Score, u.Track, job.phrase.StartBar, job.phrase.EndBar)
				if err != nil {
					return music, fmt.Errorf("%s · %s：%w", job.phrase.Name, job.part.Instrument, err)
				}
				if generationSettings(job.part).SoundType == "wordless" {
					music.Abc, err = score.PhraseVocalABC(music.Abc)
				}
				if err != nil {
					return music, err
				}
				music.Abc, err = score.PhraseReleaseABC(music.Abc, generationSettings(job.part).TailSeconds)
				if err != nil {
					return music, err
				}
				return music, nil
			}
		}
		return music, fmt.Errorf("本句在曲谱中没有匹配的声部")
	}
	return music, nil
}

// ReviewArrangementMaterial 只记录用户试听结论，不替代自动质量检查。
func (w *Workbench) reviewArrangementMaterialLegacy(id string, confirmed bool) error {
	found := false
	err := w.Store.Update(func(s *schema.State) {
		for i := range s.ArrangementMaterials {
			if s.ArrangementMaterials[i].ID == id {
				found = true
				s.ArrangementMaterials[i].ReviewStatus = "needs_review"
				if confirmed {
					s.ArrangementMaterials[i].ReviewStatus = "confirmed"
				}
			}
		}
	}, true)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("AI 乐句素材不存在")
	}
	return nil
}
