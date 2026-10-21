package store

import (
	"fmt"
	"strings"
	"testing"
	"yovoice/internal/schema"
)

// 使用接近实际整库的负载测量快照，避免只用空库掩盖助手逐词更新的拷贝开销。
func BenchmarkPopulatedStateRead(b *testing.B) {
	s, err := New(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	err = s.Update(func(state *schema.State) {
		state.Drafts = nil
		state.History = nil
		for i := 0; i < 27; i++ {
			d := schema.DefaultDraft()
			d.ID = fmt.Sprintf("draft-%d", i)
			d.Text = strings.Repeat("音乐编曲快照内容", 1200)
			state.Drafts = append(state.Drafts, d)
			state.History = append(state.History, schema.Generation{ID: fmt.Sprintf("take-%d", i), Settings: d})
		}
	}, false)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = s.Read()
	}
}
