package workbench

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
	"yovoice/internal/audio"
	"yovoice/internal/msg"
	"yovoice/internal/schema"
)

// 显式提供只读导出的测试副本；不打开或修改正在使用的真实数据库。
func TestRealConfiguredPhraseComposer(t *testing.T) {
	fixture := os.Getenv("YOVOICE_DATABASE_BACKUP_FIXTURE")
	if fixture == "" || os.Getenv("YOVOICE_REAL_ONLINE_COMPOSER") == "" {
		t.Skip("需要已配置线上服务的隔离数据库备份及显式验收开关")
	}
	root := t.TempDir()
	b, err := os.ReadFile(fixture)
	must(t, err)
	must(t, os.WriteFile(filepath.Join(root, "yovoice.db"), b, 0600))
	w, err := New(root)
	must(t, err)
	defer w.Close()
	var source schema.InstrumentSource
	for _, s := range w.Store.Read().InstrumentSources {
		if s.ID == "d643a5cdf379ebd236073d2bf669f30e" {
			source = s
		}
	}
	if source.ID == "" {
		t.Fatal("缺少当前工程的小提琴组来源")
	}
	for _, sample := range source.Samples {
		data, e := os.ReadFile(filepath.Join(os.Getenv("YOVOICE_REAL_MEDIA_ROOT"), "instruments", sample.FileName))
		must(t, e)
		must(t, os.WriteFile(filepath.Join(root, "instruments", sample.FileName), data, 0600))
	}
	d := workflowDraft()
	d.Arrangement.Sections[0].Phrases = d.Arrangement.Sections[0].Phrases[:1]
	p := &d.Arrangement.Sections[0].Phrases[0]
	p.StartBar, p.EndBar = 1, 2
	p.Parts = []schema.ArrangementPart{{Instrument: "小提琴", Role: "melody", Source: "real", SourceID: source.ID, Technique: "连弓"}}
	d.Arrangement.Sections[0].EndBar = 2
	d.Arrangement.Seconds = 4
	d.Score = nil
	// 与实际规划入口一致，由后端生成身份，而不是用旧曲谱迁移代替新规划。
	schema.EnsureArrangementIDs(d.Arrangement)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	result, err := w.ComposeScore(ctx, ScoreBrief{ProjectID: d.ID, Brief: "两小节D小调小提琴组旋律，连弓，平静自然", Seconds: 4, Arrangement: d.Arrangement})
	if err != nil {
		detail, _ := json.Marshal(msg.Encode(err))
		t.Fatalf("已配置线上模型的实际逐句写谱失败（凭证已脱敏）：%s", detail)
	}
	d.Score = &result.Score
	inspection, err := w.InspectArrangement(ArrangementInspectInput{Draft: d})
	must(t, err)
	if !inspection.Ready {
		t.Fatalf("线上写谱未通过实际采样预检：%+v", inspection.Issues)
	}
	count := 0
	for _, track := range result.Tracks {
		count += len(track.Notes)
	}
	if count == 0 {
		t.Fatal("线上模型没有返回音符")
	}
	t.Logf("已配置线上模型：实际写谱 %d 个音符，通过所绑定真实采样的逐句约束与可奏预检", count)
}

