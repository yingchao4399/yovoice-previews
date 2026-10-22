package workbench

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"slices"
	"strings"
	"time"
	"yovoice/internal/audio"
	"yovoice/internal/schema"
	"yovoice/internal/score"
)

type ArrangementMaterialFitInput struct {
	ID          string  `json:"id"`
	Offset      float64 `json:"offset"`
	Seconds     float64 `json:"seconds"`
	TailSeconds float64 `json:"tailSeconds"`
}

func (w *Workbench) checkGenerationMaterial(ctx context.Context, m schema.ArrangementMaterial, explicitFit bool) (schema.ArrangementMaterial, error) {
	c, r := m.Generation, m.GenerationResult
	if c == nil || r == nil {
		return m, fmt.Errorf("素材缺少生成合同，不能按新流程自动采用")
	}
	r.State = "checking"
	r.Checks = nil
	add := func(code, status, message string) {
		r.Checks = append(r.Checks, schema.GenerationCheck{Code: code, Status: status, Message: message})
	}
	path, err := w.Store.MediaPath("outputs", m.FileName)
	if err != nil {
		return m, err
	}
	q, qualityErr := audio.InspectSignal(path)
	if _, integrityErr := w.verifyGenerationOriginalIntegrity(m); integrityErr != nil {
		qualityErr = integrityErr
	}
	m.Peak, m.ClippedFraction, m.OnsetSeconds = q.Peak, q.ClippedFraction, q.OnsetSeconds
	if qualityErr != nil {
		add("SIGNAL", "fail", qualityErr.Error())
	} else {
		add("SIGNAL", "pass", "原片可读取且无严重静音或削波")
	}
	if qualityErr == nil && c.ExpectedOnsetSeconds != nil {
		tolerance := .35
		if c.Articulation == "pluck" || c.Articulation == "pizzicato" || c.Settings.SoundType == "percussion" {
			tolerance = .08
		}
		if math.Abs(max(0, q.OnsetSeconds-c.EffectiveOffset)-*c.ExpectedOnsetSeconds) > tolerance {
			add("ONSET", "review", fmt.Sprintf("有效区段起音约 %.2fs，曲谱首音 %.2fs，请选择合适区段或重生成；不会自动移动后续音符", max(0, q.OnsetSeconds-c.EffectiveOffset), *c.ExpectedOnsetSeconds))
		}
	}
	if r.ActualSeconds+.04 < c.EffectiveOffset+c.EffectiveSeconds+c.TailSeconds {
		add("DURATION", "fail", "原片不足以覆盖有效音乐区段及尾音，不能补静音或拉伸凑长")
	}
	if r.Truncated || !r.TruncationKnown && c.SemanticMax > 0 {
		if !explicitFit || c.EffectiveOffset+c.EffectiveSeconds+c.TailSeconds > r.ActualSeconds-.1 {
			add("TRUNCATED", "review", "语义流达到上限或未提供结束标志，须选择完整有效区段，保留截断边界前的安全余量后试听")
		}
	}
	if !explicitFit && r.ActualSeconds > c.RequestSeconds+math.Max(.5, c.RequestSeconds*.15) {
		add("OVERSIZE", "review", fmt.Sprintf("原片 %.2fs 超出请求 %.2fs，不能默认取开头", r.ActualSeconds, c.RequestSeconds))
	}
	if !explicitFit && r.ActualSeconds > c.CoreSeconds+c.TailSeconds+math.Max(.1, c.CoreSeconds*.05) {
		add("REGION_REQUIRED", "review", "原片长于所需乐句，请明确选择有效区段并试听，不能默认截取开头")
	}
	if explicitFit || c.Pitch != nil {
		if c.EffectiveOffset+c.EffectiveSeconds+c.TailSeconds <= r.ActualSeconds+.04 {
			converter, _ := audio.Converter()
			file := "import-" + schema.NewID() + ".wav"
			derived, e := w.Store.MediaPath("outputs", file)
			if e != nil {
				return m, e
			}
			if e = audio.ExtractInstrument(ctx, converter, path, derived, c.EffectiveOffset, c.EffectiveSeconds+c.TailSeconds); e != nil {
				return m, e
			}
			if c.Pitch != nil {
				measured, e := score.MeasureGeneratedSampleCore(derived, c.CoreSeconds)
				if e != nil {
					add("PITCH", "fail", e.Error())
				} else {
					p := measured.Pitch
					r.DetectedPitch = &p
					if p != *c.Pitch || math.Abs(measured.Cents) > 35 {
						add("PITCH", "fail", fmt.Sprintf("请求 MIDI %d，测得 MIDI %d（偏差 %.0f 音分），未登记错误根音", *c.Pitch, p, measured.Cents))
					} else {
						add("PITCH", "pass", fmt.Sprintf("稳定音高 MIDI %d，稳定窗口 %.0f%%", p, measured.StableFraction*100))
					}
					if c.Articulation == "pluck" || c.Articulation == "pizzicato" {
						if measured.Onsets > 1 {
							add("ATTACK", "fail", "挑弹候选存在多次明显起音，请检查是否生成了乐句")
						}
					}
					if c.Articulation != "pluck" && c.Articulation != "pizzicato" && c.Articulation != "staccato" && measured.ActiveSeconds < c.CoreSeconds*.75 {
						add("SUSTAIN", "fail", fmt.Sprintf("本句需要持续 %.2fs，核心区段仅约 %.2fs 达到有效信号阈值，不能用静音补齐长音", c.CoreSeconds, measured.ActiveSeconds))
					}
					if c.Articulation == "tremolo" {
						if measured.Onsets < 2 {
							add("ARTICULATION", "review", "未测得足够轮指起音，必须试听轮指质感，不能以普通挑弹冒充")
						}
					}
				}
			}
			// 每次适配创建新文件，保留旧文件供工程/历史引用管理，原片始终不变。
			r.DerivedFile = file
			r.DerivedSHA256, e = arrangementAudioDigest(derived)
			if e != nil {
				return m, e
			}
		}
	}
	if c.Pitch == nil && c.Settings.SoundType == "wordless" && c.Settings.Mode == "score" {
		selectedPath := path
		if r.DerivedFile != "" {
			selectedPath, err = w.Store.MediaPath("outputs", r.DerivedFile)
			if err != nil {
				return m, err
			}
		}
		ending, e := audio.InspectSignal(selectedPath)
		if e == nil && ending.EndRMS > math.Max(.004, ending.Peak*.08) {
			add("ENDING", "review", fmt.Sprintf("所选区段最后 0.1 秒仍有明显发声（RMS %.4f），尚未确认自然收尾；请增加尾音或生成余量，不能在最后一个音仍持续时直接采用", ending.EndRMS))
		}
	}
	add("IDENTITY", "review", "尚未识别乐器身份、杂入人声、音色一致性与音乐事件完整性；请试听确认")
	r.State = "needs_review"
	for _, check := range r.Checks {
		if check.Status == "fail" {
			r.State = "failed"
			break
		}
		if generationCheckNeedsFit(check) {
			r.State = "needs_fit"
		}
	}
	m.AlignmentWarning = ""
	for _, check := range r.Checks {
		if check.Status != "pass" {
			m.AlignmentWarning += check.Message + "；"
		}
	}
	return m, nil
}

