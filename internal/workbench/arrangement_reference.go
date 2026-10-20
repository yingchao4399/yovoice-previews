package workbench

import (
	"fmt"
	"os"
	"slices"
	"yovoice/internal/schema"
)

// AnalyzeArrangementReference 复用官方扒谱实现，仅访问当前作品登记的素材。
func (w *Workbench) AnalyzeArrangementReference(d schema.Draft, assetID string) error {
	if d.Kind != "score" || d.Timeline == nil {
		return fmt.Errorf("请先把参考音频添加到编曲作品")
	}
	i := slices.IndexFunc(d.Timeline.Assets, func(a schema.AudioAsset) bool { return a.ID == assetID })
	if i < 0 {
		return fmt.Errorf("作品中找不到参考音频")
	}
	path, e := w.Store.MediaPath("outputs", d.Timeline.Assets[i].FileName)
	if e != nil {
		return e
	}
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	return w.CoverMelody(d.ID, f)
}
