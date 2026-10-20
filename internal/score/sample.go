package score

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"math"
	"math/rand/v2"
	"os"
	"yovoice/internal/schema"
)

// SampleFile 指向应用托管的 44.1 kHz、16 位、立体声 WAV。
type SampleFile struct {
	Path         string
	RootPitch    int
	Articulation string
	VelocityLow  int
	VelocityHigh int
}

type SampleInstrument struct {
	Samples             []SampleFile
	DefaultArticulation string
}

type sampleAudio struct{ left, right []float32 }

// InspectSample 检查采样格式与有效信号，供导入流程拒绝静音素材。
func InspectSample(path string) (float64, error) {
	a, err := loadSample(path)
	if err != nil {
		return 0, err
	}
	peak := float64(0)
	for i := range a.left {
		peak = max(peak, math.Abs(float64(a.left[i])), math.Abs(float64(a.right[i])))
	}
	if peak < 0.003 {
		return 0, fmt.Errorf("采样几乎没有有效声音")
	}
	return peak, nil
}

// EstimateRootPitch 只接受起音后音高稳定的单音，避免把乐句或噪声映射到整条键盘。
func EstimateRootPitch(path string) (int, float64, error) {
	a, err := loadSample(path)
	if err != nil {
		return 0, 0, err
	}
	const windowSize = 4096
	if len(a.left) < windowSize+SampleRate/20 {
		return 0, 0, fmt.Errorf("采样太短，无法核验单音音高")
	}
	mono := make([]float32, len(a.left))
	for i := range mono {
		mono[i] = (a.left[i] + a.right[i]) * .5
	}
	starts := []int{SampleRate / 20, len(mono) / 3, len(mono) * 2 / 3}
	var pitches []float64
	confidence := 1.0
	for _, start := range starts {
		start = min(start, len(mono)-windowSize)
		pitch, quality := estimateWindowPitch(mono[start : start+windowSize])
		if quality < .82 {
			continue
		}
		pitches = append(pitches, pitch)
		confidence = min(confidence, quality)
	}
	if len(pitches) < 2 {
		return 0, 0, fmt.Errorf("未检测到稳定的单音音高")
	}
	for _, pitch := range pitches[1:] {
		if math.Abs(pitch-pitches[0]) > .65 {
			return 0, 0, fmt.Errorf("采样包含多个音高，不能作为单音音源")
		}
	}
	average := 0.0
	for _, pitch := range pitches {
		average += pitch
	}
	midi := int(math.Round(average / float64(len(pitches))))
	if midi < 24 || midi > 108 {
		return 0, 0, fmt.Errorf("采样音高超出可用音域")
	}
	return midi, confidence, nil
}

func estimateWindowPitch(samples []float32) (float64, float64) {
	const minLag = SampleRate / 1200
	const maxLag = SampleRate / 65
	var energy float64
	for _, sample := range samples {
		energy += float64(sample * sample)
	}
	if energy/float64(len(samples)) < .000025 {
		return 0, 0
	}
	var running float64
	bestLag, bestValue := 0, 1.0
	for lag := 1; lag <= maxLag; lag++ {
		var difference float64
		for i := 0; i < len(samples)-maxLag; i++ {
			delta := float64(samples[i] - samples[i+lag])
			difference += delta * delta
		}
		running += difference
		if lag < minLag || running == 0 {
			continue
		}
		value := difference * float64(lag) / running
		if bestLag == 0 && value >= .18 {
			continue
		}
		if value < bestValue {
			bestLag, bestValue = lag, value
		}
		if bestLag != 0 && lag > bestLag && value > bestValue+.02 {
			break
		}
	}
	if bestLag == 0 || bestValue > .18 {
		return 0, 0
	}
	frequency := float64(SampleRate) / float64(bestLag)
	return 69 + 12*math.Log2(frequency/440), 1 - bestValue
}

func loadSample(path string) (sampleAudio, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return sampleAudio{}, err
	}
	if len(b) < 44 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return sampleAudio{}, fmt.Errorf("采样不是 WAV")
	}
	var data []byte
	validFormat := false
	for pos := 12; pos+8 <= len(b); {
		size := int(binary.LittleEndian.Uint32(b[pos+4:]))
		end := pos + 8 + size
		if size < 0 || end > len(b) {
			return sampleAudio{}, fmt.Errorf("采样 WAV 已损坏")
		}
		switch string(b[pos : pos+4]) {
		case "fmt ":
			if size >= 16 {
				f := b[pos+8 : end]
				validFormat = binary.LittleEndian.Uint16(f) == 1 && binary.LittleEndian.Uint16(f[2:]) == 2 && binary.LittleEndian.Uint32(f[4:]) == SampleRate && binary.LittleEndian.Uint16(f[12:]) == 4 && binary.LittleEndian.Uint16(f[14:]) == 16
			}
		case "data":
			data = b[pos+8 : end]
		}
		pos = end + size%2
	}
	if !validFormat || len(data) == 0 || len(data)%4 != 0 {
		return sampleAudio{}, fmt.Errorf("采样需要 44.1 kHz、16 位立体声 WAV")
	}
	a := sampleAudio{left: make([]float32, len(data)/4), right: make([]float32, len(data)/4)}
	for i := range a.left {
		a.left[i] = float32(int16(binary.LittleEndian.Uint16(data[i*4:]))) / 32768
		a.right[i] = float32(int16(binary.LittleEndian.Uint16(data[i*4+2:]))) / 32768
	}
	return a, nil
}