func (w *Workbench) FitArrangementMaterial(in ArrangementMaterialFitInput) (schema.ArrangementMaterial, error) {
	w.generationRetry.Lock()
	defer w.generationRetry.Unlock()
	w.generationReview.Lock()
	defer w.generationReview.Unlock()
	state := w.Store.Read()
	i := slices.IndexFunc(state.ArrangementMaterials, func(m schema.ArrangementMaterial) bool { return m.ID == in.ID })
	if i < 0 {
		return schema.ArrangementMaterial{}, fmt.Errorf("找不到候选素材")
	}
	m := state.ArrangementMaterials[i]
	if m.RetryRequest != nil && (m.RetryRequest.Status == "queued" || m.RetryRequest.Status == "running") {
		return m, fmt.Errorf("此候选正在排队重做，请等待新候选，不能同时修改原片区段")
	}
	if m.Generation == nil || m.GenerationResult == nil {
		return m, fmt.Errorf("旧素材没有生成合同，请重新生成")
	}
	if m.ReviewStatus == "confirmed" {
		return m, fmt.Errorf("已采用素材保持不变，请另建候选或先撤销未被作品引用的确认")
	}
	c := *m.Generation
	r := *m.GenerationResult
	m.Generation = &c
	m.GenerationResult = &r
	if r.State == "interrupted" {
		path, e := w.Store.MediaPath("outputs", m.FileName)
		if e != nil {
			return m, e
		}
		actual, e := audio.Duration(path)
		if e != nil {
			return m, fmt.Errorf("中断任务没有完整原片，请重试本项")
		}
		m.Duration, r.ActualSeconds = actual, actual
	}
	if math.IsNaN(in.Offset) || math.IsNaN(in.Seconds) || math.IsNaN(in.TailSeconds) || in.Offset < 0 || in.Seconds <= 0 || in.TailSeconds < 0 || in.TailSeconds > 5 || math.IsInf(in.Offset, 0) || math.IsInf(in.Seconds, 0) {
		return m, fmt.Errorf("有效区段无效")
	}
	if math.Abs(in.Seconds-c.CoreSeconds) > .04 {
		return m, fmt.Errorf("有效长度必须覆盖合同所需的 %.2fs；不能静默修改曲谱", c.CoreSeconds)
	}
	if in.Offset+in.Seconds+in.TailSeconds > m.Duration+.001 {
		return m, fmt.Errorf("所选区段超出原片")
	}
	if c.Pitch != nil && in.Seconds+in.TailSeconds > 30 {
		return m, fmt.Errorf("单音采样最长 30 秒")
	}
	c.EffectiveOffset, c.EffectiveSeconds, c.TailSeconds = in.Offset, in.Seconds, in.TailSeconds
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	m, err := w.checkGenerationMaterial(ctx, m, true)
	if err != nil {
		return m, err
	}
	m.ReviewStatus = "needs_review"
	return m, w.persistGenerationMaterial(m)
}

