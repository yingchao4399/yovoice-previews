package store

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
	"yovoice/internal/schema"
)

func TestSnapshotCopyPreservesJSONAndIsolation(t *testing.T) {
	d := schema.DefaultDraft()
	d.ModelOptions = map[string]map[string]any{"ace_step": {"duration_seconds": 30, "nested": []any{map[string]any{"value": "original"}}}}
	original := schema.State{
		Drafts:            []schema.Draft{d},
		InstrumentSources: []schema.InstrumentSource{{ID: "source", Plugin: &schema.PluginInstrument{StateData: []byte{1, 2, 3}}, Samples: []schema.InstrumentSample{{RootPitch: 62}}}},
		ArrangementJobs:   []schema.ArrangementJob{{CreatedAt: time.Now(), Draft: d}},
	}
	copied := clone(original)
	before, err := json.Marshal(original)
	must(t, err)
	after, err := json.Marshal(copied)
	must(t, err)
	if string(before) != string(after) {
		t.Fatal("快照改变了状态的 JSON 语义")
	}
	if _, ok := copied.Drafts[0].ModelOptions["ace_step"]["duration_seconds"].(float64); !ok {
		t.Fatal("模型自由参数必须维持 JSON 数值类型")
	}
	copied.Drafts[0].ModelOptions["ace_step"]["nested"].([]any)[0].(map[string]any)["value"] = "changed"
	copied.InstrumentSources[0].Plugin.StateData[0] = 9
	copied.InstrumentSources[0].Samples[0].RootPitch = 90
	copied.ArrangementJobs[0].Draft.Emotions[0] = 1
	unchanged, err := json.Marshal(original)
	must(t, err)
	if string(before) != string(unchanged) {
		t.Fatal("修改返回快照污染了内部状态")
	}
	// 空容器不能变成 nil；界面依赖空数组仍是数组。
	input := schema.State{Drafts: []schema.Draft{}, InstrumentSources: nil}
	if !reflect.DeepEqual(input, clone(input)) {
		t.Fatal("nil 与空数组语义丢失")
	}
}
