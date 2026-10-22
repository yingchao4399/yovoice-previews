package engine

import (
	"errors"
	"strings"
	"testing"
	"yovoice/internal/msg"
	"yovoice/internal/schema"
)

func TestMusicDurationUsesNativeACEAndAdvisoryYuE(t *testing.T) {
	d := schema.DefaultDraft()
	d.Kind = "music"
	d.Text = "traditional folk"
	d.Instrumental = true
	d.ModelID = "ace-step-1.5-turbo-bf16"
	d.Music = &schema.MusicCreationOptions{Duration: schema.DurationIntent{Mode: "fixed", Seconds: 90}, Voice: "female"}
	r, e := BuildRequest(d, "", "")
	if e != nil {
		t.Fatal(e)
	}
	req := r["request"].(map[string]any)
	options := req["options"].(map[string]any)
	if options["duration_seconds"] != 90 || strings.Contains(req["text"].(string), "female vocals") {
		t.Fatal("时长没有进入原生参数或纯音乐混入演唱约束")
	}
	d.Music.Duration.Mode = "auto"
	r, e = BuildRequest(d, "", "")
	if e != nil {
		t.Fatal(e)
	}
	if _, exists := r["request"].(map[string]any)["options"].(map[string]any)["duration_seconds"]; exists {
		t.Fatal("AI自动不应伪造固定时长")
	}
	d.ModelID = "yue2-3b-q8"
	d.Music.Duration = schema.DurationIntent{Mode: "fixed", Seconds: 90}
	d.Instrumental = false
	d.Lyrics = "[Verse]\n山间的风轻轻吹"
	r, e = BuildRequest(d, "", "")
	if e != nil {
		t.Fatal(e)
	}
	options = r["request"].(map[string]any)["options"].(map[string]any)
	if _, exists := options["duration_seconds"]; exists {
		t.Fatal("YuE不支持秒数参数，不能冒充原生时长控制")
	}
	if !strings.Contains(options["style"].(string), "90 seconds") || !strings.Contains(options["style"].(string), "female vocals") {
		t.Fatal("规划目标和演唱方向未传入")
	}
	d.ModelID = "ace-step-1.5-turbo-bf16"
	d.Music.Duration.Seconds = 480
	if _, e = BuildRequest(d, "", ""); e == nil {
		t.Fatal("不支持的时长不能静默缩到300秒")
	}
}

func TestACEPromotesStyleBPMAndKeepsAutoDuration(t *testing.T) {
	d := schema.DefaultDraft()
	d.Kind, d.ModelID, d.Text, d.Lyrics = "music", "ace-step-1.5-turbo-bf16", "中国民俗，60 BPM，琵琶与箫", "[Verse]\n山风入弦"
	d.Music = &schema.MusicCreationOptions{Duration: schema.DurationIntent{Mode: "auto"}, Voice: "auto"}
	// This stale value can remain when a draft was created before the duration
	// intent was introduced. It must not override AI automatic duration.
	d.ModelOptions = map[string]map[string]any{"ace_step": {"duration_seconds": float64(60), "bpm": float64(0)}}
	p, err := BuildRequest(d, "", "")
	if err != nil {
		t.Fatal(err)
	}
	o := p["request"].(map[string]any)["options"].(map[string]any)
	if _, exists := o["duration_seconds"]; exists {
		t.Fatalf("AI自动不应发送固定时长: %#v", o)
	}
	if o["bpm"] != float64(60) {
		t.Fatalf("样式中的60 BPM没有转换成模型参数: %#v", o)
	}
}

func TestACEExplainsMissingReferenceVoiceDescription(t *testing.T) {
	d := schema.DefaultDraft()
	d.Kind, d.ModelID, d.Text, d.Lyrics = "music", "ace-step-1.5-turbo-bf16", "slow folk, 60 bpm", "[Verse]\n山风入弦"
	d.Music = &schema.MusicCreationOptions{Duration: schema.DurationIntent{Mode: "auto"}, Voice: "reference", VoiceReferenceAssetID: "ref"}
	d.Timeline = &schema.AudioTimeline{Assets: []schema.AudioAsset{{ID: "ref", Name: "参考音色", FileName: "ref.wav"}}}
	_, err := BuildRequest(d, "", "")
	var callError *msg.CallError
	if !errors.As(err, &callError) || callError.Params["detail"] != "请填写希望参考的音色特点；ACE-Step 会将参考音频作为生成上下文，但不保证克隆原音色" {
		t.Fatalf("应指出缺少音色描述及模型限制，实际错误: %#v", err)
	}
}

func TestACEReferenceAudioUsesCompleteContext(t *testing.T) {
	d := schema.DefaultDraft()
	d.Kind, d.ModelID, d.Text, d.Lyrics = "music", "ace-step-1.5-turbo-bf16", "soft folk", "[Verse]\\nhello"
	d.Music = &schema.MusicCreationOptions{Duration: schema.DurationIntent{Mode: "fixed", Seconds: 60}, Voice: "reference", VoiceReferenceAssetID: "ref", VoiceReferenceDescription: "warm, breathy female vocal"}
	d.Timeline = &schema.AudioTimeline{Assets: []schema.AudioAsset{{ID: "ref", Name: "voice reference", FileName: "ref.wav"}}}
	p, err := BuildRequestWithMusicReference(d, "", "", "C:/data/outputs/ref.wav")
	if err != nil {
		t.Fatal(err)
	}
	r := p["request"].(map[string]any)
	o := r["options"].(map[string]any)
	if o["route"] != "complete" || r["audio"] != "C:/data/outputs/ref.wav" || o["duration_seconds"] != 60 {
		t.Fatalf("参考音频没有进入 complete 上下文或目标时长丢失: %#v", r)
	}
	if r["text"] != "soft folk; vocal timbre guidance: warm, breathy female vocal" {
		t.Fatalf("参考文字描述应继续作为提示: %#v", r["text"])
	}
	d.ModelID = "yue2-3b-q8"
	if _, err = BuildRequestWithMusicReference(d, "", "", "C:/data/outputs/ref.wav"); err == nil {
		t.Fatal("不支持参考音频的模型不能静默忽略来源")
	}
}
