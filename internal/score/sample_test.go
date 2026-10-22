package score

import (
	"context"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"yovoice/internal/schema"
)

func TestEstimateRootPitchRejectsPhraseAndNoise(t *testing.T) {
	root := t.TempDir()
	write := func(name string, tone func(int) float32) string {
		t.Helper()
		path := filepath.Join(root, name)
		left := make([]float32, SampleRate)
		for i := range left {
			left[i] = tone(i)
		}
		if err := WriteWAV(path, left, left); err != nil {
			t.Fatal(err)
		}
		return path
	}
	sine := func(hz float64, i int) float32 { return float32(.3 * math.Sin(2*math.Pi*hz*float64(i)/SampleRate)) }
	path := write("single.wav", func(i int) float32 { return sine(261.6256, i) })
	pitch, quality, err := EstimateRootPitch(path)
	if err != nil || pitch != 60 || quality < .82 {
		t.Fatalf("中央 C 单音识别失败: %d %.2f %v", pitch, quality, err)
	}
	path = write("phrase.wav", func(i int) float32 {
		if i < SampleRate/2 {
			return sine(261.6256, i)
		}
		return sine(329.6276, i)
	})
	if _, _, err = EstimateRootPitch(path); err == nil {
		t.Fatal("多音乐句不应被当作单音")
	}
	rng := rand.New(rand.NewPCG(1, 2))
	path = write("noise.wav", func(int) float32 { return float32(rng.Float64()*.6 - .3) })
	if _, _, err = EstimateRootPitch(path); err == nil {
		t.Fatal("噪声不应被当作音源单音")
	}
}

func TestSampleRenderUsesRecordingAndRejectsMissingArticulation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "pluck.wav")
	left, right := make([]float32, SampleRate/4), make([]float32, SampleRate/4)
	for i := range left {
		left[i] = float32(0.3 * math.Sin(2*math.Pi*440*float64(i)/SampleRate))
		right[i] = left[i]
	}
	if err := WriteWAV(path, left, right); err != nil {
		t.Fatal(err)
	}
	s := schema.Score{Tempo: 120, TimeSignature: []int{4, 4}, Tracks: []schema.ScoreTrack{{ID: "lead", Name: "琵琶", Program: 24, SourceID: "pipa", Notes: []schema.ScoreNote{{Bar: 1, Beat: 1, Pitch: 60, Length: 1, Velocity: 100}}}}}
	source := map[string]SampleInstrument{"pipa": {Samples: []SampleFile{{Path: path, RootPitch: 60, Articulation: "normal", VelocityLow: 1, VelocityHigh: 127}}}}
	output := filepath.Join(root, "mix.wav")
	if err := RenderWithSources(context.Background(), nil, s, output, "", source, nil); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 50_000 {
		t.Fatalf("渲染输出过短: %d", len(b))
	}
	nonzero := false
	for _, v := range b[44:] {
		if v != 0 {
			nonzero = true
			break
		}
	}
	if !nonzero {
		t.Fatal("采样渲染为静音")
	}
	s.Tracks[0].Notes[0].Articulation = "tremolo"
	if err := RenderWithSources(context.Background(), nil, s, output, "", source, nil); err == nil || !strings.Contains(err.Error(), "tremolo") {
		t.Fatalf("缺少轮指采样时应报错，得到 %v", err)
	}
}

func TestGeneratedSampleChoosesLongVariantAndUsesReleaseRegion(t *testing.T) {
	root := t.TempDir()
	writeConstant := func(name string, seconds, sustain, value float64) string {
		t.Helper()
		path := filepath.Join(root, name)
		frames := int(seconds * SampleRate)
		left, right := make([]float32, frames), make([]float32, frames)
		for i := range left {
			v := float32(value)
			// Keep a small attack so the file also represents a real signal.
			if i < SampleRate/100 {
				v *= float32(i) / float32(SampleRate/100)
			}
			left[i], right[i] = v, v
		}
		if err := WriteWAV(path, left, right); err != nil {
			t.Fatal(err)
		}
		return path
	}
	short := writeConstant("short.wav", .5, .4, .2)
	long := writeConstant("long.wav", 1.2, 1, .7)
	s := schema.Score{Tempo: 60, TimeSignature: []int{4, 4}, Tracks: []schema.ScoreTrack{{ID: "lead", Name: "AI 乐器", SourceID: "ai", Notes: []schema.ScoreNote{{Bar: 1, Beat: 1, Pitch: 60, Length: .5, Velocity: 100}}}}}
	instrument := SampleInstrument{Samples: []SampleFile{
		{Path: short, RootPitch: 60, ExactPitch: true, Generated: true, SustainSeconds: .4, ReleaseSeconds: .1, Duration: .5, Articulation: "normal", VelocityLow: 1, VelocityHigh: 127},
		{Path: long, RootPitch: 60, ExactPitch: true, Generated: true, SustainSeconds: 1, ReleaseSeconds: .2, Duration: 1.2, Articulation: "normal", VelocityLow: 1, VelocityHigh: 127},
	}}
	left, _, err := renderSampleTrack(context.Background(), s, s.Tracks[0], instrument, SampleRate*3)
	if err != nil {
		t.Fatalf("应选用覆盖时长的长采样，而不是短采样: %v", err)
	}
	if got := left[int(.55*SampleRate)]; got < .35 {
		t.Fatalf("释放区没有从采样的释放段读取，得到 %.3f", got)
	}
}
