package score

import (
	"fmt"
	"math"
	"yovoice/internal/schema"
)

// SampleABC 为孤立单音选择可表示的拍号与整数速度，避免补齐小节的休止改变生成条件。
// 最多允许半个语义帧的量化差；不改用户曲谱或以裁切补偿谱面错误。
func SampleABC(pitch, velocity int, seconds float64, key, name string) (string, error) {
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < .05 || seconds > 30 {
		return "", fmt.Errorf("YuE2 单音谱条件支持 0.05–30 秒，请调整音符长度或使用其他生成路径")
	}
	best := math.Inf(1)
	var music schema.Score
	var track schema.ScoreTrack
	end := 0
	for bars := 1; bars <= 32; bars++ {
		for _, denominator := range []int{4, 8, 16} {
			for numerator := 1; numerator <= 16; numerator++ {
				beats := float64(bars*numerator*4) / float64(denominator)
				tempo := math.Round(beats * 60 / seconds)
				if tempo < 30 || tempo > 300 {
					continue
				}
				difference := math.Abs(beats*60/tempo - seconds)
				if difference >= best-1e-10 {
					continue
				}
				best, end = difference, bars
				track = schema.ScoreTrack{ID: "sample", Name: name, Notes: []schema.ScoreNote{{Bar: 1, Beat: 1, Length: beats, Pitch: pitch, Velocity: velocity}}}
				music = schema.Score{Tempo: tempo, TimeSignature: []int{numerator, denominator}, Key: key, Tracks: []schema.ScoreTrack{track}}
			}
		}
	}
	if best > .02 {
		return "", fmt.Errorf("YuE2 无法在半个语义帧内表示 %.4f 秒单音条件", seconds)
	}
	text, err := PhraseABC(music, track, 1, end)
	if err != nil {
		return "", err
	}
	parsed, err := ParseABC(text)
	if err != nil {
		return "", err
	}
	if math.Abs(parsed.Seconds()-seconds) > .02 || len(parsed.Ins().Notes) != 1 {
		return "", fmt.Errorf("单音谱条件回读的时长或音符数量不一致")
	}
	return text, nil
}
