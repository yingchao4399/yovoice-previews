package workbench

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"slices"
	"strings"
	"time"
	"yovoice/internal/audio"
	"yovoice/internal/catalog"
	"yovoice/internal/engine"
	"yovoice/internal/msg"
	"yovoice/internal/schema"
	"yovoice/internal/score"
)

type ArrangementPartRef struct {
	SectionIndex int `json:"sectionIndex"`
	PhraseIndex  int `json:"phraseIndex"`
	PartIndex    int `json:"partIndex"`
}

type ArrangementAIPrepareInput struct {
	Draft   schema.Draft        `json:"draft"`
	ModelID string              `json:"modelId"`
	Target  *ArrangementPartRef `json:"target,omitempty"`
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
	if d.Score != nil {
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
	b, _ := json.Marshal([]any{"arrangement-condition-v1", arrangementJobHash(*d.Arrangement, ref), notes})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func arrangementMaterialMatches(m schema.ArrangementMaterial, d schema.Draft, ref ArrangementPartRef) bool {
	return d.Arrangement != nil && m.PlanHash == arrangementJobHash(*d.Arrangement, ref) && m.ScoreHash != "" && m.ScoreHash == arrangementConditionHash(d, ref) && (m.PartID == "" || m.PartID == d.Arrangement.Sections[ref.SectionIndex].Phrases[ref.PhraseIndex].Parts[ref.PartIndex].ID)
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
	brief := []rune(d.Arrangement.Brief)
	if len(brief) > 160 {
		brief = brief[:160]
	}
	prompt := fmt.Sprintf("Dry isolated solo %s, %s technique, %s role. Phrase: %s. Direction: %s. %g BPM, %s. No vocals, other instruments or drums. Natural phrasing and dynamics.", job.part.Instrument, job.part.Technique, job.part.Role, job.phrase.Name, string(brief), d.Arrangement.Tempo, d.Arrangement.Key)
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
	return schema.Draft{ID: d.ID, Kind: "music", Title: string(title), Text: prompt, SynthesisSettings: schema.SynthesisSettings{ModelID: modelID, Instrumental: true, ModelOptions: map[string]map[string]any{"ace_step": {"duration_seconds": math.Max(10, math.Ceil(job.seconds)), "bpm": d.Arrangement.Tempo, "keyscale": "auto", "timesignature": meter}}}}
}

func arrangementModelDraft(d schema.Draft, modelID string, job arrangementAIJob) (schema.Draft, error) {
	music := arrangementMusicDraft(d, modelID, job)
	model, err := catalog.Lookup(modelID)
	if err != nil {
		return music, err
	}
	if model.Family == "yue2" {
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
				return music, nil
			}
		}
		return music, fmt.Errorf("本句在曲谱中没有匹配的声部")
	}
	return music, nil
}

