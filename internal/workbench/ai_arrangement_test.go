package workbench

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"yovoice/internal/schema"
)

func TestArrangementResultChecksDurationAndSourcePolicy(t *testing.T) {
	plan := schema.ArrangementPlan{Title: "山间听弦", Tempo: 80, TimeSignature: []int{4, 4}, Seconds: 90, Sections: []schema.ArrangementSection{
		{Name: "全曲", StartBar: 1, EndBar: 30, Phrases: []schema.ArrangementPhrase{
			{Name: "前奏", StartBar: 1, EndBar: 15, Parts: []schema.ArrangementPart{{Instrument: "箫", Role: "melody", Source: "real"}}},
			{Name: "独奏", StartBar: 16, EndBar: 30, Parts: []schema.ArrangementPart{{Instrument: "琵琶", Role: "melody", Technique: "轮指", Source: "ai"}}},
		}},
	}}
	in := ArrangementBrief{Brief: "民乐", Seconds: 90, Source: "hybrid"}
	if issue := arrangementResultIssue(in, &plan); issue != "" {
		t.Fatalf("valid hybrid plan rejected: %s", issue)
	}
	in.Source = "real"
	if issue := arrangementResultIssue(in, &plan); issue == "" {
		t.Fatal("real-only policy accepted AI sound")
	}
	in.Source = "hybrid"
	in.Seconds = 60
	if issue := arrangementResultIssue(in, &plan); issue == "" {
		t.Fatal("wrong requested duration accepted")
	}
	plan.Sections[0].Phrases[1].Parts[0].Source = "auto"
	in.Seconds = 90
	if issue := arrangementResultIssue(in, &plan); issue == "" {
		t.Fatal("unresolved source accepted")
	}
}

func TestComposeScoreRepairsMissingPlannedInstrument(t *testing.T) {
	bad := `{"title":"测试","tempo":120,"timeSignature":[4,4],"tracks":[{"id":"piano","name":"钢琴","role":"piano","program":0,"notes":[[1,1,60,1,80]]}]}`
	good := `{"title":"测试","tempo":120,"timeSignature":[4,4],"tracks":[{"id":"erhu","name":"二胡","role":"melody","program":40,"notes":[[1,1,62,1,80]]}]}`
	server, requests := fakeLLM(t, bad, good)
	w := aiWorkbench(t, server)
	_, err := w.AIModels(context.Background(), "")
	must(t, err)
	plan := &schema.ArrangementPlan{Title: "测试", Approved: true, Tempo: 120, TimeSignature: []int{4, 4}, Seconds: 2,
		Sections: []schema.ArrangementSection{{Name: "A", StartBar: 1, EndBar: 1, Phrases: []schema.ArrangementPhrase{{Name: "第一句", StartBar: 1, EndBar: 1,
			Parts: []schema.ArrangementPart{{Instrument: "二胡", Role: "melody", Source: "real"}}}}}}}
	result, err := w.ComposeScore(context.Background(), ScoreBrief{Brief: "二胡独奏", Seconds: 2, Arrangement: plan})
	must(t, err)
	if len(*requests) != 2 || result.Tracks[0].Name != "二胡" {
		t.Fatalf("写谱未按计划修复缺失声部：requests=%d score=%+v", len(*requests), result.Score)
	}
}

func TestComposeScoreRequiresAndPreservesPlannedPartIDs(t *testing.T) {
	id := schema.NewID()
	bad := `{"title":"测试","tempo":120,"timeSignature":[4,4],"tracks":[{"id":"erhu","name":"二胡","role":"melody","program":40,"notes":[[1,1,62,1,80]]}]}`
	good := strings.Replace(bad, `"id":"erhu"`, `"id":"erhu","partIds":["`+id+`"]`, 1)
	server, requests := fakeLLM(t, bad, good)
	w := aiWorkbench(t, server)
	_, err := w.AIModels(context.Background(), "")
	must(t, err)
	plan := &schema.ArrangementPlan{Title: "测试", Approved: true, Tempo: 120, TimeSignature: []int{4, 4}, Seconds: 2,
		Sections: []schema.ArrangementSection{{Name: "A", StartBar: 1, EndBar: 1, Phrases: []schema.ArrangementPhrase{{Name: "第一句", StartBar: 1, EndBar: 1,
			Parts: []schema.ArrangementPart{{ID: id, Instrument: "二胡", Role: "melody", Source: "real"}}}}}}}
	result, err := w.ComposeScore(context.Background(), ScoreBrief{Brief: "二胡独奏", Seconds: 2, Arrangement: plan})
	must(t, err)
	if len(*requests) != 2 || len(result.Tracks[0].PartIDs) != 1 || result.Tracks[0].PartIDs[0] != id {
		t.Fatalf("写谱必须修复并保留计划身份：requests=%d score=%+v", len(*requests), result.Score)
	}
}

