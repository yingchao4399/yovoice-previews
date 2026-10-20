package schema

import (
	"path"
	"strings"
)

// MediaRefs 是数据目录中音频文件的唯一引用登记：按目录（voices、outputs）返回仍被状态使用的文件名。
// 任何会物理删除 voices/ 或 outputs/ 文件的逻辑都必须以它为准；新增引用文件的字段时在这里登记，
// TestMediaRefsCoverEveryFileName 会检查 State 中所有 FileName 字段都已纳入。
func (s State) MediaRefs() map[string]map[string]bool {
	outputs, voices, instruments := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, source := range s.InstrumentSources {
		if source.Plugin != nil {
			for _, dep := range source.Plugin.Dependencies {
				name := path.Base(strings.ReplaceAll(dep.Path, "\\", "/"))
				if strings.HasSuffix(name, ".wav") {
					switch {
					case strings.HasPrefix(name, "sample-"):
						instruments[name] = true
					case strings.HasPrefix(name, "import-"):
						outputs[name] = true
					case strings.HasPrefix(name, "voice-"):
						voices[name] = true
					}
				}
			}
		}
		for _, sample := range source.Samples {
			instruments[sample.FileName] = true
		}
	}
	for _, g := range s.History {
		outputs[g.FileName] = true
	}
	for _, material := range s.ArrangementMaterials {
		outputs[material.FileName] = true
	}
	for _, c := range s.Characters {
		if c.Preview != nil {
			outputs[c.Preview.FileName] = true
		}
	}
	// 未保存的试听只在本次会话的内存中，同样不能删除。
	for _, p := range s.Previews {
		outputs[p.FileName] = true
	}
	drafts := append([]Draft{}, s.Drafts...)
	for _, job := range s.ArrangementJobs {
		drafts = append(drafts, job.Draft)
	}
	for _, d := range drafts {
		if d.Timeline != nil {
			for _, asset := range d.Timeline.Assets {
				outputs[asset.FileName] = true
			}
		}
	}
	for _, v := range s.Voices {
		voices[v.FileName] = true
	}
	delete(outputs, "")
	delete(voices, "")
	delete(instruments, "")
	return map[string]map[string]bool{"outputs": outputs, "voices": voices, "instruments": instruments}
}
