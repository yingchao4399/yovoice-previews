package workbench

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"yovoice/internal/schema"
)

func TestLyricsOptimizationUsesSelectedDataAndDoesNotSaveWork(t *testing.T) {
	server, requests := fakeLLM(t, `{"title":"不应自动覆盖标题","style":"folk","lyrics":"山风轻轻拂过弦","language":"zh"}`)
	w := aiWorkbench(t, server)
	_, e := w.AIModels(context.Background(), "")
	must(t, e)
	before := schema.DefaultDraft()
	must(t, w.SaveDraft(before))
	result, e := w.WriteLyrics(context.Background(), LyricsBrief{ProjectID: before.ID, Mode: "optimize", Brief: "更有画面感，保留原意", CurrentLyrics: "山风吹过弦", Seconds: 90, Language: "zh"})
	must(t, e)
	if result.Lyrics != "山风轻轻拂过弦" || len(*requests) != 1 {
		t.Fatal("优化未返回完整可预览歌词")
	}
	request := (*requests)[0]
	data, _ := json.Marshal(request)
	if !strings.Contains(string(data), "山风吹过弦") || !strings.Contains(string(data), "do not add section markers") {
		t.Fatal("选中片段约束没有传入模型")
	}
	after, e := w.assistantWork(before.ID)
	must(t, e)
	if after.Title != before.Title || after.Text != before.Text || after.Lyrics != before.Lyrics {
		t.Fatal("生成结果不应自动覆盖作品")
	}
	if _, e = w.WriteLyrics(context.Background(), LyricsBrief{Mode: "optimize", Brief: "改写"}); e == nil {
		t.Fatal("优化空歌词应提前拒绝")
	}
}
