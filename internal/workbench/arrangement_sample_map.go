package workbench

import (
	"fmt"
	"os"
	"slices"
	"yovoice/internal/schema"
)

// ensureGeneratedSampleMap 合并已确认映射，复制托管文件避免不同音源之间删除文件的所有权冲突。
// 调用方持有 edit；不会修改曲谱、锁定绑定或将真实音源混进 AI 音色。
func (w *Workbench) ensureGeneratedSampleMap(part schema.ArrangementPart, cells []ArrangementSampleCell) error {
	if len(cells) > 0 && !slices.ContainsFunc(cells, func(c ArrangementSampleCell) bool { return c.SourceID != cells[0].SourceID }) {
		return nil
	}
	state := w.Store.Read()
	source := schema.InstrumentSource{ID: schema.NewID(), Name: part.Instrument + " · AI 音色组合", Instrument: part.Instrument, Origin: "ai", Verification: "user", Provenance: "generated-map", DefaultArticulation: "normal", License: "AI 采样组合；生成模型与许可信息见各采样记录"}
	var created []string
	keep := false
	defer func() {
		if !keep {
			for _, p := range created {
				_ = os.Remove(p)
			}
		}
	}()
	for _, cell := range cells {
		i := slices.IndexFunc(state.InstrumentSources, func(s schema.InstrumentSource) bool {
			return s.ID == cell.SourceID && s.Origin == "ai" && s.Verification == "user"
		})
		if i < 0 {
			return fmt.Errorf("已确认音色已变更，请重新预检")
		}
		j := slices.IndexFunc(state.InstrumentSources[i].Samples, func(s schema.InstrumentSample) bool { return s.ID == cell.SampleID })
		if j < 0 {
			return fmt.Errorf("复用采样已不存在")
		}
		sample := state.InstrumentSources[i].Samples[j]
		from, e := w.Store.MediaPath("instruments", sample.FileName)
		if e != nil {
			return e
		}
		b, e := os.ReadFile(from)
		if e != nil {
			return e
		}
		sample.ID = schema.NewID()
		sample.FileName = "sample-" + sample.ID + ".wav"
		sample.ExactPitch = true
		if sample.SustainSeconds == 0 {
			sample.SustainSeconds = cell.SustainSeconds
		}
		to, e := w.Store.MediaPath("instruments", sample.FileName)
		if e != nil {
			return e
		}
		if e = os.WriteFile(to, b, 0600); e != nil {
			return e
		}
		created = append(created, to)
		source.Samples = append(source.Samples, sample)
	}
	if e := w.Store.Update(func(s *schema.State) { s.InstrumentSources = append(s.InstrumentSources, source) }, true); e != nil {
		return e
	}
	keep = true
	return nil
}
