package workbench

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"yovoice/internal/msg"
	"yovoice/internal/schema"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// voiceSettingsInput 是右侧检查器里「声音设置」和各模型高级参数的全部可调项，作品与角色共用。
// 只改给出的字段；校验交给 SaveDraft / SaveCharacter（与界面保存同一套规则）。
type voiceSettingsInput struct {
	ModelID           *string                   `json:"modelId,omitempty" jsonschema:"模型 ID，先用 list_models 确认已安装"`
	VoiceID           *string                   `json:"voiceId,omitempty" jsonschema:"参考音色 ID，先用 list_voices 查询；空字符串清除"`
	VoiceMode         *string                   `json:"voiceMode,omitempty" jsonschema:"OmniVoice / Qwen3-TTS 的声音来源：design（用描述设计声音）或 clone（克隆参考音频）"`
	VoxMode           *string                   `json:"voxMode,omitempty" jsonschema:"VoxCPM2 的声音来源：design、clone 或 continuation（续写参考音频）"`
	VoiceDescription  *string                   `json:"voiceDescription,omitempty" jsonschema:"声音设计 / 语音方向的描述文字（OmniVoice 的属性组合、Qwen3-TTS 与 VoxCPM2 的声音设计描述等）"`
	ReferenceText     *string                   `json:"referenceText,omitempty" jsonschema:"参考音频对应的文字，克隆和续写需要"`
	Speaker           *string                   `json:"speaker,omitempty" jsonschema:"Qwen3-TTS CustomVoice 内置说话人，或 Kokoro 的音色名"`
	Mode              *string                   `json:"mode,omitempty" jsonschema:"IndexTTS 的声音来源：speaker、reference、vector 或 text（用文字描述情绪）"`
	EmotionVoiceID    *string                   `json:"emotionVoiceId,omitempty" jsonschema:"IndexTTS 的情绪参考音色 ID；空字符串清除"`
	EmotionText       *string                   `json:"emotionText,omitempty" jsonschema:"IndexTTS 的情绪描述文字"`
	InferEmotion      *bool                     `json:"inferEmotion,omitempty" jsonschema:"IndexTTS 根据正文自动识别情绪"`
	EmotionStrength   *float64                  `json:"emotionStrength,omitempty" jsonschema:"情绪强度 0-1"`
	Emotions          []float64                 `json:"emotions,omitempty" jsonschema:"IndexTTS 情绪向量，固定 8 个 0-1 的数：高兴、愤怒、悲伤、恐惧、反感、低落、惊讶、平静"`
	RandomEmotion     *bool                     `json:"randomEmotion,omitempty" jsonschema:"IndexTTS 随机情绪"`
	Language          *string                   `json:"language,omitempty" jsonschema:"IndexTTS 的文本语言：zh、en；2.5 另有 ja、es、ar"`
	SynthesisLanguage *string                   `json:"synthesisLanguage,omitempty" jsonschema:"OmniVoice、Qwen3-TTS、Kokoro 等的合成语言，如 auto、zh、en"`
	Speed             *float64                  `json:"speed,omitempty" jsonschema:"语速 0.5-2"`
	OmniSpeed         *float64                  `json:"omniSpeed,omitempty" jsonschema:"OmniVoice 的语速 0.5-2；0 表示按默认"`
	Seed              *int                      `json:"seed,omitempty" jsonschema:"随机种子 0-2147483647；传 -1 恢复每次随机"`
	Temperature       *float64                  `json:"temperature,omitempty" jsonschema:"采样温度 0.05-2"`
	TopP              *float64                  `json:"topP,omitempty" jsonschema:"top_p 0.01-1"`
	TopK              *int                      `json:"topK,omitempty" jsonschema:"top_k 1-200"`
	RepetitionPenalty *float64                  `json:"repetitionPenalty,omitempty" jsonschema:"重复惩罚 0.1-20"`
	MaxTokens         *int                      `json:"maxTokens,omitempty" jsonschema:"每段最大 token 50-4000"`
	IntervalSilenceMs *int                      `json:"intervalSilenceMs,omitempty" jsonschema:"句间停顿毫秒 0-2000"`
	DoSample          *bool                     `json:"doSample,omitempty" jsonschema:"启用采样"`
	NumBeams          *int                      `json:"numBeams,omitempty" jsonschema:"beam 数 1-10"`
	LengthPenalty     *float64                  `json:"lengthPenalty,omitempty" jsonschema:"长度惩罚 -2 到 2"`
	GuidanceScale     *float64                  `json:"guidanceScale,omitempty" jsonschema:"VoxCPM2 的引导强度 0.5-5"`
	InferenceSteps    *int                      `json:"inferenceSteps,omitempty" jsonschema:"VoxCPM2 的推理步数 0-50"`
	ModelOptions      map[string]map[string]any `json:"modelOptions,omitempty" jsonschema:"各模型家族的高级参数，形如 {\"omnivoice\":{\"num_inference_steps\":32}}；每个模型能用哪些字段、键与取值范围见 yovoice-voice skill 里该模型的参考文档，只改给出的键"`
}

