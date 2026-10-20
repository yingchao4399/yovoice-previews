package workbench

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"yovoice/internal/download"
	"yovoice/internal/schema"
)

// ResolveResource 只接受库存查询返回的身份，不能让模型拼接任意路径或 URL。
func (w *Workbench) ResolveResource(id string) (ResourceItem, error) {
	if len(id) > 180 || !strings.Contains(id, ":") {
		return ResourceItem{}, fmt.Errorf("无效资源身份")
	}
	page, err := w.SearchResources(ResourceQuery{Query: id, Limit: 100})
	if err != nil {
		return ResourceItem{}, err
	}
	for _, item := range page.Items {
		if item.ID == id {
			return item, nil
		}
	}
	return ResourceItem{}, fmt.Errorf("资源不存在，请重新查询库存")
}

type ResourceClipInput struct {
	Draft      schema.Draft `json:"draft"`
	ResourceID string       `json:"resourceId"`
	Start      float64      `json:"start"`
	Offset     float64      `json:"offset,omitempty"`
	Duration   float64      `json:"duration,omitempty"`
}

// ImportResourceAudio 创建独立的工程引用，不把参考音频自动混入时间线。
func (w *Workbench) ImportResourceAudio(ctx context.Context, id string) (schema.AudioAsset, error) {
	resource, err := w.ResolveResource(id)
	if err != nil {
		return schema.AudioAsset{}, err
	}
	if resource.Status != "ready" || resource.MediaKind == "" || resource.FileName == "" {
		return schema.AudioAsset{}, fmt.Errorf("该资源还不是可播放的本地音频：%s", resource.Status)
	}
	path, err := w.Store.MediaPath(resource.MediaKind, resource.FileName)
	if err != nil {
		return schema.AudioAsset{}, err
	}
	name := resource.Name
	if schema.TextLen(name) > 120 {
		name = string([]rune(name)[:120])
	}
	asset, err := w.importAudioAsset(ctx, path, name, true)
	if err != nil {
		return schema.AudioAsset{}, err
	}
	if resource.SHA256 != "" {
		copied, e := w.Store.MediaPath("outputs", asset.FileName)
		if e == nil {
			var hash string
			hash, e = download.Hash(ctx, copied)
			if e == nil && hash != resource.SHA256 {
				e = fmt.Errorf("复制期间采样发生变化，请重新选择")
			}
		}
		if e != nil {
			_ = os.Remove(copied)
			return schema.AudioAsset{}, e
		}
	}
	asset.Origin, asset.SourceResourceID = resource.Origin, resource.ID
	asset.License, asset.LicenseURL, asset.SourceURL, asset.Author = resource.License, resource.LicenseURL, resource.SourceURL, resource.Author
	return asset, nil
}

func resourcePolicyForDraft(d schema.Draft) string {
	if d.Arrangement != nil && d.Arrangement.SourceMode != "" {
		return d.Arrangement.SourceMode
	}
	if d.ArrangementInput != nil && d.ArrangementInput.Source != "" {
		return d.ArrangementInput.Source
	}
	return "auto"
}

// AddResourceClip 复用本地音频，保留原文件并登记新的工程素材引用。
// 它添加手工音轨，重拼合时不覆盖；不会将完整歌曲当作逐音采样乐器。
func (w *Workbench) AddResourceClip(ctx context.Context, in ResourceClipInput) (schema.Draft, error) {
	if !schema.InRange(in.Start, 0, 86400) || !schema.InRange(in.Offset, 0, 3600) || !schema.InRange(in.Duration, 0, 3600) {
		return schema.Draft{}, fmt.Errorf("音频放置范围无效")
	}
	resource, err := w.ResolveResource(in.ResourceID)
	if err != nil {
		return schema.Draft{}, err
	}
	if resource.Status != "ready" || resource.MediaKind == "" || resource.FileName == "" {
		return schema.Draft{}, fmt.Errorf("该资源还不是可播放的本地音频：%s", resource.Status)
	}
	var d schema.Draft
	b, _ := json.Marshal(in.Draft)
	_ = json.Unmarshal(b, &d)
	mode := resourcePolicyForDraft(d)
	if mode == "real" && resource.Origin != "real" || mode == "ai" && resource.Origin != "ai" {
		return d, fmt.Errorf("该音频的已知来源不符合当前真实／AI限制，请选择来源符合要求的素材")
	}
	current, err := w.assistantWork(d.ID)
	if err != nil {
		return d, err
	}
	if questionRevision(current) != questionRevision(d) {
		return d, fmt.Errorf("作品已变化，请保存并重新读取后再放置音频")
	}
	asset, err := w.ImportResourceAudio(ctx, in.ResourceID)
	if err != nil {
		return d, err
	}
	keep := false
	defer func() {
		if !keep {
			if p, e := w.Store.MediaPath("outputs", asset.FileName); e == nil {
				_ = os.Remove(p)
			}
		}
	}()
	if asset.Origin != resource.Origin {
		return d, fmt.Errorf("复制期间音频来源已变化，请重新查询库存")
	}
	duration := in.Duration
	if duration == 0 {
		duration = asset.Duration - in.Offset
	}
	if duration < .01 || in.Offset+duration > asset.Duration+.001 || in.Start+duration > 86400 {
		return d, fmt.Errorf("所选范围超出了原音频实际时长")
	}
	// 读取/转换音频不持有编辑锁，提交前重新检查，避免长音频阻塞保存或覆盖新编辑。
	w.edit.Lock()
	defer w.edit.Unlock()
	current, err = w.assistantWork(d.ID)
	if err != nil {
		return d, err
	}
	if questionRevision(current) != questionRevision(d) {
		return d, fmt.Errorf("处理音频期间作品已变化，请重新读取后再放置")
	}
	if d.Timeline == nil {
		d.Timeline = &schema.AudioTimeline{Tracks: []schema.AudioLane{}}
	}
	d.Timeline.Assets = append(d.Timeline.Assets, asset)
	d.Timeline.Tracks = append(d.Timeline.Tracks, schema.AudioLane{ID: schema.NewID(), Name: asset.Name, Clips: []schema.AudioClip{{ID: schema.NewID(), AssetID: asset.ID, Start: in.Start, Offset: in.Offset, Duration: duration}}})
	if err = w.saveDraft(d); err != nil {
		return d, err
	}
	keep = true
	w.chatUpdate(d.ID)
	return d, nil
}