// PrepareArrangementAI materializes every selected AI phrase into an indexed
// library stem. Existing stems for the identical approved plan are reused.
func (w *Workbench) PrepareArrangementAI(in ArrangementAIPrepareInput) error {
	w.edit.Lock()
	defer w.edit.Unlock()
	d := in.Draft
	if in.ModelID == "" && d.Arrangement != nil {
		in.ModelID = d.Arrangement.AIModelID
	}
	if d.Kind != "score" || d.Arrangement == nil {
		return msg.Err(msg.ErrRequestInvalid, nil)
	}
	jobs, err := arrangementAIJobs(*d.Arrangement, in.Target)
	if err != nil {
		return err
	}
	state := w.Store.Read()
	if d.Timeline == nil {
		if existing := slices.IndexFunc(state.Drafts, func(item schema.Draft) bool { return item.ID == d.ID }); existing >= 0 {
			d.Timeline = state.Drafts[existing].Timeline
		}
	}
	hash := arrangementRevision(d)
	inspection, inspectErr := w.InspectArrangement(ArrangementInspectInput{Draft: d, Stage: "score", AIModelID: in.ModelID})
	if inspectErr != nil {
		return inspectErr
	}
	pending := make([]arrangementAIJob, 0, len(jobs))
	for _, job := range jobs {
		found := false
		for _, part := range inspection.Parts {
			if part.Ref == job.ref && part.SourceID != "" {
				if part.Ready {
					found = true
				} else if len(part.Issues) > 0 {
					return msg.Err(msg.ErrArrangementBlocked, msg.Params{"detail": part.Issues[0].Message, "reason": part.Issues[0].Code})
				}
			}
		}
		for _, material := range state.ArrangementMaterials {
			if material.ProjectID == d.ID && material.ModelID == in.ModelID && arrangementMaterialMatches(material, d, job.ref) && material.SectionIndex == job.ref.SectionIndex && material.PhraseIndex == job.ref.PhraseIndex && material.PartIndex == job.ref.PartIndex {
				path, pathErr := w.Store.MediaPath("outputs", material.FileName)
				if pathErr == nil {
					_, pathErr = os.Stat(path)
				}
				if pathErr == nil {
					found = true
					break
				}
			}
		}
		if !found {
			pending = append(pending, job)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	model, lookupErr := catalog.Lookup(in.ModelID)
	if lookupErr != nil || (model.Family != "ace_step" && model.Family != "yue2") {
		return fmt.Errorf("请在设置中安装并选择 ACE-Step 或 YuE2 音频模型；DeepSeek 只负责规划与写谱")
	}
	index := slices.IndexFunc(state.Models, func(item schema.InstalledModel) bool { return item.ID == in.ModelID })
	if index < 0 {
		return msg.Err(msg.ErrModelRequired, nil)
	}
	if !RuntimeReady(state) {
		return msg.Err(msg.ErrRuntimeRequired, nil)
	}
	if err = modelEngineReady(state, in.ModelID); err != nil {
		return err
	}
	for _, job := range pending {
		musicDraft, prepareErr := arrangementModelDraft(d, in.ModelID, job)
		if prepareErr != nil {
			return prepareErr
		}
		if _, err = engine.BuildRequest(musicDraft, "", ""); err != nil {
			return err
		}
	}
	if err = w.saveDraft(d); err != nil {
		return err
	}
	_, err = w.start("generate", msg.ActivityGenerate, nil, nil, func(ctx context.Context) error {
		created := make([]string, 0, len(pending))
		keep := false
		defer func() {
			if !keep {
				for _, path := range created {
					_ = os.Remove(path)
				}
			}
		}()
		for i, job := range pending {
			if err := ctx.Err(); err != nil {
				return err
			}
			w.progress(msg.ActivityRendering, msg.Params{"stage": "ai", "done": i, "total": len(pending)}, int64(i), int64(len(pending)))
			id := schema.NewID()
			file := "import-" + id + ".wav"
			path, pathErr := w.Store.MediaPath("outputs", file)
			if pathErr != nil {
				return pathErr
			}
			created = append(created, path)
			musicDraft, prepareErr := arrangementModelDraft(d, in.ModelID, job)
			if prepareErr != nil {
				return prepareErr
			}
			if _, generateErr := w.engine.Generate(ctx, *state.RuntimePath, state.Models[index], state.Preferences.Backend, musicDraft, "", "", path, func(code msg.Code, params msg.Params) { w.progress(code, params, int64(i), int64(len(pending))) }); generateErr != nil {
				return generateErr
			}
			duration, durationErr := audio.Duration(path)
			if durationErr != nil {
				return durationErr
			}
			if duration+0.1 < job.seconds {
				return fmt.Errorf("%s · %s 的 AI 音频不足计划时长：%.1f / %.1f 秒", job.phrase.Name, job.part.Instrument, duration, job.seconds)
			}
			quality, qualityErr := audio.InspectSignal(path)
			if qualityErr != nil {
				return qualityErr
			}
			warning := ""
			if d.Score != nil {
				units, _ := arrangementUnits(*d.Arrangement, *d.Score)
				for _, u := range units {
					if u.SectionIndex == job.ref.SectionIndex && u.PhraseIndex == job.ref.PhraseIndex && u.PartIndex == job.ref.PartIndex {
						expected := job.seconds
						for _, n := range u.Track.Notes {
							expected = min(expected, d.Score.Seconds(n.Bar, n.Beat)-u.Start)
						}
						if math.Abs(quality.OnsetSeconds-expected) > .35 {
							warning = fmt.Sprintf("起音约 %.2fs，曲谱首音 %.2fs；请试听确认或手动对齐", quality.OnsetSeconds, expected)
						}
					}
				}
			}
			if duration-job.seconds > math.Max(.5, job.seconds*.15) {
				warning = fmt.Sprintf("%s 生成时长 %.1fs，计划乐句 %.1fs；拼合默认取前 %.1fs，请试听并在时间线裁切、对齐。", warning, duration, job.seconds, job.seconds)
			}
			condition := "free_audio"
			if musicDraft.Abc != "" {
				condition = "score_conditioned"
			}
			material := schema.ArrangementMaterial{ID: id, ProjectID: d.ID, PlanHash: arrangementJobHash(*d.Arrangement, job.ref), ScoreHash: arrangementConditionHash(d, job.ref), PartID: job.part.ID, Condition: condition, ReviewStatus: "needs_review", Peak: quality.Peak, ClippedFraction: quality.ClippedFraction, OnsetSeconds: quality.OnsetSeconds, AlignmentWarning: warning, SectionIndex: job.ref.SectionIndex, PhraseIndex: job.ref.PhraseIndex, PartIndex: job.ref.PartIndex, Instrument: job.part.Instrument, Role: job.part.Role, Technique: job.part.Technique, Phrase: job.phrase.Name, Prompt: musicDraft.Text, Tempo: d.Arrangement.Tempo, Key: d.Arrangement.Key, ModelID: in.ModelID, FileName: file, Duration: duration, CreatedAt: time.Now().UTC()}
			w.edit.Lock()
			current := w.Store.Read()
			draftIndex := slices.IndexFunc(current.Drafts, func(item schema.Draft) bool { return item.ID == d.ID })
			if draftIndex < 0 || arrangementRevision(current.Drafts[draftIndex]) != hash {
				w.edit.Unlock()
				return fmt.Errorf("生成期间计划或曲谱已更改；已完成素材保留，请对新修订重新准备")
			}
			persistErr := w.Store.Update(func(state *schema.State) { state.ArrangementMaterials = append(state.ArrangementMaterials, material) }, true)
			w.edit.Unlock()
			if persistErr != nil {
				return persistErr
			}
			// 每句成功立即提交；后续失败仅清理尚未提交的文件。
			created = nil
		}
		keep = true
		return nil
	}, "", d.ID)
	return err
}

// ReviewArrangementMaterial 只记录用户试听结论，不替代自动质量检查。
func (w *Workbench) ReviewArrangementMaterial(id string, confirmed bool) error {
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
