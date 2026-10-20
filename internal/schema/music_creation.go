package schema

import "strings"

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
	if !d.Instrumental && in.Voice == "reference" && (in.VoiceReferenceAssetID == "" || strings.TrimSpace(in.VoiceReferenceDescription) == "") {
		return "请选择参考音频并填写音色参考描述，当前音乐模型不支持直接音色克隆"
	}
	for _, id := range []string{in.VoiceReferenceAssetID, in.AccompanimentAssetID} {
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

// MusicStyle 只传语义演唱方向；参考描述不被当作已实现音色克隆。
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