func closestSample(files []SampleFile, note schema.ScoreNote, defaultArticulation string, occurrence int) (int, error) {
	index, distance := -1, 128
	articulation := note.Articulation
	if articulation == "" {
		articulation = defaultArticulation
	}
	if articulation == "" {
		articulation = "normal"
	}
	for i, file := range files {
		if file.Articulation != articulation || note.Velocity < file.VelocityLow || note.Velocity > file.VelocityHigh {
			continue
		}
		delta := int(math.Abs(float64(file.RootPitch - note.Pitch)))
		if delta < distance {
			index, distance = i, delta
		}
	}
	if index < 0 {
		return -1, fmt.Errorf("音符 %d 缺少奏法 %s、力度 %d 的采样", note.Pitch, articulation, note.Velocity)
	}
	if distance > 7 {
		return -1, fmt.Errorf("音符 %d 距离最近的 %s 采样超过 7 个半音", note.Pitch, articulation)
	}
	variants := []int{}
	for i, file := range files {
		if file.Articulation == articulation && note.Velocity >= file.VelocityLow && note.Velocity <= file.VelocityHigh && int(math.Abs(float64(file.RootPitch-note.Pitch))) == distance {
			variants = append(variants, i)
		}
	}
	if len(variants) > 1 {
		index = variants[occurrence%len(variants)]
	}
	return index, nil
}

func renderSampleTrack(ctx context.Context, s schema.Score, t schema.ScoreTrack, instrument SampleInstrument, frames int) ([]float32, []float32, error) {
	if len(instrument.Samples) == 0 {
		return nil, nil, fmt.Errorf("采样音源没有音符")
	}
	loaded := make([]sampleAudio, len(instrument.Samples))
	for i, file := range instrument.Samples {
		var err error
		loaded[i], err = loadSample(file.Path)
		if err != nil {
			return nil, nil, err
		}
	}
	left, right := make([]float32, frames), make([]float32, frames)
	h := fnv.New64a()
	_, _ = h.Write([]byte(t.ID))
	rng := rand.New(rand.NewPCG(h.Sum64(), 0x9e3779b97f4a7c15))
	per := s.BeatsPerBar()
	panL, panR := math.Min(1, 1-t.Pan), math.Min(1, 1+t.Pan)
	for noteIndex, note := range t.Notes {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		index, err := closestSample(instrument.Samples, note, instrument.DefaultArticulation, noteIndex)
		if err != nil {
			return nil, nil, fmt.Errorf("声部 %s: %w", t.Name, err)
		}
		file, sample := instrument.Samples[index], loaded[index]
		start := s.Seconds(note.Bar, note.Beat)
		position := float64(note.Bar-1)*per + note.Beat - 1 + note.Length
		bar := int(position/per) + 1
		end := s.Seconds(bar, position-float64(bar-1)*per+1)
		velocity := note.Velocity
		if t.Humanize != nil {
			if t.Humanize.Velocity > 0 {
				velocity += rng.IntN(2*t.Humanize.Velocity+1) - t.Humanize.Velocity
			}
			if t.Humanize.TimingMs > 0 {
				shift := (rng.Float64()*2 - 1) * t.Humanize.TimingMs / 1000
				start, end = math.Max(0, start+shift), math.Max(0, end+shift)
			}
		}
		velocity = max(1, min(127, velocity))
		gain := float32(float64(velocity) / 127)
		step := math.Pow(2, float64(note.Pitch-file.RootPitch)/12)
		startFrame := int(start * SampleRate)
		length := min(int((end-start)*SampleRate), frames-startFrame)
		if length <= 0 {
			continue
		}
		for j := 0; j < length; j++ {
			pos := float64(j) * step
			k := int(pos)
			if k+1 >= len(sample.left) {
				break
			}
			frac := float32(pos - float64(k))
			envelope := float32(1)
			if remaining := length - j; remaining < SampleRate/40 {
				envelope = float32(remaining) / float32(SampleRate/40)
			}
			expressionGain := float32(expression(s, t.Dynamics, float64(startFrame+j)/SampleRate))
			left[startFrame+j] += (sample.left[k]*(1-frac) + sample.left[k+1]*frac) * gain * envelope * expressionGain * float32(panL)
			right[startFrame+j] += (sample.right[k]*(1-frac) + sample.right[k+1]*frac) * gain * envelope * expressionGain * float32(panR)
		}
	}
	return left, right, nil
}

// NormalizeStemFile 对外部插件分轨使用与采样渲染器相同的活动电平目标和峰值上限。
func NormalizeStemFile(path string, track schema.ScoreTrack) error {
	a, err := loadSample(path)
	if err != nil {
		return err
	}
	if level := activeLevel(a.left, a.right); level > -90 {
		gain := float32(math.Pow(10, (track.TargetLevel()-level)/20))
		for i := range a.left {
			a.left[i] *= gain
			a.right[i] *= gain
		}
	}
	limit(a.left, a.right)
	return WriteWAV(path, a.left, a.right)
}
