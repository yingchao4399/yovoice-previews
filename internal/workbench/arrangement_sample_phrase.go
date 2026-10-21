package workbench

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"time"
	"yovoice/internal/audio"
	"yovoice/internal/schema"
	"yovoice/internal/score"
)

// renderGeneratedSamplePhrase 使用真实已确认 AI 音色演奏本句，音符执行来自采样器。
func (w *Workbench) renderGeneratedSamplePhrase(ctx context.Context, d schema.Draft, t arrangementGenerationTask) error {
	inspection, e := w.InspectArrangement(ArrangementInspectInput{Draft: d, Stage: "audition"})
	if e != nil {
		return e
	}
	if inspection.Score == nil {
		return fmt.Errorf("没有可以演奏的曲谱")
	}
	units, e := arrangementUnits(*d.Arrangement, *inspection.Score)
	if e != nil {
		return e
	}
	var unit *arrangementUnit
	for i := range units {
		u := &units[i]
		if u.SectionIndex == t.job.ref.SectionIndex && u.PhraseIndex == t.job.ref.PhraseIndex && u.PartIndex == t.job.ref.PartIndex {
			unit = u
			break
		}
	}
	if unit == nil || unit.Track.SourceID == "" {
		return fmt.Errorf("本句尚无完整的已确认 AI 音色映射")
	}
	state := w.Store.Read()
	i := slices.IndexFunc(state.InstrumentSources, func(s schema.InstrumentSource) bool {
		return s.ID == unit.Track.SourceID && s.Origin == "ai" && s.Verification == "user"
	})
	if i < 0 {
		return fmt.Errorf("AI 音色未确认")
	}
	b, _ := json.Marshal(state.InstrumentSources[i])
	hash := sha256.Sum256(b)
	fingerprint := hex.EncodeToString(hash[:])
	for _, m := range state.ArrangementMaterials {
		if arrangementMaterialMatches(m, d, t.job.ref) && m.Generation != nil && m.Generation.SourceFingerprint == fingerprint && m.Condition == "sampler_score" && m.GenerationResultState() != "rejected" {
			return nil
		}
	}
	music := *d.Score
	track := unit.Track
	track.Notes = slices.Clone(track.Notes)
	// 在原始时间坐标上渲染本句，再提取区段，保留原速度段落与渐强渐弱。
	// 不将本句移到第一小节后丢失其所在段落的速度与表情。
	music.Tracks = []schema.ScoreTrack{track}
	sources, e := w.scoreSources(music)
	if e != nil {
		return e
	}
	id := schema.NewID()
	file := "import-" + id + ".wav"
	path, e := w.Store.MediaPath("outputs", file)
	if e != nil {
		return e
	}
	if e = score.RenderWithSources(ctx, nil, music, path, "", sources, nil); e != nil {
		return e
	}
	duration, e := audio.Duration(path)
	if e != nil {
		return e
	}
	q, e := audio.InspectSignal(path)
	if e != nil {
		return e
	}
	rawHash, e := arrangementAudioDigest(path)
	if e != nil {
		return e
	}
	c := t.contract
	c.SourceFingerprint = fingerprint
	c.SemanticMin = 0
	c.SemanticMax = 0
	c.EffectiveOffset = unit.Start
	c.CoreSeconds = unit.End - unit.Start
	c.EffectiveSeconds = c.CoreSeconds
	c.RequestSeconds = c.CoreSeconds + c.TailSeconds
	derivedFile := "import-" + schema.NewID() + ".wav"
	derived, e := w.Store.MediaPath("outputs", derivedFile)
	if e != nil {
		return e
	}
	if e = audio.ExtractInstrument(ctx, "", path, derived, unit.Start, c.CoreSeconds+c.TailSeconds); e != nil {
		return e
	}
	derivedHash, e := arrangementAudioDigest(derived)
	if e != nil {
		return e
	}
	r := schema.ArrangementGenerationResult{State: "needs_review", ActualSeconds: duration, TruncationKnown: true, RawSHA256: rawHash, DerivedSHA256: derivedHash, DerivedFile: derivedFile, SourceID: unit.Track.SourceID, Attempt: 1, Checks: []schema.GenerationCheck{{Code: "SAMPLER_SCORE", Status: "pass", Message: "由已确认 AI 音色按原曲谱执行音高、时值和力度，未改变其他声部"}, {Code: "IDENTITY", Status: "review", Message: "请试听当前乐句的表现与完整性后采用"}}}
	m := schema.ArrangementMaterial{ID: id, ProjectID: d.ID, PartID: t.job.part.ID, PlanHash: arrangementJobHash(*d.Arrangement, t.job.ref), ScoreHash: arrangementConditionHash(d, t.job.ref), Condition: "sampler_score", ReviewStatus: "needs_review", Generation: &c, GenerationResult: &r, SectionIndex: t.job.ref.SectionIndex, PhraseIndex: t.job.ref.PhraseIndex, PartIndex: t.job.ref.PartIndex, Instrument: t.job.part.Instrument, Role: t.job.part.Role, Technique: t.job.part.Technique, Phrase: t.job.phrase.Name, ModelID: c.ModelID, FileName: file, Duration: duration, Peak: q.Peak, Prompt: "使用已确认的 AI 音色按谱演奏", CreatedAt: time.Now().UTC()}
	return w.persistGenerationMaterial(m)
}
