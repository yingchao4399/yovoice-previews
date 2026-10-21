package audio

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strconv"
	"yovoice/internal/platform"
)

// ExtractInstrument 只提取显式选择的区段，不拉伸、不补静音，原片保持不变。
func ExtractInstrument(ctx context.Context, executable, input, output string, offset, seconds float64) error {
	if math.IsNaN(offset) || math.IsNaN(seconds) || math.IsInf(offset, 0) || math.IsInf(seconds, 0) || offset < 0 || seconds <= 0 || seconds > 305 {
		return fmt.Errorf("音频区段无效")
	}
	if handled, err := extractSamplerPCM(input, output, offset, seconds); handled {
		return err
	}
	if executable == "" {
		return fmt.Errorf("需要随包音频转换器才能适配此格式，请使用完整安装包")
	}
	cmd := exec.CommandContext(ctx, executable, "-nostdin", "-v", "error", "-xerror", "-protocol_whitelist", "file", "-i", input, "-ss", strconv.FormatFloat(offset, 'f', 6, 64), "-t", strconv.FormatFloat(seconds, 'f', 6, 64), "-map", "0:a:0", "-vn", "-ac", "2", "-ar", "44100", "-c:a", "pcm_s16le", "-map_metadata", "-1", "-y", output)
	platform.ConfigureProcess(cmd)
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("提取采样失败：%w", err)
	}
	return nil
}

// 已规范化 PCM 可按帧直接截取，避免再次编解码；其他格式仍由随包转换器处理。
func extractSamplerPCM(input, output string, offset, seconds float64) (bool, error) {
	b, err := os.ReadFile(input)
	if err != nil {
		return true, err
	}
	if len(b) < 44 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return false, nil
	}
	valid := false
	var data []byte
	for pos := 12; pos+8 <= len(b); {
		size := int(binary.LittleEndian.Uint32(b[pos+4:]))
		if size < 0 || size > len(b)-pos-8 {
			return true, fmt.Errorf("WAV 数据损坏")
		}
		chunk := b[pos+8 : pos+8+size]
		if string(b[pos:pos+4]) == "fmt " && len(chunk) >= 16 {
			valid = binary.LittleEndian.Uint16(chunk) == 1 && binary.LittleEndian.Uint16(chunk[2:]) == 2 && binary.LittleEndian.Uint32(chunk[4:]) == 44100 && binary.LittleEndian.Uint16(chunk[14:]) == 16
		}
		if string(b[pos:pos+4]) == "data" {
			data = chunk
		}
		pos += 8 + size + size%2
	}
	if !valid || data == nil {
		return false, nil
	}
	start := int(math.Round(offset*44100)) * 4
	length := int(math.Round(seconds*44100)) * 4
	if start < 0 || length <= 0 || start > len(data) || length > len(data)-start {
		return true, fmt.Errorf("原片不足以覆盖所选区段")
	}
	out := make([]byte, 44+length)
	copy(out, "RIFF")
	binary.LittleEndian.PutUint32(out[4:], uint32(len(out)-8))
	copy(out[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(out[16:], 16)
	binary.LittleEndian.PutUint16(out[20:], 1)
	binary.LittleEndian.PutUint16(out[22:], 2)
	binary.LittleEndian.PutUint32(out[24:], 44100)
	binary.LittleEndian.PutUint32(out[28:], 176400)
	binary.LittleEndian.PutUint16(out[32:], 4)
	binary.LittleEndian.PutUint16(out[34:], 16)
	copy(out[36:], "data")
	binary.LittleEndian.PutUint32(out[40:], uint32(length))
	copy(out[44:], data[start:start+length])
	return true, os.WriteFile(output, out, 0600)
}
