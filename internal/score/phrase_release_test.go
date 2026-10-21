package score

import (
	"math"
	"slices"
	"testing"
)

func TestPhraseReleasePreservesNotesAndFitsOnlyReservedTail(t *testing.T) {
	for _, core := range []float64{1.730769230769, 3, 9, 30} {
		original, err := SampleABC(81, 70, core, "Dm", "箫")
		if err != nil {
			t.Fatal(err)
		}
		for _, vocal := range []bool{false, true} {
			text := original
			if vocal {
				text, err = PhraseVocalABC(text)
				if err != nil {
					t.Fatal(err)
				}
			}
			before, _ := ParseABC(text)
			for _, tail := range []float64{0, .1, .5, 2, 5} {
				result, err := PhraseReleaseABC(text, tail)
				if err != nil {
					t.Fatal(err)
				}
				after, err := ParseABC(result)
				if err != nil {
					t.Fatal(err)
				}
				if !slices.Equal(before.Ins().Notes, after.Ins().Notes) || !slices.Equal(before.Vocal().Notes, after.Vocal().Notes) || after.Seconds()-before.Seconds() > tail+1e-6 {
					t.Fatalf("主体发生变化或预算超限 core=%g tail=%g", core, tail)
				}
				if tail >= .5 && math.Abs(after.Seconds()-before.Seconds()) < 1e-6 {
					t.Fatal("尾音未写入休止条件")
				}
			}
		}
	}
}
