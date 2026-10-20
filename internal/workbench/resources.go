package workbench

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"yovoice/internal/catalog"
	"yovoice/internal/schema"
)

// ResourceQuery 是界面与助手共用的库存查询。只查托管记录，不联网、不猜测路径。
type ResourceQuery struct {
	Query    string `json:"query,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Origin   string `json:"origin,omitempty"`
	ParentID string `json:"parentId,omitempty"`
	Offset   int    `json:"offset,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

type ResourceItem struct {
	ID           string            `json:"id"`
	Kind         string            `json:"kind"`
	Name         string            `json:"name"`
	ParentID     string            `json:"parentId,omitempty"`
	SourceID     string            `json:"sourceId,omitempty"`
	SampleID     string            `json:"sampleId,omitempty"`
	CatalogID    string            `json:"catalogId,omitempty"`
	Instrument   string            `json:"instrument,omitempty"`
	Origin       string            `json:"origin,omitempty"`
	Status       string            `json:"status"`
	Issue        string            `json:"issue,omitempty"`
	Usage        string            `json:"usage,omitempty"`
	MediaKind    string            `json:"mediaKind,omitempty"`
	FileName     string            `json:"fileName,omitempty"`
	Duration     float64           `json:"duration,omitempty"`
	RootPitch    *int              `json:"rootPitch,omitempty"`
	Articulation string            `json:"articulation,omitempty"`
	VelocityLow  int               `json:"velocityLow,omitempty"`
	VelocityHigh int               `json:"velocityHigh,omitempty"`
	License      string            `json:"license,omitempty"`
	Bytes        int64             `json:"bytes,omitempty"`
	Manifest     string            `json:"manifest,omitempty"`
	MemberIDs    []string          `json:"memberIds,omitempty"`
	BandParts    []schema.BandPart `json:"bandParts,omitempty"`
	AIModelID    string            `json:"aiModelId,omitempty"`
	LicenseURL   string            `json:"licenseUrl,omitempty"`
	SourceURL    string            `json:"sourceUrl,omitempty"`
	Author       string            `json:"author,omitempty"`
	SHA256       string            `json:"sha256,omitempty"`
}

type ResourcePage struct {
	Items      []ResourceItem `json:"items"`
	Total      int            `json:"total"`
	Offset     int            `json:"offset"`
	NextOffset *int           `json:"nextOffset,omitempty"`
}

func (w *Workbench) resourceFileIssue(kind, name string) string {
	p, err := w.Store.MediaPath(kind, name)
	if err != nil {
		return "FILE_MISSING"
	}
	i, err := os.Stat(p)
	if err != nil || !i.Mode().IsRegular() || i.Size() == 0 {
		return "FILE_MISSING"
	}
	return ""
}

// SearchResources 返回可追溯的资源身份；长音频只作为片段素材，不能伪装成逐音乐器。
func (w *Workbench) SearchResources(in ResourceQuery) (ResourcePage, error) {
	if in.Offset < 0 || in.Limit < 0 || len(in.Query) > 300 {
		return ResourcePage{}, fmt.Errorf("无效资源查询")
	}
	limit := in.Limit
	if limit == 0 {
		limit = 40
	}
	limit = min(limit, 100)
	s := w.Store.Read()
	items := []ResourceItem{}
	query := strings.ToLower(strings.TrimSpace(in.Query))
	add := func(item ResourceItem) {
		if in.Kind == "playable" && !slices.Contains([]string{"audio", "voice", "instrument", "sample"}, item.Kind) {
			return
		}
		if in.Kind != "" && in.Kind != "playable" && item.Kind != in.Kind || in.Origin != "" && item.Origin != in.Origin || in.ParentID != "" && item.ParentID != in.ParentID {
			return
		}
		if query != "" && !strings.Contains(strings.ToLower(item.Name+" "+item.Instrument+" "+item.Articulation+" "+item.ID), query) {
			return
		}
		items = append(items, item)
	}
	installed := map[string]string{}
	for _, source := range s.InstrumentSources {
		issue := w.sourceFilesIssue(source)
		if source.Plugin == nil && len(source.Samples) == 0 {
			issue = "SOURCE_EMPTY"
		}
		status := "ready"
		if issue != "" {
			status = "repair"
		} else if source.Verification == "metadata" {
			status = "review"
		}
		id := "source:" + source.ID
		if source.CatalogID != "" && status == "ready" {
			installed[source.CatalogID] = id
		}
		add(ResourceItem{ID: id, Kind: "instrument", Name: source.Name, SourceID: source.ID, CatalogID: source.CatalogID, Instrument: source.Instrument, Origin: source.Origin, Status: status, Issue: issue, Usage: "notes", License: source.License, LicenseURL: source.LicenseURL, SourceURL: source.SourceURL, Author: source.Author})
		for _, sample := range source.Samples {
			pitch := sample.RootPitch
			item := ResourceItem{ID: "sample:" + source.ID + "/" + sample.ID, Kind: "sample", ParentID: id, SourceID: source.ID, SampleID: sample.ID, Name: fmt.Sprintf("%s · MIDI %d · %s", source.Name, pitch, sample.Articulation), Instrument: source.Instrument, Origin: source.Origin, Status: status, Issue: issue, Usage: "notes", MediaKind: "instruments", FileName: sample.FileName, Duration: sample.Duration, RootPitch: &pitch, Articulation: sample.Articulation, VelocityLow: sample.VelocityLow, VelocityHigh: sample.VelocityHigh, License: source.License}
			item.LicenseURL, item.SourceURL, item.Author = source.LicenseURL, source.SourceURL, source.Author
			item.SHA256 = sample.SHA256
			// 某个同组采样损坏不应阻止试听/复用另一个完整采样；逐音演奏仍检查整件音源。
			item.Issue = ""
			if path, err := w.Store.MediaPath("instruments", sample.FileName); err != nil {
				item.Issue = "SOURCE_FILE_MISSING"
			} else {
				item.Issue = w.checkedInstrumentFile(path, sample.SHA256)
			}
			item.Status = "ready"
			if item.Issue != "" {
				item.Status = "repair"
			} else if source.Verification == "metadata" {
				item.Status = "review"
			}
			add(item)
		}
	}
	var library []xsxbLibraryEntry
	if err := json.Unmarshal(xsxbLibraryJSON, &library); err != nil {
		return ResourcePage{}, err
	}
	for _, entry := range library {
		if entry.License != "CC0-1.0" || len(entry.Samples) == 0 || slices.Contains([]string{"amps", "cabinets", "samples"}, entry.Family) {
			continue
		}
		item := ResourceItem{ID: "catalog:" + entry.ID, Kind: "catalog", CatalogID: entry.ID, Name: entry.Name, Instrument: entry.Name, Origin: "real", Status: "download", Usage: "notes", License: entry.License, Manifest: xsxbLibrarySHA256}
		for _, sample := range entry.Samples {
			item.Bytes += sample.Bytes
		}
		if parent := installed[entry.ID]; parent != "" {
			item.Status = "installed"
			item.ParentID = parent
		}
		add(item)
	}
	seenAudio := map[string]bool{}
	addAudio := func(id, name, file, origin string, duration float64, asset *schema.AudioAsset) {
		if seenAudio[file] {
			return
		}
		seenAudio[file] = true
		issue := w.resourceFileIssue("outputs", file)
		status := "ready"
		if issue != "" {
			status = "repair"
		}
		item := ResourceItem{ID: id, Kind: "audio", Name: name, Origin: origin, Status: status, Issue: issue, Usage: "clip", MediaKind: "outputs", FileName: file, Duration: duration}
		if asset != nil {
			item.License, item.LicenseURL, item.SourceURL, item.Author = asset.License, asset.LicenseURL, asset.SourceURL, asset.Author
		}
		add(item)
	}
	for _, material := range s.ArrangementMaterials {
		addAudio("material:"+material.ID, material.Instrument+" · "+material.Phrase, material.FileName, "ai", material.Duration, nil)
	}
	for _, g := range s.History {
		origin := "generated"
		if model, err := catalog.Lookup(g.Settings.ModelID); err == nil && catalog.IsMusic(model.Family) && g.Source != "score" && g.Settings.Kind != "score" {
			origin = "ai"
		}
		addAudio("generation:"+g.ID, g.Title, g.FileName, origin, g.Duration, nil)
	}
	for _, d := range s.Drafts {
		if d.Timeline != nil {
			for _, a := range d.Timeline.Assets {
				origin := a.Origin
				if origin == "" {
					origin = "imported"
				}
				addAudio("asset:"+d.ID+"/"+a.ID, a.Name, a.FileName, origin, a.Duration, &a)
			}
		}
	}
	for _, voice := range s.Voices {
		issue := w.resourceFileIssue("voices", voice.FileName)
		status := "ready"
		if issue != "" {
			status = "repair"
		}
		add(ResourceItem{ID: "voice:" + voice.ID, Kind: "voice", Name: voice.Name, Status: status, Issue: issue, Usage: "reference", MediaKind: "voices", FileName: voice.FileName, Duration: voice.Duration})
	}
	for _, band := range s.BandPresets {
		item := ResourceItem{ID: "band:" + band.ID, Kind: "band", Name: band.Name, Status: "ready", Usage: "preset", BandParts: band.Parts, AIModelID: band.AIModelID}
		for _, part := range band.Parts {
			if part.SourceID != "" {
				item.MemberIDs = append(item.MemberIDs, "source:"+part.SourceID)
			}
		}
		add(item)
	}
	for _, model := range catalog.Models {
		if model.Family != "yue2" && model.Family != "ace_step" {
			continue
		}
		status := "download"
		for _, installed := range s.Models {
			if installed.ID == model.ID {
				status = "ready"
				break
			}
		}
		add(ResourceItem{ID: "model:" + model.ID, Kind: "model", Name: model.Name, Origin: "ai", Status: status, Usage: "generate", Bytes: model.Size})
	}
	// 本地健康资源优先；目录候选仍明确标为待下载，绝不假装可直接播放。
	slices.SortStableFunc(items, func(a, b ResourceItem) int {
		rank := func(i ResourceItem) int {
			if i.Status == "ready" {
				return 0
			}
			if i.Status == "installed" {
				return 1
			}
			return 2
		}
		return rank(a) - rank(b)
	})
	page := ResourcePage{Items: []ResourceItem{}, Total: len(items), Offset: in.Offset}
	start := min(in.Offset, len(items))
	end := min(start+limit, len(items))
	page.Items = items[start:end]
	if end < len(items) {
		page.NextOffset = &end
	}
	return page, nil
}
