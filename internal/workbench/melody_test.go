package workbench

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"yovoice/internal/catalog"
	"yovoice/internal/msg"
	"yovoice/internal/schema"
	"yovoice/internal/score"
	"yovoice/internal/testkit"
)

func yueWorkbench(t *testing.T) (*Workbench, schema.Draft) {
	wb, err := New(t.TempDir())
	must(t, err)
	t.Cleanup(wb.Close)
	executable, err := os.Executable()
	must(t, err)
	model := filepath.Join(wb.Store.Root, "models", "Yue2-3B-GGUF")
	must(t, os.MkdirAll(model, 0700))
	must(t, wb.Store.Update(func(s *schema.State) {
		s.Models = []schema.InstalledModel{{ID: "yue2-3b-q8", Path: model}}
		s.RuntimePath = &executable
		s.RuntimeBackend = ptr("cpu")
		s.RuntimeVersion = ptr(catalog.EngineVersion)
		s.Preferences.Backend = "cpu"
	}, true))
	d := schema.DefaultDraft()
	d.Kind, d.ModelID, d.Title, d.Text, d.Lyrics = "music", "yue2-3b-q8", "夏夜", "Mandarin, city pop", "[verse]\n晚风"
	return wb, d
}

// 先出谱：结果放在会话状态里等界面填入，作品里已有的谱不被覆盖。
func TestPlanMelody(t *testing.T) {
	wb, d := yueWorkbench(t)
	d.Abc = "旧谱"
	must(t, wb.Store.Update(func(s *schema.State) { s.Drafts = []schema.Draft{d} }, true))
	must(t, wb.PlanMelody(d))
	<-wb.Done()
	state := wb.Store.Read()
	if state.Activity.Status != "completed" || state.Activity.Task != "melody" || state.Melody == nil || state.Melody.Abc != testkit.ABC || state.Melody.ProjectID != d.ID || state.Melody.Source != "plan" {
		t.Fatalf("%+v %+v", state.Activity, state.Melody)
	}
	if len(state.History) != 0 || state.Drafts[0].Abc != "旧谱" {
		t.Fatal("出谱不产生版本，也不改写作品里的谱")
	}
	must(t, wb.DismissMelody(state.Melody.ID))
	if wb.Store.Read().Melody != nil {
		t.Fatal("取用后应清除")
	}
}

// 直接生成时引擎附带这次所用的谱，存进版本；按谱生成时版本保存的是作品里的谱。
func TestGenerationKeepsMelody(t *testing.T) {
	wb, d := yueWorkbench(t)
	must(t, wb.Generate(d))
	<-wb.Done()
	if h := wb.Store.Read().History; len(h) != 1 || h[0].Settings.Abc != testkit.ABC {
		t.Fatalf("%+v", h)
	}
	if wb.Store.Read().Drafts[0].Abc != "" {
		t.Fatal("作品本身的谱不被引擎结果改写")
	}
	// 作品没有谱时，YuE2 写的谱作为出谱结果交给界面填入。
	if m := wb.Store.Read().Melody; m == nil || m.ProjectID != d.ID || m.Abc != testkit.ABC || m.Source != "plan" {
		t.Fatalf("%+v", m)
	}
	must(t, wb.DismissMelody(wb.Store.Read().Melody.ID))
	d.Abc, _ = score.StripABCChords(testkit.ABC)
	must(t, wb.Generate(d))
	<-wb.Done()
	if h := wb.Store.Read().History; len(h) != 2 || h[0].Settings.Abc != d.Abc {
		t.Fatalf("%+v", h[0].Settings.Abc)
	}
	if wb.Store.Read().Melody != nil {
		t.Fatal("作品已有谱时不再提供出谱结果")
	}
	var ce *msg.CallError
	d.Abc = strings.Replace(testkit.ABC, "c16|", "c8|", 1)
	if err := wb.Generate(d); !errors.As(err, &ce) || ce.Code != msg.ErrMelodyInvalid || ce.Params["bar"] != 1 {
		t.Fatal(err)
	}
}

// YuE2 纯音乐：没谱时先出谱，再把人声旋律移到器乐声部生成，版本里存实际演奏的谱；歌词可以留空。
func TestInstrumentalGeneration(t *testing.T) {
	wb, d := yueWorkbench(t)
	d.Instrumental, d.Lyrics = true, ""
	must(t, wb.Generate(d))
	<-wb.Done()
	state := wb.Store.Read()
	if state.Activity.Status != "completed" || len(state.History) != 1 {
		t.Fatalf("%+v", state.Activity)
	}
	played, err := score.ParseABC(state.History[0].Settings.Abc)
	must(t, err)
	if len(played.Vocal().Notes) != 0 || len(played.Ins().Notes) == 0 || !played.HasChords() {
		t.Fatal(state.History[0].Settings.Abc)
	}
	// 已有谱时直接转谱生成，作品里的谱保持原样。
	d.Abc = testkit.ABC
	must(t, wb.Generate(d))
	<-wb.Done()
	if h := wb.Store.Read().History; len(h) != 2 || h[0].Settings.Abc != state.History[0].Settings.Abc || wb.Store.Read().Drafts[0].Abc != testkit.ABC {
		t.Fatalf("%+v", h[0].Settings.Abc)
	}
}

