package workbench

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"yovoice/internal/schema"
)

func TestSamplePlanningBoundsMatchPlaybackRules(t *testing.T) {
	for _, sample := range []schema.InstrumentSample{
		{RootPitch: 62, Articulation: "normal", VelocityLow: 43, VelocityHigh: 85},
		{RootPitch: 62, ExactPitch: true, Articulation: "normal", VelocityLow: 43, VelocityHigh: 85, SustainSeconds: 2},
		{RootPitch: 62, GenerationID: "legacy-generated", Articulation: "normal", VelocityLow: 43, VelocityHigh: 85, SustainSeconds: 3},
	} {
		source := schema.InstrumentSource{Samples: []schema.InstrumentSample{sample}}
		cap := arrangementSampleCapabilities(source)["normal"][0]
		for pitch := 0; pitch <= 127; pitch++ {
			for _, velocity := range []int{42, 43, 85, 86} {
				accepted := sampleNoteIssue(schema.ArrangementPart{}, schema.ScoreNote{Pitch: pitch, Velocity: velocity}, source) == ""
				advertised := pitch >= cap.Low && pitch <= cap.High && velocity >= cap.VelocityLow && velocity <= cap.VelocityHigh
				if accepted != advertised {
					t.Fatalf("提示与演奏规则不同：sample=%+v pitch=%d velocity=%d", sample, pitch, velocity)
				}
			}
		}
		if sample.ExactPitch || sample.GenerationID != "" {
			if cap.MaxSustainSeconds == nil || *cap.MaxSustainSeconds != sample.SustainSeconds {
				t.Fatal("丢失生成采样可用持续时长")
			}
		}
	}
}

func TestCompositionUsesPartModelAndGenerationMethod(t *testing.T) {
	chord := `{"tracks":[{"partIds":["p1"],"notes":[[1,1,62,1,80],[1,1,65,1,80]]}]}`
	line := `{"tracks":[{"partIds":["p1"],"notes":[[1,1,62,1,80],[1,2,65,1,80]]}]}`
	for _, tc := range []struct {
		name, global, override, method string
		calls                          int
		wantError                      bool
	}{
		{"声部YuE2覆盖整曲ACE并局部修复和弦", "ace-step-1.5-turbo-q8", "yue2-3b-q8", "direct", 2, false},
		{"采样器允许和弦且不套用YuE2单旋律限制", "yue2-3b-q8", "", "sampler", 1, false},
		{"声部ACE覆盖整曲YuE2并提前拒绝不支持的跟谱", "yue2-3b-q8", "ace-step-1.5-turbo-q8", "direct", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, requests := fakeLLM(t, chord, line)
			w := aiWorkbench(t, server)
			_, err := w.AIModels(context.Background(), "")
			must(t, err)
			d := workflowDraft()
			d.Arrangement.Sections[0].Phrases = d.Arrangement.Sections[0].Phrases[:1]
			d.Arrangement.Sections[0].EndBar = 1
			d.Score.Tracks = d.Score.Tracks[:1]
			d.Arrangement.AIModelID = tc.global
			part := &d.Arrangement.Sections[0].Phrases[0].Parts[0]
			part.Source = "ai"
			part.Generation = &schema.ArrangementGenerationSettings{AssetKind: "phrase", Mode: "score", Method: tc.method, LengthMode: "phrase", SoundType: "instrument", ModelID: tc.override}
			_, err = migrateLegacyArrangementIDs(&d)
			must(t, err)
			out, err := w.ComposeScore(context.Background(), ScoreBrief{ProjectID: d.ID, Brief: "按当前声部谱条件写谱", Seconds: 4, Arrangement: d.Arrangement})
			if (err != nil) != tc.wantError || len(*requests) != tc.calls {
				t.Fatalf("calls=%d err=%v", len(*requests), err)
			}
			if tc.wantError {
				return
			}
			if len(out.Tracks[0].Notes) != 2 {
				t.Fatal("丢失声部音符")
			}
			if tc.method == "direct" && out.Tracks[0].Notes[1].Beat != 2 {
				t.Fatal("没有在写谱阶段修复无法编码的重叠音符")
			}
			prompt, _ := json.Marshal((*requests)[0])
			if strings.Contains(string(prompt), "one monophonic line") != (tc.method == "direct") {
				t.Fatal("写谱提示忽略逐声部实现方式")
			}
		})
	}
}

