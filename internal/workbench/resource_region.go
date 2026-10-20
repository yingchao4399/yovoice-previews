package workbench

import (
	"context"
	"fmt"
	"os"
	"yovoice/internal/audio"
	"yovoice/internal/schema"
)

type ResourceRegionInput struct {
	ID     string              `json:"id"`
	Region audio.RegionOptions `json:"region"`
}

// ResourceRegion 只处理明确选择的库内音频，输出独立副本，不覆写历史或工程。
func (w *Workbench) ResourceRegion(ctx context.Context, in ResourceRegionInput) (schema.AudioAsset, error) {
	r, err := w.ResolveResource(in.ID)
	if err != nil {
		return schema.AudioAsset{}, err
	}
	if r.Status != "ready" || r.MediaKind == "" || r.FileName == "" {
		return schema.AudioAsset{}, fmt.Errorf("请选择可播放的本地音频")
	}
	source, err := w.Store.MediaPath(r.MediaKind, r.FileName)
	if err != nil {
		return schema.AudioAsset{}, err
	}
	id := schema.NewID()
	asset := schema.AudioAsset{ID: id, FileName: "import-" + id + ".wav", Name: r.Name, Origin: r.Origin, SourceResourceID: r.ID, License: r.License, LicenseURL: r.LicenseURL, SourceURL: r.SourceURL, Author: r.Author}
	output, err := w.Store.MediaPath("outputs", asset.FileName)
	if err != nil {
		return schema.AudioAsset{}, err
	}
	if err = audio.WriteRegion(ctx, source, output, in.Region); err != nil {
		return schema.AudioAsset{}, err
	}
	asset.Duration, err = audio.Duration(output)
	if err != nil {
		_ = os.Remove(output)
		return schema.AudioAsset{}, err
	}
	return asset, nil
}

// DiscardAudioAsset 仅清理未进入工程的导出/分析副本；被引用的素材不能删除。
func (w *Workbench) DiscardAudioAsset(id string) error {
	if !schema.ValidID(id) {
		return fmt.Errorf("无效音频素材身份")
	}
	w.edit.Lock()
	defer w.edit.Unlock()
	name := "import-" + id + ".wav"
	if w.Store.Read().MediaRefs()["outputs"][name] {
		return fmt.Errorf("素材已被作品引用，不能作为临时副本清理")
	}
	path, err := w.Store.MediaPath("outputs", name)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
