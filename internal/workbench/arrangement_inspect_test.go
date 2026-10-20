package workbench

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"yovoice/internal/schema"
)

func inspectionSource(t *testing.T, w *Workbench, instrument string, pitch int) schema.InstrumentSource {
	t.Helper()
	id := schema.NewID()
	data := []byte("recorded sample fixture")
	file := "sample-" + id + ".wav"
	must(t, os.WriteFile(filepath.Join(w.Store.Root, "instruments", file), data, 0600))
	h := sha256.Sum256(data)
	return schema.InstrumentSource{ID: id, Name: instrument, Instrument: instrument, Origin: "real", DefaultArticulation: "sustain", Samples: []schema.InstrumentSample{{ID: schema.NewID(), FileName: file, SHA256: hex.EncodeToString(h[:]), RootPitch: pitch, Articulation: "sustain", VelocityLow: 1, VelocityHigh: 127}}}
}

func TestInspectWrongViolinBindingIsNotPitchError(t *testing.T) {
	w, e := New(t.TempDir())
	must(t, e)
	defer w.Close()
	d := workflowDraft()
	d.Arrangement.Sections[0].Phrases = d.Arrangement.Sections[0].Phrases[:1]
	d.Arrangement.Sections[0].Phrases[0].EndBar = 2
	d.Score.Tracks = d.Score.Tracks[:1]
	d.Score.Tracks[0].Name = "小提琴"
	d.Score.Tracks[0].Notes[0].Pitch = 86
	p := &d.Arrangement.Sections[0].Phrases[0].Parts[0]
	p.ID = schema.NewID()
	p.Instrument = "小提琴"
	d.Score.Tracks[0].PartIDs = []string{p.ID}
	bass := inspectionSource(t, w, "低音提琴", 54)
	violin := inspectionSource(t, w, "弦乐 · 小提琴组", 86)
	p.SourceID = bass.ID
	must(t, w.Store.Update(func(s *schema.State) { s.InstrumentSources = []schema.InstrumentSource{bass, violin} }, true))
	before, _ := json.Marshal(d)
	r, e := w.InspectArrangement(ArrangementInspectInput{Draft: d})
	must(t, e)
	if r.Ready || len(r.Issues) != 1 || r.Issues[0].Code != "SOURCE_INSTRUMENT_MISMATCH" || !strings.Contains(r.Issues[0].Message, "低音提琴") {
		t.Fatalf("错误绑定应优先报告乐器不符：%+v", r)
	}
	if len(r.Parts[0].Candidates) != 1 || r.Parts[0].Candidates[0].ID != violin.ID || !r.Parts[0].Candidates[0].Ensemble {
		t.Fatalf("没有匹配合奏采样候选：%+v", r.Parts)
	}
	fixed, e := w.BindArrangementSource(ArrangementBindInput{Draft: d, PartID: p.ID, SourceID: violin.ID, ExpectedRevision: r.Revision})
	must(t, e)
	r, e = w.InspectArrangement(ArrangementInspectInput{Draft: fixed, Stage: "render"})
	must(t, e)
	if !r.Ready || len(r.Score.Tracks) != 1 || r.Score.Tracks[0].Notes[0].Pitch != 86 {
		t.Fatalf("仅换源后原音符应可奏：%+v", r)
	}
	after, _ := json.Marshal(d)
	if string(before) != string(after) {
		t.Fatal("只读预检或返回副本的绑定修改了输入")
	}
	undone, e := w.UndoArrangementBinding(fixed)
	must(t, e)
	if undone.Arrangement.Sections[0].Phrases[0].Parts[0].SourceID != bass.ID || undone.Score.Tracks[0].Notes[0].Pitch != 86 {
		t.Fatal("撤销没有保留原曲谱和绑定")
	}
	fixed.Score.Tracks[0].Notes[0].Pitch--
	if _, e = w.UndoArrangementBinding(fixed); e == nil {
		t.Fatal("撤销不得覆盖后续改谱")
	}
	p.Locked = true
	if _, e = w.BindArrangementSource(ArrangementBindInput{Draft: d, PartID: p.ID, SourceID: violin.ID}); e == nil {
		t.Fatal("锁定声部不能被自动改绑")
	}
}

