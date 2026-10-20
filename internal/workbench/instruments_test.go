package workbench

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"yovoice/internal/schema"
	"yovoice/internal/score"
)

func TestInstrumentImportPersistsAndProjectKeepsSource(t *testing.T) {
	root := t.TempDir()
	w, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	left, right := make([]float32, 4410), make([]float32, 4410)
	for i := range left {
		left[i], right[i] = .2, .2
	}
	wav := filepath.Join(root, "input.wav")
	if err := score.WriteWAV(wav, left, right); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(wav)
	if err != nil {
		t.Fatal(err)
	}
	in := InstrumentSampleInput{Name: "测试琵琶", Instrument: "琵琶", Origin: "real", RootPitch: 60, Articulation: "normal", VelocityLow: 1, VelocityHigh: 127}
	source, err := w.ImportInstrumentSampleFrom(context.Background(), bytes.NewReader(b), in)
	if err != nil {
		t.Fatal(err)
	}
	in.SourceID, in.RootPitch, in.Articulation = source.ID, 64, "tremolo"
	if _, err = w.ImportInstrumentSampleFrom(context.Background(), bytes.NewReader(b), in); err != nil {
		t.Fatal(err)
	}
	w.Close()
	w, err = New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	state := w.Store.Read()
	if len(state.InstrumentSources) != 1 || len(state.InstrumentSources[0].Samples) != 2 {
		t.Fatalf("音源未持久化: %+v", state.InstrumentSources)
	}
	d := schema.DefaultDraft()
	d.ID = schema.NewID()
	d.Kind = "score"
	d.Title = "采样工程"
	d.Score = &schema.Score{Tempo: 120, TimeSignature: []int{4, 4}, Tracks: []schema.ScoreTrack{{ID: "lead", Name: "琵琶", Program: 24, SourceID: source.ID, Notes: []schema.ScoreNote{{Bar: 1, Beat: 1, Pitch: 60, Length: 1, Velocity: 100}}}}}
	if err = w.Store.Update(func(s *schema.State) { s.Drafts = append(s.Drafts, d) }, true); err != nil {
		t.Fatal(err)
	}
	pack := filepath.Join(root, "source.yovoice")
	if err = w.ExportProject(d.ID, pack); err != nil {
		t.Fatal(err)
	}
	imported, err := w.ImportProject(pack)
	if err != nil {
		t.Fatal(err)
	}
	if imported.Score == nil || imported.Score.Tracks[0].SourceID == source.ID {
		t.Fatalf("导入未重映射音源: %+v", imported.Score)
	}
	if _, err = w.scoreSources(*imported.Score); err != nil {
		t.Fatal(err)
	}
}
