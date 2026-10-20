package workbench

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"yovoice/internal/audio"
	"yovoice/internal/msg"
	"yovoice/internal/schema"
)

// 资源页汇总生成音频、故事配音片段、参考音频、时间轴素材与 AI 图片，标出大小、所属作品与引用者。
func TestListAndDeleteAssets(t *testing.T) {
	w, err := New(t.TempDir())
	must(t, err)
	defer w.Close()
	root := w.Store.Root
	write := func(dir, name string) { must(t, os.WriteFile(filepath.Join(root, dir, name), wav(), 0600)) }

	story := schema.DefaultDraft()
	story.Title, story.Kind = "第一章", "story"
	free, used, segment, voice, material := schema.NewID(), schema.NewID(), schema.NewID(), schema.NewID(), schema.NewID()
	story.Timeline = &schema.AudioTimeline{
		Tracks: []schema.AudioLane{{ID: "lane", Name: "旁白", Clips: []schema.AudioClip{{ID: "clip", GenerationID: used, Duration: 1}}}},
	}
	for _, id := range []string{free, used, segment} {
		write("outputs", id+".wav")
	}
	// 时间轴素材的文件名固定为 import-<ID>.wav，时长须与真实文件一致。
	write("outputs", "import-"+material+".wav")
	materialSeconds, err := audio.Duration(filepath.Join(root, "outputs", "import-"+material+".wav"))
	must(t, err)
	story.Timeline.Assets = []schema.AudioAsset{{ID: material, Name: "雨声", FileName: "import-" + material + ".wav", Duration: materialSeconds}}
	write("voices", voice+".wav")
	must(t, os.MkdirAll(filepath.Join(root, "images"), 0700))
	image := schema.NewID()
	must(t, os.WriteFile(filepath.Join(root, "images", image+".png"), []byte("\x89PNG\r\n\x1a\n"), 0600))
	must(t, os.WriteFile(filepath.Join(root, "images", "notes.txt"), []byte("x"), 0600)) // 非图片、非 ID 命名的文件不列出
	must(t, w.Store.Update(func(s *schema.State) {
		s.History = append(s.History,
			schema.Generation{ID: free, Title: "清晨旁白", FileName: free + ".wav", Duration: 1, Settings: story},
			schema.Generation{ID: used, Title: "被引用", FileName: used + ".wav", Duration: 1, Settings: story},
			schema.Generation{ID: segment, Title: "片段", FileName: segment + ".wav", Duration: 1, Settings: story, Segment: &schema.GenerationSegment{SpeakerName: "旁白"}})
		s.Voices = append(s.Voices, schema.Voice{ID: voice, Name: "我的声音", FileName: voice + ".wav", Duration: 3})
		s.Characters = append(s.Characters, schema.Character{ID: schema.NewID(), Name: "林夕", Settings: schema.SynthesisSettings{VoiceID: &voice}})
	}, true))
	// 时间轴引用的版本和素材文件要先存在，保存作品时会校验。
	must(t, w.SaveDraft(story))

	assets, err := w.ListAssets()
	must(t, err)
	byID := map[string]Asset{}
	for _, a := range assets {
		byID[a.ID] = a
	}
	if len(byID) != 6 {
		t.Fatal("应列出 3 条生成、1 条参考音频、1 个素材和 1 张图片", assets)
	}
	if a := byID[free]; a.Kind != AssetGeneration || a.ProjectTitle != "第一章" || a.ProjectKind != "story" || a.Bytes != int64(len(wav())) || len(a.Users) != 0 {
		t.Fatal("生成音频信息不正确", a)
	}
	if a := byID[used]; !slices.Equal(a.Users, []string{"第一章"}) {
		t.Fatal("被时间轴引用的版本应标出作品", a)
	}
	if a := byID[segment]; a.Kind != AssetSegment || !strings.HasPrefix(a.Name, "旁白") {
		t.Fatal("故事配音片段应单独标出并带说话人", a)
	}
	if a := byID[voice]; a.Kind != AssetVoice || !slices.Equal(a.Users, []string{"林夕"}) || a.CreatedAt.IsZero() {
		t.Fatal("参考音频应标出引用它的角色", a)
	}
	if a := byID[material]; a.Kind != AssetMaterial || a.ProjectID != story.ID {
		t.Fatal("时间轴素材应归属作品", a)
	}
	if a := byID[image]; a.Kind != AssetImage || a.Media != "images" {
		t.Fatal("AI 图片应列出", a)
	}
	preview, err := w.ImagePreview(image)
	must(t, err)
	if !strings.HasPrefix(preview, "data:image/png;base64,") {
		t.Fatal("图片预览应为 data URL", preview[:20])
	}

	results := w.DeleteAssets([]AssetRef{{AssetGeneration, free}, {AssetGeneration, used}, {AssetVoice, voice}, {AssetMaterial, material}, {AssetImage, image}, {"model", free}})
	codes := map[string]msg.Code{}
	for _, r := range results {
		codes[r.Kind+":"+r.ID] = r.Error
	}
	want := map[string]msg.Code{
		AssetGeneration + ":" + free: "", AssetGeneration + ":" + used: msg.ErrTimelineInUse, AssetVoice + ":" + voice: msg.ErrVoiceReferenced,
		AssetMaterial + ":" + material: msg.ErrTimelineInUse, AssetImage + ":" + image: "", "model:" + free: msg.ErrAudioKindInvalid,
	}
	for k, v := range want {
		if codes[k] != v {
			t.Fatalf("%s 应得到 %q，实际 %q", k, v, codes[k])
		}
	}
	if _, e := os.Stat(filepath.Join(root, "outputs", free+".wav")); !os.IsNotExist(e) {
		t.Fatal("删除后应移除音频文件")
	}
	if _, e := os.Stat(filepath.Join(root, "images", image+".png")); !os.IsNotExist(e) {
		t.Fatal("删除后应移除图片")
	}
	if _, e := os.Stat(filepath.Join(root, "outputs", used+".wav")); e != nil {
		t.Fatal("被引用的版本不能删除文件")
	}
	if _, e := w.ImagePreview("../" + image); e == nil {
		t.Fatal("非法 ID 应被拒绝")
	}
}
