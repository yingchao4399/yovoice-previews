package workbench

import (
	"strings"
	"time"
	"unicode"
)

// outputFileName 生成 outputs 目录中可直接辨认的文件名：本地生成时间、内容摘要与 ID 前 8 位，
// 例如 20261016-091530_今天天气不错_3822129d.wav。摘要只保留文字和数字，避免各平台的非法字符。
func outputFileName(id string, at time.Time, summary string) string {
	var b strings.Builder
	n, gap := 0, false
	for _, r := range summary {
		if n >= 20 {
			break
		}
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) {
			gap = b.Len() > 0
			continue
		}
		if gap {
			b.WriteByte('-')
			n++
			gap = false
		}
		b.WriteRune(r)
		n++
	}
	name := at.Local().Format("20060102-150405")
	if text := strings.TrimRight(b.String(), "-"); text != "" {
		name += "_" + text
	}
	return name + "_" + id[:min(8, len(id))] + ".wav"
}

// outputSummary 优先使用正文，没有正文时（如乐谱）使用作品标题。
func outputSummary(text, title string) string {
	if strings.TrimSpace(text) != "" {
		return text
	}
	return title
}
