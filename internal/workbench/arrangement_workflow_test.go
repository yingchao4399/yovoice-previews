package workbench

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"yovoice/internal/catalog"
	"yovoice/internal/schema"
	"yovoice/internal/score"
	"yovoice/internal/testkit"
)

func workflowDraft() schema.Draft {
	d := schema.DefaultDraft()
	d.Kind, d.ModelID, d.Title = "score", "musescore-general-sf2", "民乐测试"
	d.Arrangement = &schema.ArrangementPlan{Approved: true, Brief: "箫和二胡", Title: d.Title, Tempo: 120, TimeSignature: []int{4, 4}, Seconds: 4,
		Sections: []schema.ArrangementSection{{Name: "A", StartBar: 1, EndBar: 2, Phrases: []schema.ArrangementPhrase{
			{Name: "真实句", StartBar: 1, EndBar: 1, Parts: []schema.ArrangementPart{{Instrument: "二胡", Role: "melody", Source: "real"}}},
			{Name: "AI 句", StartBar: 2, EndBar: 2, Parts: []schema.ArrangementPart{{Instrument: "箫", Role: "melody", Source: "ai"}}},
		}}}}
	d.Score = &schema.Score{Tempo: 120, TimeSignature: []int{4, 4}, Tracks: []schema.ScoreTrack{
		{ID: "erhu", Name: "二胡", Role: "melody", Notes: []schema.ScoreNote{{Bar: 1, Beat: 1, Pitch: 62, Length: 1, Velocity: 90}}},
		{ID: "xiao", Name: "箫", Role: "melody", Notes: []schema.ScoreNote{{Bar: 2, Beat: 1, Pitch: 67, Length: 1, Velocity: 80}}},
	}}
	return d
}

func TestArrangementBambooFluteUsesDiziInstrument(t *testing.T) {
	if !arrangementSameInstrument("竹笛", "笛子") || !arrangementSameInstrument("洞箫", "箫") {
		t.Fatal("民乐常用名称没有匹配到对应音源")
	}
}

func TestArrangementUnitsBindSameNameTracksByPlannedPartID(t *testing.T) {
	d := workflowDraft()
	plan := d.Arrangement
	first := schema.NewID()
	second := schema.NewID()
	plan.Sections[0].Phrases[0].Parts[0].ID = first
	plan.Sections[0].Phrases[1].Parts[0].ID = second
	plan.Sections[0].Phrases[1].Parts[0].Instrument = "二胡"
	plan.Sections[0].Phrases[1].Parts[0].Source = "real"
	d.Score.Tracks = []schema.ScoreTrack{
		{ID: "a", PartIDs: []string{first}, Name: "二胡", Role: "melody", Notes: []schema.ScoreNote{{Bar: 1, Beat: 1, Pitch: 62, Length: 1, Velocity: 90}}},
		{ID: "b", PartIDs: []string{second}, Name: "二胡", Role: "melody", Notes: []schema.ScoreNote{{Bar: 2, Beat: 1, Pitch: 64, Length: 1, Velocity: 90}}},
	}
	units, err := arrangementUnits(*plan, *d.Score)
	if err != nil || len(units) != 2 || units[0].Track.ID != "a" || units[1].Track.ID != "b" {
		t.Fatalf("同名声部应按 partId 分开绑定：units=%+v err=%v", units, err)
	}
	d.Score.Tracks[1].PartIDs = []string{first}
	if _, err := arrangementUnits(*plan, *d.Score); err == nil {
		t.Fatal("partId 不匹配时不得按乐器名回退")
	}
}

func TestSaveDraftMigratesUnambiguousLegacyArrangementIDs(t *testing.T) {
	wb, err := New(t.TempDir())
	must(t, err)
	defer wb.Close()
	d := workflowDraft()
	must(t, wb.SaveDraft(d))
	state := wb.Store.Read()
	draftIndex := slices.IndexFunc(state.Drafts, func(item schema.Draft) bool { return item.ID == d.ID })
	if draftIndex < 0 || state.Drafts[draftIndex].Arrangement == nil || state.Drafts[draftIndex].Score == nil {
		t.Fatalf("工程没有保存或编曲内容为空：drafts=%+v", state.Drafts)
	}
	saved := state.Drafts[draftIndex]
	for _, section := range saved.Arrangement.Sections {
		if !schema.ValidID(section.ID) {
			t.Fatalf("旧工程段落 ID 未迁移：%+v", section)
		}
		for _, phrase := range section.Phrases {
			if !schema.ValidID(phrase.ID) {
				t.Fatalf("旧工程乐句 ID 未迁移：%+v", phrase)
			}
			for _, part := range phrase.Parts {
				if !schema.ValidID(part.ID) {
					t.Fatalf("旧工程声部 ID 未迁移：%+v", part)
				}
				if !slices.ContainsFunc(saved.Score.Tracks, func(track schema.ScoreTrack) bool {
					return slices.Contains(track.PartIDs, part.ID)
				}) {
					t.Fatalf("迁移后的曲谱没有引用计划声部 %s", part.ID)
				}
			}
		}
	}
	if _, err := arrangementUnits(*saved.Arrangement, *saved.Score); err != nil {
		t.Fatalf("迁移后应按稳定 ID 继续拼合校验：%v", err)
	}
}

