package score

import (
	"strings"
	"testing"
	"yovoice/internal/schema"
)

func TestPhraseABCExactAndRejectsPolyphony(t *testing.T) {
	track := schema.ScoreTrack{ID: "solo", Name: "箫", Notes: []schema.ScoreNote{{Bar: 15, Beat: 1, Pitch: 86, Length: 1.5, Velocity: 100}, {Bar: 16, Beat: 2, Pitch: 77, Length: 2, Velocity: 80}}}
	music := schema.Score{Tempo: 90, TimeSignature: []int{4, 4}, Key: "D minor", Tracks: []schema.ScoreTrack{track}}
	text, err := PhraseABC(music, track, 15, 16)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseABC(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Ins().Notes) != 2 || parsed.Ins().Notes[0].Pitch != 86 || parsed.Ins().Notes[1].Start != 5*abcQuarter || !strings.Contains(text, "Q:1/4=90") {
		t.Fatalf("音符变化：%s", text)
	}
	track.Notes = append(track.Notes, schema.ScoreNote{Bar: 15, Beat: 1, Pitch: 60, Length: 1, Velocity: 80})
	if _, err = PhraseABC(music, track, 15, 16); err == nil {
		t.Fatal("不得静默丢弃复调音符")
	}
}