func TestPlanArrangementRepairsMissingPhraseAndKeepsSourceChoices(t *testing.T) {
	bad := `{"title":"山间听弦","tempo":80,"timeSignature":[4,4],"seconds":90,"sections":[{"name":"全曲","startBar":1,"endBar":30,"phrases":[{"name":"前奏","startBar":1,"endBar":14,"parts":[{"instrument":"箫","role":"melody","source":"real"}]}]}]}`
	good := `{"title":"山间听弦","tempo":80,"timeSignature":[4,4],"seconds":90,"sections":[{"name":"全曲","startBar":1,"endBar":30,"phrases":[{"name":"前奏","startBar":1,"endBar":14,"parts":[{"instrument":"箫","role":"melody","source":"real","reason":"真实气息"}]},{"name":"独奏","startBar":15,"endBar":30,"parts":[{"instrument":"琵琶","role":"melody","technique":"轮指","source":"ai","reason":"补齐奏法"}]}]}]}`
	server, requests := fakeLLM(t, bad, good)
	w := aiWorkbench(t, server)
	_, err := w.AIModels(context.Background(), "")
	must(t, err)
	plan, err := w.PlanArrangement(context.Background(), ArrangementBrief{Brief: "琵琶与箫民乐", Seconds: 90, Source: "hybrid", Locale: schema.UiLocaleZhCN})
	must(t, err)
	if len(*requests) != 2 || plan.Sections[0].Phrases[1].Parts[0].Technique != "轮指" || plan.Sections[0].Phrases[1].Parts[0].Source != "ai" {
		t.Fatalf("整曲修复或逐句音源决定不对：%+v", plan)
	}
	if !schema.ValidID(plan.Sections[0].ID) || !schema.ValidID(plan.Sections[0].Phrases[0].ID) || !schema.ValidID(plan.Sections[0].Phrases[1].Parts[0].ID) {
		t.Fatalf("规划成功后应生成稳定层级 ID：%+v", plan)
	}
	first := (*requests)[0]["messages"].([]any)[0].(map[string]any)["content"].(string)
	if !strings.Contains(first, "hybrid") || !strings.Contains(first, "90 seconds") {
		t.Fatal("提示缺少音源策略或时长", first)
	}
}

func TestArrangementChecksRequestedPipaSoloAndTechniques(t *testing.T) {
	plan := schema.ArrangementPlan{Title: "山间听弦", Tempo: 80, TimeSignature: []int{4, 4}, Seconds: 90, Sections: []schema.ArrangementSection{{
		Name: "全曲", StartBar: 1, EndBar: 30, Phrases: []schema.ArrangementPhrase{
			{Name: "合奏", StartBar: 1, EndBar: 14, Parts: []schema.ArrangementPart{{Instrument: "琵琶", Role: "melody", Technique: "挑弹", Source: "real"}, {Instrument: "箫", Role: "other", Source: "real"}}},
			{Name: "独奏", StartBar: 15, EndBar: 30, Parts: []schema.ArrangementPart{{Instrument: "琵琶", Role: "melody", Technique: "轮指", Source: "real"}}},
		},
	}}}
	in := ArrangementBrief{Brief: "90 秒中国民俗配乐，以琵琶和箫为主，有琵琶独奏，琵琶需要轮指和挑弹", Seconds: 90, Source: "real"}
	if issue := arrangementResultIssue(in, &plan); issue != "" {
		t.Fatalf("完整民乐方案被拒绝: %s", issue)
	}
	plan.Sections[0].Phrases[0].Parts[0].Technique = "拨弦"
	if issue := arrangementResultIssue(in, &plan); !strings.Contains(issue, "挑弹") {
		t.Fatalf("缺少挑弹未被发现: %s", issue)
	}
	plan.Sections[0].Phrases[0].Parts[0].Technique = "挑弹"
	plan.Sections[0].Phrases[1].Parts = append(plan.Sections[0].Phrases[1].Parts, schema.ArrangementPart{Instrument: "箫", Role: "other", Source: "real"})
	if issue := arrangementResultIssue(in, &plan); !strings.Contains(issue, "solo") {
		t.Fatalf("琵琶独奏被合奏替代却未发现: %s", issue)
	}
}