// applyVoiceSettings 把补丁写进合成设置，返回改动了哪些项；校验交给保存时的统一规则。
func (w *Workbench) applyVoiceSettings(s *schema.SynthesisSettings, in voiceSettingsInput, kind string) ([]string, error) {
	var changed []string
	patch := func(name string) { changed = append(changed, name) }
	voice := func(value *string) (*string, error) {
		id := strings.TrimSpace(*value)
		if id != "" && !w.voiceExists(id) {
			return nil, fmt.Errorf("没有找到参考音色 %s，请先用 list_voices 查询", id)
		}
		return stringPtr(id), nil
	}
	if in.ModelID != nil {
		model := strings.TrimSpace(*in.ModelID)
		if model != "" {
			if e := w.assistantModelCheck(model, kind); e != nil {
				return nil, e
			}
		}
		s.ModelID = model
		patch("模型")
	}
	if in.VoiceID != nil {
		id, e := voice(in.VoiceID)
		if e != nil {
			return nil, e
		}
		s.VoiceID = id
		patch("音色")
	}
	if in.EmotionVoiceID != nil {
		id, e := voice(in.EmotionVoiceID)
		if e != nil {
			return nil, e
		}
		s.EmotionVoiceID = id
		patch("情绪参考音色")
	}
	text := func(target *string, value *string, name string) {
		if value != nil {
			*target = *value
			patch(name)
		}
	}
	text(&s.VoiceMode, in.VoiceMode, "声音来源")
	text(&s.VoxMode, in.VoxMode, "声音来源")
	text(&s.VoiceDescription, in.VoiceDescription, "声音设计描述")
	text(&s.ReferenceText, in.ReferenceText, "参考文字")
	text(&s.Speaker, in.Speaker, "说话人")
	text(&s.Mode, in.Mode, "声音来源")
	text(&s.EmotionText, in.EmotionText, "情绪描述")
	text(&s.Language, in.Language, "语言")
	text(&s.SynthesisLanguage, in.SynthesisLanguage, "合成语言")
	flag := func(target *bool, value *bool, name string) {
		if value != nil {
			*target = *value
			patch(name)
		}
	}
	flag(&s.InferEmotion, in.InferEmotion, "自动识别情绪")
	flag(&s.RandomEmotion, in.RandomEmotion, "随机情绪")
	flag(&s.DoSample, in.DoSample, "采样")
	number := func(target *float64, value *float64, name string) {
		if value != nil {
			*target = *value
			patch(name)
		}
	}
	number(&s.EmotionStrength, in.EmotionStrength, "情绪强度")
	number(&s.Speed, in.Speed, "语速")
	number(&s.OmniSpeed, in.OmniSpeed, "语速")
	number(&s.Temperature, in.Temperature, "温度")
	number(&s.TopP, in.TopP, "top_p")
	number(&s.RepetitionPenalty, in.RepetitionPenalty, "重复惩罚")
	number(&s.LengthPenalty, in.LengthPenalty, "长度惩罚")
	number(&s.GuidanceScale, in.GuidanceScale, "引导强度")
	integer := func(target *int, value *int, name string) {
		if value != nil {
			*target = *value
			patch(name)
		}
	}
	integer(&s.TopK, in.TopK, "top_k")
	integer(&s.MaxTokens, in.MaxTokens, "最大 token")
	integer(&s.IntervalSilenceMs, in.IntervalSilenceMs, "句间停顿")
	integer(&s.NumBeams, in.NumBeams, "beam 数")
	integer(&s.InferenceSteps, in.InferenceSteps, "推理步数")
	if in.Emotions != nil {
		if len(in.Emotions) != 8 {
			return nil, fmt.Errorf("emotions 必须是 8 个数")
		}
		s.Emotions = slices.Clone(in.Emotions)
		patch("情绪向量")
	}
	if in.Seed != nil {
		if *in.Seed < 0 {
			s.Seed = nil
		} else {
			s.Seed = in.Seed
		}
		patch("种子")
	}
	for family, options := range in.ModelOptions {
		if s.ModelOptions == nil {
			s.ModelOptions = map[string]map[string]any{}
		}
		merged := map[string]any{}
		for key, value := range s.ModelOptions[family] {
			merged[key] = value
		}
		for key, value := range options {
			merged[key] = value
		}
		s.ModelOptions[family] = merged
		patch("高级参数")
	}
	return slices.Compact(changed), nil
}

