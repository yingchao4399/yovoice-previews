package workbench

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
	"yovoice/internal/catalog"
	"yovoice/internal/msg"
	"yovoice/internal/schema"
	"yovoice/internal/testkit"
)

// 历史快照不携带时间轴，作品自身的时间轴保持不变。
func TestGenerationSnapshotOmitsTimeline(t *testing.T) {
	wb, err := New(t.TempDir())
	must(t, err)
	defer wb.Close()
	executable, err := os.Executable()
	must(t, err)
	model := filepath.Join(wb.Store.Root, "models", "fake.gguf")
	must(t, os.WriteFile(model, []byte("test"), 0600))
	reference := filepath.Join(t.TempDir(), "reference.wav")
	must(t, os.WriteFile(reference, wav(), 0600))
	voice, err := wb.ImportVoice(context.Background(), reference, "")
	must(t, err)
	must(t, wb.Store.Update(func(s *schema.State) {
		s.Models = []schema.InstalledModel{{ID: "index-2.5-q8", Path: model}}
		s.RuntimePath = &executable
		s.RuntimeBackend = ptr("cpu")
		s.RuntimeVersion = ptr(catalog.EngineVersion)
		s.Preferences.Backend = "cpu"
	}, true))
	d := schema.DefaultDraft()
	d.VoiceID = &voice.ID
	d.Timeline = &schema.AudioTimeline{Tracks: []schema.AudioLane{{ID: "lane", Name: "旁白", Clips: []schema.AudioClip{}}}}
	must(t, wb.Generate(d))
	done := wb.Done()
	<-done
	state := wb.Store.Read()
	if state.Activity == nil || state.Activity.Status != "completed" || len(state.History) != 1 {
		t.Fatalf("%+v", state.Activity)
	}
	if state.History[0].Settings.Timeline != nil {
		t.Fatal("历史快照不应携带时间轴")
	}
	if state.Drafts[0].ID != d.ID || state.Drafts[0].Timeline == nil {
		t.Fatal("作品时间轴应保留")
	}
}

// 音乐模型需要较新的内核：仅满足应用最低版本时拒绝生成并提示更新，升级后完成闭环。
func TestMusicGenerationRequiresEngineVersion(t *testing.T) {
	wb, err := New(t.TempDir())
	must(t, err)
	defer wb.Close()
	executable, err := os.Executable()
	must(t, err)
	model := filepath.Join(wb.Store.Root, "models", "ace-step.gguf")
	must(t, os.WriteFile(model, []byte("test"), 0600))
	install := func(version string) {
		must(t, wb.Store.Update(func(s *schema.State) {
			s.Models = []schema.InstalledModel{{ID: "ace-step-1.5-turbo-bf16", Path: model}}
			s.RuntimePath = &executable
			s.RuntimeBackend = ptr("cpu")
			s.RuntimeVersion = ptr(version)
			s.Preferences.Backend = "cpu"
		}, true))
	}
	d := schema.DefaultDraft()
	d.Kind, d.ModelID, d.Text, d.Lyrics = "music", "ace-step-1.5-turbo-bf16", "city pop, chill", "[verse]\n晚风"
	install(catalog.EngineMinimum)
	if catalog.EngineMinimum != "v0.9.1" {
		var ce *msg.CallError
		if err := wb.Generate(d); !errors.As(err, &ce) || ce.Code != msg.ErrEngineUpgradeRequired {
			t.Fatalf("旧内核应提示更新：%v", err)
		}
	}
	install(catalog.EngineVersion)
	must(t, wb.Generate(d))
	<-wb.Done()
	state := wb.Store.Read()
	if state.Activity == nil || state.Activity.Status != "completed" || len(state.History) != 1 || state.History[0].Settings.Lyrics != d.Lyrics || state.Drafts[0].Kind != "music" {
		t.Fatalf("%+v", state.Activity)
	}
}

// 编曲作品不需要推理内核：装好音色库即可渲染为一个版本，未装音色库时提示下载。
func TestScoreRendersWithoutEngine(t *testing.T) {
	wb, err := New(t.TempDir())
	must(t, err)
	defer wb.Close()
	d := schema.DefaultDraft()
	d.Kind, d.ModelID, d.Title = "score", "musescore-general-sf2", "片头"
	d.Score = &schema.Score{Tempo: 120, TimeSignature: []int{4, 4}, Tracks: []schema.ScoreTrack{{ID: "piano", Name: "钢琴", Role: "piano", Notes: []schema.ScoreNote{{Bar: 1, Beat: 1, Pitch: 62, Length: 2, Velocity: 90}}}}}
	var ce *msg.CallError
	if err := wb.Generate(d); !errors.As(err, &ce) || ce.Code != msg.ErrModelRequired {
		t.Fatalf("未装音色库应提示下载：%v", err)
	}
	font := filepath.Join(wb.Store.Root, "models", "test.sf2")
	must(t, os.WriteFile(font, testkit.SoundFont(), 0600))
	must(t, wb.Store.Update(func(s *schema.State) { s.Models = []schema.InstalledModel{{ID: "musescore-general-sf2", Path: font}} }, true))
	broken := d
	broken.Score = &schema.Score{Tempo: 120, TimeSignature: []int{4, 4}}
	if err := wb.Generate(broken); !errors.As(err, &ce) || ce.Code != msg.ErrScoreInvalid {
		t.Fatalf("空乐谱应校验失败：%v", err)
	}
	must(t, wb.Generate(d))
	<-wb.Done()
	state := wb.Store.Read()
	if state.Activity == nil || state.Activity.Status != "completed" || len(state.History) != 1 || state.History[0].Settings.Score == nil || state.Drafts[0].Kind != "score" {
		t.Fatalf("%+v", state.Activity)
	}
	if g := state.History[0]; g.Duration < 3.99 || g.Duration > 4.01 { // 一小节 2 秒加 2 秒收尾
		t.Fatal(g.Duration)
	}
}