func TestCompositionRepairsUnsupportedExactPitchAndSustain(t *testing.T) {
	wrongPitch := `{"tracks":[{"partIds":["p1"],"notes":[[1,1,63,1,80]]}]}`
	wrongLength := `{"tracks":[{"partIds":["p1"],"notes":[[1,1,62,2,80]]}]}`
	good := `{"tracks":[{"partIds":["p1"],"notes":[[1,1,62,1,80]]}]}`
	for _, bad := range []string{wrongPitch, wrongLength} {
		server, requests := fakeLLM(t, bad, good)
		w := aiWorkbench(t, server)
		_, err := w.AIModels(context.Background(), "")
		must(t, err)
		d := workflowDraft()
		d.Arrangement.Sections[0].Phrases = d.Arrangement.Sections[0].Phrases[:1]
		d.Arrangement.Sections[0].EndBar = 1
		d.Score.Tracks = d.Score.Tracks[:1]
		part := &d.Arrangement.Sections[0].Phrases[0].Parts[0]
		part.Source, part.SourceID = "ai", schema.NewID()
		file := "confirmed-ai.wav"
		must(t, os.WriteFile(filepath.Join(w.Store.Root, "instruments", file), []byte("sample"), 0600))
		must(t, w.Store.Update(func(s *schema.State) {
			s.InstrumentSources = []schema.InstrumentSource{{ID: part.SourceID, Name: "已确认二胡 D4", Instrument: part.Instrument, Origin: "ai", Verification: "user", Samples: []schema.InstrumentSample{{ID: schema.NewID(), FileName: file, SHA256: fixtureHash([]byte("sample")), RootPitch: 62, ExactPitch: true, SustainSeconds: .5, Duration: 1, Articulation: "normal", VelocityLow: 43, VelocityHigh: 85}}}}
		}, true))
		_, err = migrateLegacyArrangementIDs(&d)
		must(t, err)
		out, err := w.ComposeScore(context.Background(), ScoreBrief{ProjectID: d.ID, Brief: "使用已确认音色", Seconds: 4, Arrangement: d.Arrangement})
		must(t, err)
		if len(*requests) != 2 || out.Tracks[0].Notes[0].Pitch != 62 || out.Tracks[0].Notes[0].Length != 1 {
			t.Fatal("没有局部修复不适用的音符")
		}
		prompt, _ := json.Marshal((*requests)[0])
		if !strings.Contains(string(prompt), `\"low\":62,\"high\":62`) || !strings.Contains(string(prompt), `\"maxSustainSeconds\":0.5`) {
			t.Fatalf("未提供精确音高和时长：%s", prompt)
		}
	}
}

func TestPlanExposesVerifiedSampleCoverageWithoutRequiringAudioModel(t *testing.T) {
	reply := `{"title":"AI二胡","tempo":120,"timeSignature":[4,4],"seconds":32,"sections":[{"name":"全曲","startBar":1,"endBar":16,"phrases":[{"name":"主题","startBar":1,"endBar":16,"parts":[{"instrument":"二胡","role":"melody","source":"ai"}]}]}]}`
	server, requests := fakeLLM(t, reply)
	w := aiWorkbench(t, server)
	_, err := w.AIModels(context.Background(), "")
	must(t, err)
	file := "reviewed-erhu.wav"
	must(t, os.WriteFile(filepath.Join(w.Store.Root, "instruments", file), []byte("sample"), 0600))
	id := schema.NewID()
	must(t, w.Store.Update(func(s *schema.State) {
		s.InstrumentSources = []schema.InstrumentSource{{ID: id, Name: "已确认D4", Instrument: "二胡", Origin: "ai", Verification: "user", Samples: []schema.InstrumentSample{{ID: schema.NewID(), FileName: file, SHA256: fixtureHash([]byte("sample")), RootPitch: 62, ExactPitch: true, SustainSeconds: 2, Articulation: "normal", VelocityLow: 43, VelocityHigh: 85}}}}
	}, true))
	plan, err := w.PlanArrangement(context.Background(), ArrangementBrief{Brief: "二胡配乐", Seconds: 32, Source: "ai"})
	must(t, err)
	if plan.Sections[0].Phrases[0].Parts[0].SourceID != id {
		t.Fatal("未复用已确认本地AI音色")
	}
	system := (*requests)[0]["messages"].([]any)[0].(map[string]any)["content"].(string)
	if !strings.Contains(system, `"playableMidiLow":62,"playableMidiHigh":62`) || !strings.Contains(system, `"maxSustainSeconds":2`) {
		t.Fatalf("规划扩大了已验证音域或缺少时长：%s", system)
	}
}

