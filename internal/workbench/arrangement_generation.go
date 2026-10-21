package workbench

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"yovoice/internal/catalog"
	"yovoice/internal/schema"
	"yovoice/internal/score"
)

const arrangementAdapterVersion = schema.ArrangementGenerationVersion

type ArrangementGenerationCapability struct {
	ModelID                       string   `json:"modelId"`
	Family                        string   `json:"family"`
	EngineVersion                 string   `json:"engineVersion"`
	MinDuration                   float64  `json:"minDuration"`
	MaxDuration                   float64  `json:"maxDuration"`
	DurationGranularity           float64  `json:"durationGranularity"`
	ScoreConditioning             bool     `json:"scoreConditioning"`
	IsolatedInstrumentReliability string   `json:"isolatedInstrumentReliability"`
	VocalMode                     []string `json:"vocalMode"`
	Continuation                  bool     `json:"continuation"`
	SeedControl                   bool     `json:"seedControl"`
	Evidence                      string   `json:"evidence"`
}

type ArrangementGenerationPreflight struct {
	Warnings         []string                             `json:"warnings"`
	Settings         schema.ArrangementGenerationSettings `json:"settings"`
	Capability       ArrangementGenerationCapability      `json:"capability"`
	Contract         schema.ArrangementGenerationContract `json:"contract"`
	Cells            []ArrangementSampleCell              `json:"cells"`
	Reused           int                                  `json:"reused"`
	Candidates       int                                  `json:"candidates"`
	Issues           []string                             `json:"issues"`
	EstimatedSeconds *float64                             `json:"estimatedSeconds"`
}

type ArrangementSampleCell struct {
	Key            string  `json:"key"`
	Pitch          int     `json:"pitch"`
	VelocityLow    int     `json:"velocityLow"`
	VelocityHigh   int     `json:"velocityHigh"`
	Articulation   string  `json:"articulation"`
	SustainSeconds float64 `json:"sustainSeconds"`
	SourceID       string  `json:"sourceId,omitempty"`
	SampleID       string  `json:"sampleId,omitempty"`
	MaterialID     string  `json:"materialId,omitempty"`
	Pilot          bool    `json:"pilot"`
	State          string  `json:"state"`
}

func generationSettings(p schema.ArrangementPart) schema.ArrangementGenerationSettings {
	s := schema.ArrangementGenerationSettings{AssetKind: "instrument", Mode: "score", Method: "sampler", LengthMode: "phrase", SoundType: "instrument", TailSeconds: .5}
	name := strings.ToLower(p.Instrument)
	if strings.Contains(name, "合唱") || strings.Contains(name, "人声") || strings.Contains(name, "choir") || strings.Contains(name, "vocal") {
		s.SoundType = "wordless"
		s.AssetKind = "phrase"
		s.Mode = "score"
		s.Method = "direct"
	}
	if p.Role == "drums" {
		s.SoundType = "percussion"
		s.AssetKind = "phrase"
		s.Mode = "free"
		s.Method = "direct"
	}
	if p.Generation != nil {
		s = *p.Generation
	}
	return s
}

func validateGenerationSettings(s schema.ArrangementGenerationSettings) error {
	if !slices.Contains([]string{"phrase", "instrument"}, s.AssetKind) || !slices.Contains([]string{"score", "free"}, s.Mode) || !slices.Contains([]string{"sampler", "direct"}, s.Method) || !slices.Contains([]string{"phrase", "bars", "seconds"}, s.LengthMode) || !slices.Contains([]string{"instrument", "wordless", "percussion", "effect"}, s.SoundType) || math.IsNaN(s.Seconds) || math.IsInf(s.Seconds, 0) || math.IsNaN(s.TailSeconds) || s.TailSeconds < 0 || s.TailSeconds > 5 {
		return fmt.Errorf("生成设置无效")
	}
	if s.AssetKind == "instrument" && (s.Mode != "score" || s.Method != "sampler") {
		return fmt.Errorf("可演奏音色必须按谱由采样器演奏；自由音频请选择独立乐句")
	}
	if s.Mode == "free" && s.Method != "direct" {
		return fmt.Errorf("自由模式只能生成独立音频，不能宣称按谱演奏")
	}
	if s.Method == "sampler" && (s.SoundType == "percussion" || s.SoundType == "effect") {
		return fmt.Errorf("本轮打击和环境音请使用独立乐句，未验证的非音高采样不会登记为旋律音色")
	}
	if s.Method == "sampler" && s.SoundType == "wordless" {
		return fmt.Errorf("无词人声只能作为独立乐句试听和采用，不能登记为可演奏器乐音色")
	}
	if s.Mode == "score" && (s.SoundType == "percussion" || s.SoundType == "effect") {
		return fmt.Errorf("打击与环境音当前不使用单音旋律 ABC，请选择自由乐句，保留节奏、配器与长度意图")
	}
	if s.LengthMode == "seconds" && (s.Seconds <= 0 || s.Seconds > 300) || s.LengthMode == "bars" && (s.Bars < 1 || s.Bars > 128) {
		return fmt.Errorf("自定义生成长度无效")
	}
	return nil
}

