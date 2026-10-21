package desktop

import (
	"encoding/json"
	"testing"
)

func TestStateStreamSendsChangedFields(t *testing.T) {
	var stream stateStream
	first, e := stream.next([]byte(`{"drafts":[{"id":"a"}],"activity":{"received":1}}`))
	must(t, e)
	var full struct {
		Event string          `json:"event"`
		State json.RawMessage `json:"state"`
	}
	must(t, json.Unmarshal(first, &full))
	if full.Event != "state" || string(full.State) != `{"drafts":[{"id":"a"}],"activity":{"received":1}}` {
		t.Fatal(string(first))
	}
	// 只有进度变化时不再带上作品列表。
	b, e := stream.next([]byte(`{"drafts":[{"id":"a"}],"activity":{"received":2}}`))
	must(t, e)
	if string(b) != `{"event":"patch","patch":{"activity":{"received":2}},"removed":[]}` {
		t.Fatal(string(b))
	}
	b, e = stream.next([]byte(`{"drafts":[{"id":"a"}],"activity":{"received":2}}`))
	must(t, e)
	if b != nil {
		t.Fatal(string(b))
	}
	// 被省略的字段列在 removed 里，由界面删除。
	b, e = stream.next([]byte(`{"drafts":[{"id":"a"}]}`))
	must(t, e)
	if string(b) != `{"event":"patch","patch":{},"removed":["activity"]}` {
		t.Fatal(string(b))
	}
}
