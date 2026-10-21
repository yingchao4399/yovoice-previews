package score

import (
	"math"
	"testing"
	"yovoice/internal/schema"
)

func TestSampleABCMatchesRequestedNoteAndDuration(t *testing.T) {
	for _, seconds := range []float64{.05, .1, .25, .5, 1.153846153846, 1.730769230769, 3, 9, 9.230769230769, 30} {
		text, err := SampleABC(81, 70, seconds, "Dm", "箫")
		if err != nil {
			t.Fatalf("%.6fs: %v", seconds, err)
		}
		parsed, err := ParseABC(text)
		if err != nil {
			t.Fatal(err)
		}
		if math.Abs(parsed.Seconds()-seconds) > .02 || len(parsed.Ins().Notes) != 1 || parsed.Ins().Notes[0].Pitch != 81 || parsed.Ins().Notes[0].Start != 0 || len(parsed.Vocal().Notes) != 0 {
			t.Fatalf("条件失真 %.6fs: %s", seconds, text)
		}
		if parsed.Ins().Notes[0].Length != parsed.Ins().Bars[len(parsed.Ins().Bars)-1].Start+parsed.Ins().Bars[len(parsed.Ins().Bars)-1].Length {
			t.Fatal("单音之后出现了补齐小节的休止", text)
		}
	}
}

func TestPhraseABCRejectsTempoChangesRatherThanDroppingThem(t *testing.T) {
	track := schema.ScoreTrack{ID: "solo", Name: "箫", Notes: []schema.ScoreNote{{Bar: 1, Beat: 1, Pitch: 62, Length: 1, Velocity: 80}}}
	music := schema.Score{Tempo: 80, TimeSignature: []int{4, 4}, Tracks: []schema.ScoreTrack{track}, Sections: []schema.ScoreSection{{Name: "渐慢", Start: 2, End: 2, Tempo: 60}}}
	if _, err := PhraseABC(music, track, 1, 2); err == nil {
		t.Fatal("不得静默丢失句内变速")
	}
}