// characterSummary 是角色在工具结果里的简要形态。
func characterSummary(c schema.Character) map[string]any {
	out := map[string]any{"id": c.ID, "name": c.Name, "modelId": c.Settings.ModelID, "demoText": c.DemoText}
	if c.Settings.VoiceID != nil && *c.Settings.VoiceID != "" {
		out["voiceId"] = *c.Settings.VoiceID
	}
	if c.Settings.VoiceDescription != "" {
		out["voiceDescription"] = c.Settings.VoiceDescription
	}
	if len(c.Performances) > 0 {
		names := make([]string, 0, len(c.Performances))
		for _, p := range c.Performances {
			names = append(names, p.Name)
		}
		out["performances"] = names
	}
	return out
}

func (w *Workbench) listCharactersTool(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, any, error) {
	characters := w.Store.Read().Characters
	list := make([]map[string]any, 0, len(characters))
	for _, c := range characters {
		list = append(list, characterSummary(c))
	}
	w.chatCard("list_characters", fmt.Sprintf("查看了声音库的角色（%d 个）", len(list)), fmt.Sprintf("Viewed characters (%d)", len(list)), "done")
	return nil, map[string]any{"characters": list}, nil
}

type saveCharacterInput struct {
	ID       string              `json:"id,omitempty" jsonschema:"要修改的角色 ID；留空表示新建"`
	Name     *string             `json:"name,omitempty" jsonschema:"角色名；新建时必填"`
	DemoText *string             `json:"demoText,omitempty" jsonschema:"试听台词"`
	FromWork string              `json:"fromWork,omitempty" jsonschema:"新建时：以这个作品当前的声音设置为起点（模型、参考音色、声音设计描述、参数）"`
	Settings *voiceSettingsInput `json:"settings,omitempty" jsonschema:"声音设置，在起点上只改给出的项"`
}