func TestRealCurrentViolinProjectRepairRenderExport(t *testing.T) {
	fixture := os.Getenv("YOVOICE_REAL_PROJECT_FIXTURE")
	if fixture == "" {
		t.Skip("需要当前故障工程的只读导出副本")
	}
	var in struct {
		Draft   schema.Draft              `json:"draft"`
		Sources []schema.InstrumentSource `json:"sources"`
	}
	b, e := os.ReadFile(fixture)
	must(t, e)
	must(t, json.Unmarshal(b, &in))
	w, e := New(t.TempDir())
	must(t, e)
	defer w.Close()
	for _, s := range in.Sources {
		for _, sample := range s.Samples {
			b, e = os.ReadFile(filepath.Join(os.Getenv("YOVOICE_REAL_MEDIA_ROOT"), "instruments", sample.FileName))
			must(t, e)
			must(t, os.WriteFile(filepath.Join(w.Store.Root, "instruments", sample.FileName), b, 0600))
		}
	}
	must(t, w.Store.Update(func(s *schema.State) { s.InstrumentSources = in.Sources }, true))
	r, e := w.InspectArrangement(ArrangementInspectInput{Draft: in.Draft, Stage: "render"})
	must(t, e)
	if r.Ready || !slices.ContainsFunc(r.Issues, func(i ArrangementIssueDetail) bool {
		return i.Code == "SOURCE_INSTRUMENT_MISMATCH" && i.SourceName == "低音提琴"
	}) {
		t.Fatalf("实际故障未复现：%+v", r.Issues)
	}
	p := in.Draft.Arrangement.Sections[0].Phrases[0].Parts[0]
	for _, s := range in.Draft.Arrangement.Sections {
		for _, phrase := range s.Phrases {
			for _, part := range phrase.Parts {
				if part.SourceID == "6597b7d62db2c6bd2a9bb8212c2e98b8" {
					p = part
				}
			}
		}
	}
	fixed, e := w.BindArrangementSource(ArrangementBindInput{Draft: in.Draft, PartID: p.ID, SourceID: "d643a5cdf379ebd236073d2bf669f30e"})
	must(t, e)
	before, after := map[string]int{}, map[string]int{}
	for _, t := range in.Draft.Score.Tracks {
		for _, n := range t.Notes {
			key, _ := json.Marshal([]any{n.Bar, n.Beat, n.Pitch, n.Length, n.Velocity})
			before[string(key)]++
		}
	}
	for _, t := range fixed.Score.Tracks {
		for _, n := range t.Notes {
			key, _ := json.Marshal([]any{n.Bar, n.Beat, n.Pitch, n.Length, n.Velocity})
			after[string(key)]++
		}
	}
	a, _ := json.Marshal(before)
	b, _ = json.Marshal(after)
	if string(a) != string(b) {
		t.Fatal("修复改变了原旋律")
	}
	must(t, w.RenderArrangement(ArrangementRenderInput{Draft: fixed}))
	<-w.Done()
	st := w.Store.Read()
	if st.Activity == nil || st.Activity.Status != "completed" {
		t.Fatalf("实际小提琴工程渲染失败：%+v", st.Activity)
	}
	idx := slices.IndexFunc(st.Drafts, func(d schema.Draft) bool { return d.ID == fixed.ID })
	result := st.Drafts[idx]
	if result.Timeline == nil || len(result.Timeline.Assets) == 0 {
		t.Fatal("没有生成分轨")
	}
	for _, a := range result.Timeline.Assets {
		path, e := w.Store.MediaPath("outputs", a.FileName)
		must(t, e)
		q, e := audio.InspectSignal(path)
		must(t, e)
		t.Logf("实际录音采样渲染：%.1fs peak=%.4f", a.Duration, q.Peak)
	}
	archive := filepath.Join(t.TempDir(), "violin.yovoice")
	must(t, w.ExportProject(fixed.ID, archive))
	other, e := New(t.TempDir())
	must(t, e)
	defer other.Close()
	opened, e := other.ImportProject(archive)
	must(t, e)
	checked, e := other.InspectArrangement(ArrangementInspectInput{Draft: opened, Stage: "render"})
	must(t, e)
	if !checked.Ready {
		t.Fatalf("导出重开绑定丢失：%+v", checked.Issues)
	}
}

