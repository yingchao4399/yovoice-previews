package workbench

import (
	"fmt"
	"os"
	"slices"
	"yovoice/internal/schema"
)

// 已确认音色的托管副本丢失时，从同一已核验派生文件修复，不重新联网或生成。
func (w *Workbench) repairConfirmedAISamples(part schema.ArrangementPart) error {
	state := w.Store.Read()
	for _, source := range state.InstrumentSources {
		if source.Origin != "ai" || !arrangementSameInstrument(source.Instrument, part.Instrument) || part.SourceID != "" && source.ID != part.SourceID {
			continue
		}
		for _, sample := range source.Samples {
			if sample.GenerationID == "" {
				continue
			}
			path, e := w.Store.MediaPath("instruments", sample.FileName)
			if e != nil {
				return e
			}
			if hash, e := arrangementAudioDigest(path); e == nil && hash == sample.SHA256 {
				continue
			}
			i := slices.IndexFunc(state.ArrangementMaterials, func(m schema.ArrangementMaterial) bool {
				return m.ID == sample.GenerationID && m.ReviewStatus == "confirmed" && m.GenerationResult != nil && m.GenerationResult.DerivedFile != ""
			})
			if i < 0 {
				continue
			}
			m := state.ArrangementMaterials[i]
			from, e := w.Store.MediaPath("outputs", m.GenerationResult.DerivedFile)
			if e != nil {
				return e
			}
			digest, e := arrangementAudioDigest(from)
			if e != nil || digest != sample.SHA256 {
				return fmt.Errorf("%s 的已确认派生采样也缺失或变化，不能自动恢复，请重新准备并试听", source.Name)
			}
			b, e := os.ReadFile(from)
			if e != nil {
				return e
			}
			if e = os.WriteFile(path+".part", b, 0600); e != nil {
				return e
			}
			if e = os.Rename(path+".part", path); e != nil {
				_ = os.Remove(path + ".part")
				return e
			}
		}
	}
	return nil
}