func TestSaveDraftLeavesAmbiguousLegacyArrangementUntouched(t *testing.T) {
	wb, err := New(t.TempDir())
	must(t, err)
	defer wb.Close()
	d := workflowDraft()
	duplicate := d.Score.Tracks[0]
	duplicate.ID = "erhu-duplicate"
	d.Score.Tracks = append(d.Score.Tracks, duplicate)
	must(t, wb.SaveDraft(d))
	saved := wb.Store.Read().Drafts[0]
	for _, section := range saved.Arrangement.Sections {
		if section.ID != "" {
			t.Fatalf("歧义迁移不应部分写入段落 ID：%+v", section)
		}
		for _, phrase := range section.Phrases {
			if phrase.ID != "" {
				t.Fatalf("歧义迁移不应部分写入乐句 ID：%+v", phrase)
			}
			for _, part := range phrase.Parts {
				if part.ID != "" {
					t.Fatalf("歧义迁移不应部分写入声部 ID：%+v", part)
				}
			}
		}
	}
	if len(saved.Score.Tracks[0].PartIDs) != 0 || len(saved.Score.Tracks[2].PartIDs) != 0 {
		t.Fatal("歧义迁移不应写入曲谱声部引用")
	}
}

func TestArrangementThreeStagesCreateEditableTimeline(t *testing.T) {
	wb, err := New(t.TempDir())
	must(t, err)
	defer wb.Close()
	d := workflowDraft()
	model := filepath.Join(wb.Store.Root, "models", "ace-step.gguf")
	must(t, os.WriteFile(model, []byte("test"), 0600))
	executable, err := os.Executable()
	must(t, err)
	must(t, wb.Store.Update(func(s *schema.State) {
		s.Models = []schema.InstalledModel{{ID: "ace-step-1.5-turbo-bf16", Path: model}}
		s.RuntimePath = &executable
		s.RuntimeBackend = ptr("cpu")
		s.RuntimeVersion = ptr(catalog.EngineVersion)
		s.Preferences.Backend = "cpu"
	}, true))
	// Stage 1: an AI phrase really becomes a WAV in the source library.
	must(t, wb.PrepareArrangementAI(ArrangementAIPrepareInput{Draft: d, ModelID: "ace-step-1.5-turbo-bf16"}))
	<-wb.Done()
	state := wb.Store.Read()
	if state.Activity == nil || state.Activity.Status != "completed" || len(state.ArrangementMaterials) != 1 {
		t.Fatalf("AI preparation: %+v, materials=%d", state.Activity, len(state.ArrangementMaterials))
	}
	if state.ArrangementMaterials[0].Duration < 2 {
		t.Fatalf("AI phrase too short: %+v", state.ArrangementMaterials[0])
	}
	// Stage 2: prepare a verified real note sample.
	sampleID := schema.NewID()
	sampleFile := "sample-" + sampleID + ".wav"
	samplePath := filepath.Join(wb.Store.Root, "instruments", sampleFile)
	data := make([]float32, score.SampleRate)
	must(t, score.WriteWAV(samplePath, data, data))
	contents, err := os.ReadFile(samplePath)
	must(t, err)
	digest := sha256.Sum256(contents)
	source := schema.InstrumentSource{ID: schema.NewID(), Name: "二胡测试", Instrument: "二胡", Origin: "real", Verification: "user", DefaultArticulation: "normal", Samples: []schema.InstrumentSample{{ID: sampleID, FileName: sampleFile, SHA256: hex.EncodeToString(digest[:]), RootPitch: 62, Articulation: "normal", VelocityLow: 1, VelocityHigh: 127, Duration: 1}}}
	must(t, wb.Store.Update(func(s *schema.State) { s.InstrumentSources = append(s.InstrumentSources, source) }, true))
	d.Score.Tracks[0].SourceID = source.ID
	band, err := wb.SaveArrangementBand(BandPresetInput{Draft: d, AIModelID: "ace-step-1.5-turbo-bf16"})
	must(t, err)
	if len(band.Parts) != 2 || band.Parts[0].SourceID != source.ID || band.AIModelID != "ace-step-1.5-turbo-bf16" {
		t.Fatalf("band did not keep reusable sources: %+v", band)
	}
	// Stage 3: render real notes, use the prepared AI phrase, and place each at
	// its planned second. The plan and score remain editable in the same draft.
	for _, m := range wb.Store.Read().ArrangementMaterials {
		if m.ReviewStatus == "needs_review" {
			must(t, wb.ReviewArrangementMaterial(m.ID, true))
		}
	}
	must(t, wb.RenderArrangement(ArrangementRenderInput{Draft: d}))
	<-wb.Done()
	state = wb.Store.Read()
	if state.Activity == nil || state.Activity.Status != "completed" {
		t.Fatalf("assembly: %+v", state.Activity)
	}
	index := slices.IndexFunc(state.Drafts, func(item schema.Draft) bool { return item.ID == d.ID })
	if index < 0 || state.Drafts[index].Timeline == nil {
		t.Fatal("no editable timeline")
	}
	saved := state.Drafts[index]
	if saved.Score == nil || saved.Arrangement == nil || len(saved.Timeline.Tracks) != 2 || len(saved.Timeline.Assets) != 2 {
		t.Fatalf("lost score or stems: %+v", saved.Timeline)
	}
	starts := []float64{saved.Timeline.Tracks[0].Clips[0].Start, saved.Timeline.Tracks[1].Clips[0].Start}
	slices.Sort(starts)
	if starts[0] != 0 || starts[1] != 2 {
		t.Fatalf("wrong planned positions: %v", starts)
	}
	if err := schema.ValidateTimeline(saved.Timeline, state.History); err != nil {
		t.Fatal(err)
	}
	manual := schema.AudioLane{ID: schema.NewID(), Name: "手动添加的参考音", Clips: []schema.AudioClip{{ID: schema.NewID(), AssetID: state.ArrangementMaterials[0].ID, Start: 0, Duration: .5}}}
	saved.Timeline.Tracks = append(saved.Timeline.Tracks, manual)
	must(t, wb.SaveDraft(saved))
	must(t, wb.RenderArrangement(ArrangementRenderInput{Draft: saved}))
	<-wb.Done()
	state = wb.Store.Read()
	index = slices.IndexFunc(state.Drafts, func(item schema.Draft) bool { return item.ID == d.ID })
	saved = state.Drafts[index]
	if state.Activity == nil || state.Activity.Status != "completed" || len(saved.Timeline.Tracks) != 5 || !slices.ContainsFunc(saved.Timeline.Tracks, func(lane schema.AudioLane) bool { return lane.ID == manual.ID && !lane.Muted }) {
		t.Fatal("reassembly changed or lost a manually edited lane")
	}
	if err := schema.ValidateTimeline(saved.Timeline, state.History); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "folk.yovoice")
	must(t, wb.ExportProject(d.ID, archive))
	other, err := New(t.TempDir())
	must(t, err)
	defer other.Close()
	imported, err := other.ImportProject(archive)
	must(t, err)
	otherState := other.Store.Read()
	if imported.Timeline == nil || len(imported.Timeline.Assets) != 3 || !slices.ContainsFunc(otherState.ArrangementMaterials, func(material schema.ArrangementMaterial) bool { return material.ProjectID == imported.ID }) {
		t.Fatal("project export/import lost prepared AI audio or assembled tracks")
	}
}