func generationCapability(modelID, version string) (ArrangementGenerationCapability, error) {
	m, err := catalog.Lookup(modelID)
	if err != nil {
		return ArrangementGenerationCapability{}, err
	}
	c := ArrangementGenerationCapability{ModelID: modelID, Family: m.Family, EngineVersion: version, MaxDuration: 300, SeedControl: true, VocalMode: []string{"instrument", "wordless"}, IsolatedInstrumentReliability: "unverified", Evidence: "未通过当前任务的实机生成与试听；模型可接收参数不代表能生成合格音色"}
	switch m.Family {
	case "yue2":
		c.MinDuration = .04
		c.DurationGranularity = .04
		c.ScoreConditioning = true
		if version != "v0.9.1" {
			return c, fmt.Errorf("当前引擎未验证编曲语义长度控制，请使用 v0.9.1")
		}
		if modelID == "yue2-3b-q8" {
			c.IsolatedInstrumentReliability = "requires_review"
			c.Evidence = "本机 Q8 已验证语义帧预算会生效；旧 melody 路线存在截断与错八度。编曲已改用 full 谱条件，每项仍须核验实际音高、选择完整区段并试听，不能把传入曲谱等同于准确演奏。"
		}
	case "ace_step":
		c.MinDuration = 10
		c.DurationGranularity = 1
		if modelID == "ace-step-1.5-turbo-q8" {
			c.Evidence = "本机 Q8 已返回 10 秒原片，但尚未确认独立音色；9 秒轮指实测出现 VAE 显存分配失败。8GB 显存机器请逐项运行，不宣称 300 秒上限已通过实测。"
		}
	default:
		return c, fmt.Errorf("请选择 YuE2 或 ACE-Step 音频模型；线上语言模型只负责规划和写谱")
	}
	return c, nil
}

