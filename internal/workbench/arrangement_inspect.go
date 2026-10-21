package workbench

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"yovoice/internal/msg"
	"yovoice/internal/schema"
)

type ArrangementInspectInput struct {
	Draft     schema.Draft `json:"draft"`
	AIModelID string       `json:"aiModelId,omitempty"`
	Stage     string       `json:"stage,omitempty"`
}

type ArrangementIssueDetail struct {
	Code       string             `json:"code"`
	Ref        ArrangementPartRef `json:"ref"`
	StartBar   int                `json:"startBar"`
	EndBar     int                `json:"endBar"`
	PartID     string             `json:"partId,omitempty"`
	Phrase     string             `json:"phrase"`
	Instrument string             `json:"instrument"`
	SourceID   string             `json:"sourceId,omitempty"`
	SourceName string             `json:"sourceName,omitempty"`
	Bar        int                `json:"bar,omitempty"`
	Pitch      int                `json:"pitch,omitempty"`
	Message    string             `json:"message"`
}

type ArrangementSourceCandidate struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Instrument string `json:"instrument"`
	Ensemble   bool   `json:"ensemble"`
}

type ArrangementPartInspection struct {
	PartID            string                       `json:"partId,omitempty"`
	Ref               ArrangementPartRef           `json:"ref"`
	Phrase            string                       `json:"phrase"`
	Instrument        string                       `json:"instrument"`
	SourceID          string                       `json:"sourceId,omitempty"`
	MaterialID        string                       `json:"materialId,omitempty"`
	MaterialCondition string                       `json:"materialCondition,omitempty"`
	ReviewStatus      string                       `json:"reviewStatus,omitempty"`
	Ready             bool                         `json:"ready"`
	Candidates        []ArrangementSourceCandidate `json:"candidates"`
	Issues            []ArrangementIssueDetail     `json:"issues"`
}

type ArrangementInspection struct {
	Revision string                      `json:"revision"`
	Ready    bool                        `json:"ready"`
	Parts    []ArrangementPartInspection `json:"parts"`
	Issues   []ArrangementIssueDetail    `json:"issues"`
	Score    *schema.Score               `json:"score,omitempty"`
}