func TestArrangementRejectsMissingRealAndAIWithoutSubstitution(t *testing.T) {
	wb, err := New(t.TempDir())
	must(t, err)
	defer wb.Close()
	d := workflowDraft()
	font := filepath.Join(wb.Store.Root, "models", "test.sf2")
	must(t, os.WriteFile(font, testkit.SoundFont(), 0600))
	must(t, wb.Store.Update(func(s *schema.State) { s.Models = []schema.InstalledModel{{ID: d.ModelID, Path: font}} }, true))
	if err := wb.RenderArrangement(ArrangementRenderInput{Draft: d}); err == nil {
		t.Fatal("missing real source must not fall back to SF2")
	}
}

func TestArrangementUsesOnlySelectedAIModel(t *testing.T) {
	wb, err := New(t.TempDir())
	must(t, err)
	defer wb.Close()
	d := workflowDraft()
	d.Arrangement.Sections[0].Phrases[0].Parts[0].Source = "ai"
	materials := make([]schema.ArrangementMaterial, 0, 4)
	for _, model := range []string{"model-a", "model-b"} {
		for phrase := range d.Arrangement.Sections[0].Phrases {
			id := schema.NewID()
			file := "import-" + id + ".wav"
			path, pathErr := wb.Store.MediaPath("outputs", file)
			must(t, pathErr)
			audioData := make([]float32, score.SampleRate*2)
			must(t, score.WriteWAV(path, audioData, audioData))
			ref := ArrangementPartRef{SectionIndex: 0, PhraseIndex: phrase, PartIndex: 0}
			materials = append(materials, schema.ArrangementMaterial{ID: id, ProjectID: d.ID, PlanHash: arrangementJobHash(*d.Arrangement, ref), ScoreHash: arrangementConditionHash(d, ref), SectionIndex: 0, PhraseIndex: phrase, PartIndex: 0, Instrument: d.Arrangement.Sections[0].Phrases[phrase].Parts[0].Instrument, ModelID: model, FileName: file, Duration: 2})
		}
	}
	must(t, wb.Store.Update(func(s *schema.State) { s.ArrangementMaterials = materials }, true))
	if err := wb.RenderArrangement(ArrangementRenderInput{Draft: d, AIModelID: "model-c"}); err == nil {
		t.Fatal("unprepared selected model must not silently use another model")
	}
	must(t, wb.RenderArrangement(ArrangementRenderInput{Draft: d, AIModelID: "model-a"}))
	<-wb.Done()
	state := wb.Store.Read()
	index := slices.IndexFunc(state.Drafts, func(item schema.Draft) bool { return item.ID == d.ID })
	if state.Activity == nil || state.Activity.Status != "completed" || index < 0 || state.Drafts[index].Timeline == nil {
		t.Fatalf("selected model assembly failed: activity=%+v drafts=%d index=%d", state.Activity, len(state.Drafts), index)
	}
	for _, asset := range state.Drafts[index].Timeline.Assets {
		if slices.ContainsFunc(materials[2:], func(material schema.ArrangementMaterial) bool { return material.ID == asset.ID }) {
			t.Fatalf("assembled an asset from the other AI model: %s", asset.ID)
		}
	}
}