func TestRealDecentPresetTrackAndReopen(t *testing.T) {
	if os.Getenv("YOVOICE_REAL_VST_TEST") == "" {
		t.Skip("需要本机 VST3 及编译后的宿主")
	}
	plugin := ""
	for _, p := range ScanPlugins() {
		if p.Name == "DecentSampler" {
			plugin = p.Path
		}
	}
	if plugin == "" {
		t.Fatal("缺少 Decent Sampler")
	}
	w, e := New(t.TempDir())
	must(t, e)
	defer w.Close()
	src, e := w.CapturePluginInstrument(PluginInstrumentInput{Path: plugin, Name: "CC0 小提琴预设", Instrument: "小提琴", PresetPath: os.Getenv("YOVOICE_REAL_VST_PRESET"), Provenance: "recorded", Articulation: "sustain"})
	must(t, e)
	d := workflowDraft()
	d.Arrangement.Sections[0].Phrases = d.Arrangement.Sections[0].Phrases[:1]
	phrase := &d.Arrangement.Sections[0].Phrases[0]
	phrase.EndBar = 2
	part := &phrase.Parts[0]
	part.ID = schema.NewID()
	part.Instrument = "小提琴"
	part.SourceID = src.ID
	part.Technique = "连弓"
	d.Score.Tracks = d.Score.Tracks[:1]
	d.Score.Tracks[0].Name = "小提琴"
	d.Score.Tracks[0].PartIDs = []string{part.ID}
	d.Score.Tracks[0].Notes = append(d.Score.Tracks[0].Notes, schema.ScoreNote{Bar: 2, Beat: 2, Pitch: 69, Length: 1.5, Velocity: 75})
	must(t, w.RenderArrangement(ArrangementRenderInput{Draft: d}))
	<-w.Done()
	st := w.Store.Read()
	if st.Activity == nil || st.Activity.Status != "completed" {
		t.Fatalf("真实预设整轨失败：%+v", st.Activity)
	}
	idx := slices.IndexFunc(st.Drafts, func(v schema.Draft) bool { return v.ID == d.ID })
	saved := st.Drafts[idx]
	out, _ := w.Store.MediaPath("outputs", saved.Timeline.Assets[0].FileName)
	q, e := audio.InspectSignal(out)
	must(t, e)
	t.Logf("Decent 实际录音整轨 peak=%.4f", q.Peak)
	archive := filepath.Join(t.TempDir(), "violin-plugin.yovoice")
	must(t, w.ExportProject(d.ID, archive))
	other, e := New(t.TempDir())
	must(t, e)
	defer other.Close()
	opened, e := other.ImportProject(archive)
	must(t, e)
	got := other.Store.Read().InstrumentSources[0]
	if got.Plugin == nil || !slices.Equal(got.Plugin.StateData, src.Plugin.StateData) {
		t.Fatal("重开预设状态不一致")
	}
	out2 := filepath.Join(t.TempDir(), "reopened.wav")
	must(t, other.renderPluginTrack(context.Background(), *opened.Score, got, out2))
	q, e = audio.InspectSignal(out2)
	must(t, e)
	t.Logf("重开实际录音 peak=%.4f", q.Peak)
	bad := *opened.Score
	bad.Tracks = append([]schema.ScoreTrack{}, opened.Score.Tracks...)
	bad.Tracks[0].Notes = append([]schema.ScoreNote{}, bad.Tracks[0].Notes...)
	bad.Tracks[0].Notes[0].Pitch = 86
	if err := other.renderPluginTrack(context.Background(), bad, got, filepath.Join(t.TempDir(), "missing-note.wav")); err == nil {
		t.Fatal("整轨部分有声时，也必须拦住不在预设键区的 MIDI 86")
	}

}

func TestRealDSKPipaPresetTrackAndReopen(t *testing.T) {
	if os.Getenv("YOVOICE_REAL_DSK_TEST") == "" {
		t.Skip("需要本机 VST3 及编译后的宿主")
	}
	plugin := ""
	for _, p := range ScanPlugins() {
		if p.Name == "DSK Asian DreamZ" {
			plugin = p.Path
		}
	}
	if plugin == "" {
		t.Fatal("缺少 DSK Asian DreamZ")
	}
	w, e := New(t.TempDir())
	must(t, e)
	defer w.Close()
	src, e := w.CapturePluginInstrument(PluginInstrumentInput{Path: plugin, Name: "DSK 琵琶挑弹", Instrument: "琵琶", Program: "01 Pipa", Provenance: "recorded", Articulation: "pluck"})
	must(t, e)
	d := workflowDraft()
	d.Arrangement.Sections[0].Phrases = d.Arrangement.Sections[0].Phrases[:1]
	phrase := &d.Arrangement.Sections[0].Phrases[0]
	phrase.EndBar = 2
	part := &phrase.Parts[0]
	part.ID = schema.NewID()
	part.Instrument = "琵琶"
	part.SourceID = src.ID
	part.Technique = "挑弹"
	d.Score.Tracks = d.Score.Tracks[:1]
	d.Score.Tracks[0].Name = "琵琶"
	d.Score.Tracks[0].PartIDs = []string{part.ID}
	d.Score.Tracks[0].Notes = append(d.Score.Tracks[0].Notes, schema.ScoreNote{Bar: 2, Beat: 2, Pitch: 69, Length: 1.5, Velocity: 75})
	must(t, w.RenderArrangement(ArrangementRenderInput{Draft: d}))
	<-w.Done()
	st := w.Store.Read()
	if st.Activity == nil || st.Activity.Status != "completed" {
		t.Fatalf("真实预设整轨失败：%+v", st.Activity)
	}
	idx := slices.IndexFunc(st.Drafts, func(v schema.Draft) bool { return v.ID == d.ID })
	saved := st.Drafts[idx]
	out, _ := w.Store.MediaPath("outputs", saved.Timeline.Assets[0].FileName)
	q, e := audio.InspectSignal(out)
	must(t, e)
	t.Logf("DSK 实际琵琶整轨 peak=%.4f", q.Peak)
	archive := filepath.Join(t.TempDir(), "pipa.yovoice")
	must(t, w.ExportProject(d.ID, archive))
	other, e := New(t.TempDir())
	must(t, e)
	defer other.Close()
	opened, e := other.ImportProject(archive)
	must(t, e)
	got := other.Store.Read().InstrumentSources[0]
	if got.Plugin == nil || !slices.Equal(got.Plugin.StateData, src.Plugin.StateData) {
		t.Fatal("重开预设状态不一致")
	}
	out2 := filepath.Join(t.TempDir(), "reopened.wav")
	must(t, other.renderPluginTrack(context.Background(), *opened.Score, got, out2))
	q, e = audio.InspectSignal(out2)
	must(t, e)
	t.Logf("重开实际琵琶 peak=%.4f", q.Peak)
}