// saveCharacterTool 新建或修改声音库里的角色，保存规则与界面一致；不会改动任何作品。
func (w *Workbench) saveCharacterTool(ctx context.Context, req *mcp.CallToolRequest, in saveCharacterInput) (*mcp.CallToolResult, any, error) {
	state := w.Store.Read()
	var c schema.Character
	created := in.ID == ""
	if created {
		if in.Name == nil || strings.TrimSpace(*in.Name) == "" {
			return nil, nil, fmt.Errorf("新建角色需要 name")
		}
		c = schema.Character{ID: schema.NewID(), DemoText: "你好，这是我的声音。"}
		base := schema.DefaultDraft()
		if in.FromWork != "" {
			d, err := w.assistantWork(in.FromWork)
			if err != nil {
				return nil, nil, err
			}
			base = d
		} else {
			// 没有起点时用已安装的语音模型，避免角色指向没装的模型。
			for _, m := range state.Models {
				if !strings.HasPrefix(m.ID, "ace-step-") && !strings.HasPrefix(m.ID, "yue2-") && !strings.HasSuffix(m.ID, "-sf2") {
					base.ModelID = m.ID
					break
				}
			}
		}
		c.Settings = base.SynthesisSettings
		// 角色是声音的配置，不带作品里的歌词、旋律谱这类内容。
		c.Settings.Lyrics, c.Settings.Abc, c.Settings.Instrumental = "", "", false
	} else {
		index := slices.IndexFunc(state.Characters, func(v schema.Character) bool { return v.ID == in.ID })
		if index < 0 {
			return nil, nil, fmt.Errorf("没有找到角色 %s，请先用 list_characters 查看", in.ID)
		}
		c = state.Characters[index]
		c.Settings = deepCopySettings(c.Settings)
		if in.FromWork != "" {
			return nil, nil, fmt.Errorf("fromWork 只用于新建角色；要改已有角色请直接给 settings")
		}
	}
	var changed []string
	if in.Name != nil {
		c.Name = strings.TrimSpace(*in.Name)
		changed = append(changed, "名称")
	}
	if in.DemoText != nil {
		c.DemoText = *in.DemoText
		changed = append(changed, "试听台词")
	}
	if in.Settings != nil {
		patched, err := w.applyVoiceSettings(&c.Settings, *in.Settings, "text")
		if err != nil {
			return nil, nil, err
		}
		changed = append(changed, patched...)
	}
	if !created && len(changed) == 0 {
		return nil, nil, fmt.Errorf("没有要修改的字段")
	}
	saved, err := w.SaveCharacter(c)
	if err != nil {
		return nil, nil, err
	}
	verb := "修改了"
	verbEN := "Updated"
	if created {
		verb, verbEN = "新建了", "Created"
	}
	w.chatCard("save_character", fmt.Sprintf("%s角色《%s》", verb, saved.Name), fmt.Sprintf("%s character %q", verbEN, saved.Name), "done")
	out := characterSummary(saved)
	if !created {
		out["changed"] = changed
	}
	return nil, out, nil
}

// deepCopySettings 复制合成设置里的引用字段，避免补丁改到状态里的同一份数据。
func deepCopySettings(s schema.SynthesisSettings) schema.SynthesisSettings {
	s.Emotions = slices.Clone(s.Emotions)
	if s.Seed != nil {
		seed := *s.Seed
		s.Seed = &seed
	}
	if s.ModelOptions != nil {
		options := make(map[string]map[string]any, len(s.ModelOptions))
		for family, values := range s.ModelOptions {
			copied := make(map[string]any, len(values))
			for key, value := range values {
				copied[key] = value
			}
			options[family] = copied
		}
		s.ModelOptions = options
	}
	return s
}

// applyCharacterToWork 与检查器里「选择角色」一致：作品用角色的声音设置，并记下角色，清掉单句演绎。
func applyCharacterToWork(d *schema.Draft, c schema.Character) {
	d.SynthesisSettings = deepCopySettings(c.Settings)
	d.CharacterID = c.ID
	d.Performance = nil
}

// findSpeakerIndex 按角色名或 1 开始的序号找到故事配音里的角色。
func findSpeakerIndex(sub *schema.SubtitleDocument, key string) (int, error) {
	key = strings.TrimSpace(key)
	for i, speaker := range sub.Speakers {
		ordinal := 0
		if _, e := fmt.Sscanf(key, "%d", &ordinal); e == nil && ordinal == i+1 || speaker.SourceName == key {
			return i, nil
		}
	}
	return -1, fmt.Errorf("%q 不在角色列表里", key)
}