func TestArrangementAIJobsCanSelectOnePart(t *testing.T) {
	d := workflowDraft()
	ref := ArrangementPartRef{SectionIndex: 0, PhraseIndex: 1, PartIndex: 0}
	before := arrangementJobHash(*d.Arrangement, ref)
	d.Arrangement.Sections[0].Phrases[0].Parts[0].Technique = "颤弓"
	if after := arrangementJobHash(*d.Arrangement, ref); after != before {
		t.Fatal("editing an unrelated real part invalidated the AI phrase")
	}
	d.Arrangement.Sections[0].Phrases[0].Parts[0].Source = "ai"
	all, err := arrangementAIJobs(*d.Arrangement, nil)
	must(t, err)
	if len(all) != 2 {
		t.Fatalf("expected two AI phrase jobs, got %d", len(all))
	}
	one, err := arrangementAIJobs(*d.Arrangement, &ref)
	must(t, err)
	if len(one) != 1 || one[0].phrase.Name != "AI 句" {
		t.Fatalf("single-part selection failed: %+v", one)
	}
}

func TestArrangementAIOnlyDoesNotRequireSoundFont(t *testing.T) {
	wb, err := New(t.TempDir())
	must(t, err)
	defer wb.Close()
	d := workflowDraft()
	d.Arrangement.Sections[0].Phrases[0].Parts[0].Source = "ai"
	model := filepath.Join(wb.Store.Root, "models", "ace-step.gguf")
	must(t, os.WriteFile(model, []byte("test"), 0600))
	executable, err := os.Executable()
	must(t, err)
	must(t, wb.Store.Update(func(s *schema.State) {
		s.Models = []schema.InstalledModel{{ID: "ace-step-1.5-turbo-bf16", Path: model}}
		s.RuntimePath = &executable
		s.RuntimeBackend = ptr("cpu")
		s.RuntimeVersion = ptr(catalog.EngineVersion)
		s.Preferences.Backend = "cpu"
	}, true))
	must(t, wb.PrepareArrangementAI(ArrangementAIPrepareInput{Draft: d, ModelID: "ace-step-1.5-turbo-bf16"}))
	<-wb.Done()
	for _, m := range wb.Store.Read().ArrangementMaterials {
		if m.ReviewStatus == "needs_review" {
			must(t, wb.ReviewArrangementMaterial(m.ID, true))
		}
	}
	must(t, wb.RenderArrangement(ArrangementRenderInput{Draft: d}))
	<-wb.Done()
	state := wb.Store.Read()
	if state.Activity == nil || state.Activity.Status != "completed" || len(state.Drafts[0].Timeline.Tracks) != 2 {
		t.Fatalf("AI-only without soundfont: %+v", state.Activity)
	}
}

