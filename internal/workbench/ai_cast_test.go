package workbench

import (
	"context"
	"strings"
	"testing"
)

// 分角色：按段请求，模型结果覆盖规则初判，旁白清空，角色按台词数排序，选角只接受声音库中的 ID。
func TestCastBook(t *testing.T) {
	server, requests := fakeLLM(t,
		`好的：{"speakers":{"2":"小王子","3":"@narrator","5":"狐狸"},"characters":[{"name":"小王子","gender":"male","age":"child","description":"天真"},{"name":"狐狸","gender":"male","age":"adult","description":"温和"}]}`,
		`{"cast":{"@narrator":"v1","小王子":"v2","狐狸":"missing"}}`)
	w := aiWorkbench(t, server)
	_, err := w.AIModels(context.Background(), "")
	must(t, err)
	in := CastBrief{Title: "小王子", Chapters: []CastChapter{
		{Title: "第一章", Lines: []CastLine{{Text: "他说："}, {Text: "请给我画一只羊。", Quote: true, Speaker: "@unknown"}, {Text: "你说什么？", Quote: true}}},
		{Title: "第二章", Lines: []CastLine{{Text: "狐狸来了。"}, {Text: "你好。", Quote: true, Speaker: "小王子"}, {Text: "再见。", Quote: true, Speaker: "狐狸"}}},
	}, Voices: []CastVoice{{ID: "v1", Name: "温柔旁白"}, {ID: "v2", Name: "少年", Description: "清亮的男孩声"}}}
	cast, err := w.CastBook(context.Background(), in)
	must(t, err)
	want := [][]string{{"", "小王子", ""}, {"", "狐狸", "狐狸"}}
	for ci := range want {
		if strings.Join(cast.Lines[ci], ",") != strings.Join(want[ci], ",") {
			t.Fatalf("第 %d 章说话人错误：%q", ci, cast.Lines[ci])
		}
	}
	if len(cast.Roles) != 2 || cast.Roles[0].Name != "狐狸" || cast.Roles[1].Description != "天真" {
		t.Fatalf("角色表错误：%+v", cast.Roles)
	}
	if len(cast.Voices) != 2 || cast.Voices[""] != "v1" || cast.Voices["小王子"] != "v2" {
		t.Fatalf("选角错误：%+v", cast.Voices)
	}
	if len(*requests) != 2 {
		t.Fatalf("应发送一段分角色和一次选角请求，实际 %d", len(*requests))
	}
	prompt := (*requests)[0]["messages"].([]any)[1].(map[string]any)["content"].(string)
	if !strings.Contains(prompt, "[2] Q: 请给我画一只羊。\n") || !strings.Contains(prompt, "[5] Q: 你好。 => 小王子") || !strings.Contains(prompt, "## 第二章") {
		t.Fatalf("请求内容错误：%s", prompt)
	}
	if !strings.Contains((*requests)[1]["messages"].([]any)[1].(map[string]any)["content"].(string), "id v2: 少年 (清亮的男孩声)") {
		t.Fatal("选角请求应带上声音描述")
	}
}

// 某段输出两次都不合规时保留规则初判；所有段都失败才报错。
func TestCastBookKeepsGuessesOnInvalidOutput(t *testing.T) {
	server, _ := fakeLLM(t, `不是 JSON`)
	w := aiWorkbench(t, server)
	_, err := w.AIModels(context.Background(), "")
	must(t, err)
	in := CastBrief{Chapters: []CastChapter{{Lines: []CastLine{{Text: "你好。", Quote: true, Speaker: "狐狸"}}}}}
	if _, err = w.CastBook(context.Background(), in); err == nil {
		t.Fatal("唯一一段失败时应报错")
	}
	long := strings.Repeat("长", castChunkRunes)
	in.Chapters = append(in.Chapters, CastChapter{Lines: []CastLine{{Text: long}}})
	server, _ = fakeLLM(t, `不是 JSON`, `不是 JSON`, `{"speakers":{"1":"小王子"},"characters":[]}`)
	w = aiWorkbench(t, server)
	_, err = w.AIModels(context.Background(), "")
	must(t, err)
	in.Chapters = []CastChapter{{Lines: []CastLine{{Text: "你好。", Quote: true, Speaker: "狐狸"}, {Text: long}}}, {Lines: []CastLine{{Text: "再见。", Quote: true, Speaker: "@unknown"}}}}
	cast, err := w.CastBook(context.Background(), in)
	must(t, err)
	if cast.Failed != 1 || cast.Lines[0][0] != "狐狸" || cast.Lines[1][0] != "小王子" || len(cast.Roles) != 2 {
		t.Fatalf("%+v", cast)
	}
	if _, err := w.StartCast(CastBrief{}); err == nil {
		t.Fatal("空书稿应被拒绝")
	}
}
