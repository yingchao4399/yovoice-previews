package workbench

import (
	"context"
	"fmt"
	"math"
	"os"
	"reflect"
	"slices"
	"strings"
	"yovoice/internal/audio"
	"yovoice/internal/msg"
	"yovoice/internal/schema"
	"yovoice/internal/score"
)

type ArrangementRenderInput struct {
	Draft     schema.Draft `json:"draft"`
	AIModelID string       `json:"aiModelId,omitempty"`
}

// RenderArrangement follows three distinct stages: validate the source plan,
// render the real source audio, then place it with prepared AI stems on an editable
// timeline. It never substitutes an SF2 note for a missing real or AI source.
func (w *Workbench) RenderArrangement(in ArrangementRenderInput) error {
	w.edit.Lock()
	defer w.edit.Unlock()
	d := in.Draft
	if in.AIModelID == "" && d.Arrangement != nil {
		in.AIModelID = d.Arrangement.AIModelID
	}
	if d.Kind != "score" || d.Score == nil || d.Arrangement == nil {
		return msg.Err(msg.ErrRequestInvalid, nil)
	}
	inspection, inspectErr := w.InspectArrangement(ArrangementInspectInput{Draft: d, AIModelID: in.AIModelID, Stage: "render"})
	if inspectErr != nil {
		return inspectErr
	}
	if !inspection.Ready {
		issue := inspection.Issues[0]
		return msg.Err(msg.ErrArrangementBlocked, msg.Params{"detail": issue.Message, "reason": issue.Code, "partId": issue.PartID, "bar": issue.Bar})
	}
	if inspection.Score != nil {
		d.Score = inspection.Score
	}
	units, err := arrangementUnits(*d.Arrangement, *d.Score)
	if err != nil {
		return err
	}
	// Preparation is separate: AI stems already exist in the library. This
	// operation only renders verified note samples and places both on the grid.
	state := w.Store.Read()
	if d.Timeline == nil {
		if existing := slices.IndexFunc(state.Drafts, func(item schema.Draft) bool { return item.ID == d.ID }); existing >= 0 {
			d.Timeline = state.Drafts[existing].Timeline
		}
	}
	sources := make(map[string]schema.InstrumentSource)
	for _, source := range state.InstrumentSources {
		sources[source.ID] = source
	}
	materials := map[ArrangementPartRef]schema.ArrangementMaterial{}
	for _, material := range state.ArrangementMaterials {
		if !generationMaterialUsable(material) {
			continue
		}
		ref := ArrangementPartRef{material.SectionIndex, material.PhraseIndex, material.PartIndex}
		if ref.SectionIndex < 0 || ref.SectionIndex >= len(d.Arrangement.Sections) || ref.PhraseIndex < 0 || ref.PhraseIndex >= len(d.Arrangement.Sections[ref.SectionIndex].Phrases) || ref.PartIndex < 0 || ref.PartIndex >= len(d.Arrangement.Sections[ref.SectionIndex].Phrases[ref.PhraseIndex].Parts) {
			continue
		}
		modelID := in.AIModelID
		part := d.Arrangement.Sections[ref.SectionIndex].Phrases[ref.PhraseIndex].Parts[ref.PartIndex]
		if part.Generation != nil && part.Generation.ModelID != "" {
			modelID = part.Generation.ModelID
		}
		if material.ProjectID == d.ID && (modelID == "" || material.ModelID == modelID) && arrangementMaterialMatches(material, d, ref) {
			materials[ref] = material
		}
	}
	for _, unit := range units {
		if unit.Part.SourceID != "" && unit.Track.SourceID != unit.Part.SourceID && !(unit.Part.Source == "ai" && generationSettings(unit.Part).AssetKind == "phrase") {
			return fmt.Errorf("%s · %s 固定的音源不可用，请恢复该音源或解除固定", unit.Phrase, unit.Part.Instrument)
		}
		source, found := sources[unit.Track.SourceID]
		if unit.Part.Source == "real" {
			if !found || sourceIdentityIssue(unit.Part, source) != "" {
				return fmt.Errorf("%s · %s 缺少匹配的真实采样；请先在音源库下载或导入", unit.Phrase, unit.Part.Instrument)
			}
		} else if found && source.Origin == "ai" && len(source.Samples) > 0 && (source.ID == unit.Part.SourceID || arrangementSameInstrument(source.Instrument, unit.Part.Instrument)) {
		} else if !found || source.Origin != "ai" || len(source.Samples) == 0 || (source.ID != unit.Part.SourceID && !arrangementSameInstrument(source.Instrument, unit.Part.Instrument)) {
			material, exists := materials[ArrangementPartRef{unit.SectionIndex, unit.PhraseIndex, unit.PartIndex}]
			if !exists {
				return fmt.Errorf("%s · %s 缺少所选模型的 AI 乐句音源；请先点击生成 AI 音源", unit.Phrase, unit.Part.Instrument)
			}
			if material.Generation != nil && math.Abs(material.Generation.EffectiveSeconds-(unit.End-unit.Start)) > .04 {
				return fmt.Errorf("%s 的素材有效长度 %.2fs 与本句 %.2fs 不符，请作为自由素材手工放置或重新生成，不能静默裁掉内容", unit.Phrase, material.Generation.EffectiveSeconds, unit.End-unit.Start)
			}
			if material.Generation != nil {
				if _, integrityErr := w.verifyGenerationMaterialIntegrity(material); integrityErr != nil {
					return fmt.Errorf("%s · %s 的 AI 音源未通过完整性检查：%w", unit.Phrase, unit.Part.Instrument, integrityErr)
				}
			}
			path, pathErr := w.Store.MediaPath("outputs", material.FileName)
			if pathErr != nil {
				return pathErr
			}
			if _, pathErr = os.Stat(path); pathErr != nil {
				return fmt.Errorf("%s · %s 的 AI 乐句音源文件已丢失，请重新生成", unit.Phrase, unit.Part.Instrument)
			}
		}
	}
	if err = w.saveDraft(d); err != nil {
		return err
	}
	_, err = w.start("generate", msg.ActivityGenerate, nil, nil, func(ctx context.Context) error {
		assets := make([]schema.AudioAsset, 0, len(units))
		seenAssets := make(map[string]bool)
		lanes := make([]schema.AudioLane, 0, len(units))
		laneIndexes := make(map[string]int)
		realTracks := make(map[string]schema.ScoreTrack)
		for _, unit := range units {
			if unit.Part.Source != "real" && sources[unit.Track.SourceID].Origin != "ai" {
				continue
			}
			key := unit.Track.ID + ":" + unit.Track.SourceID
			track := realTracks[key]
			if track.ID == "" {
				track = unit.Track
				track.Notes = nil
			}
			track.Notes = append(track.Notes, unit.Track.Notes...)
			realTracks[key] = track
		}
		renderedReal := make(map[string]schema.AudioAsset)
		created := make([]string, 0, len(units))
		keep := false
		defer func() {
			if !keep {
				for _, path := range created {
					_ = os.Remove(path)
				}
			}
		}()
		lastBar := d.Arrangement.Sections[len(d.Arrangement.Sections)-1].EndBar
		for index, unit := range units {
			if err := ctx.Err(); err != nil {
				return err
			}
			w.progress(msg.ActivityRendering, msg.Params{"done": index, "total": len(units), "stage": unit.Part.Source}, int64(index), int64(len(units)))
			id := schema.NewID()
			asset := schema.AudioAsset{ID: id, Name: strings.TrimSpace(unit.Part.Instrument + " · " + unit.Phrase + " · " + unit.Part.Source), FileName: "import-" + id + ".wav", Origin: unit.Part.Source}
			if schema.TextLen(asset.Name) > 120 {
				asset.Name = string([]rune(asset.Name)[:120])
			}
			source, found := sources[unit.Track.SourceID]
			if found {
				asset.SourceResourceID = "source:" + source.ID
				asset.License, asset.LicenseURL, asset.SourceURL, asset.Author = source.License, source.LicenseURL, source.SourceURL, source.Author
			}
			clipOffset := 0.0
			clipTail := 0.0
			if found && source.Origin == unit.Part.Source && (len(source.Samples) > 0 || source.Plugin != nil) && (source.ID == unit.Part.SourceID || arrangementSameInstrument(source.Instrument, unit.Part.Instrument)) {
				clipTail = generatedSourceTailSeconds(source, unit.Track)
				key := unit.Track.ID + ":" + unit.Track.SourceID
				if cached, ok := renderedReal[key]; ok {
					asset = cached
				} else {
					path, pathErr := w.Store.MediaPath("outputs", asset.FileName)
					if pathErr != nil {
						return pathErr
					}
					created = append(created, path)
					music := *d.Score
					music.Tracks = []schema.ScoreTrack{realTracks[key]}
					music.Sections = append(slices.Clone(music.Sections), schema.ScoreSection{Name: "Arrangement length", Start: lastBar, End: lastBar})
					if source.Plugin != nil {
						if renderErr := w.renderPluginTrack(ctx, music, source, path); renderErr != nil {
							return renderErr
						}
					} else {
						verified, verifyErr := w.scoreSources(music)
						if verifyErr != nil {
							return verifyErr
						}
						if renderErr := score.RenderWithSources(ctx, nil, music, path, "", verified, nil); renderErr != nil {
							return renderErr
						}
					}
					var durationErr error
					asset.Duration, durationErr = audio.Duration(path)
					if durationErr != nil {
						return durationErr
					}
					renderedReal[key] = asset
				}
				clipOffset = unit.Start
			} else {
				material := materials[ArrangementPartRef{unit.SectionIndex, unit.PhraseIndex, unit.PartIndex}]
				asset.ID, asset.FileName, asset.Duration = material.ID, material.FileName, material.Duration
				asset.SourceResourceID = "material:" + material.ID
				if material.Generation != nil {
					clipOffset = material.Generation.EffectiveOffset
					clipTail = material.Generation.TailSeconds
				}
			}
			clipDuration := unit.End - unit.Start + clipTail
			if asset.Duration+0.1 < clipOffset+clipDuration {
				return fmt.Errorf("%s · %s 的音频只有 %.1f 秒，无法覆盖计划中的 %.1f 秒", unit.Phrase, unit.Part.Instrument, asset.Duration-clipOffset, clipDuration)
			}
			clipDuration = math.Min(clipDuration, asset.Duration-clipOffset)
			if !seenAssets[asset.ID] {
				assets = append(assets, asset)
				seenAssets[asset.ID] = true
			}
			laneKey := unit.Track.ID + ":" + unit.Part.Source + ":" + unit.Track.SourceID
			laneIndex, exists := laneIndexes[laneKey]
			if !exists {
				laneIndex = len(lanes)
				laneIndexes[laneKey] = laneIndex
				lanes = append(lanes, schema.AudioLane{ID: schema.NewID(), Origin: "arrangement", Name: unit.Track.Name + " · " + unit.Part.Source})
			}
			lanes[laneIndex].Clips = append(lanes[laneIndex].Clips, schema.AudioClip{ID: schema.NewID(), AssetID: asset.ID, Start: unit.Start, Offset: clipOffset, Duration: clipDuration, FadeIn: math.Min(.04, clipDuration/4), FadeOut: math.Min(.12, clipDuration/4)})
		}
		markers := make([]schema.AudioMarker, 0, len(d.Arrangement.Sections))
		for _, section := range d.Arrangement.Sections {
			markers = append(markers, schema.AudioMarker{ID: schema.NewID(), Name: section.Name, Time: d.Score.Seconds(section.StartBar, 1)})
		}
		w.edit.Lock()
		defer w.edit.Unlock()
		current := w.Store.Read()
		idx := slices.IndexFunc(current.Drafts, func(item schema.Draft) bool { return item.ID == d.ID })
		if idx < 0 || !reflect.DeepEqual(current.Drafts[idx].Score, d.Score) || !reflect.DeepEqual(current.Drafts[idx].Arrangement, d.Arrangement) {
			return fmt.Errorf("编曲期间曲谱或计划已更改，请重新拼合")
		}
		for id, source := range sources {
			used := slices.ContainsFunc(units, func(u arrangementUnit) bool { return u.Track.SourceID == id })
			if !used {
				continue
			}
			now := slices.IndexFunc(current.InstrumentSources, func(s schema.InstrumentSource) bool { return s.ID == id })
			if now < 0 || !reflect.DeepEqual(current.InstrumentSources[now], source) {
				return fmt.Errorf("渲染期间音源或预设已变更，未覆盖作品；请重新拼合")
			}
		}
		updated := current.Drafts[idx]
		if updated.Timeline != nil {
			for _, lane := range updated.Timeline.Tracks {
				if lane.Origin == "arrangement" {
					lane.Muted = true
				}
				lanes = append([]schema.AudioLane{lane}, lanes...)
			}
			for _, old := range updated.Timeline.Assets {
				if !seenAssets[old.ID] {
					assets = append(assets, old)
					seenAssets[old.ID] = true
				}
			}
			markers = append(updated.Timeline.Markers, markers...)
		}
		updated.Timeline = &schema.AudioTimeline{Assets: assets, Tracks: lanes, Markers: markers}
		if err := w.saveDraft(updated); err != nil {
			return err
		}
		keep = true
		return nil
	}, "", d.ID)
	return err
}

func generatedSourceTailSeconds(source schema.InstrumentSource, track schema.ScoreTrack) float64 {
	tail := 0.0
	for _, note := range track.Notes {
		articulation := note.Articulation
		if articulation == "" {
			articulation = source.DefaultArticulation
		}
		if articulation == "" {
			articulation = "normal"
		}
		for _, sample := range source.Samples {
			if sample.GenerationID != "" && sample.RootPitch == note.Pitch && sample.Articulation == articulation && note.Velocity >= sample.VelocityLow && note.Velocity <= sample.VelocityHigh {
				tail = max(tail, sample.Duration-sample.SustainSeconds)
			}
		}
	}
	return max(0, tail)
}
