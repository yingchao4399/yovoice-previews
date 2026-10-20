package workbench

import (
	"context"
	"testing"
)

func TestArrangementCompositionResumesValidatedPhrases(t *testing.T) {
	first := `{"tempo":120,"timeSignature":[4,4],"tracks":[{"id":"p1","partIds":["p1"],"name":"二胡","role":"melody","program":40,"notes":[[1,1,62,1,90]]}]}`
	second := `{"tempo":120,"timeSignature":[4,4],"tracks":[{"id":"p1","partIds":["p1"],"name":"箫","role":"melody","program":73,"notes":[[2,1,67,1,80]]}]}`
	server, requests := fakeLLM(t, first, `{}`, `{}`, second)
	w := aiWorkbench(t, server)
	_, err := w.AIModels(context.Background(), "")
	must(t, err)
	d := workflowDraft()
	d.Arrangement.Sections[0].Phrases[0].Parts[0].Source = "ai"
	_, e := migrateLegacyArrangementIDs(&d)
	must(t, e)
	in := ScoreBrief{ProjectID: d.ID, Brief: "两句器乐", Seconds: 4, Arrangement: d.Arrangement}
	if _, e = w.ComposeScore(context.Background(), in); e == nil {
		t.Fatal("第二句应校验失败")
	}
	if len(w.Store.Read().ArrangementScores) != 1 {
		t.Fatalf("第一句校验成功后没有持久保存，calls=%d", len(*requests))
	}
	out, e := w.ComposeScore(context.Background(), in)
	must(t, e)
	if len(*requests) != 4 || len(out.Tracks) != 2 || len(w.Store.Read().ArrangementScores) != 2 {
		t.Fatalf("重试未复用第一句：calls=%d tracks=%d", len(*requests), len(out.Tracks))
	}
}
