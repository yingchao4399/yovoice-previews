package audio

import (
	"encoding/binary"
	"math"
	"os"
	"yovoice/internal/msg"
)

// ReadMono 读取 PCM（16/24/32 位整数或 32 位浮点）WAV，多声道取平均，返回 -1~1 的采样与采样率。
func ReadMono(path string) ([]float32, int, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return nil, 0, e
	}
	bad := msg.Err(msg.ErrAudioFormat, nil)
	if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return nil, 0, bad
	}
	var format, channels, bits uint16
	var rate uint32
	var data []byte
	for pos := 12; pos+8 <= len(b); {
		size := int(binary.LittleEndian.Uint32(b[pos+4:]))
		body := pos + 8
		if size < 0 || body+size > len(b) {
			// 流式写出的 WAV 可能把 data 长度写成 0 或超出文件，按到文件末尾处理。
			if string(b[pos:pos+4]) != "data" {
				return nil, 0, bad
			}
			size = len(b) - body
		}
		switch string(b[pos : pos+4]) {
		case "fmt ":
			if size < 16 {
				return nil, 0, bad
			}
			format, channels = binary.LittleEndian.Uint16(b[body:]), binary.LittleEndian.Uint16(b[body+2:])
			rate, bits = binary.LittleEndian.Uint32(b[body+4:]), binary.LittleEndian.Uint16(b[body+14:])
			// WAVE_FORMAT_EXTENSIBLE 的真实格式在子格式 GUID 的前两个字节。
			if format == 0xFFFE && size >= 26 {
				format = binary.LittleEndian.Uint16(b[body+24:])
			}
		case "data":
			data = b[body : body+size]
		}
		pos = body + size + size%2
	}
	width := int(bits / 8)
	if channels == 0 || rate == 0 || data == nil || width == 0 || !(format == 1 && (bits == 16 || bits == 24 || bits == 32) || format == 3 && bits == 32) {
		return nil, 0, bad
	}
	frame := width * int(channels)
	samples := make([]float32, len(data)/frame)
	for i := range samples {
		var sum float64
		for c := 0; c < int(channels); c++ {
			at := data[i*frame+c*width:]
			switch {
			case format == 3:
				sum += float64(math.Float32frombits(binary.LittleEndian.Uint32(at)))
			case bits == 16:
				sum += float64(int16(binary.LittleEndian.Uint16(at))) / 32768
			case bits == 24:
				sum += float64(int32(uint32(at[0])<<8|uint32(at[1])<<16|uint32(at[2])<<24)>>8) / 8388608
			default:
				sum += float64(int32(binary.LittleEndian.Uint32(at))) / 2147483648
			}
		}
		samples[i] = float32(sum / float64(channels))
	}
	return samples, int(rate), nil
}

// Resample 线性插值改变采样率；降采样时先按比例做滑动平均，压住混叠。只用于分析（分离人声、对齐歌词），不用于输出。
func Resample(samples []float32, from, to int) []float32 {
	if from == to || len(samples) == 0 {
		return samples
	}
	source := samples
	if from > to {
		width := from / to
		if width > 1 {
			source = make([]float32, len(samples))
			var sum float64
			for i, v := range samples {
				sum += float64(v)
				if i >= width {
					sum -= float64(samples[i-width])
				}
				source[i] = float32(sum / float64(min(i+1, width)))
			}
		}
	}
	out := make([]float32, int(int64(len(samples))*int64(to)/int64(from)))
	step := float64(from) / float64(to)
	for i := range out {
		at := float64(i) * step
		j := int(at)
		if j+1 >= len(source) {
			out[i] = source[len(source)-1]
			continue
		}
		f := float32(at - float64(j))
		out[i] = source[j]*(1-f) + source[j+1]*f
	}
	return out
}

// WriteMono 把采样写成 16 位单声道 WAV。
func WriteMono(path string, samples []float32, rate int) error {
	b := make([]byte, 44+len(samples)*2)
	copy(b, "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-8))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], uint32(rate))
	binary.LittleEndian.PutUint32(b[28:], uint32(rate*2))
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(len(samples)*2))
	for i, v := range samples {
		binary.LittleEndian.PutUint16(b[44+i*2:], uint16(int16(math.Round(float64(max(-1, min(1, v)))*32767))))
	}
	return os.WriteFile(path, b, 0600)
}
