package workbench

import (
	"fmt"
	"os"
	"slices"
	"yovoice/internal/schema"
)

// DeleteArrangementMaterial 只删除未被工程/音色引用的候选，不破坏已采用素材。
func (w *Workbench) DeleteArrangementMaterial(id string) error {
	w.generationReview.Lock()
	defer w.generationReview.Unlock()
	w.edit.Lock()
	defer w.edit.Unlock()
	state := w.Store.Read()
	i := slices.IndexFunc(state.ArrangementMaterials, func(m schema.ArrangementMaterial) bool { return m.ID == id })
	if i < 0 {
		return fmt.Errorf("候选已经不存在")
	}
	m := state.ArrangementMaterials[i]
	if m.GenerationResultState() == "generating" || m.GenerationResultState() == "checking" {
		return fmt.Errorf("请先取消该生成任务，再删除候选")
	}
	for _, s := range state.InstrumentSources {
		if slices.ContainsFunc(s.Samples, func(n schema.InstrumentSample) bool { return n.GenerationID == id }) {
			return fmt.Errorf("此记录仍被可演奏音色引用，请先移除相关音色")
		}
	}
	files := []string{m.FileName}
	if m.GenerationResult != nil {
		files = append(files, m.GenerationResult.DerivedFile)
	}
	remaining := state
	remaining.ArrangementMaterials = slices.DeleteFunc(slices.Clone(state.ArrangementMaterials), func(m schema.ArrangementMaterial) bool { return m.ID == id })
	refs := remaining.MediaRefs()["outputs"]
	for _, f := range files {
		if f != "" && refs[f] {
			return fmt.Errorf("此候选仍被工程或历史结果引用，不能删除")
		}
	}
	if e := w.Store.Update(func(s *schema.State) {
		s.ArrangementMaterials = slices.DeleteFunc(s.ArrangementMaterials, func(m schema.ArrangementMaterial) bool { return m.ID == id })
	}, true); e != nil {
		return e
	}
	for _, f := range files {
		if f == "" {
			continue
		}
		path, e := w.Store.MediaPath("outputs", f)
		if e == nil {
			_ = os.Remove(path)
		}
	}
	return nil
}
