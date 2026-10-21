package score

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"yovoice/internal/schema"
)

// PhraseABC 保留乐句绝对音高和时值；受限模型不接受复调或网格外音符时直接报错。
func PhraseABC(music schema.Score, track schema.ScoreTrack, start, end int) (string, error) {
	if err := schema.ValidateScore(&music); err != nil {
		return "", err
	}
	if start < 1 || end < start || end > schema.ScoreMaxBars {
		return "", fmt.Errorf("乐句范围无效")
	}
	for bar := start + 1; bar <= end; bar++ {
		if music.BarTempo(bar) != music.BarTempo(start) {
			return "", fmt.Errorf("YuE2 当前谱条件不支持句内变速，请按速度分段，或使用音色按谱演奏")
		}
	}
	per := music.BeatsPerBar()
	barTicks := int(math.Round(per * abcQuarter))
	var notes []ABCNote
	for _, n := range track.Notes {
		if n.Bar < start || n.Bar > end {
			continue
		}
		at := (float64(n.Bar-start)*per + n.Beat - 1) * abcQuarter
		length := n.Length * abcQuarter
		if math.Abs(at-math.Round(at)) > .0001 || math.Abs(length-math.Round(length)) > .0001 {
			return "", fmt.Errorf("YuE2 无法无损表示当前时值，请调整本句网格或选择自由音频模型")
		}
		note := ABCNote{Start: int(math.Round(at)), Length: int(math.Round(length)), Pitch: n.Pitch}
		if note.Start+note.Length > (end-start+1)*barTicks {
			return "", fmt.Errorf("YuE2 乐句包含越过句尾的音符，请拆分连音")
		}
		notes = append(notes, note)
	}
	slices.SortFunc(notes, func(a, b ABCNote) int { return a.Start - b.Start })
	if len(notes) == 0 {
		return "", fmt.Errorf("YuE2 跟谱生成需要先写好本句音符")
	}
	for i := 1; i < len(notes); i++ {
		if notes[i].Start < notes[i-1].Start+notes[i-1].Length {
			return "", fmt.Errorf("YuE2 当前旋律条件只支持单声部，本句存在重叠音符")
		}
	}
	key, keyErr := NormalizeKey(music.Key)
	if keyErr != nil {
		return "", keyErr
	}
	unit := 32
	for ; unit <= 1024; unit *= 2 {
		step := ABCTicksPerWhole / unit
		fits := barTicks%step == 0
		for _, n := range notes {
			fits = fits && n.Start%step == 0 && n.Length%step == 0
		}
		if fits {
			break
		}
	}
	if unit > 1024 {
		return "", fmt.Errorf("YuE2 无法无损表示当前时值，请调整本句网格")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "X:1\nT:\nM:%d/%d\nL:1/%d\nQ:1/4=%g\n%s\n%s\nK:%s\n%% verse\n", music.TimeSignature[0], music.TimeSignature[1], unit, music.BarTempo(start), abcVoiceHeader[0], abcVoiceHeader[1], key)
	writer := abcWriter{unit: unit}
	for first := 0; first < end-start+1; first += 4 {
		var rests, bars []string
		for i := first; i < min(first+4, end-start+1); i++ {
			bar := ABCBar{Start: i * barTicks, Length: barTicks, Meter: [2]int{music.TimeSignature[0], music.TimeSignature[1]}}
			body, err := writer.bar(bar, key, notes, nil)
			if err != nil {
				return "", err
			}
			rests = append(rests, "Z")
			bars = append(bars, body)
		}
		fmt.Fprintf(&b, "V: Vocal\n%s\nV: Ins\n%s\n", joinBars(rests), joinBars(bars))
	}
	text := b.String()
	parsed, err := ParseABC(text)
	if err != nil {
		return "", err
	}
	if !slices.Equal(parsed.Ins().Notes, notes) {
		return "", fmt.Errorf("YuE2 条件转换改变了音符，已拒绝生成")
	}
	return text, nil
}
