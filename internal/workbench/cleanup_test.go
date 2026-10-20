package workbench

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"yovoice/internal/schema"
)

// 启动时回收中途退出遗留的临时文件与不再使用的运行时，保留缓存、当前运行时与其他后端最新的内核。
func TestRemoveStaleFiles(t *testing.T) {
	root := t.TempDir()
	w, err := New(root)
	must(t, err)
	current := filepath.Join(root, "runtime", "v0.7.4-cuda-222", "bin", "audiocpp_server")
	must(t, os.MkdirAll(filepath.Dir(current), 0700))
	must(t, os.WriteFile(current, nil, 0700))
	must(t, w.Store.Update(func(s *schema.State) { s.RuntimePath = &current }, true))
	w.Close()
	// 键为遗留文件，值为应被整体删除的目标。
	stale := map[string]string{
		".project-1/outputs/a.wav":                       ".project-1",
		".state-1":                                       ".state-1",
		"downloads/upload-1":                             "downloads/upload-1",
		"downloads/api-upload-1":                         "downloads/api-upload-1",
		"downloads/audio-1/reference.wav":                "downloads/audio-1",
		"downloads/0123456789abcdef0123456789abcdef.wav": "downloads/0123456789abcdef0123456789abcdef.wav",
		"runtime/v0.7.3-cuda-111/audiocpp_server":        "runtime/v0.7.3-cuda-111",
	}
	kept := []string{
		"downloads/audio-v0.7.4-bin-windows-x64-cuda12.4.zip",
		"downloads/audio-v0.7.4-bin-windows-x64-cuda12.4.zip.part",
		"runtime/server.json",
		"runtime/v0.7.4-vulkan-333/" + executableName(),
		"voices/keep.wav",
		"outputs/keep.wav",
	}
	files := append([]string{}, kept...)
	for file := range stale {
		files = append(files, file)
	}
	for _, name := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		must(t, os.MkdirAll(filepath.Dir(path), 0700))
		must(t, os.WriteFile(path, []byte("x"), 0600))
	}
	w, err = New(root)
	must(t, err)
	defer w.Close()
	for _, target := range stale {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(target))); !os.IsNotExist(err) {
			t.Error("应清理", target)
		}
	}
	for _, name := range append(kept, "runtime/v0.7.4-cuda-222/bin/audiocpp_server") {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(name))); err != nil {
			t.Error("应保留", name, err)
		}
	}
	if r := w.Store.Read().Runtimes["vulkan"]; r.Path != filepath.Join(root, "runtime", "v0.7.4-vulkan-333", executableName()) || r.Version != "v0.7.4" {
		t.Error("应登记未记录的 Vulkan 内核", r)
	}
}

func executableName() string {
	if runtime.GOOS == "windows" {
		return "audiocpp_server.exe"
	}
	return "audiocpp_server"
}

// 删除生成记录或作品时，指向它们的引用一起清掉：已采纳标记、声音来源、作品待取的出谱结果。
func TestDeleteClearsReferences(t *testing.T) {
	w, err := New(t.TempDir())
	must(t, err)
	defer w.Close()
	d := schema.DefaultDraft()
	must(t, w.SaveDraft(d))
	for _, name := range []string{"g1.wav", "g2.wav"} {
		path, err := w.Store.MediaPath("outputs", name)
		must(t, err)
		must(t, os.WriteFile(path, []byte("x"), 0600))
	}
	must(t, w.Store.Update(func(s *schema.State) {
		s.History = append(s.History, schema.Generation{ID: "g1", FileName: "g1.wav", Settings: schema.Draft{ID: d.ID}}, schema.Generation{ID: "g2", FileName: "g2.wav", Settings: schema.Draft{ID: d.ID}})
		s.Voices = append(s.Voices, schema.Voice{ID: "v1", Name: "来自 g1", FileName: "v1.wav", SourceGenerationID: "g1"}, schema.Voice{ID: "v2", Name: "来自 g2", FileName: "v2.wav", SourceGenerationID: "g2"})
		s.Melody = &schema.MelodyResult{ID: "m1", ProjectID: d.ID}
		s.Drafts[0].Timeline = &schema.AudioTimeline{AcceptedGenerations: []string{"g1", "g2"}}
	}, true))
	must(t, w.DeleteMedia("outputs", "g1"))
	s := w.Store.Read()
	if got := s.Drafts[0].Timeline.AcceptedGenerations; len(got) != 1 || got[0] != "g2" {
		t.Fatalf("删除生成后应移除已采纳标记：%v", got)
	}
	if s.Voices[0].SourceGenerationID != "" || s.Voices[1].SourceGenerationID != "g2" {
		t.Fatalf("删除生成后应清掉声音来源：%+v", s.Voices)
	}
	must(t, w.DeleteDraft(d.ID))
	s = w.Store.Read()
	if len(s.History) != 0 || s.Melody != nil || s.Voices[1].SourceGenerationID != "" {
		t.Fatalf("删除作品后不应留下生成、出谱结果或声音来源：%+v %+v %+v", s.History, s.Melody, s.Voices)
	}
	if path, _ := w.Store.MediaPath("outputs", "g2.wav"); fileExists(path) {
		t.Fatal("删除作品后应删除音频文件")
	}
}

func fileExists(path string) bool { _, err := os.Stat(path); return err == nil }