func TestPlanningRepairsUnsupportedGenerationSettingsBeforeAcceptingPlan(t *testing.T) {
	bad := `{"title":"AI二胡","tempo":120,"timeSignature":[4,4],"seconds":32,"sections":[{"name":"全曲","startBar":1,"endBar":16,"phrases":[{"name":"主题","startBar":1,"endBar":16,"parts":[{"instrument":"二胡","role":"melody","source":"ai","generation":{"assetKind":"phrase","mode":"score","method":"direct","lengthMode":"phrase","soundType":"instrument","tailSeconds":0.5,"modelId":"ace-step-1.5-turbo-q8"}}]}]}]}`
	good := strings.Replace(bad, `"method":"direct"`, `"method":"sampler"`, 1)
	server, requests := fakeLLM(t, bad, good)
	w := aiWorkbench(t, server)
	_, err := w.AIModels(context.Background(), "")
	must(t, err)
	must(t, w.Store.Update(func(s *schema.State) {
		s.Models = []schema.InstalledModel{{ID: "ace-step-1.5-turbo-q8"}}
		s.RuntimeVersion = ptr("v0.9.1")
	}, true))
	plan, err := w.PlanArrangement(context.Background(), ArrangementBrief{Brief: "按谱演奏的二胡配乐", Seconds: 32, Source: "ai"})
	must(t, err)
	if len(*requests) != 2 || generationSettings(plan.Sections[0].Phrases[0].Parts[0]).Method != "sampler" {
		t.Fatal("不支持的模型跟谱设置未在规划阶段修复")
	}
	system := (*requests)[0]["messages"].([]any)[0].(map[string]any)["content"].(string)
	if !strings.Contains(system, `"scoreConditioning":false`) || !strings.Contains(system, `"minDuration":10`) || !strings.Contains(system, `"modelId":"ace-step-1.5-turbo-q8"`) {
		t.Fatal("规划没有收到实际适配器能力")
	}
}

func TestExpandableAISourceKeepsNewMelodyAndPreparesMissingCoverage(t *testing.T) {
	reply := `{"tracks":[{"partIds":["p1"],"notes":[[1,1,65,2,100]]}]}`
	server, requests := fakeLLM(t, reply)
	w := aiWorkbench(t, server)
	_, err := w.AIModels(context.Background(), "")
	must(t, err)
	d := workflowDraft()
	d.Arrangement.Sections[0].Phrases = d.Arrangement.Sections[0].Phrases[:1]
	d.Arrangement.Sections[0].EndBar = 1
	d.Arrangement.AIModelID = "yue2-3b-q8"
	d.Score.Tracks = d.Score.Tracks[:1]
	part := &d.Arrangement.Sections[0].Phrases[0].Parts[0]
	part.Source, part.SourceID = "ai", schema.NewID()
	file := "partial-ai.wav"
	must(t, os.WriteFile(filepath.Join(w.Store.Root, "instruments", file), []byte("sample"), 0600))
	must(t, w.Store.Update(func(s *schema.State) {
		s.RuntimeVersion = ptr("v0.9.1")
		s.InstrumentSources = []schema.InstrumentSource{{ID: part.SourceID, Name: "二胡部分音高", Instrument: part.Instrument, Origin: "ai", Verification: "user", Provenance: "generated:yue2-3b-q8", Samples: []schema.InstrumentSample{{ID: schema.NewID(), FileName: file, SHA256: fixtureHash([]byte("sample")), RootPitch: 62, ExactPitch: true, SustainSeconds: .5, Duration: 1, Articulation: "normal", VelocityLow: 43, VelocityHigh: 85}}}}
	}, true))
	_, err = migrateLegacyArrangementIDs(&d)
	must(t, err)
	out, err := w.ComposeScore(context.Background(), ScoreBrief{ProjectID: d.ID, Brief: "需要F4强力度长音，不改变已有音色", Seconds: 4, Arrangement: d.Arrangement})
	must(t, err)
	if len(*requests) != 1 || out.Tracks[0].Notes[0].Pitch != 65 || out.Tracks[0].Notes[0].Velocity != 100 {
		t.Fatal("音色部分覆盖不应强迫改写所需旋律")
	}
	d.Score = &out.Score
	ref := ArrangementPartRef{0, 0, 0}
	pre, err := w.ArrangementGenerationPreflight(ArrangementAIPrepareInput{Draft: d, Target: &ref})
	must(t, err)
	if len(pre.Issues) != 0 || pre.Reused != 0 || len(pre.Cells) != 1 || pre.Cells[0].Pitch != 65 || pre.Cells[0].State != "missing" || pre.Cells[0].SustainSeconds != 1 {
		t.Fatalf("未将新增音高、力度和长度登记为缺口：%+v", pre)
	}
	inspection, err := w.InspectArrangement(ArrangementInspectInput{Draft: d, Stage: "render"})
	must(t, err)
	if inspection.Ready {
		t.Fatal("缺口未生成和确认前不能拼合")
	}
}