// workSpeakerSetting 给故事配音里的一个角色套用声音库角色，或只改它自己的声音设置。
type workSpeakerSetting struct {
	Speaker     string              `json:"speaker" jsonschema:"角色名或 1 开始的序号"`
	CharacterID *string             `json:"characterId,omitempty" jsonschema:"套用声音库里的角色（list_characters），会替换该角色当前的声音设置"`
	Settings    *voiceSettingsInput `json:"settings,omitempty" jsonschema:"在此基础上只改给出的声音设置"`
}

// applySpeakerSettings 先套用角色再打补丁；每个角色的设置独立于作品级设置。
func (w *Workbench) applySpeakerSettings(d *schema.Draft, items []workSpeakerSetting, characters []schema.Character) ([]string, error) {
	if d.Subtitles == nil {
		return nil, fmt.Errorf("speakerSettings 只适用于已有角色的故事配音作品")
	}
	var changed []string
	for _, item := range items {
		index, err := findSpeakerIndex(d.Subtitles, item.Speaker)
		if err != nil {
			return nil, fmt.Errorf("speakerSettings 里的%v", err)
		}
		speaker := &d.Subtitles.Speakers[index]
		if item.CharacterID != nil {
			character := slices.IndexFunc(characters, func(c schema.Character) bool { return c.ID == *item.CharacterID })
			if character < 0 {
				return nil, fmt.Errorf("没有找到角色 %s，请先用 list_characters 查看", *item.CharacterID)
			}
			settings := deepCopySettings(characters[character].Settings)
			speaker.CharacterID, speaker.Settings = characters[character].ID, &settings
			// 角色换了声音，该角色台词上的单句演绎不再适用。
			for i := range d.Subtitles.Cues {
				if d.Subtitles.Cues[i].SpeakerID == speaker.ID {
					d.Subtitles.Cues[i].Performance = nil
				}
			}
			changed = append(changed, "角色声音")
		}
		if item.Settings != nil {
			if speaker.Settings == nil {
				settings := deepCopySettings(d.SynthesisSettings)
				speaker.Settings = &settings
			}
			patched, err := w.applyVoiceSettings(speaker.Settings, *item.Settings, "story")
			if err != nil {
				return nil, err
			}
			changed = append(changed, patched...)
		}
	}
	return slices.Compact(changed), nil
}

// 这些错误只说明配置还没填全（缺参考音频、缺描述），编辑过程中允许存在，到生成时再提示。
var incompleteSettings = []msg.Code{msg.ErrOmniReferenceRequired, msg.ErrBreezeReferenceRequired, msg.ErrVoxReferenceRequired, msg.ErrVoiceDescriptionRequired, msg.ErrTextRequired, msg.ErrTitleLength}

// voiceSettingsError 在助手改声音设置时提前挡住越界或不合法的取值，免得到生成时才失败；
// 配置没填全不算错误。作品里每一份设置（含故事配音各角色）都检查。
func voiceSettingsError(d schema.Draft) error {
	for _, settings := range d.AllSettings() {
		probe := d
		probe.SynthesisSettings = settings
		if strings.TrimSpace(probe.Text) == "" {
			probe.Text = "试听"
		}
		if strings.TrimSpace(probe.Title) == "" {
			probe.Title = "作品"
		}
		err := schema.Validate(probe)
		if err == nil {
			continue
		}
		code, _ := msg.Encode(err)["code"].(msg.Code)
		if slices.Contains(incompleteSettings, code) {
			continue
		}
		return fmt.Errorf("声音设置不合法（%s）：请对照取值范围修改后重试，各模型的可用字段与范围见 yovoice-voice skill 里该模型的参考文档", strings.TrimPrefix(string(code), "@yovoice."))
	}
	return nil
}
