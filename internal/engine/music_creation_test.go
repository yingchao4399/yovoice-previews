package engine

import (
	"strings"
	"testing"
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