func TestArrangementRealPhrasesShareOneTrackWithoutSoundFont(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		t.Run(map[bool]string{false: "sameInstrument", true: "explicitReplacement"}[replacement], func(t *testing.T) {
			testArrangementRealPhrases(t, replacement)
		})
	}
}

func testArrangementRealPhrases(t *testing.T, replacement bool) {
	wb, err := New(t.TempDir())
	must(t, err)
	defer wb.Close()
	d := workflowDraft()
	d.Arrangement.Sections[0].Phrases[1].Parts[0].Instrument = "二胡"
	d.Arrangement.Sections[0].Phrases[1].Parts[0].Source = "real"
	d.Score.Tracks[0].Notes = append(d.Score.Tracks[0].Notes, schema.ScoreNote{Bar: 2, Beat: 1, Pitch: 62, Length: 1, Velocity: 80})
	d.Score.Tracks = d.Score.Tracks[:1]
	sampleID := schema.NewID()
	file := "sample-" + sampleID + ".wav"
	path := filepath.Join(wb.Store.Root, "instruments", file)
	data := make([]float32, score.SampleRate)
	must(t, score.WriteWAV(path, data, data))
	contents, err := os.ReadFile(path)
	must(t, err)
	digest := sha256.Sum256(contents)
	source := schema.InstrumentSource{ID: schema.NewID(), Name: "二胡测试", Instrument: "二胡", Origin: "real", Verification: "user", DefaultArticulation: "normal", Samples: []schema.InstrumentSample{{ID: sampleID, FileName: file, SHA256: hex.EncodeToString(digest[:]), RootPitch: 62, Articulation: "normal", VelocityLow: 1, VelocityHigh: 127, Duration: 1}}}
	must(t, wb.Store.Update(func(s *schema.State) { s.InstrumentSources = append(s.InstrumentSources, source) }, true))
	d.Score.Tracks[0].SourceID = source.ID
	if replacement {
		d.Arrangement.Sections[0].Phrases[0].Parts[0].AllowSubstitution = true
		// 计划和曲谱仍是二胡；手动选择的笛子只替换实际发声音源。
		source.Instrument = "笛子"
		must(t, wb.Store.Update(func(s *schema.State) { s.InstrumentSources = []schema.InstrumentSource{source} }, true))
		for pi := range d.Arrangement.Sections[0].Phrases {
			d.Arrangement.Sections[0].Phrases[pi].Parts[0].AllowSubstitution = true
			d.Arrangement.Sections[0].Phrases[pi].Parts[0].SourceID = source.ID
		}
	}
	for _, m := range wb.Store.Read().ArrangementMaterials {
		if m.ReviewStatus == "needs_review" {
			must(t, wb.ReviewArrangementMaterial(m.ID, true))
		}
	}
	must(t, wb.RenderArrangement(ArrangementRenderInput{Draft: d}))
	<-wb.Done()
	state := wb.Store.Read()
	if state.Activity == nil || state.Activity.Status != "completed" {
		t.Fatalf("real-only render without SF2: %+v", state.Activity)
	}
	timeline := state.Drafts[0].Timeline
	if timeline == nil || len(timeline.Tracks) != 1 || len(timeline.Tracks[0].Clips) != 2 || len(timeline.Assets) != 1 {
		t.Fatalf("real phrases should share one stem and lane: %+v", timeline)
	}
	if err := schema.ValidateTimeline(timeline, state.History); err != nil {
		t.Fatal(err)
	}
}