func TestArrangementKeepsRequestedViolinAcrossLanguageAliases(t *testing.T) {
	plan := schema.ArrangementPlan{Title: "小提琴", Tempo: 80, TimeSignature: []int{4, 4}, Seconds: 90, Sections: []schema.ArrangementSection{{
		Name: "全曲", StartBar: 1, EndBar: 30, Phrases: []schema.ArrangementPhrase{{
			Name: "独奏", StartBar: 1, EndBar: 30, Parts: []schema.ArrangementPart{{Instrument: "violin", Role: "melody", Source: "real"}},
		}},
	}}}
	in := ArrangementBrief{Brief: "给我一首小提琴独奏", Seconds: 90, Source: "real"}
	if issue := arrangementResultIssue(in, &plan); issue != "" {
		t.Fatalf("violin 应被识别为用户要求的小提琴: %s", issue)
	}
	plan.Sections[0].Phrases[0].Parts[0].Instrument = "钢琴"
	if issue := arrangementResultIssue(in, &plan); !strings.Contains(issue, "小提琴") {
		t.Fatalf("用户指定小提琴时不应允许改成其他音源: %s", issue)
	}
}

func TestPlanArrangementBindsDownloadedViolinInsteadOfSearchingOnline(t *testing.T) {
	reply := `{"title":"小提琴独奏","tempo":80,"timeSignature":[4,4],"seconds":90,"sections":[{"name":"全曲","startBar":1,"endBar":30,"phrases":[{"name":"独奏","startBar":1,"endBar":30,"parts":[{"instrument":"violin","role":"melody","source":"real"}]}]}]}`
	server, requests := fakeLLM(t, reply)
	w := aiWorkbench(t, server)
	_, err := w.AIModels(context.Background(), "")
	must(t, err)
	id := schema.NewID()
	file := "sample-" + id + ".wav"
	must(t, os.WriteFile(filepath.Join(w.Store.Root, "instruments", file), []byte("sample"), 0600))
	must(t, w.Store.Update(func(s *schema.State) {
		s.InstrumentSources = append(s.InstrumentSources, schema.InstrumentSource{ID: id, Name: "弦乐 · 小提琴组", Instrument: "弦乐 · 小提琴组", Origin: "real", Samples: []schema.InstrumentSample{{ID: schema.NewID(), FileName: file, SHA256: fixtureHash([]byte("sample")), Articulation: "sustain"}}})
	}, true))
	plan, err := w.PlanArrangement(context.Background(), ArrangementBrief{Brief: "给我一首小提琴独奏", Seconds: 90, Source: "real"})
	must(t, err)
	if got := plan.Sections[0].Phrases[0].Parts[0].SourceID; got != id {
		t.Fatalf("未优先绑定已下载的小提琴采样: got %q, want %q", got, id)
	}
	if len(*requests) != 1 {
		t.Fatalf("已下载音源不应触发修复或在线寻找，调用次数: %d", len(*requests))
	}
	system := (*requests)[0]["messages"].([]any)[0].(map[string]any)["content"].(string)
	if !strings.Contains(system, "弦乐 · 小提琴组") {
		t.Fatal("规划模型没有收到已安装音源清单")
	}
}

func TestRealOnlyPlanRepairsUnrequestedMissingInstrument(t *testing.T) {
	bad := `{"title":"竹笛独奏","tempo":80,"timeSignature":[4,4],"seconds":90,"sections":[{"name":"全曲","startBar":1,"endBar":30,"phrases":[{"name":"独奏","startBar":1,"endBar":30,"parts":[{"instrument":"violin","role":"melody","source":"real"}]}]}]}`
	good := strings.Replace(bad, "violin", "dizi", 1)
	server, requests := fakeLLM(t, bad, good)
	w := aiWorkbench(t, server)
	_, err := w.AIModels(context.Background(), "")
	must(t, err)
	id := schema.NewID()
	file := "sample-" + id + ".wav"
	must(t, os.WriteFile(filepath.Join(w.Store.Root, "instruments", file), []byte("sample"), 0600))
	must(t, w.Store.Update(func(s *schema.State) {
		s.InstrumentSources = []schema.InstrumentSource{{ID: id, Name: "笛子", Instrument: "笛子", Origin: "real", Samples: []schema.InstrumentSample{{ID: schema.NewID(), FileName: file, SHA256: fixtureHash([]byte("sample")), Articulation: "sustain"}}}}
	}, true))
	plan, err := w.PlanArrangement(context.Background(), ArrangementBrief{Brief: "竹笛独奏", Seconds: 90, Source: "real"})
	must(t, err)
	if len(*requests) != 2 || plan.Sections[0].Phrases[0].Parts[0].SourceID != id {
		t.Fatalf("未拒绝未请求且不在库中的小提琴并改用笛子：requests=%d plan=%+v", len(*requests), plan)
	}
}

func fixtureHash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
