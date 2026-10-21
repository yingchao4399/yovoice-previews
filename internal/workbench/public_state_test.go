package workbench

import (
	"encoding/json"
	"strings"
	"testing"
	"yovoice/internal/schema"
)

// 增量推送也必须经过定制投影，不能把插件预设和任务完整快照送到界面。
func TestPublicStateKeepsExecutionDataServerSide(t *testing.T) {
	w, err := New(t.TempDir())
	must(t, err)
	defer w.Close()
	must(t, w.Store.Update(func(s *schema.State) {
		s.InstrumentSources = []schema.InstrumentSource{{ID: "preset", Plugin: &schema.PluginInstrument{StateData: []byte("private-plugin-preset")}}}
		s.ArrangementJobs = []schema.ArrangementJob{{ID: "job", Draft: schema.Draft{Text: "private-execution-draft"}}}
	}, false))
	public := w.PublicState()
	b, err := json.Marshal(public)
	must(t, err)
	if strings.Contains(string(b), "private-execution-draft") || len(public.InstrumentSources[0].Plugin.StateData) != 0 {
		t.Fatal("界面状态泄漏执行快照")
	}
	stored := w.Store.Read()
	if string(stored.InstrumentSources[0].Plugin.StateData) != "private-plugin-preset" || stored.ArrangementJobs[0].Draft.Text != "private-execution-draft" {
		t.Fatal("界面投影改写了原始执行数据")
	}
}