func TestCoverRequiresModel(t *testing.T) {
	wb, d := yueWorkbench(t)
	var ce *msg.CallError
	if err := wb.CoverMelody(d.ID, bytes.NewReader(wav())); !errors.As(err, &ce) || ce.Code != msg.ErrCoverModelRequired {
		t.Fatal(err)
	}
	if err := wb.CoverMelody("bad", bytes.NewReader(wav())); !errors.As(err, &ce) || ce.Code != msg.ErrDraftIDInvalid {
		t.Fatal(err)
	}
}

func TestInspectAndTransformMelody(t *testing.T) {
	in := InspectMelody(testkit.ABC)
	if in.Issue != nil || in.Tempo != 120 || in.Bars != 2 || in.Seconds != 4 || !in.Chords || in.Key != "C major" || len(in.Score.Tracks) != 3 {
		t.Fatalf("%+v", in)
	}
	if bad := InspectMelody(strings.Replace(testkit.ABC, "c16|", "c8|", 1)); bad.Issue == nil || bad.Issue.Bar != 1 || bad.Issue.Voice != "Vocal" || bad.Score != nil {
		t.Fatalf("%+v", bad)
	}
	out, err := TransformMelody(testkit.ABC, "tempo", 90)
	must(t, err)
	if !strings.Contains(out, "Q:1/4=90") {
		t.Fatal(out)
	}
	if out, err = TransformMelody(out, "stripChords", 0); err != nil || InspectMelody(out).Chords {
		t.Fatal(out, err)
	}
	if out, err = TransformMelody(testkit.ABC, "instrumental", 0); err != nil || len(InspectMelody(out).Score.Tracks) != 3 || !strings.Contains(out, `"C"z`) {
		t.Fatal(out, err)
	}
	if _, err = TransformMelody(out, "transpose", 0); err == nil {
		t.Fatal("未知操作应失败")
	}
}

// 助手经 update_work 写旋律谱：与界面导入同一套方言校验，出错时指出小节；只适用于音乐作品。
func TestAssistantWritesMelody(t *testing.T) {
	wb, d := yueWorkbench(t)
	must(t, wb.saveDraft(d))
	bad := strings.Replace(testkit.ABC, "c16|", "c8|", 1)
	if _, _, err := wb.updateWorkTool(context.Background(), toolReq, updateWorkInput{ID: d.ID, Abc: &bad}); err == nil || !strings.Contains(err.Error(), "invalid ABC") {
		t.Fatalf("非法谱应被拒绝：%v", err)
	}
	abc := testkit.ABC
	if _, _, err := wb.updateWorkTool(context.Background(), toolReq, updateWorkInput{ID: d.ID, Abc: &abc}); err != nil {
		t.Fatal(err)
	}
	saved, err := wb.assistantWork(d.ID)
	must(t, err)
	if saved.Abc != strings.TrimSpace(testkit.ABC)+"\n" {
		t.Fatalf("旋律谱未写入：%q", saved.Abc)
	}
	text := schema.DefaultDraft()
	must(t, wb.saveDraft(text))
	if _, _, err = wb.updateWorkTool(context.Background(), toolReq, updateWorkInput{ID: text.ID, Abc: &abc}); err == nil {
		t.Fatal("非音乐作品不应接受旋律谱")
	}
}