func TestSampleCoverageSeparatesTechniqueVelocityAndPitch(t *testing.T) {
	s := schema.InstrumentSource{DefaultArticulation: "sustain", Samples: []schema.InstrumentSample{{RootPitch: 60, Articulation: "sustain", VelocityLow: 1, VelocityHigh: 63}}}
	p := schema.ArrangementPart{}
	n := schema.ScoreNote{Pitch: 60, Velocity: 50}
	for _, tc := range []struct {
		name string
		note schema.ScoreNote
		want string
	}{
		{"有效", n, ""}, {"边界", schema.ScoreNote{Pitch: 67, Velocity: 50}, ""}, {"超范围", schema.ScoreNote{Pitch: 68, Velocity: 50}, "PITCH_OUT_OF_RANGE"},
		{"力度", schema.ScoreNote{Pitch: 60, Velocity: 100}, "VELOCITY_LAYER_UNAVAILABLE"}, {"奏法", schema.ScoreNote{Pitch: 60, Velocity: 50, Articulation: "tremolo"}, "ARTICULATION_UNAVAILABLE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := sampleNoteIssue(p, tc.note, s); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}

func TestGenericGenerationCannotBypassArrangementSources(t *testing.T) {
	w, e := New(t.TempDir())
	must(t, e)
	defer w.Close()
	if e = w.Generate(workflowDraft()); e == nil {
		t.Fatal("智能编曲不能绕过预检退回 SF2")
	}
}
func TestLockedPlanRetainsExactSourceSelection(t *testing.T) {
	old := workflowDraft().Arrangement
	old.Sections[0].Phrases[0].Parts[0].Locked = true
	old.Sections[0].Phrases[0].Parts[0].SourceID = "pinned"
	next := workflowDraft().Arrangement
	if issue := preserveLockedArrangementParts(old, next); issue != "" {
		t.Fatal(issue)
	}
	p := next.Sections[0].Phrases[0].Parts[0]
	if p.SourceID != "pinned" || !p.Locked {
		t.Fatal("锁定音源被重新规划覆盖")
	}
	next.Sections[0].Phrases[0].EndBar = 2
	if preserveLockedArrangementParts(old, next) == "" {
		t.Fatal("锁定句子不能被静默移位")
	}
}

func TestOnlineMetadataSourceRequiresAuditionBeforeArrangement(t *testing.T) {
	w, e := New(t.TempDir())
	must(t, e)
	defer w.Close()
	d := workflowDraft()
	d.Arrangement.Sections[0].Phrases = d.Arrangement.Sections[0].Phrases[:1]
	d.Arrangement.Sections[0].Phrases[0].EndBar = 2
	d.Score.Tracks = d.Score.Tracks[:1]
	source := inspectionSource(t, w, "二胡", 62)
	source.Verification = "metadata"
	d.Arrangement.Sections[0].Phrases[0].Parts[0].SourceID = source.ID
	must(t, w.Store.Update(func(s *schema.State) { s.InstrumentSources = []schema.InstrumentSource{source} }, true))
	r, e := w.InspectArrangement(ArrangementInspectInput{Draft: d, Stage: "render"})
	must(t, e)
	if r.Ready || len(r.Issues) == 0 || r.Issues[0].Code != "SOURCE_REVIEW_REQUIRED" {
		t.Fatalf("元数据核验不能冒充乐器试听确认：%+v", r.Issues)
	}
	if len(w.availableRealArrangementSources()) != 0 {
		t.Fatal("规划不应把待试听来源列为就绪库存")
	}
}

func TestLegacySourceBindingMigratesOnlyUnambiguousPart(t *testing.T) {
	w, e := New(t.TempDir())
	must(t, e)
	defer w.Close()
	d := workflowDraft()
	source := inspectionSource(t, w, "二胡", 62)
	must(t, w.Store.Update(func(s *schema.State) { s.InstrumentSources = []schema.InstrumentSource{source} }, true))
	fixed, e := w.BindArrangementSource(ArrangementBindInput{Draft: d, Ref: &ArrangementPartRef{0, 0, 0}, SourceID: source.ID})
	must(t, e)
	if fixed.Arrangement.Sections[0].Phrases[0].Parts[0].ID == "" || fixed.Arrangement.Sections[0].Phrases[0].Parts[0].SourceID != source.ID || fixed.Score.Tracks[0].Notes[0].Pitch != 62 {
		t.Fatal("旧工程绑定不应要求重写曲谱")
	}
}
