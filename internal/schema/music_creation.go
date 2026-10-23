package schema

import (
	"regexp"
	"strconv"
	"strings"
)

var musicStyleBPMPattern = regexp.MustCompile(`(?i)(?:^|[^0-9])([0-9]{1,3})\s*bpm(?:$|[^a-z])`)

// DurationIntent 保存创作目标；它不意味着模型会输出精确长度，定长导出另行处理。
type DurationIntent struct {
	Mode    string `json:"mode"`
	Seconds int    `json:"seconds,omitempty"`
}

type MusicCreationOptions struct {
	Duration                  DurationIntent `json:"duration"`
	Voice                     string         `json:"voice,omitempty"`
	VoiceReferenceAssetID     string         `json:"voiceReferenceAssetId,omitempty"`
	VoiceReferenceDescription string         `json:"voiceReferenceDescription,omitempty"`
	AccompanimentAssetID      string         `json:"accompanimentAssetId,omitempty"`
	AccompanimentDescription  string         `json:"accompanimentDescription,omitempty"`
}

func MusicCreationIssue(d Draft, family string) string {
	if d.Music == nil {
		return ""
	}
	in := d.Music
	if in.Duration.Mode != "auto" && in.Duration.Mode != "fixed" {
		return "请选择AI自动或固定目标时长"
	}
	if in.Duration.Mode == "fixed" && (in.Duration.Seconds < 15 || in.Duration.Seconds > 600) {
		return "目标时长需在15至600秒之间"
	}
	if family == "ace_step" && in.Duration.Mode == "fixed" && in.Duration.Seconds > 300 {
		return "当前ACE-Step适配支持最长300秒，请选择较短时长或使用其他模型"
	}
	if in.Voice != "" && in.Voice != "auto" && in.Voice != "male" && in.Voice != "female" && in.Voice != "reference" {
		return "无效的演唱音色选项"
	}
	if TextLen(in.VoiceReferenceDescription) > 1500 {
		return "参考音色描述过长"
	}
	if TextLen(in.AccompanimentDescription) > 1500 {
		return "参考配乐描述过长"
	}
	activeVoiceReference := !d.Instrumental && in.Voice == "reference" && in.VoiceReferenceAssetID != ""
	if activeVoiceReference && in.AccompanimentAssetID != "" {
		return "当前音乐模型一次只能接收一条参考音频，请保留参考人声或参考配乐其中一项"
	}
	if (activeVoiceReference || in.AccompanimentAssetID != "") && family != "ace_step" {
		return "当前所选模型不支持把参考音频作为生成条件，请切换到 ACE-Step；仅填写文字描述仍可用于风格引导"
	}
	if !d.Instrumental && in.Voice == "reference" {
		// 音频和文字是两种独立的参考条件：从声音库选了音频后不应再强制填写
		// 描述；只有两者都为空时才拦截，否则用户无法直接使用已选参考音频。
		if in.VoiceReferenceAssetID == "" && strings.TrimSpace(in.VoiceReferenceDescription) == "" {
			return "请选择参考音频，或填写希望参考的音色特点；ACE-Step 会将参考音频作为生成上下文，但不保证克隆原音色"
		}
	}
	voiceReferenceID := ""
	if activeVoiceReference {
		voiceReferenceID = in.VoiceReferenceAssetID
	}
	for _, id := range []string{voiceReferenceID, in.AccompanimentAssetID} {
		if id == "" {
			continue
		}
		found := false
		if d.Timeline != nil {
			for _, asset := range d.Timeline.Assets {
				if asset.ID == id {
					found = true
					break
				}
			}
		}
		if !found {
			return "参考音频已缺失，请重新选择"
		}
	}
	return ""
}

// MusicStyle 传风格与音色文字条件；音频参考由引擎通过 ACE-Step complete 路线单独提供。
func MusicStyle(d Draft) string {
	style := strings.TrimSpace(d.Text)
	if d.Music != nil && strings.TrimSpace(d.Music.AccompanimentDescription) != "" {
		style += "; accompaniment guidance: " + strings.TrimSpace(d.Music.AccompanimentDescription)
	}
	if d.Instrumental || d.Music == nil {
		return style
	}
	switch d.Music.Voice {
	case "male":
		style += ", male vocals"
	case "female":
		style += ", female vocals"
	case "reference":
		if description := strings.TrimSpace(d.Music.VoiceReferenceDescription); description != "" {
			style += "; vocal timbre guidance: " + description
		}
	}
	return style
}

// MusicStyleBPM extracts an explicitly written tempo from the user's style text.
// The style field is the natural place users enter values such as "60 BPM", but
// a model adapter still needs the numeric option to actually receive that value.
// Out-of-range values are ignored and are left to the model's automatic tempo
// selection; numeric model options remain authoritative when supplied separately.
func MusicStyleBPM(d Draft) (int, bool) {
	style := strings.TrimSpace(d.Text)
	match := musicStyleBPMPattern.FindStringSubmatch(style)
	if len(match) != 2 {
		return 0, false
	}
	value, err := strconv.Atoi(match[1])
	if err != nil || value < 30 || value > 300 {
		return 0, false
	}
	return value, true
}