func TestRealYuE2PhraseGenerationAndAssembly(t *testing.T) {
	fixture := os.Getenv("YOVOICE_REAL_AUDIO_RUNTIME")
	if fixture == "" {
		t.Skip("需要已安装的实际音频内核与模型")
	}
	var runtime schema.State
	b, e := os.ReadFile(fixture)
	must(t, e)
	must(t, json.Unmarshal(b, &runtime))
	w, e := New(t.TempDir())
	must(t, e)
	defer w.Close()
	must(t, w.Store.Update(func(s *schema.State) {
		s.Models = runtime.Models
		s.RuntimePath = runtime.RuntimePath
		s.RuntimeBackend = runtime.RuntimeBackend
		s.RuntimeVersion = runtime.RuntimeVersion
		s.Runtimes = runtime.Runtimes
		s.Preferences.Backend = runtime.Preferences.Backend
	}, true))
	d := workflowDraft()
	d.Arrangement.Sections[0].Phrases = d.Arrangement.Sections[0].Phrases[1:]
	phrase := &d.Arrangement.Sections[0].Phrases[0]
	phrase.StartBar = 1
	phrase.EndBar = 4
	d.Arrangement.Sections[0].EndBar = 4
	d.Arrangement.Seconds = 8
	d.Arrangement.Brief = "干净的箫独奏，中国民俗器乐，无人声"
	d.Score.Tracks = d.Score.Tracks[1:]
	track := &d.Score.Tracks[0]
	track.Notes = []schema.ScoreNote{{Bar: 1, Beat: 1, Pitch: 67, Length: 3, Velocity: 80}, {Bar: 2, Beat: 2, Pitch: 69, Length: 2, Velocity: 90}, {Bar: 3, Beat: 1, Pitch: 72, Length: 2, Velocity: 70}, {Bar: 4, Beat: 1, Pitch: 67, Length: 4, Velocity: 60}}
	must(t, w.PrepareArrangementAI(ArrangementAIPrepareInput{Draft: d, ModelID: "yue2-3b-q8"}))
	<-w.Done()
	state := w.Store.Read()
	if state.Activity == nil || state.Activity.Status != "completed" || len(state.ArrangementMaterials) != 1 {
		t.Fatalf("实际 YuE2 备料失败：%+v", state.Activity)
	}
	material := state.ArrangementMaterials[0]
	if material.Condition != "score_conditioned" || material.ScoreHash == "" || material.ReviewStatus != "needs_review" {
		t.Fatalf("生成条件或审核标记不正确：%+v", material)
	}
	if w.RenderArrangement(ArrangementRenderInput{Draft: d, AIModelID: material.ModelID}) == nil {
		t.Fatal("未经试听确认不应正式拼合")
	}
	// 此处只验证确认接口和拼合技术路径；不是对听感的人工背书。
	must(t, w.ReviewArrangementMaterial(material.ID, true))
	must(t, w.RenderArrangement(ArrangementRenderInput{Draft: d, AIModelID: material.ModelID}))
	<-w.Done()
	state = w.Store.Read()
	if state.Activity.Status != "completed" {
		t.Fatalf("实际模型素材拼合失败：%+v", state.Activity)
	}
	result := state.Drafts[slices.IndexFunc(state.Drafts, func(x schema.Draft) bool { return x.ID == d.ID })]
	archive := filepath.Join(t.TempDir(), "ai.yovoice")
	must(t, w.ExportProject(d.ID, archive))
	other, e := New(t.TempDir())
	must(t, e)
	defer other.Close()
	_, e = other.ImportProject(archive)
	must(t, e)
	if output := os.Getenv("YOVOICE_AUDIT_OUTPUT"); output != "" {
		must(t, os.MkdirAll(output, 0700))
		path, _ := w.Store.MediaPath("outputs", material.FileName)
		data, e := os.ReadFile(path)
		must(t, e)
		must(t, os.WriteFile(filepath.Join(output, "yue2-phrase.wav"), data, 0600))
		data, e = os.ReadFile(archive)
		must(t, e)
		must(t, os.WriteFile(filepath.Join(output, "yue2-phrase.yovoice"), data, 0600))
	}
	if realFixture := os.Getenv("YOVOICE_REAL_PROJECT_FIXTURE"); realFixture != "" {
		var library struct {
			Sources []schema.InstrumentSource `json:"sources"`
		}
		b, e := os.ReadFile(realFixture)
		must(t, e)
		must(t, json.Unmarshal(b, &library))
		var violin schema.InstrumentSource
		for _, source := range library.Sources {
			if source.ID == "d643a5cdf379ebd236073d2bf669f30e" {
				violin = source
			}
		}
		for _, sample := range violin.Samples {
			b, e := os.ReadFile(filepath.Join(os.Getenv("YOVOICE_REAL_MEDIA_ROOT"), "instruments", sample.FileName))
			must(t, e)
			must(t, os.WriteFile(filepath.Join(w.Store.Root, "instruments", sample.FileName), b, 0600))
		}
		must(t, w.Store.Update(func(s *schema.State) { s.InstrumentSources = append(s.InstrumentSources, violin) }, true))
		d.Arrangement.Seconds = 12
		d.Arrangement.Sections[0].EndBar = 6
		d.Arrangement.Sections[0].Phrases = append(d.Arrangement.Sections[0].Phrases, schema.ArrangementPhrase{Name: "实际采样句", StartBar: 5, EndBar: 6, Parts: []schema.ArrangementPart{{Instrument: "小提琴", Role: "melody", Source: "real", SourceID: violin.ID}}})
		d.Score.Tracks = append(d.Score.Tracks, schema.ScoreTrack{ID: "violin", Name: "小提琴", Role: "melody", SourceID: violin.ID, Notes: []schema.ScoreNote{{Bar: 5, Beat: 1, Pitch: 67, Length: 2, Velocity: 80}, {Bar: 6, Beat: 1, Pitch: 69, Length: 4, Velocity: 70}}})
		must(t, w.RenderArrangement(ArrangementRenderInput{Draft: d, AIModelID: material.ModelID}))
		<-w.Done()
		mixed := w.Store.Read()
		if mixed.Activity.Status != "completed" {
			t.Fatalf("实际混合拼合失败：%+v", mixed.Activity)
		}
		project := mixed.Drafts[slices.IndexFunc(mixed.Drafts, func(v schema.Draft) bool { return v.ID == d.ID })]
		archive := filepath.Join(t.TempDir(), "hybrid.yovoice")
		must(t, w.ExportProject(d.ID, archive))
		fresh, e := New(t.TempDir())
		must(t, e)
		defer fresh.Close()
		opened, e := fresh.ImportProject(archive)
		must(t, e)
		if len(opened.Timeline.Tracks) < 2 || len(mixed.ArrangementMaterials) != 1 {
			t.Fatal("混合工程丢轨或重复生成 AI 乐句")
		}
		if output := os.Getenv("YOVOICE_AUDIT_OUTPUT"); output != "" {
			b, e := os.ReadFile(archive)
			must(t, e)
			must(t, os.WriteFile(filepath.Join(output, "hybrid.yovoice"), b, 0600))
		}
		t.Logf("实际 YuE2 + CC0 小提琴混合：%d 条轨道（含历史静音轨），导出重开成功", len(project.Timeline.Tracks))
	}
	t.Logf("实际 YuE2：%.2fs，peak %.4f，条件 %s，分轨 %d；尚需人工听感确认", material.Duration, material.Peak, material.Condition, len(result.Timeline.Tracks))
}