// 助手的旋律谱工具：出谱结果经 get_generation_status 读到，update_work 填入后不再等界面确认；
// 转谱直接改写作品；扒谱需要 SheetSage2；不是 YuE2 的作品不接受。
func TestAssistantMelodyTools(t *testing.T) {
	wb, d := yueWorkbench(t)
	must(t, wb.saveDraft(d))
	// 工具注册时会推导参数 schema，写法不合规会直接 panic。
	_ = wb.newAssistantServer()
	ctx := context.Background()
	_, _, err := wb.planMelodyTool(ctx, toolReq, workIDInput{ID: d.ID})
	must(t, err)
	<-wb.Done()
	_, status, err := wb.getGenerationStatusTool(ctx, toolReq, workIDInput{ID: d.ID})
	must(t, err)
	melody, _ := status.(map[string]any)["melody"].(map[string]any)
	if melody == nil || melody["abc"] != testkit.ABC || melody["source"] != "plan" || melody["bars"] != 2 || melody["vocalMelody"] != true {
		t.Fatalf("%+v", status)
	}
	abc := melody["abc"].(string)
	_, _, err = wb.updateWorkTool(ctx, toolReq, updateWorkInput{ID: d.ID, Abc: &abc})
	must(t, err)
	if wb.Store.Read().Melody != nil {
		t.Fatal("填入作品后应清除等待中的出谱结果")
	}
	_, out, err := wb.transformMelodyTool(ctx, toolReq, transformMelodyInput{ID: d.ID, Op: "instrumental"})
	must(t, err)
	summary := out.(map[string]any)
	if summary["melody"].(map[string]any)["vocalMelody"] != false || len(summary["melodyChanges"].(score.ABCChanges).Vocal) != 2 {
		t.Fatalf("%+v", summary)
	}
	saved, err := wb.assistantWork(d.ID)
	must(t, err)
	if parsed, e := score.ParseABC(saved.Abc); e != nil || len(parsed.Vocal().Notes) != 0 {
		t.Fatal(saved.Abc)
	}
	song := filepath.Join(t.TempDir(), "song.wav")
	must(t, os.WriteFile(song, wav(), 0600))
	var ce *msg.CallError
	if _, _, err = wb.coverMelodyTool(ctx, toolReq, coverMelodyInput{ID: d.ID, Path: song}); !errors.As(err, &ce) || ce.Code != msg.ErrCoverModelRequired {
		t.Fatal(err)
	}
	text := schema.DefaultDraft()
	must(t, wb.saveDraft(text))
	if _, _, err = wb.planMelodyTool(ctx, toolReq, workIDInput{ID: text.ID}); err == nil {
		t.Fatal("非 YuE2 作品不应出谱")
	}
}

// 试听旋律谱：用已装的音色库渲染，不进历史；只保留最近一次的试听文件；没有音色库时提示下载。
func TestListenMelody(t *testing.T) {
	wb, _ := yueWorkbench(t)
	var ce *msg.CallError
	if _, err := wb.ListenMelody(context.Background(), testkit.ABC); !errors.As(err, &ce) || ce.Code != msg.ErrMelodySoundFontRequired {
		t.Fatal(err)
	}
	font := filepath.Join(wb.Store.Root, "models", "test.sf2")
	must(t, os.WriteFile(font, testkit.SoundFont(), 0600))
	must(t, wb.Store.Update(func(s *schema.State) {
		s.Models = append(s.Models, schema.InstalledModel{ID: "generaluser-gs-sf2", Path: font})
	}, true))
	first, err := wb.ListenMelody(context.Background(), testkit.ABC)
	must(t, err)
	second, err := wb.ListenMelody(context.Background(), testkit.ABC)
	must(t, err)
	if _, err := os.Stat(filepath.Join(wb.Store.Root, "outputs", second)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(wb.Store.Root, "outputs", first)); !os.IsNotExist(err) {
		t.Fatal("旧的试听文件应删除", err)
	}
	if len(wb.Store.Read().History) != 0 {
		t.Fatal("试听不进历史")
	}
	if _, err := wb.ListenMelody(context.Background(), strings.Replace(testkit.ABC, "c16|", "c8|", 1)); !errors.As(err, &ce) || ce.Code != msg.ErrMelodyInvalid {
		t.Fatal(err)
	}
}

// 助手为按谱生成的 YuE2 作品发起生成前必须先有谱；自由发挥或用户明确不看谱时直接生成。
func TestAssistantGenerateNeedsScore(t *testing.T) {
	wb, d := yueWorkbench(t)
	d.ID = schema.NewID()
	must(t, wb.Store.Update(func(s *schema.State) { s.Drafts = []schema.Draft{d} }, true))
	if _, _, err := wb.generateWorkTool(context.Background(), nil, generateWorkInput{ID: d.ID}); err == nil || !strings.Contains(err.Error(), "plan_melody") {
		t.Fatal("没谱时应先出谱", err)
	}
	variant := func(change func(*schema.Draft)) schema.Draft { v := d; change(&v); return v }
	free := func(v *schema.Draft) { v.ModelOptions = map[string]map[string]any{"yue2": {"cot": "off"}} }
	if !needsScoreReview(variant(func(v *schema.Draft) { free(v); v.Instrumental = true })) {
		t.Fatal("纯音乐总是按谱")
	}
	if needsScoreReview(variant(free)) || needsScoreReview(variant(func(v *schema.Draft) { v.Abc = testkit.ABC })) || needsScoreReview(variant(func(v *schema.Draft) { v.ModelID = "ace-step-1.5-turbo-q8" })) {
		t.Fatal("自由发挥、已有谱或 ACE-Step 不需要先出谱")
	}
	_, _, err := wb.generateWorkTool(context.Background(), nil, generateWorkInput{ID: d.ID, WithoutScore: true})
	must(t, err)
	<-wb.Done()
}