func (w *Workbench) ArrangementGenerationPreflight(in ArrangementAIPrepareInput) (ArrangementGenerationPreflight, error) {
	r := ArrangementGenerationPreflight{Cells: []ArrangementSampleCell{}, Issues: []string{}}
	if in.Draft.Arrangement == nil || in.Target == nil {
		return r, fmt.Errorf("请选择具体 AI 声部")
	}
	jobs, err := arrangementAIJobs(*in.Draft.Arrangement, in.Target)
	if err != nil {
		return r, err
	}
	job := jobs[0]
	r.Settings = generationSettings(job.part)
	if err = validateGenerationSettings(r.Settings); err != nil {
		return r, err
	}
	modelID := r.Settings.ModelID
	if modelID == "" {
		modelID = in.ModelID
	}
	if modelID == "" {
		modelID = in.Draft.Arrangement.AIModelID
	}
	state := w.Store.Read()
	version := ""
	if state.RuntimeVersion != nil {
		version = *state.RuntimeVersion
	}
	r.Capability, err = generationCapability(modelID, version)
	if err != nil {
		return r, err
	}
	if r.Settings.Method == "direct" && r.Settings.Mode == "score" && !r.Capability.ScoreConditioning {
		r.Issues = append(r.Issues, "当前模型适配器不接收逐音符曲谱；请选择已支持谱条件的模型，或改为音色按谱演奏／自由乐句。不会忽略曲谱继续生成")
	}
	core := job.seconds
	if in.Draft.Score != nil {
		core = in.Draft.Score.Seconds(job.phrase.EndBar+1, 1) - in.Draft.Score.Seconds(job.phrase.StartBar, 1)
	}
	target := core
	if r.Settings.LengthMode == "seconds" {
		target = r.Settings.Seconds
	}
	if r.Settings.LengthMode == "bars" {
		target = float64(r.Settings.Bars) * float64(in.Draft.Arrangement.TimeSignature[0]) * 4 / float64(in.Draft.Arrangement.TimeSignature[1]) * 60 / in.Draft.Arrangement.Tempo
	}
	if r.Settings.Mode == "score" && target+.04 < core {
		r.Issues = append(r.Issues, fmt.Sprintf("自定义长度 %.2fs 小于本句 %.2fs，不能覆盖已审核曲谱；请调整长度或另存自由素材", target, core))
	}
	if r.Settings.Method == "sampler" && math.Abs(target-core) > .04 {
		r.Issues = append(r.Issues, "采样器按原曲谱演奏，不能通过生成长度改动乐句网格；请使用跟随本句，或先修改曲谱")
	}
	if r.Settings.Mode == "free" && math.Abs(target-core) > .04 {
		r.Warnings = append(r.Warnings, "自由素材长度与已审核乐句网格不同；可入库后手工放置，不会自动改谱或裁掉内容")
	}
	key, err := score.NormalizeKey(in.Draft.Arrangement.Key)
	if err != nil {
		r.Issues = append(r.Issues, err.Error())
	}
	if strings.ContainsAny(in.Draft.Arrangement.Key, "（(") {
		r.Warnings = append(r.Warnings, "保留原始调式声明；模型记谱上下文使用括号中明确声明的大小调，不宣称两种调式完全等价，实际音高仍以原曲谱为准")
	}
	if r.Capability.Family == "ace_step" && !slices.Contains([]int{2, 3, 4, 6}, in.Draft.Arrangement.TimeSignature[0]) {
		r.Warnings = append(r.Warnings, "当前 ACE-Step 接口不能准确传递此拍号，按自由音频候选处理，不能宣称精确跟随小节网格")
	}
	r.Contract = schema.ArrangementGenerationContract{Version: arrangementAdapterVersion, Settings: r.Settings, PartID: job.part.ID, ScoreHash: arrangementConditionHash(in.Draft, job.ref), CoreSeconds: target, TailSeconds: r.Settings.TailSeconds, EffectiveSeconds: target, ModelID: modelID, EngineVersion: version, OriginalKey: in.Draft.Arrangement.Key, NormalizedKey: key}
	setGenerationBudget(&r.Contract, r.Capability)
	r.Contract.TargetSourceID = job.part.SourceID
	if in.Draft.Score != nil && r.Settings.Mode == "score" {
		units, e := arrangementUnits(*in.Draft.Arrangement, *in.Draft.Score)
		if e != nil {
			return r, e
		}
		for _, u := range units {
			if u.SectionIndex == job.ref.SectionIndex && u.PhraseIndex == job.ref.PhraseIndex && u.PartIndex == job.ref.PartIndex {
				first := core
				for _, n := range u.Track.Notes {
					first = min(first, in.Draft.Score.Seconds(n.Bar, n.Beat)-u.Start)
				}
				r.Contract.ExpectedOnsetSeconds = &first
				break
			}
		}
	}
	if definition, e := catalog.Lookup(modelID); e == nil {
		r.Contract.ModelRevision = definition.Revision
		r.Contract.ModelDigest = definition.SHA256
		if r.Contract.ModelRevision == "" {
			r.Contract.ModelRevision = catalog.Revision
		}
	}
	if r.Contract.RequestSeconds > r.Capability.MaxDuration {
		r.Issues = append(r.Issues, "音乐有效长度加尾音超过当前请求上限，请缩短本句或拆分任务")
	}
	if r.Settings.Method == "sampler" {
		if in.Draft.Score == nil {
			return r, fmt.Errorf("按谱采样前请先完成曲谱")
		}
		units, e := arrangementUnits(*in.Draft.Arrangement, *in.Draft.Score)
		if e != nil {
			return r, e
		}
		for _, u := range units {
			if u.SectionIndex != job.ref.SectionIndex || u.PhraseIndex != job.ref.PhraseIndex || u.PartIndex != job.ref.PartIndex {
				continue
			}
			for _, n := range u.Track.Notes {
				low, high := velocityLayer(n.Velocity)
				art := sampleTechnique(job.part, n, schema.InstrumentSource{})
				if len(requiredArticulations(job.part.Technique)) > 1 && n.Articulation == "" {
					r.Issues = append(r.Issues, fmt.Sprintf("第%d小节包含多种奏法意图，请先为每个音符指定挑弹或轮指等奏法", n.Bar))
					continue
				}
				length := in.Draft.Score.Seconds(n.Bar, n.Beat+n.Length) - in.Draft.Score.Seconds(n.Bar, n.Beat)
				k := fmt.Sprintf("%d:%d:%s", n.Pitch, low, art)
				i := slices.IndexFunc(r.Cells, func(c ArrangementSampleCell) bool { return c.Key == k })
				if i >= 0 {
					r.Cells[i].SustainSeconds = max(r.Cells[i].SustainSeconds, length)
				} else {
					r.Cells = append(r.Cells, ArrangementSampleCell{Key: k, Pitch: n.Pitch, VelocityLow: low, VelocityHigh: high, Articulation: art, SustainSeconds: length, State: "missing"})
				}
			}
		}
		for i := range r.Cells {
			c := &r.Cells[i]
			for _, s := range state.InstrumentSources {
				if s.Origin != "ai" || s.Verification != "user" || !arrangementSameInstrument(s.Instrument, job.part.Instrument) || w.sourceFilesIssue(s) != "" || (job.part.SourceID != "" && s.ID != job.part.SourceID) {
					continue
				}
				for _, sample := range s.Samples {
					if sample.RootPitch == c.Pitch && sample.Articulation == c.Articulation && sample.VelocityLow <= c.VelocityLow && sample.VelocityHigh >= c.VelocityHigh && sample.Duration >= c.SustainSeconds+r.Settings.TailSeconds && (sample.GenerationID == "" || sample.SustainSeconds >= c.SustainSeconds) {
						c.SourceID = s.ID
						c.SampleID = sample.ID
						c.State = "reused"
						break
					}
				}
				if c.State == "reused" {
					break
				}
			}
			if c.State == "reused" {
				r.Reused++
				continue
			}
			for j := len(state.ArrangementMaterials) - 1; j >= 0; j-- {
				m := state.ArrangementMaterials[j]
				if m.ProjectID != in.Draft.ID || m.PartID != job.part.ID || m.Technique != job.part.Technique || m.Generation == nil || m.Generation.TargetSourceID != job.part.SourceID || m.GenerationResultState() == "rejected" || m.Generation.Version != arrangementAdapterVersion || m.Generation.EngineVersion != version || m.Generation.Settings.SoundType != r.Settings.SoundType || m.Generation.Pitch == nil || !arrangementSameInstrument(m.Instrument, job.part.Instrument) || m.Generation.ModelID != modelID || *m.Generation.Pitch != c.Pitch || m.Generation.Articulation != c.Articulation || m.Generation.VelocityLow != c.VelocityLow || m.Generation.CoreSeconds+.001 < c.SustainSeconds {
					continue
				}
				c.MaterialID = m.ID
				c.State = m.GenerationResultState()
				break
			}
		}
		seen := map[string]bool{}
		for i := range r.Cells {
			c := &r.Cells[i]
			if !seen[c.Articulation] {
				c.Pilot = true
				seen[c.Articulation] = true
			}
		}
		if len(r.Cells) == 0 {
			r.Issues = append(r.Issues, "本句没有需要演奏的音符")
		}
		for _, c := range r.Cells {
			if c.SustainSeconds+r.Settings.TailSeconds > 30 {
				r.Issues = append(r.Issues, "本轮单音采样最长 30 秒；长音请缩短音符或改用独立乐句")
			}
		}
		if job.part.SourceID != "" && r.Reused < len(r.Cells) {
			i := slices.IndexFunc(state.InstrumentSources, func(s schema.InstrumentSource) bool { return s.ID == job.part.SourceID })
			if i < 0 || state.InstrumentSources[i].Origin != "ai" || state.InstrumentSources[i].Plugin != nil || state.InstrumentSources[i].Provenance != "generated:"+modelID {
				r.Issues = append(r.Issues, "当前固定音色不能由所选模型补齐，请先解除固定并选择可补充的 AI 音色；不会偷偷换成其他音源")
			}
		}
	}
	r.Candidates = (len(r.Cells) - r.Reused) * 3
	if r.Settings.Method == "direct" {
		r.Candidates = 3
	}
	return r, nil
}

func velocityLayer(velocity int) (int, int) {
	if velocity <= 42 {
		return 1, 42
	}
	if velocity <= 85 {
		return 43, 85
	}
	return 86, 127
}

func setGenerationBudget(c *schema.ArrangementGenerationContract, cap ArrangementGenerationCapability) {
	c.RequestSeconds = math.Ceil(max(cap.MinDuration, c.CoreSeconds+c.TailSeconds)/cap.DurationGranularity) * cap.DurationGranularity
	if cap.Family == "yue2" {
		c.SemanticMin = int(math.Ceil(c.CoreSeconds * 25))
		// 请求长度已量化到整数帧；再 ceil 会把 2.24*25 的浮点尾数误算成 57 帧。
		c.SemanticMax = int(math.Round(c.RequestSeconds * 25))
	}
}