// 歌曲中的编曲使用独立音色库渲染，历史版本和作品仍保留歌曲模型。
func TestMusicScoreKeepsSongSettings(t *testing.T) {
	wb, err := New(t.TempDir())
	must(t, err)
	defer wb.Close()
	d := schema.DefaultDraft()
	d.Kind, d.ModelID, d.SoundFontID, d.Title = "music", "ace-step-1.5-turbo-bf16", "musescore-general-sf2", "江南小曲"
	d.Text = "Jiangnan sizhu"
	d.Timeline = &schema.AudioTimeline{Tracks: []schema.AudioLane{}}
	d.Score = &schema.Score{Tempo: 120, TimeSignature: []int{4, 4}, Tracks: []schema.ScoreTrack{{ID: "dizi", Name: "笛子", Role: "melody", Notes: []schema.ScoreNote{{Bar: 1, Beat: 1, Pitch: 62, Length: 2, Velocity: 80}}}}}
	font := filepath.Join(wb.Store.Root, "models", "test.sf2")
	must(t, os.WriteFile(font, testkit.SoundFont(), 0600))
	must(t, wb.Store.Update(func(s *schema.State) { s.Models = []schema.InstalledModel{{ID: d.SoundFontID, Path: font}} }, true))
	must(t, wb.RenderMusicScore(d))
	<-wb.Done()
	state := wb.Store.Read()
	saved := slices.IndexFunc(state.Drafts, func(item schema.Draft) bool { return item.ID == d.ID })
	if saved < 0 || len(state.History) != 1 || state.Drafts[saved].Kind != "music" || state.Drafts[saved].ModelID != d.ModelID || state.Drafts[saved].Timeline == nil || state.History[0].Source != "score" || state.History[0].Settings.Kind != "music" || state.History[0].Settings.SoundFontID != d.SoundFontID || state.History[0].Settings.Timeline != nil {
		t.Fatalf("编曲渲染改变了歌曲设置：drafts=%+v history=%+v", state.Drafts, state.History)
	}
}

// 语音生成作品只保留最近 10 个版本；时间线使用的版本保留且不占名额，其他作品不受影响。
func TestTextGenerationKeepsLatestVersions(t *testing.T) {
	wb, err := New(t.TempDir())
	must(t, err)
	defer wb.Close()
	executable, err := os.Executable()
	must(t, err)
	model := filepath.Join(wb.Store.Root, "models", "fake.gguf")
	must(t, os.WriteFile(model, []byte("test"), 0600))
	reference := filepath.Join(t.TempDir(), "reference.wav")
	must(t, os.WriteFile(reference, wav(), 0600))
	voice, err := wb.ImportVoice(context.Background(), reference, "")
	must(t, err)
	d := schema.DefaultDraft()
	d.VoiceID = &voice.ID
	other := schema.DefaultDraft()
	other.ID = schema.NewID()
	var history []schema.Generation
	for i := range 11 {
		g := schema.Generation{ID: schema.NewID(), Title: "旧版本", FileName: schema.NewID() + ".wav", CreatedAt: time.Now().Add(-time.Duration(i+1) * time.Minute).UTC(), Duration: 1, Settings: d}
		must(t, os.WriteFile(filepath.Join(wb.Store.Root, "outputs", g.FileName), wav(), 0600))
		history = append(history, g)
	}
	foreign := schema.Generation{ID: schema.NewID(), Title: "其他作品", FileName: schema.NewID() + ".wav", CreatedAt: time.Now().Add(-time.Hour).UTC(), Duration: 1, Settings: other}
	must(t, os.WriteFile(filepath.Join(wb.Store.Root, "outputs", foreign.FileName), wav(), 0600))
	// 最旧的一条放在时间线上，不能被删除。
	used := history[10]
	d.Timeline = &schema.AudioTimeline{Tracks: []schema.AudioLane{{ID: "lane", Name: "旁白", Clips: []schema.AudioClip{{ID: "clip", GenerationID: used.ID, Duration: 1}}}}}
	must(t, wb.Store.Update(func(s *schema.State) {
		s.Models = []schema.InstalledModel{{ID: "index-2.5-q8", Path: model}}
		s.RuntimePath = &executable
		s.RuntimeBackend = ptr("cpu")
		s.RuntimeVersion = ptr(catalog.EngineVersion)
		s.Preferences.Backend = "cpu"
		s.Drafts = []schema.Draft{d, other}
		s.History = append(history, foreign)
	}, true))
	must(t, wb.Generate(d))
	<-wb.Done()
	state := wb.Store.Read()
	if state.Activity == nil || state.Activity.Status != "completed" {
		t.Fatalf("%+v", state.Activity)
	}
	ids := map[string]bool{}
	for _, g := range state.History {
		ids[g.ID] = true
	}
	// 新版本 + 9 个最新旧版本 + 时间线版本 + 其他作品 = 12；第 10、11 新的旧版本（history[9]）被删除。
	if len(state.History) != 12 || ids[history[9].ID] || !ids[history[8].ID] || !ids[used.ID] || !ids[foreign.ID] {
		t.Fatalf("history = %d", len(state.History))
	}
	if _, err := os.Stat(filepath.Join(wb.Store.Root, "outputs", history[9].FileName)); !os.IsNotExist(err) {
		t.Fatal("超出上限的旧版本音频应删除")
	}
}
