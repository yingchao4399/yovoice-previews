package workbench

import (
	"yovoice/internal/schema"
)

// 界面只需要插件身份、状态摘要和任务进度；预设二进制与执行快照留在服务端。
func (w *Workbench) PublicState() schema.State {
	s := w.Store.Read()
	for i := range s.InstrumentSources {
		source := &s.InstrumentSources[i]
		source.Health = "ready"
		issue := w.sourceFilesIssue(*source)
		if len(source.Samples) == 0 && source.Plugin == nil {
			issue = "SOURCE_FILE_MISSING"
		}
		if issue == "" && source.Verification == "metadata" || issue == "PLUGIN_PRESET_UNVERIFIED" {
			source.Health = "unverified"
		}
		if issue != "" && issue != "PLUGIN_PRESET_UNVERIFIED" {
			source.Health = "missing"
			if issue == "SOURCE_FILE_CORRUPT" || issue == "PLUGIN_VERSION_CHANGED" {
				source.Health = "corrupt"
			}
		}
		if source.Plugin != nil {
			source.Plugin.StateData = nil
		}
	}

	for i := range s.ArrangementJobs {
		s.ArrangementJobs[i].Draft = schema.Draft{}
	}
	for i := range s.ArrangementScores {
		s.ArrangementScores[i].Score = schema.Score{}
	}
	return s
}
