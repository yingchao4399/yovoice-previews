package audio

import (
	"math"
	"path/filepath"
	"testing"
)

// 写出再读回保持采样（16 位量化误差内），重采样后长度和正弦频率不变。
func TestPCMRoundTrip(t *testing.T) {
	samples := make([]float32, 44100)
	for i := range samples {
		samples[i] = float32(0.5 * math.Sin(2*math.Pi*440*float64(i)/44100))
	}
	path := filepath.Join(t.TempDir(), "a.wav")
	if e := WriteMono(path, samples, 44100); e != nil {
		t.Fatal(e)
	}
	back, rate, e := ReadMono(path)
	if e != nil || rate != 44100 || len(back) != len(samples) {
		t.Fatal(e, rate, len(back))
	}
	for i := range back {
		if math.Abs(float64(back[i]-samples[i])) > 1e-4 {
			t.Fatalf("%d %v %v", i, back[i], samples[i])
		}
	}
	down := Resample(back, 44100, 16000)
	if len(down) != 16000 {
		t.Fatal(len(down))
	}
	// 440 Hz 正弦一秒过零约 880 次。
	crossings := 0
	for i := 1; i < len(down); i++ {
		if (down[i-1] < 0) != (down[i] < 0) {
			crossings++
		}
	}
	if crossings < 870 || crossings > 890 {
		t.Fatal(crossings)
	}
}
