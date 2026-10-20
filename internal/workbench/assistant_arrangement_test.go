package workbench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"yovoice/internal/assistant"
	"yovoice/internal/assistant/fakeocho"
	"yovoice/internal/schema"
)

func TestAssistantArrangementBindingUsesUnifiedGuard(t *testing.T) {
	w := assistantWorkbench(t, fakeocho.Plan{})
	d := workflowDraft()
	d.Arrangement.Sections[0].Phrases = d.Arrangement.Sections[0].Phrases[:1]
	phrase := &d.Arrangement.Sections[0].Phrases[0]
	phrase.EndBar = 2
	part := &phrase.Parts[0]
	part.Instrument = "小提琴"
	part.ID = schema.NewID()
	d.Score.Tracks = d.Score.Tracks[:1]
	d.Score.Tracks[0].Name = "小提琴"
	d.Score.Tracks[0].PartIDs = []string{part.ID}
	d.Score.Tracks[0].Notes[0].Pitch = 86
	bass := inspectionSource(t, w, "低音提琴", 54)
	violin := inspectionSource(t, w, "弦乐 · 小提琴组", 86)
	part.SourceID = bass.ID
	must(t, w.Store.Update(func(s *schema.State) { s.InstrumentSources = []schema.InstrumentSource{bass, violin} }, true))
	must(t, w.SaveDraft(d))
	project, _ := json.Marshal(map[string]any{"projectId": d.ID})
	binding, _ := json.Marshal(map[string]any{"projectId": d.ID, "partId": part.ID, "sourceId": violin.ID})
	plan := fakeocho.Plan{Turns: [][]fakeocho.Step{{{Call: "arrangement_inventory", Args: json.RawMessage(`{}`)}, {Call: "inspect_arrangement", Args: project}, {Call: "bind_arrangement_source", Args: binding}, {Call: "inspect_arrangement", Args: project}, {Result: "success"}}}}
	data, e := json.Marshal(plan)
	must(t, e)
	path := filepath.Join(t.TempDir(), "plan.json")
	must(t, os.WriteFile(path, data, 0600))
	w.assistant = assistant.New(w.Store.Root, assistant.Options{Tools: assistantTools, NewServer: w.newAssistantServer, ExtraEnv: []string{fakeocho.PlanEnv + "=" + path}})
	must(t, w.SendAssistant("把错误的小提琴音源改为库里的小提琴组，保留旋律", AssistantContext{Page: "arrangement", WorkID: d.ID, Locale: schema.UiLocaleZhCN}))
	messages := waitChatIdle(t, w)
	if messages[len(messages)-1].Status != "completed" {
		t.Fatalf("助手工具调用失败：%+v", messages[len(messages)-1])
	}
	fixed, e := w.assistantWork(d.ID)
	must(t, e)
	if fixed.Arrangement.Sections[0].Phrases[0].Parts[0].SourceID != violin.ID || fixed.Score.Tracks[0].Notes[0].Pitch != 86 {
		t.Fatal("助手没有通过同一绑定服务修复来源并保留旋律")
	}
}
