package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"yovoice/internal/audio"
	"yovoice/internal/catalog"
	"yovoice/internal/msg"
	"yovoice/internal/schema"
	"yovoice/internal/store"
	"yovoice/internal/testkit"
)

func TestEngineLifecycle(t *testing.T) {
	root := t.TempDir()
	_, e := store.New(root)
	must(t, e)
	engine := New(root)
	defer engine.Stop()
	executable, e := os.Executable()
	must(t, e)
	model := filepath.Join(root, "model.gguf")
	must(t, os.WriteFile(model, []byte("test"), 0600))
	d := schema.DefaultDraft()
	out := filepath.Join(root, "outputs", "test.wav")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, e = engine.Generate(ctx, executable, schema.InstalledModel{ID: d.ModelID, Path: model}, "cpu", d, "voice.wav", "", out, func(msg.Code, msg.Params) {})
	must(t, e)
	pid := engine.process.Process.Pid
	if seconds, e := audio.Duration(out); e != nil || seconds != 1 {
		t.Fatal(seconds, e)
	}
	must(t, os.Remove(out))
	_, e = engine.Generate(ctx, executable, schema.InstalledModel{ID: d.ModelID, Path: model}, "cpu", d, "voice.wav", "", out, func(msg.Code, msg.Params) {})
	must(t, e)
	if engine.process.Process.Pid != pid {
		t.Fatal("引擎未复用")
	}

	// 旧草稿中的流式标记应被忽略，始终使用完整生成。
	must(t, json.Unmarshal([]byte(`{"modelId":"voxcpm2-q8","streaming":true}`), &d))
	_, e = engine.Generate(ctx, executable, schema.InstalledModel{ID: d.ModelID, Path: model}, "cpu", d, "", "", out, func(msg.Code, msg.Params) {})
	must(t, e)
	if engine.process.Process.Pid == pid {
		t.Fatal("切换模型类型后必须重新启动引擎")
	}
	config, err := os.ReadFile(filepath.Join(root, "runtime", "server.json"))
	must(t, err)
	if !bytes.Contains(config, []byte(`"family":"voxcpm2"`)) || !bytes.Contains(config, []byte(`"mode":"offline"`)) {
		t.Fatal(string(config))
	}
	// ACE-Step XL 与 Turbo 共用内核，但需要随模型传入 DiT 变体。
	music := schema.DefaultDraft()
	must(t, json.Unmarshal([]byte(`{"kind":"music","modelId":"ace-step-1.5-xl-turbo-q8","text":"city pop","lyrics":"[verse]\n夏夜"}`), &music))
	_, e = engine.Generate(ctx, executable, schema.InstalledModel{ID: music.ModelID, Path: model}, "cpu", music, "", "", out, func(msg.Code, msg.Params) {})
	must(t, e)
	config, err = os.ReadFile(filepath.Join(root, "runtime", "server.json"))
	must(t, err)
	if !bytes.Contains(config, []byte(`"load_options":{"ace_step.dit_model_path":"acestep-v15-xl-turbo"}`)) {
		t.Fatal(string(config))
	}
	// YuE2 自己规划时附带旋律谱；只出谱时没有音频；按谱演唱时不再返回谱；SheetSage2 扒谱只返回谱。
	song := schema.DefaultDraft()
	must(t, json.Unmarshal([]byte(`{"kind":"music","modelId":"yue2-3b-q8","text":"city pop","lyrics":"[verse]\n夏夜"}`), &song))
	yue := schema.InstalledModel{ID: song.ModelID, Path: model}
	generated, e := engine.Generate(ctx, executable, yue, "cpu", song, "", "", out, func(msg.Code, msg.Params) {})
	if e != nil || generated != testkit.ABC {
		t.Fatal(generated, e)
	}
	planned, truncated, e := engine.PlanMelody(ctx, executable, yue, "cpu", song, func(msg.Code, msg.Params) {})
	if e != nil || planned != testkit.ABC || truncated {
		t.Fatal(planned, e)
	}
	song.Abc = testkit.ABC
	if generated, e = engine.Generate(ctx, executable, yue, "cpu", song, "", "", out, func(msg.Code, msg.Params) {}); e != nil || generated != "" {
		t.Fatal(generated, e)
	}
	transcribed, e := engine.Transcribe(ctx, executable, schema.InstalledModel{ID: "sheetsage2-orig", Path: model}, "cpu", out, func(msg.Code, msg.Params) {})
	if e != nil || transcribed != testkit.ABC {
		t.Fatal(transcribed, e)
	}
	config, err = os.ReadFile(filepath.Join(root, "runtime", "server.json"))
	must(t, err)
	if !bytes.Contains(config, []byte(`"family":"sheetsage2"`)) || !bytes.Contains(config, []byte(`"task":"midi"`)) {
		t.Fatal(string(config))
	}
	d.Text = "等待取消"
	cancelled, stop := context.WithCancel(ctx)
	timer := time.AfterFunc(200*time.Millisecond, stop)
	defer timer.Stop()
	if _, e = engine.Generate(cancelled, executable, schema.InstalledModel{ID: d.ModelID, Path: model}, "cpu", d, "voice.wav", "", out, func(msg.Code, msg.Params) {}); e == nil {
		t.Fatal("未取消")
	}
	if engine.process != nil {
		t.Fatal("取消后引擎仍存活")
	}
	if runtime.GOOS == "darwin" {
		a, e := catalog.RuntimeArchives("metal")
		must(t, e)
		if len(a) != 1 || !strings.Contains(a[0].Name, "macos") {
			t.Fatal(a)
		}
	}
	if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
		for _, backend := range []string{"cpu", "vulkan"} {
			a, err := catalog.RuntimeArchives(backend)
			must(t, err)
			if len(a) != 1 || a[0].Name != "audio-"+catalog.EngineVersion+"-bin-ubuntu-x64-"+backend+"-portable.tar.gz" || len(a[0].Hash) != 64 {
				t.Fatal(a)
			}
		}
		if _, err := catalog.RuntimeArchives("metal"); err == nil {
			t.Fatal("Linux 不应接受 Metal 后端")
		}
	}
}