func (w *Workbench) ReviewArrangementMaterial(id string, confirmed bool) error {
	w.generationRetry.Lock()
	defer w.generationRetry.Unlock()
	w.generationReview.Lock()
	defer w.generationReview.Unlock()
	state := w.Store.Read()
	i := slices.IndexFunc(state.ArrangementMaterials, func(m schema.ArrangementMaterial) bool { return m.ID == id })
	if i < 0 {
		return fmt.Errorf("AI 素材不存在")
	}
	m := state.ArrangementMaterials[i]
	if m.Generation == nil {
		return w.reviewArrangementMaterialLegacy(id, confirmed)
	}
	if confirmed && m.RetryRequest != nil && (m.RetryRequest.Status == "queued" || m.RetryRequest.Status == "running") {
		return fmt.Errorf("此候选已提交重新生成，请试听新候选后再采用")
	}
	if !confirmed {
		for _, source := range state.InstrumentSources {
			if !slices.ContainsFunc(source.Samples, func(n schema.InstrumentSample) bool { return n.GenerationID == id }) {
				continue
			}
			used := false
			for _, d := range state.Drafts {
				if d.Score != nil && slices.ContainsFunc(d.Score.Tracks, func(t schema.ScoreTrack) bool { return t.SourceID == source.ID }) {
					used = true
				}
				if d.Arrangement != nil {
					for _, s := range d.Arrangement.Sections {
						for _, p := range s.Phrases {
							if slices.ContainsFunc(p.Parts, func(a schema.ArrangementPart) bool { return a.SourceID == source.ID }) {
								used = true
							}
						}
					}
				}
			}
			for _, band := range state.BandPresets {
				if slices.ContainsFunc(band.Parts, func(p schema.BandPart) bool { return p.SourceID == source.ID }) {
					used = true
				}
			}
			if used {
				return fmt.Errorf("此音色已被作品或乐队组合引用，请另建候选，不能通过撤销确认破坏已采用的音色")
			}
		}
		if err := w.Store.Update(func(s *schema.State) {
			for i := range s.InstrumentSources {
				s.InstrumentSources[i].Samples = slices.DeleteFunc(s.InstrumentSources[i].Samples, func(n schema.InstrumentSample) bool { return n.GenerationID == id })
			}
		}, true); err != nil {
			return err
		}
		m.ReviewStatus = "needs_review"
		if m.GenerationResult != nil {
			m.GenerationResult.State = "needs_review"
		}
		return w.persistGenerationMaterial(m)
	}
	if m.ReviewStatus == "confirmed" && m.GenerationResult != nil && m.GenerationResult.State == "confirmed" {
		return nil
	}
	if m.GenerationResult == nil {
		return fmt.Errorf("候选缺少检查结果，不能确认")
	}
	if _, err := w.verifyGenerationMaterialIntegrity(m); err != nil {
		return err
	}
	// 技术检查结果只作为风险提示；最终是否采用由用户明确决定。
	// 即使系统没有稳定识别音高、检测到截断或用户没有在本机播放，也不能替用户锁死确认按钮。
	m.GenerationResult.Checks = append(m.GenerationResult.Checks, schema.GenerationCheck{Code: "USER_CONFIRMED", Status: "review", Message: "用户已明确确认采用；自动检查中的风险仍保留供追溯"})
	if m.Generation.Pitch != nil {
		// 根音、稳定性和身份检查用于提示风险，不替用户否决采用。
		// 只要原片或适配片通过完整性校验，就允许用户明确采用；未知根音
		// 仍会随检查记录保存，后续演奏时由音源映射和试听结果承担风险。
		if m.GenerationResult.DerivedFile == "" {
			return fmt.Errorf("单音候选缺少可采用的适配音频")
		}
		path, err := w.Store.MediaPath("outputs", m.GenerationResult.DerivedFile)
		if err != nil {
			return err
		}
		if m.GenerationResult.DerivedSHA256 != "" {
			digest, e := arrangementAudioDigest(path)
			if e != nil || digest != m.GenerationResult.DerivedSHA256 {
				return fmt.Errorf("待采用采样已变化，请重新检查并试听")
			}
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		// 同一乐器与模型的一组已确认采样逐步合并；不会冒用真实采样或模拟预设。
		sourceID := ""
		if m.Generation.TargetSourceID != "" {
			for _, s := range state.InstrumentSources {
				if s.ID == m.Generation.TargetSourceID && s.Origin == "ai" && s.Provenance == "generated:"+m.ModelID && arrangementSameInstrument(s.Instrument, m.Instrument) {
					sourceID = s.ID
					break
				}
			}
			if sourceID == "" {
				return fmt.Errorf("生成时固定的 AI 音色已变更，请重新检查绑定，未自动替换音色")
			}
		}
		for _, s := range state.InstrumentSources {
			if sourceID == "" && s.Origin == "ai" && s.Provenance == "generated:"+m.ModelID && arrangementSameInstrument(s.Instrument, m.Instrument) {
				sourceID = s.ID
				break
			}
		}
		source, err := w.ImportInstrumentSampleFrom(context.Background(), file, InstrumentSampleInput{SourceID: sourceID, Name: m.Instrument + " · AI 音色", Instrument: m.Instrument, Origin: "ai", Verification: "user", Author: m.ModelID, License: "模型生成素材，请核对模型许可", RootPitch: *m.Generation.Pitch, Articulation: m.Generation.Articulation, VelocityLow: m.Generation.VelocityLow, VelocityHigh: m.Generation.VelocityHigh})
		if err != nil {
			return err
		}
		m.GenerationResult.SourceID = source.ID
		if err = w.Store.Update(func(s *schema.State) {
			for i := range s.InstrumentSources {
				if s.InstrumentSources[i].ID == source.ID {
					s.InstrumentSources[i].Provenance = "generated:" + m.ModelID
					s.InstrumentSources[i].Verification = "user"
					last := len(s.InstrumentSources[i].Samples) - 1
					s.InstrumentSources[i].Samples[last].GenerationID = m.ID
					s.InstrumentSources[i].Samples[last].Generation = m.Generation
					s.InstrumentSources[i].Samples[last].GenerationPrompt = m.Prompt
					s.InstrumentSources[i].Samples[last].SustainSeconds = m.Generation.CoreSeconds
				}
			}
		}, true); err != nil {
			return err
		}
	}
	m.ReviewStatus = "confirmed"
	m.GenerationResult.State = "confirmed"
	return w.persistGenerationMaterial(m)
}

func arrangementAudioDigest(path string) (string, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return "", e
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

func (w *Workbench) verifyGenerationOriginalIntegrity(m schema.ArrangementMaterial) (string, error) {
	if m.Generation == nil || m.GenerationResult == nil {
		return "", fmt.Errorf("候选缺少生成记录，不能验证音频内容")
	}
	r := m.GenerationResult
	if r.RawSHA256 == "" {
		return "", fmt.Errorf("原片缺少内容校验摘要，请重新生成")
	}
	rawPath, err := w.Store.MediaPath("outputs", m.FileName)
	if err != nil {
		return "", err
	}
	rawDigest, err := arrangementAudioDigest(rawPath)
	if err != nil || rawDigest != r.RawSHA256 {
		return "", fmt.Errorf("原片已经变化或损坏，请重新生成")
	}
	return rawPath, nil
}

func (w *Workbench) verifyGenerationMaterialIntegrity(m schema.ArrangementMaterial) (string, error) {
	rawPath, err := w.verifyGenerationOriginalIntegrity(m)
	if err != nil {
		return "", err
	}
	r := m.GenerationResult
	if r.DerivedFile == "" {
		return rawPath, nil
	}
	if r.DerivedSHA256 == "" {
		return "", fmt.Errorf("适配试听文件缺少内容校验摘要，请重新检查")
	}
	derivedPath, err := w.Store.MediaPath("outputs", r.DerivedFile)
	if err != nil {
		return "", err
	}
	derivedDigest, err := arrangementAudioDigest(derivedPath)
	if err != nil || derivedDigest != r.DerivedSHA256 {
		return "", fmt.Errorf("适配试听文件已经变化或损坏，请重新检查")
	}
	return derivedPath, nil
}

func generationCheckNeedsFit(c schema.GenerationCheck) bool {
	return c.Status == "review" && c.Code != "IDENTITY" && c.Code != "ARTICULATION"
}

func generationMaterialUsable(m schema.ArrangementMaterial) bool {
	if m.Generation == nil {
		return m.ReviewStatus != "needs_review"
	}
	return m.Generation.Pitch == nil && m.ReviewStatus == "confirmed" && m.GenerationResult != nil && m.GenerationResult.State == "confirmed"
}

// 保留明确的失败候选；单项重试只清除该候选的复用资格，不删除原始文件。
func (w *Workbench) RetryArrangementMaterial(id string, d schema.Draft) error {
	_, err := w.RetryArrangementMaterialWithFeedback(id, d, "")
	return err
}

func (w *Workbench) RetryArrangementMaterialWithRound(id string, d schema.Draft) (int, error) {
	return w.RetryArrangementMaterialWithFeedback(id, d, "")
}

func (w *Workbench) RetryArrangementMaterialWithFeedback(id string, d schema.Draft, feedback string) (int, error) {
	w.generationRetry.Lock()
	defer w.generationRetry.Unlock()
	state := w.Store.Read()
	i := slices.IndexFunc(state.ArrangementMaterials, func(m schema.ArrangementMaterial) bool { return m.ID == id })
	if i < 0 {
		return 0, fmt.Errorf("素材不存在")
	}
	m := state.ArrangementMaterials[i]
	if m.ProjectID != d.ID || m.Generation == nil {
		return 0, fmt.Errorf("不能重试其他作品或旧格式素材")
	}
	ref := ArrangementPartRef{m.SectionIndex, m.PhraseIndex, m.PartIndex}
	if !arrangementMaterialMatches(m, d, ref) {
		return 0, fmt.Errorf("曲谱或设置已经变更，请使用当前声部重新准备")
	}
	if m.RetryRequest != nil && (m.RetryRequest.Status == "queued" || m.RetryRequest.Status == "running") {
		if m.RetryRequest.Status == "queued" {
			go w.resumeArrangementRetriesAfterOperation(d.ID, w.Done())
		}
		return m.RetryRequest.Round, nil
	}
	if m.GenerationResult == nil || m.GenerationResult.State != "failed" && m.GenerationResult.State != "needs_review" && m.GenerationResult.State != "needs_fit" {
		return 0, fmt.Errorf("只允许重试失败或待审核的 AI 候选")
	}
	cellKey := func(candidate schema.ArrangementMaterial) string {
		if candidate.Generation == nil || candidate.Generation.Pitch == nil {
			return "phrase"
		}
		return fmt.Sprintf("%d:%d:%s", *candidate.Generation.Pitch, candidate.Generation.VelocityLow, candidate.Generation.Articulation)
	}
	completedRounds := 0
	for _, candidate := range state.ArrangementMaterials {
		if candidate.ProjectID == d.ID && arrangementMaterialMatches(candidate, d, ref) && candidate.SectionIndex == m.SectionIndex && candidate.PhraseIndex == m.PhraseIndex && candidate.PartIndex == m.PartIndex && cellKey(candidate) == cellKey(m) && candidate.GenerationResult != nil {
			completedRounds = max(completedRounds, candidate.GenerationResult.RepairRound)
			if candidate.RetryRequest != nil {
				completedRounds = max(completedRounds, candidate.RetryRequest.Round)
			}
		}
	}
	if completedRounds >= 3 {
		return completedRounds, fmt.Errorf("该失败声部已完成三轮自主修复；第四轮需要先向用户说明情况并通过问答卡征求选择")
	}
	for _, check := range m.GenerationResult.Checks {
		if check.Status != "fail" {
			continue
		}
		if slices.Contains([]string{"DURATION", "TRUNCATED", "OVERSIZE", "REGION_REQUIRED", "IDENTITY", "ARTICULATION"}, check.Code) {
			return m.GenerationResult.RepairRound, fmt.Errorf("%s 属于合同、时长或内容适配问题，禁止原样重试；请先预检并提出需要用户决定的局部调整", check.Message)
		}
	}
	cell := ""
	if m.Generation.Pitch != nil {
		cell = fmt.Sprintf("%d:%d:%s", *m.Generation.Pitch, m.Generation.VelocityLow, m.Generation.Articulation)
	}
	round := completedRounds + 1
	feedback = strings.TrimSpace(feedback)
	if len([]rune(feedback)) > 1000 {
		return round, fmt.Errorf("自定义试听意见不能超过1000字")
	}
	if state.Activity != nil && state.Activity.Kind == "generate" && state.Activity.ProjectID == d.ID && state.Activity.Status == "running" {
		m.RetryRequest = &schema.ArrangementMaterialRetry{Status: "queued", Feedback: feedback, Round: round}
		if err := w.persistGenerationMaterial(m); err != nil {
			return round, err
		}
		go w.resumeArrangementRetriesAfterOperation(d.ID, w.Done())
		return round, nil
	}
	if err := w.PrepareArrangementAI(ArrangementAIPrepareInput{Draft: d, ModelID: m.ModelID, Target: &ref, Retry: true, RetryCell: cell, RepairRound: round, RetryFeedback: feedback}); err != nil {
		return round, err
	}
	m.GenerationResult.State = "rejected"
	if err := w.persistGenerationMaterial(m); err != nil {
		return round, err
	}
	return round, nil
}

// 仅供界面说明，未知内容不伪装成自动音频识别。
func generationReviewSummary(m schema.ArrangementMaterial) string {
	if m.GenerationResult == nil {
		return "旧素材"
	}
	return strings.Join([]string{m.GenerationResult.State, fmt.Sprint(m.CreatedAt.Format(time.RFC3339))}, " · ")
}