// 修订只包含编曲执行输入，不受标题和更新时间变化影响。
func arrangementRevision(d schema.Draft) string {
	b, _ := json.Marshal([]any{d.Arrangement, d.Score})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func requiredArticulations(technique string) []string {
	var required []string
	for _, entry := range []struct {
		Terms []string
		Art   string
	}{{[]string{"轮指", "tremolo"}, "tremolo"}, {[]string{"挑弹", "pluck"}, "pluck"}, {[]string{"拨奏", "pizzicato"}, "pizzicato"}, {[]string{"跳弓", "spiccato"}, "spiccato"}} {
		for _, term := range entry.Terms {
			if strings.Contains(strings.ToLower(technique), term) {
				required = append(required, entry.Art)
				break
			}
		}
	}
	return required
}
func sourceSupportsArticulation(s schema.InstrumentSource, art string) bool {
	if s.Plugin != nil {
		return slices.Contains(s.Plugin.Articulations, art)
	}
	return slices.ContainsFunc(s.Samples, func(n schema.InstrumentSample) bool { return n.Articulation == art })
}

func sampleTechnique(part schema.ArrangementPart, note schema.ScoreNote, source schema.InstrumentSource) string {
	if note.Articulation != "" {
		return note.Articulation
	}
	if required := requiredArticulations(part.Technique); len(required) == 1 {
		return required[0]
	}
	if source.DefaultArticulation != "" {
		return source.DefaultArticulation
	}
	return "normal"
}

// 此判定与采样渲染器共用相同的七半音、奏法和力度层规则。
func sampleNoteIssue(part schema.ArrangementPart, note schema.ScoreNote, source schema.InstrumentSource) string {
	art := sampleTechnique(part, note, source)
	if source.Plugin != nil {
		if !slices.Contains(source.Plugin.Articulations, art) {
			return "ARTICULATION_UNAVAILABLE"
		}
		return ""
	}
	tech := slices.DeleteFunc(slices.Clone(source.Samples), func(s schema.InstrumentSample) bool { return s.Articulation != art })
	if len(tech) == 0 {
		return "ARTICULATION_UNAVAILABLE"
	}
	pitchMatch := false
	for _, s := range tech {
		if absInt(s.RootPitch-note.Pitch) <= 7 && (s.GenerationID == "" && !s.ExactPitch || s.RootPitch == note.Pitch) {
			pitchMatch = true
			if note.Velocity >= s.VelocityLow && note.Velocity <= s.VelocityHigh {
				return ""
			}
		}
	}
	if !pitchMatch {
		return "PITCH_OUT_OF_RANGE"
	}
	return "VELOCITY_LAYER_UNAVAILABLE"

}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func generatedSustainIssue(music *schema.Score, part schema.ArrangementPart, n schema.ScoreNote, s schema.InstrumentSource) bool {
	if music == nil {
		return false
	}
	generated := false
	length := music.Seconds(n.Bar, n.Beat+n.Length) - music.Seconds(n.Bar, n.Beat)
	for _, sample := range s.Samples {
		if sample.GenerationID == "" && !sample.ExactPitch {
			continue
		}
		generated = true
		if sample.RootPitch == n.Pitch && n.Velocity >= sample.VelocityLow && n.Velocity <= sample.VelocityHigh && sample.Articulation == sampleTechnique(part, n, s) && sample.SustainSeconds+.001 >= length {
			return false
		}
	}
	return generated
}

func sourceIdentityIssue(part schema.ArrangementPart, source schema.InstrumentSource) string {
	if source.Origin != part.Source {
		return "SOURCE_ORIGIN_MISMATCH"
	}
	if !part.AllowSubstitution && (!arrangementSameInstrument(part.Instrument, source.Instrument) || strings.Contains(source.Instrument, "模拟") || source.Provenance == "synthesized") {
		return "SOURCE_INSTRUMENT_MISMATCH"
	}
	if len(source.Samples) == 0 && source.Plugin == nil {
		return "SOURCE_FILE_MISSING"
	}
	return ""
}

type instrumentFileCheck struct {
	Size     int64
	Modified int64
	Expected string
	Issue    string
}

func (w *Workbench) sourceFilesIssue(source schema.InstrumentSource) string {
	if source.Plugin != nil {
		if issue := pluginFileIssue(source); issue != "" {
			return issue
		}
		for _, dep := range source.Plugin.Dependencies {
			if dep.SHA256 == "" || w.checkedInstrumentFile(dep.Path, dep.SHA256) != "" {
				return "PLUGIN_ASSET_MISSING"
			}
		}
		if !source.Plugin.Validated {
			return "PLUGIN_PRESET_UNVERIFIED"
		}
		return ""
	}
	for _, s := range source.Samples {
		p, e := w.Store.MediaPath("instruments", s.FileName)
		if e != nil {
			return "SOURCE_FILE_MISSING"
		}
		if issue := w.checkedInstrumentFile(p, s.SHA256); issue != "" {
			return issue
		}
	}
	return ""
}

func inspectionMessage(code string, part schema.ArrangementPart, phrase string, source schema.InstrumentSource, note schema.ScoreNote) string {
	label := phrase + " · " + part.Instrument
	switch code {
	case "SOURCE_INSTRUMENT_MISMATCH":
		return fmt.Sprintf("%s 绑定了 %s（%s），乐器不符；请选择匹配音源或明确设为替代音色", label, source.Name, source.Instrument)
	case "SOURCE_ORIGIN_MISMATCH":
		return label + " 的音源不符合真实/AI 来源限制"
	case "PLUGIN_ASSET_MISSING":
		return label + " 的插件音色包或采样依赖缺失 / 已变化，请在原生编辑器重新加载音色并试听"
	case "PLUGIN_PRESET_UNVERIFIED":
		return label + " 的插件预设已修改，请先试听当前预设以确认可发声"
	case "PLUGIN_PRESET_MISSING":
		return label + " 的插件或预设依赖缺失，请在设置中安装插件并重新保存音色"
	case "PLUGIN_VERSION_CHANGED":
		return label + " 的插件版本已变化，请重新测试并保存预设"
	case "SOURCE_FILE_MISSING":
		return label + " 的采样文件缺失，请修复或更换音源"
	case "SOURCE_FILE_CORRUPT":
		return label + " 的采样校验失败，请修复下载"
	case "SOURCE_REVIEW_REQUIRED":
		return label + " 的在线采样只完成名称与音高核验，请先在音源库试听并确认乐器，再继续备料"
	case "SOURCE_MISSING":
		return label + " 尚未绑定可用音源，请选择本地音源或准备素材"
	case "ARTICULATION_UNAVAILABLE":
		return fmt.Sprintf("%s 第%d小节缺少奏法 %s", label, note.Bar, sampleTechnique(part, note, source))
	case "VELOCITY_LAYER_UNAVAILABLE":
		return fmt.Sprintf("%s 第%d小节缺少力度 %d 的采样层", label, note.Bar, note.Velocity)
	case "PITCH_OUT_OF_RANGE":
		return fmt.Sprintf("%s 第%d小节 MIDI %d 超出 %s 的可用音域", label, note.Bar, note.Pitch, source.Name)
	case "AI_MATERIAL_REVIEW_REQUIRED":
		return label + " 的 AI 乐句已生成，请在音源库试听并确认；条件生成仍需检查演奏与对齐"
	case "SAMPLE_SUSTAIN_INSUFFICIENT":
		return fmt.Sprintf("%s 第%d小节的 AI 采样长音覆盖不足，请补充更长采样", label, note.Bar)
	case "AI_MATERIAL_MISSING":
		return label + " 尚未准备当前曲谱和模型的 AI 乐句"
	case "AI_MATERIAL_CORRUPT":
		return label + " 的 AI 乐句原片或适配文件缺失或已变化，请重新检查或生成；不会使用损坏素材"
	}
	return label + " 无法使用音源：" + code
}

// InspectArrangement 是选源、写谱、备料和拼合共同使用的只读预检。
func (w *Workbench) InspectArrangement(in ArrangementInspectInput) (ArrangementInspection, error) {
	d := in.Draft
	if in.AIModelID == "" && d.Arrangement != nil {
		in.AIModelID = d.Arrangement.AIModelID
	}
	r := ArrangementInspection{Revision: arrangementRevision(d), Ready: true, Parts: []ArrangementPartInspection{}, Issues: []ArrangementIssueDetail{}}
	if d.Arrangement == nil {
		return r, fmt.Errorf("作品没有编曲计划")
	}
	if e := schema.ArrangementIssue(d.Arrangement); e != "" {
		return r, fmt.Errorf("编曲计划无效：%s", e)
	}
	state := w.Store.Read()
	fileIssues := map[string]string{}
	filesChecked := map[string]bool{}
	checkFiles := func(s schema.InstrumentSource) string {
		if !filesChecked[s.ID] {
			fileIssues[s.ID] = w.sourceFilesIssue(s)
			filesChecked[s.ID] = true
		}
		return fileIssues[s.ID]
	}
	var units []arrangementUnit
	if d.Score != nil {
		plan := *d.Arrangement
		plan.Approved = true
		var e error
		units, e = arrangementUnits(plan, *d.Score)
		if e != nil {
			r.Ready = false
			r.Issues = append(r.Issues, ArrangementIssueDetail{Code: "PLAN_SCORE_UNCOVERED", Message: e.Error()})
			return r, nil
		}
	}
	bound := schema.Score{}
	boundIndexes := map[string]int{}
	boundIDs := map[string]bool{}
	if d.Score != nil {
		bound = *d.Score
		bound.Tracks = nil
	}
	for si, section := range d.Arrangement.Sections {
		for pi, phrase := range section.Phrases {
			for ai, part := range phrase.Parts {
				materialModel := in.AIModelID
				if part.Generation != nil && part.Generation.ModelID != "" {
					materialModel = part.Generation.ModelID
				}
				ref := ArrangementPartRef{si, pi, ai}
				item := ArrangementPartInspection{PartID: part.ID, Ref: ref, Phrase: phrase.Name, Instrument: part.Instrument, SourceID: part.SourceID, Ready: true, Candidates: []ArrangementSourceCandidate{}, Issues: []ArrangementIssueDetail{}}
				var tracks []schema.ScoreTrack
				for _, u := range units {
					if u.SectionIndex == si && u.PhraseIndex == pi && u.PartIndex == ai {
						tracks = append(tracks, u.Track)
					}
				}
				add := func(code string, s schema.InstrumentSource, n schema.ScoreNote) {
					if n.Bar == 0 {
						n.Bar = phrase.StartBar
					}
					issue := ArrangementIssueDetail{Ref: ref, StartBar: phrase.StartBar, EndBar: phrase.EndBar, Code: code, PartID: part.ID, Phrase: phrase.Name, Instrument: part.Instrument, SourceID: s.ID, SourceName: s.Name, Bar: n.Bar, Pitch: n.Pitch, Message: inspectionMessage(code, part, phrase.Name, s, n)}
					item.Issues = append(item.Issues, issue)
					item.Ready = false
				}
				var selected *schema.InstrumentSource
				for _, s := range state.InstrumentSources {
					if s.ID == part.SourceID {
						copy := s
						selected = &copy
					}
					if s.Verification == "metadata" {
						continue
					}
					if sourceIdentityIssue(schema.ArrangementPart{Instrument: part.Instrument, Source: part.Source, Technique: part.Technique}, s) != "" || checkFiles(s) != "" {
						continue
					}
					ok := true
					for _, art := range requiredArticulations(part.Technique) {
						if !sourceSupportsArticulation(s, art) {
							ok = false
						}
					}
					for _, t := range tracks {
						for _, n := range t.Notes {
							if sampleNoteIssue(part, n, s) != "" || generatedSustainIssue(d.Score, part, n, s) {
								ok = false
							}
						}
					}

					if ok {
						item.Candidates = append(item.Candidates, ArrangementSourceCandidate{ID: s.ID, Name: s.Name, Instrument: s.Instrument, Ensemble: strings.Contains(s.Name, "组") || strings.Contains(strings.ToLower(s.Name), "ensemble")})
					}
				}
				if selected == nil && part.SourceID == "" && len(item.Candidates) > 0 && (part.Source != "ai" || generationSettings(part).Method == "sampler" && (generationSettings(part).AssetKind == "instrument" || in.Stage == "audition")) {
					for _, s := range state.InstrumentSources {
						if s.ID == item.Candidates[0].ID {
							copy := s
							selected = &copy
							break
						}
					}
				}
				phraseAsset := part.Source == "ai" && generationSettings(part).AssetKind == "phrase" && in.Stage != "audition"
				if phraseAsset {
					selected = nil
					item.SourceID = ""
				}
				if selected != nil {
					item.SourceID = selected.ID
					if issue := sourceIdentityIssue(part, *selected); issue != "" {
						add(issue, *selected, schema.ScoreNote{})
					} else if issue := checkFiles(*selected); issue != "" {
						add(issue, *selected, schema.ScoreNote{})
					} else if selected.Verification == "metadata" {
						add("SOURCE_REVIEW_REQUIRED", *selected, schema.ScoreNote{})
					} else {
						for _, art := range requiredArticulations(part.Technique) {
							if !sourceSupportsArticulation(*selected, art) {
								add("ARTICULATION_UNAVAILABLE", *selected, schema.ScoreNote{Articulation: art})
							}
						}
						for _, t := range tracks {
							for _, n := range t.Notes {
								if code := sampleNoteIssue(part, n, *selected); code != "" {
									add(code, *selected, n)
								} else if generatedSustainIssue(d.Score, part, n, *selected) {
									add("SAMPLE_SUSTAIN_INSUFFICIENT", *selected, n)
								}
							}
						}
					}
				} else if part.SourceID != "" && !phraseAsset {
					add("SOURCE_MISSING", schema.InstrumentSource{}, schema.ScoreNote{})
				} else if part.Source == "ai" {
					ready := false
					reviewNeeded := false
					var chosen *schema.ArrangementMaterial
					for _, m := range state.ArrangementMaterials {
						if m.ProjectID == d.ID && m.SectionIndex == si && m.PhraseIndex == pi && m.PartIndex == ai && (materialModel == "" || materialModel == m.ModelID) && arrangementMaterialMatches(m, d, ref) {
							if m.Generation != nil && (m.Generation.Pitch != nil || m.GenerationResultState() == "rejected") {
								continue
							}
							// 新候选待审核期间继续使用已确认版本，不能让新候选遮蔽可用素材。
							if ready && !reviewNeeded && !generationMaterialUsable(m) {
								continue
							}
							p, e := w.Store.MediaPath("outputs", m.FileName)
							if e == nil {
								info, e := os.Stat(p)
								if e == nil && info.Size() > 0 {
									ready = true
									item.MaterialID = m.ID
									item.MaterialCondition = m.Condition
									item.ReviewStatus = m.ReviewStatus
									reviewNeeded = !generationMaterialUsable(m)
									candidate := m
									chosen = &candidate
								}
							}
						}
					}
					if in.Stage == "render" {
						if !ready {
							add("AI_MATERIAL_MISSING", schema.InstrumentSource{}, schema.ScoreNote{})
						} else if reviewNeeded {
							add("AI_MATERIAL_REVIEW_REQUIRED", schema.InstrumentSource{}, schema.ScoreNote{})
						} else if chosen != nil && chosen.Generation != nil {
							if _, e := w.verifyGenerationMaterialIntegrity(*chosen); e != nil {
								add("AI_MATERIAL_CORRUPT", schema.InstrumentSource{}, schema.ScoreNote{})
							}
						}
					}

				} else {
					add("SOURCE_MISSING", schema.InstrumentSource{}, schema.ScoreNote{})
				}
				if item.Ready {
					for _, track := range tracks {
						track.Notes = slices.Clone(track.Notes)
						if part.ID != "" {
							track.PartIDs = []string{part.ID}
						}
						if selected != nil {
							track.SourceID = selected.ID
							for ni := range track.Notes {
								track.Notes[ni].Articulation = sampleTechnique(part, track.Notes[ni], *selected)
							}
						} else {
							track.SourceID = ""
						}
						key := track.ID + ":" + track.SourceID
						if idx, ok := boundIndexes[key]; ok {
							bound.Tracks[idx].Notes = append(bound.Tracks[idx].Notes, track.Notes...)
							for _, id := range track.PartIDs {
								if !slices.Contains(bound.Tracks[idx].PartIDs, id) {
									bound.Tracks[idx].PartIDs = append(bound.Tracks[idx].PartIDs, id)
								}
							}
						} else {
							if boundIDs[track.ID] {
								digest := sha256.Sum256([]byte(key))
								track.ID = "arr-" + hex.EncodeToString(digest[:12])
							}
							boundIDs[track.ID] = true
							boundIndexes[key] = len(bound.Tracks)
							bound.Tracks = append(bound.Tracks, track)
						}
					}
				}
				r.Ready = r.Ready && item.Ready
				r.Issues = append(r.Issues, item.Issues...)
				r.Parts = append(r.Parts, item)
			}
		}
	}
	if d.Score != nil && r.Ready {
		if len(bound.Tracks) > schema.ScoreMaxTracks {
			return r, fmt.Errorf("按乐句绑定后超过 %d 条声部，请合并同源声部", schema.ScoreMaxTracks)
		}
		r.Score = &bound
	}
	return r, nil
}

type ArrangementBindInput struct {
	Draft             schema.Draft        `json:"draft"`
	PartID            string              `json:"partId,omitempty"`
	Ref               *ArrangementPartRef `json:"ref,omitempty"`
	SourceID          string              `json:"sourceId"`
	AllowSubstitution bool                `json:"allowSubstitution,omitempty"`
	ExpectedRevision  string              `json:"expectedRevision,omitempty"`
}

// BindArrangementSource 返回可撤销的编辑副本；只有正常保存流程会持久化用户选择。
func (w *Workbench) BindArrangementSource(in ArrangementBindInput) (schema.Draft, error) {
	var d schema.Draft
	b, _ := json.Marshal(in.Draft)
	_ = json.Unmarshal(b, &d)
	if d.Arrangement == nil {
		return d, fmt.Errorf("作品没有编曲计划")
	}
	if in.ExpectedRevision != "" && in.ExpectedRevision != arrangementRevision(d) {
		return d, fmt.Errorf("编曲已变更，请刷新诊断后重试")
	}
	if in.PartID == "" && in.Ref != nil {
		ref := *in.Ref
		if ref.SectionIndex < 0 || ref.SectionIndex >= len(d.Arrangement.Sections) {
			return d, fmt.Errorf("乐句位置无效")
		}
		section := &d.Arrangement.Sections[ref.SectionIndex]
		if ref.PhraseIndex < 0 || ref.PhraseIndex >= len(section.Phrases) || ref.PartIndex < 0 || ref.PartIndex >= len(section.Phrases[ref.PhraseIndex].Parts) {
			return d, fmt.Errorf("声部位置无效")
		}
		approved := d.Arrangement.Approved
		if d.Score == nil {
			schema.EnsureArrangementIDs(d.Arrangement)
		} else {
			d.Arrangement.Approved = true
			_, err := migrateLegacyArrangementIDs(&d)
			d.Arrangement.Approved = approved
			if err != nil {
				return d, err
			}
		}
		in.PartID = d.Arrangement.Sections[ref.SectionIndex].Phrases[ref.PhraseIndex].Parts[ref.PartIndex].ID
	}
	edit := schema.ArrangementBindingEdit{PartID: in.PartID, PreviousScore: d.Score}
	found := false
	for si := range d.Arrangement.Sections {
		for pi := range d.Arrangement.Sections[si].Phrases {
			for ai := range d.Arrangement.Sections[si].Phrases[pi].Parts {
				p := &d.Arrangement.Sections[si].Phrases[pi].Parts[ai]
				if p.ID != in.PartID {
					continue
				}
				found = true
				edit.PreviousSourceID = p.SourceID
				edit.PreviousSubstitution = p.AllowSubstitution
				if p.Locked && (p.SourceID != in.SourceID || p.AllowSubstitution != in.AllowSubstitution) {
					return d, fmt.Errorf("声部已锁定，请先解除锁定")
				}
				if in.SourceID != "" {
					var selected *schema.InstrumentSource
					for _, s := range w.Store.Read().InstrumentSources {
						if s.ID == in.SourceID {
							copy := s
							selected = &copy
						}
					}
					if selected == nil {
						return d, fmt.Errorf("音源不存在")
					}
					p.AllowSubstitution = in.AllowSubstitution
					if code := sourceIdentityIssue(*p, *selected); code != "" {
						return d, msg.Err(msg.ErrArrangementBlocked, msg.Params{"detail": inspectionMessage(code, *p, d.Arrangement.Sections[si].Phrases[pi].Name, *selected, schema.ScoreNote{}), "reason": code, "partId": p.ID})
					}
					if code := w.sourceFilesIssue(*selected); code != "" {
						return d, msg.Err(msg.ErrArrangementBlocked, msg.Params{"detail": inspectionMessage(code, *p, "", *selected, schema.ScoreNote{}), "reason": code, "partId": p.ID})
					}
				}
				p.SourceID = in.SourceID
				p.AllowSubstitution = in.AllowSubstitution
			}
		}
	}
	if !found {
		return d, fmt.Errorf("找不到编曲声部")
	}
	r, e := w.InspectArrangement(ArrangementInspectInput{Draft: d})
	if e != nil {
		return d, e
	}
	for _, issue := range r.Issues {
		if issue.PartID == in.PartID && !(in.SourceID == "" && issue.Code == "SOURCE_MISSING") {
			return d, msg.Err(msg.ErrArrangementBlocked, msg.Params{"detail": issue.Message, "reason": issue.Code, "partId": issue.PartID, "bar": issue.Bar})
		}
	}
	if r.Score != nil {
		d.Score = r.Score
	}
	edit.AppliedRevision = arrangementRevision(d)
	d.ArrangementBindings = append(d.ArrangementBindings, edit)
	if len(d.ArrangementBindings) > 10 {
		d.ArrangementBindings = d.ArrangementBindings[len(d.ArrangementBindings)-10:]
	}
	return d, nil
}

func (w *Workbench) UndoArrangementBinding(in schema.Draft) (schema.Draft, error) {
	var d schema.Draft
	b, _ := json.Marshal(in)
	_ = json.Unmarshal(b, &d)
	if len(d.ArrangementBindings) == 0 || d.Arrangement == nil {
		return d, fmt.Errorf("没有可撤销的音源替换")
	}
	i := len(d.ArrangementBindings) - 1
	edit := d.ArrangementBindings[i]
	if edit.AppliedRevision != arrangementRevision(d) {
		return d, fmt.Errorf("替换后曲谱或计划已有修改，请直接更换音源，以免覆盖编辑")
	}
	for si := range d.Arrangement.Sections {
		for pi := range d.Arrangement.Sections[si].Phrases {
			for ai := range d.Arrangement.Sections[si].Phrases[pi].Parts {
				p := &d.Arrangement.Sections[si].Phrases[pi].Parts[ai]
				if p.ID == edit.PartID {
					p.SourceID = edit.PreviousSourceID
					p.AllowSubstitution = edit.PreviousSubstitution
					d.Score = edit.PreviousScore
					d.ArrangementBindings = d.ArrangementBindings[:i]
					return d, nil
				}
			}
		}
	}
	return d, fmt.Errorf("原声部已不存在")
}

func (w *Workbench) checkedInstrumentFile(p, expected string) string {
	info, e := os.Stat(p)
	if e != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return "SOURCE_FILE_MISSING"
	}
	if cached, ok := w.instrumentFileChecks.Load(p); ok {
		old := cached.(instrumentFileCheck)
		if old.Size == info.Size() && old.Modified == info.ModTime().UnixNano() && old.Expected == expected {
			if old.Issue != "" {
				return old.Issue
			}
			return ""
		}
	}
	b, e := os.ReadFile(p)
	if e != nil || len(b) == 0 {
		return "SOURCE_FILE_MISSING"
	}
	h := sha256.Sum256(b)
	issue := ""
	if hex.EncodeToString(h[:]) != expected {
		issue = "SOURCE_FILE_CORRUPT"
	}
	w.instrumentFileChecks.Store(p, instrumentFileCheck{info.Size(), info.ModTime().UnixNano(), expected, issue})
	if issue != "" {
		return issue
	}
	return ""
}
