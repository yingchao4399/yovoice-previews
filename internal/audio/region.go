package audio

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
)

// RegionOptions 的长度按输入采样率换算成帧，不重采样、不修改原文件。
// 短素材只有明确允许 Pad 时才补静音；淡出作用于实际声音结尾。
type RegionOptions struct {
	Offset  float64 `json:"offset"`
	Seconds float64 `json:"seconds"`
	FadeIn  float64 `json:"fadeIn,omitempty"`
	FadeOut float64 `json:"fadeOut,omitempty"`
	Pad     bool    `json:"pad,omitempty"`
}

type pcmRegionFormat struct {
	format, channels, bits, align uint16
	rate                          uint32
	start                         int64
	length                        uint32
}

func readRegionFormat(f *os.File) (pcmRegionFormat, error) {
	var format pcmRegionFormat
	var riff [12]byte
	if _, err := f.ReadAt(riff[:], 0); err != nil || string(riff[:4]) != "RIFF" || string(riff[8:]) != "WAVE" {
		return format, fmt.Errorf("无效WAV音频")
	}
	info, err := f.Stat()
	if err != nil {
		return format, err
	}
	for pos := int64(12); pos+8 <= info.Size(); {
		var header [8]byte
		if _, err = f.ReadAt(header[:], pos); err != nil {
			return format, err
		}
		size := binary.LittleEndian.Uint32(header[4:])
		if pos+8+int64(size) > info.Size() {
			return format, fmt.Errorf("音频数据块损坏")
		}
		switch string(header[:4]) {
		case "fmt ":
			var data [16]byte
			if size < 16 {
				return format, fmt.Errorf("音频格式无效")
			}
			if _, err = f.ReadAt(data[:], pos+8); err != nil {
				return format, err
			}
			format.format = binary.LittleEndian.Uint16(data[:])
			format.channels = binary.LittleEndian.Uint16(data[2:])
			format.rate = binary.LittleEndian.Uint32(data[4:])
			format.align = binary.LittleEndian.Uint16(data[12:])
			format.bits = binary.LittleEndian.Uint16(data[14:])
		case "data":
			format.start, format.length = pos+8, size
		}
		pos += 8 + int64(size) + int64(size%2)
	}
	if (format.format != 1 && format.format != 3) || format.channels < 1 || format.channels > 2 || format.rate < 8000 || format.rate > 192000 || (format.bits != 16 && format.bits != 24 && format.bits != 32) || format.format == 3 && format.bits != 32 || format.align != format.channels*format.bits/8 || format.length == 0 || format.length%uint32(format.align) != 0 {
		return format, fmt.Errorf("音频没有完整采样帧")
	}
	return format, nil
}

