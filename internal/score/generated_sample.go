package score

import (
	"fmt"
	"math"
	"slices"
)

type GeneratedSampleMeasurement struct {
	Pitch          int     `json:"pitch"`
	Cents          float64 `json:"cents"`
	StableFraction float64 `json:"stableFraction"`
	ActiveSeconds  float64 `json:"activeSeconds"`
	Onsets         int     `json:"onsets"`
}

// MeasureGeneratedSample 检查候选的音高与多次起音，不推断乐器或是否有人声。
func MeasureGeneratedSample(path string) (GeneratedSampleMeasurement, error) {
	return MeasureGeneratedSampleCore(path, 0)
}

// 释放尾音可以衰减或滑落；根音核验只测音乐核心，不把余音误判为主体变调。
func MeasureGeneratedSampleCore(path string, coreSeconds float64) (GeneratedSampleMeasurement, error) {
	r := GeneratedSampleMeasurement{}
	a, err := loadSample(path)
	if err != nil {
		return r, err
	}
	if coreSeconds > 0 {
		frames := min(len(a.left), int(math.Round(coreSeconds*SampleRate)))
		a.left = a.left[:frames]
		a.right = a.right[:frames]
	}
	window := 4096
	if len(a.left) < SampleRate/3 {
		window = 2048
	}
	if len(a.left) < window {
		return r, fmt.Errorf("采样太短，无法测量")
	}
	mono := make([]float32, len(a.left))
	for i := range mono {
		mono[i] = (a.left[i] + a.right[i]) * .5
	}
	// 用独立能量区块测实际发声覆盖，不能把“文件够长”当成长音够长。
	var previous, peak float64
	var envelopes []float64
	block := SampleRate / 100
	for at := 0; at < len(mono); at += block {
		until := min(at+block, len(mono))
		var e float64
		for _, v := range mono[at:until] {
			e += float64(v * v)
		}
		e = math.Sqrt(e / float64(until-at))
		envelopes = append(envelopes, e)
		peak = max(peak, e)
		if e >= .005 {
			r.ActiveSeconds += float64(until-at) / SampleRate
		}
	}
	last := -100
	for i, e := range envelopes {
		if e > peak*.2 && e > previous*2.5 && i-last > 5 {
			r.Onsets++
			last = i
		}
		previous = e
	}
	var pitches []float64
	windows := 0
	hop := max(64, min(SampleRate/10, (len(mono)-window)/8))
	for at := 0; at+window <= len(mono); at += hop {
		var energy float64
		for _, v := range mono[at : at+window] {
			energy += float64(v * v)
		}
		if energy/float64(window) < .000025 {
			continue
		}
		p, q := estimateWindowPitch(mono[at : at+window])
		windows++
		if q >= .82 {
			pitches = append(pitches, p)
		}
	}
	if len(pitches) < 2 || windows == 0 {
		return r, fmt.Errorf("未测得足够稳定的单音，候选保留但不登记根音")
	}
	slices.Sort(pitches)
	middle := pitches[len(pitches)/2]
	stable := 0
	for _, p := range pitches {
		if math.Abs(p-middle) <= .5 {
			stable++
		}
	}
	r.StableFraction = float64(stable) / float64(windows)
	r.Pitch = int(math.Round(middle))
	r.Cents = (middle - float64(r.Pitch)) * 100
	if r.StableFraction < .8 || r.Pitch < 24 || r.Pitch > 108 {
		return r, fmt.Errorf("候选含变音或不稳定单音，稳定窗口 %.0f%%", r.StableFraction*100)
	}
	return r, nil
}
