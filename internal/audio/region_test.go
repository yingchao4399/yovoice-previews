package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func regionFixture(t *testing.T, path string, bits, channels, format uint16) []byte {
	t.Helper()
	align := channels * bits / 8
	data := make([]byte, 44+8000*int(align))
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], format)
	binary.LittleEndian.PutUint16(data[22:], channels)
	binary.LittleEndian.PutUint32(data[24:], 8000)
	binary.LittleEndian.PutUint32(data[28:], 8000*uint32(align))
	binary.LittleEndian.PutUint16(data[32:], align)
	binary.LittleEndian.PutUint16(data[34:], bits)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], uint32(len(data)-44))
	for frame := 0; frame < 8000; frame++ {
		for c := 0; c < int(channels); c++ {
			sample := data[44+frame*int(align)+c*int(bits/8):]
			v := .25
			if c == 1 {
				v = -.5
			}
			switch {
			case format == 3:
				binary.LittleEndian.PutUint32(sample, math.Float32bits(float32(v)))
			case bits == 16:
				binary.LittleEndian.PutUint16(sample, uint16(int16(v*32768)))
			case bits == 24:
				n := int32(v * 8388608)
				sample[0], sample[1], sample[2] = byte(n), byte(n>>8), byte(n>>16)
			case bits == 32:
				binary.LittleEndian.PutUint32(sample, uint32(int32(v*2147483648)))
			}
		}
	}
	must(t, os.WriteFile(path, data, 0600))
	return data
}

func TestRegionPreservesPCMRateChannelsAndOriginal(t *testing.T) {
	for _, spec := range [][3]uint16{{16, 1, 1}, {16, 2, 1}, {24, 1, 1}, {24, 2, 1}, {32, 2, 1}, {32, 2, 3}} {
		t.Run(fmt.Sprint(spec), func(t *testing.T) {
			root := t.TempDir()
			source, target := filepath.Join(root, "source.wav"), filepath.Join(root, "region.wav")
			original := regionFixture(t, source, spec[0], spec[1], spec[2])
			must(t, WriteRegion(context.Background(), source, target, RegionOptions{Offset: .25, Seconds: .5}))
			duration, e := Duration(target)
			must(t, e)
			if duration != .5 {
				t.Fatal("输出长度不准确", duration)
			}
			f, e := os.Open(target)
			must(t, e)
			defer f.Close()
			format, e := readRegionFormat(f)
			must(t, e)
			if format.rate != 8000 || format.channels != spec[1] || format.bits != spec[0] || format.format != spec[2] {
				t.Fatal("改变采样格式", format)
			}
			actual := make([]byte, format.length)
			_, e = f.ReadAt(actual, format.start)
			must(t, e)
			start := 44 + 2000*int(format.align)
			if !bytes.Equal(actual, original[start:start+len(actual)]) {
				t.Fatal("无淡化裁剪不应改变PCM采样")
			}
			after, e := os.ReadFile(source)
			must(t, e)
			if !bytes.Equal(after, original) {
				t.Fatal("改写了原始音频")
			}
		})
	}
}

func TestRegionRequiresExplicitPaddingAndFadesBeforeSilence(t *testing.T) {
	root := t.TempDir()
	source, target := filepath.Join(root, "source.wav"), filepath.Join(root, "region.wav")
	regionFixture(t, source, 16, 2, 1)
	if WriteRegion(context.Background(), source, target, RegionOptions{Seconds: 2}) == nil {
		t.Fatal("不能默默补静音冒充完整生成")
	}
	if _, e := os.Stat(target); !os.IsNotExist(e) {
		t.Fatal("失败不应产生输出")
	}
	must(t, WriteRegion(context.Background(), source, target, RegionOptions{Seconds: 2, Pad: true, FadeIn: .01, FadeOut: .01}))
	duration, e := Duration(target)
	must(t, e)
	if duration != 2 {
		t.Fatal(duration)
	}
	data, e := os.ReadFile(target)
	must(t, e)
	if !bytes.Equal(data[44:48], make([]byte, 4)) || !bytes.Equal(data[44+7999*4:], make([]byte, len(data)-44-7999*4)) {
		t.Fatal("首尾淡化及补零不正确")
	}
	if bytes.Equal(data[44+4000*4:44+4001*4], make([]byte, 4)) {
		t.Fatal("不能把有效声音全部抹掉")
	}
	if WriteRegion(context.Background(), source, source, RegionOptions{Seconds: .5}) == nil {
		t.Fatal("不得覆盖原文件")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if WriteRegion(ctx, source, filepath.Join(root, "cancel.wav"), RegionOptions{}) != context.Canceled {
		t.Fatal("取消必须生效")
	}
}

func TestRegion24BitOddFrameHasRIFFPadding(t *testing.T) {
	root := t.TempDir()
	source, target := filepath.Join(root, "source.wav"), filepath.Join(root, "region.wav")
	regionFixture(t, source, 24, 1, 1)
	must(t, WriteRegion(context.Background(), source, target, RegionOptions{Seconds: 1.0 / 8000}))
	data, e := os.ReadFile(target)
	must(t, e)
	if len(data) != 48 || binary.LittleEndian.Uint32(data[4:]) != 40 || binary.LittleEndian.Uint32(data[40:]) != 3 {
		t.Fatal("24位单帧应有字节对齐填充")
	}
}
