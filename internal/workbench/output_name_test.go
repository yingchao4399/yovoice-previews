package workbench

import (
	"testing"
	"time"
)

func TestOutputFileName(t *testing.T) {
	at := time.Date(2026, 10, 16, 9, 15, 30, 0, time.Local)
	id := "3822129de52ad4059c05dc6ec67948c3"
	cases := map[string]string{
		"今天天气不错，我们出去走走吧！": "20261016-091530_今天天气不错-我们出去走走吧_3822129d.wav",
		"a/b\\c:d*?\"<>|e": "20261016-091530_a-b-c-d-e_3822129d.wav",
		"  ":               "20261016-091530_3822129d.wav",
		"一二三四五六七八九十一二三四五六七八九十多余": "20261016-091530_一二三四五六七八九十一二三四五六七八九十_3822129d.wav",
	}
	for summary, want := range cases {
		if got := outputFileName(id, at, summary); got != want {
			t.Errorf("outputFileName(%q) = %q, want %q", summary, got, want)
		}
	}
	if outputSummary(" ", "曲名") != "曲名" || outputSummary("正文", "曲名") != "正文" {
		t.Fatal("outputSummary 应优先使用正文")
	}
}