// WriteRegion 用于选定片段分析及定长音频副本。输出必须是新文件，失败会移除不完整副本。
// 流式处理 PCM16/24/32 与 float32，保留采样率、声道和位深，内存用量与曲长无关。
func WriteRegion(ctx context.Context, input, output string, in RegionOptions) (err error) {
	for _, v := range []float64{in.Offset, in.Seconds, in.FadeIn, in.FadeOut} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 3600 {
			return fmt.Errorf("片段范围或淡化时长无效")
		}
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if _, err = Duration(input); err != nil {
		return err
	}
	src, err := os.Open(input)
	if err != nil {
		return err
	}
	defer src.Close()
	f, err := readRegionFormat(src)
	if err != nil {
		return err
	}
	start := int64(math.Round(in.Offset * float64(f.rate)))
	available := int64(f.length)/int64(f.align) - start
	if available <= 0 {
		return fmt.Errorf("片段起点超出音频范围")
	}
	frames := available
	if in.Seconds > 0 {
		frames = int64(math.Round(in.Seconds * float64(f.rate)))
	}
	if frames <= 0 || frames*int64(f.align) > math.MaxUint32-49 {
		return fmt.Errorf("片段长度无效或超过WAV格式容量")
	}
	if frames > available && !in.Pad {
		return fmt.Errorf("原音频短于目标长度，需明确允许补静音")
	}
	soundFrames := min(frames, available)
	if _, err = src.Seek(f.start+start*int64(f.align), io.SeekStart); err != nil {
		return err
	}
	out, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	complete := false
	defer func() {
		if closeErr := out.Close(); err == nil {
			err = closeErr
		}
		if !complete || err != nil {
			_ = os.Remove(output)
		}
	}()
	extra := 0
	if f.format == 3 {
		extra = 12
	}
	dataBytes := frames * int64(f.align)
	header := make([]byte, 44+extra)
	copy(header[:], "RIFF")
	binary.LittleEndian.PutUint32(header[4:], uint32(36+int64(extra)+dataBytes+dataBytes%2))
	copy(header[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(header[16:], 16)
	binary.LittleEndian.PutUint16(header[20:], f.format)
	binary.LittleEndian.PutUint16(header[22:], f.channels)
	binary.LittleEndian.PutUint32(header[24:], f.rate)
	binary.LittleEndian.PutUint32(header[28:], f.rate*uint32(f.align))
	binary.LittleEndian.PutUint16(header[32:], f.align)
	binary.LittleEndian.PutUint16(header[34:], f.bits)
	if extra > 0 {
		copy(header[36:], "fact")
		binary.LittleEndian.PutUint32(header[40:], 4)
		binary.LittleEndian.PutUint32(header[44:], uint32(frames))
	}
	copy(header[36+extra:], "data")
	binary.LittleEndian.PutUint32(header[40+extra:], uint32(dataBytes))
	if _, err = out.Write(header[:]); err != nil {
		return err
	}
	fadeIn := min(soundFrames, int64(math.Round(in.FadeIn*float64(f.rate))))
	fadeOut := min(soundFrames, int64(math.Round(in.FadeOut*float64(f.rate))))
	stride := int(f.bits / 8)
	buffer := make([]byte, 8192*int(f.align))
	for pos := int64(0); pos < frames; {
		if err = ctx.Err(); err != nil {
			return err
		}
		n := min(int64(8192), frames-pos)
		block := buffer[:int(n)*int(f.align)]
		clear(block)
		readFrames := max(int64(0), min(n, soundFrames-pos))
		if _, err = io.ReadFull(src, block[:int(readFrames)*int(f.align)]); err != nil {
			return err
		}
		for frame := int64(0); frame < readFrames; frame++ {
			absolute := pos + frame
			gain := 1.0
			if absolute < fadeIn {
				gain = min(gain, float64(absolute)/float64(max(int64(1), fadeIn-1)))
			}
			if absolute >= soundFrames-fadeOut {
				gain = min(gain, float64(soundFrames-absolute-1)/float64(max(int64(1), fadeOut-1)))
			}
			for channel := 0; channel < int(f.channels); channel++ {
				sample := block[int(frame)*int(f.align)+channel*stride:]
				if f.format == 3 {
					v := math.Float32frombits(binary.LittleEndian.Uint32(sample))
					if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
						return fmt.Errorf("音频含无效浮点采样")
					}
					if gain < 1 {
						binary.LittleEndian.PutUint32(sample, math.Float32bits(v*float32(gain)))
					}
				} else if gain < 1 {
					switch f.bits {
					case 16:
						binary.LittleEndian.PutUint16(sample, uint16(int16(math.Round(float64(int16(binary.LittleEndian.Uint16(sample)))*gain))))
					case 24:
						v := int32(sample[0]) | int32(sample[1])<<8 | int32(sample[2])<<16
						v = int32(math.Round(float64(v<<8>>8) * gain))
						sample[0], sample[1], sample[2] = byte(v), byte(v>>8), byte(v>>16)
					case 32:
						binary.LittleEndian.PutUint32(sample, uint32(int32(math.Round(float64(int32(binary.LittleEndian.Uint32(sample)))*gain))))
					}
				}
			}
		}
		if _, err = out.Write(block); err != nil {
			return err
		}
		pos += n
	}
	if dataBytes%2 != 0 {
		if _, err = out.Write([]byte{0}); err != nil {
			return err
		}
	}
	if err = out.Sync(); err != nil {
		return err
	}
	complete = true
	return nil
}
