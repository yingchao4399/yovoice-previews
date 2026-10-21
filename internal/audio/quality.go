package audio

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
)

// SignalQuality 仅检查可测量的波形问题，不代表音色、演奏或逐音符对齐已得到验证。
type SignalQuality struct {
	Peak            float64 `json:"peak"`
	ClippedFraction float64 `json:"clippedFraction"`
	OnsetSeconds    float64 `json:"onsetSeconds"`
	EndRMS          float64 `json:"endRms"`
}

func InspectSignal(path string) (SignalQuality, error) {
	q := SignalQuality{}
	if _, err := Duration(path); err != nil {
		return q, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return q, err
	}
	var format, bits, channels uint16
	var rate uint32
	var data []byte
	for pos := 12; pos+8 <= len(b); {
		size := int(binary.LittleEndian.Uint32(b[pos+4:]))
		if size < 0 || pos+8+size > len(b) {
			return q, fmt.Errorf("音频数据损坏")
		}
		chunk := b[pos+8 : pos+8+size]
		switch string(b[pos : pos+4]) {
		case "fmt ":
			if len(chunk) >= 16 {
				format = binary.LittleEndian.Uint16(chunk)
				channels = binary.LittleEndian.Uint16(chunk[2:])
				rate = binary.LittleEndian.Uint32(chunk[4:])
				bits = binary.LittleEndian.Uint16(chunk[14:])
			}
		case "data":
			data = chunk
		}
		pos += 8 + size + size%2
	}
	stride := int(bits / 8)
	if stride == 0 || channels == 0 || rate == 0 {
		return q, fmt.Errorf("音频格式无效")
	}
	count, clipped := 0, 0
	endEnergy, endCount := 0.0, 0
	endStart := max(0, len(data)-int(rate)*int(channels)*stride/10)
	first := -1
	for i := 0; i+stride <= len(data); i += stride {
		var v float64
		switch {
		case format == 3:
			v = float64(math.Float32frombits(binary.LittleEndian.Uint32(data[i:])))
		case bits == 16:
			v = float64(int16(binary.LittleEndian.Uint16(data[i:]))) / 32768
		case bits == 24:
			n := int32(data[i]) | int32(data[i+1])<<8 | int32(data[i+2])<<16
			v = float64(n<<8>>8) / 8388608
		case bits == 32:
			v = float64(int32(binary.LittleEndian.Uint32(data[i:]))) / 2147483648
		}
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return q, fmt.Errorf("音频含无效数值")
		}
		v = math.Abs(v)
		if i >= endStart {
			endEnergy += v * v
			endCount++
		}
		q.Peak = max(q.Peak, v)
		if v >= .999 {
			clipped++
		}
		if first < 0 && v > .005 {
			first = count
		}
		count++
	}
	if count == 0 || q.Peak < .003 {
		return q, fmt.Errorf("AI 乐句没有有效声音，请重新生成")
	}
	q.ClippedFraction = float64(clipped) / float64(count)
	if endCount > 0 {
		q.EndRMS = math.Sqrt(endEnergy / float64(endCount))
	}
	q.OnsetSeconds = float64(max(first, 0)) / float64(channels) / float64(rate)
	if q.ClippedFraction > .003 {
		return q, fmt.Errorf("AI 乐句削波比例 %.2f%%，请重新生成或降低模型输出增益", q.ClippedFraction*100)
	}
	return q, nil
}
