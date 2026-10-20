package workbench

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
	"yovoice/internal/audio"
	"yovoice/internal/schema"
	"yovoice/internal/score"
)

// 用真实目录验证长余音、转码、完整安装和曲谱渲染；缓存仅复制到隔离目录。
func TestExpandedCatalogGongLive(t *testing.T) {
	if os.Getenv("YOVOICE_TEST_CATALOG_LIVE") != "1" {
		t.Skip("需要显式启用在线目录下载测试")
	}
	w, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if cache := os.Getenv("YOVOICE_TEST_CATALOG_CACHE"); cache != "" {
		var library []xsxbLibraryEntry
		if err := json.Unmarshal(xsxbLibraryJSON, &library); err != nil {
			t.Fatal(err)
		}
		for _, item := range library {
			if item.ID != "gong" {
				continue
			}
			for _, sample := range item.Samples {
				b, err := os.ReadFile(filepath.Join(cache, sample.SHA256+".bin"))
				if os.IsNotExist(err) {
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				dest := filepath.Join(w.Store.Root, "downloads", "instrument-cache", sample.SHA256+".bin")
				if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(dest, b, 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	if err := w.DownloadCatalogInstrument("gong"); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(3 * time.Minute)
	for {
		state := w.Store.Read()
		if a := state.ModelActivity; a != nil && a.Status != "running" {
			if a.Status != "completed" || len(state.InstrumentSources) != 1 || len(state.InstrumentSources[0].Samples) != 6 {
				log, _ := os.ReadFile(filepath.Join(w.Store.Root, "logs", "service.log"))
				t.Fatalf("大锣完整安装失败: %+v %s", a, log)
			}
			source := state.InstrumentSources[0]
			if math.Abs(source.Samples[2].Duration-34.0043764) > .001 {
				t.Fatalf("第三采样余音被截断: %.6f", source.Samples[2].Duration)
			}
			converter, err := audio.Converter()
			if err != nil {
				t.Fatal(err)
			}
			input, err := w.Store.MediaPath("instruments", source.Samples[2].FileName)
			if err != nil {
				t.Fatal(err)
			}
			converted := filepath.Join(t.TempDir(), "converted-gong.wav")
			if err := audio.ConvertInstrumentWithLimit(t.Context(), converter, input, converted, catalogSampleMaxSeconds); err != nil {
				t.Fatal(err)
			}
			actual, err := audio.Duration(converted)
			if err != nil || math.Abs(actual-source.Samples[2].Duration) > .001 {
				t.Fatalf("转码截断长余音: %.6f %v", actual, err)
			}
			music := schema.Score{Tempo: 60, TimeSignature: []int{4, 4}, Tracks: []schema.ScoreTrack{{ID: "gong", Name: "大锣", SourceID: source.ID, Notes: []schema.ScoreNote{{Bar: 1, Beat: 1, Pitch: 60, Length: 1, Velocity: 64}}}}}
			sources, err := w.scoreSources(music)
			if err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(t.TempDir(), "gong.wav")
			if err := score.RenderWithSources(t.Context(), nil, music, output, "", sources, nil); err != nil {
				t.Fatal(err)
			}
			if _, err := score.InspectSample(output); err != nil {
				t.Fatal(err)
			}
			duration, err := audio.Duration(output)
			if err != nil || duration <= 0 {
				t.Fatalf("渲染结果无效: %v", err)
			}
			t.Logf("大锣 6 个力度采样安装成功；第 3 个保留 %.6f 秒；曲谱已渲染为有效音频", source.Samples[2].Duration)
			return
		}
		select {
		case <-deadline:
			t.Fatal("大锣安装超时")
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// 只在显式启用时访问固定在线目录；数据写入隔离的测试目录。
func TestCatalogDownloadLive(t *testing.T) {
	if os.Getenv("YOVOICE_TEST_CATALOG_LIVE") != "1" {
		t.Skip("需要显式启用在线音源下载测试")
	}
	w, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	// 集成测试使用开发环境代理；产品运行时仍使用应用设置中的代理配置。
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyFromEnvironment
	w.client.Transport = transport
	if err := w.DownloadCatalogInstrument("guzheng"); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(5 * time.Minute)
	for {
		state := w.Store.Read()
		if state.ModelActivity != nil && state.ModelActivity.Status != "running" {
			if state.ModelActivity.Status != "completed" {
				b, _ := os.ReadFile(filepath.Join(w.Store.Root, "logs", "last-error.txt"))
				t.Fatalf("下载状态 %s: %s", state.ModelActivity.Status, b)
			}
			if len(state.InstrumentSources) != 1 || len(state.InstrumentSources[0].Samples) != 34 {
				t.Fatalf("音源登记不完整: %+v", state.InstrumentSources)
			}
			if _, err := w.scoreSources(schema.Score{Tracks: []schema.ScoreTrack{{ID: "lead", Name: "古筝", SourceID: state.InstrumentSources[0].ID}}}); err != nil {
				t.Fatal(err)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("在线下载超时")
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// 使用随包 ffmpeg 的独立测试进程验证 Openverse → 原站 CC0 → 单音下载 → 音源库。
func TestAutoFindErhuLive(t *testing.T) {
	if os.Getenv("YOVOICE_TEST_AUTO_LIVE") != "1" {
		t.Skip("需要显式启用在线自动寻源测试")
	}
	w, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyFromEnvironment
	w.client.Transport = transport
	if err := w.AutoFindInstrument("二胡"); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(3 * time.Minute)
	for {
		state := w.Store.Read()
		if state.ModelActivity != nil && state.ModelActivity.Status != "running" {
			if state.ModelActivity.Status != "completed" || len(state.InstrumentSources) != 1 || len(state.InstrumentSources[0].Samples) == 0 {
				log, _ := os.ReadFile(filepath.Join(w.Store.Root, "logs", "service.log"))
				t.Fatalf("在线自动寻源失败: %+v %s", state.ModelActivity, log)
			}
			if _, err := w.scoreSources(schema.Score{Tracks: []schema.ScoreTrack{{ID: "lead", Name: "二胡", SourceID: state.InstrumentSources[0].ID}}}); err != nil {
				t.Fatal(err)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("在线自动寻源超时")
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func TestExpandedCatalogErhuLive(t *testing.T) {
	if os.Getenv("YOVOICE_TEST_CATALOG_LIVE") != "1" {
		t.Skip("需要显式启用在线目录下载测试")
	}
	w, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyFromEnvironment
	w.client.Transport = transport
	if err := w.DownloadCatalogInstrument("erhu"); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(3 * time.Minute)
	for {
		state := w.Store.Read()
		if state.ModelActivity != nil && state.ModelActivity.Status != "running" {
			if state.ModelActivity.Status != "completed" || len(state.InstrumentSources) != 1 || len(state.InstrumentSources[0].Samples) != 9 || state.InstrumentSources[0].Verification != "catalog" {
				log, _ := os.ReadFile(filepath.Join(w.Store.Root, "logs", "service.log"))
				t.Fatalf("扩展目录二胡下载失败: %+v %+v %s", state.ModelActivity, state.InstrumentSources, log)
			}
			if _, err := w.scoreSources(schema.Score{Tracks: []schema.ScoreTrack{{ID: "lead", Name: "二胡", SourceID: state.InstrumentSources[0].ID}}}); err != nil {
				t.Fatal(err)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("扩展目录二胡下载超时")
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// 对照音源库首项，验证随包目录无需读取 GitHub raw 清单即可下载并试听采样。
func TestExpandedCatalogPianoLive(t *testing.T) {
	if os.Getenv("YOVOICE_TEST_CATALOG_LIVE") != "1" {
		t.Skip("需要显式启用在线目录下载测试")
	}
	w, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyFromEnvironment
	w.client.Transport = transport
	if err := w.DownloadCatalogInstrument("piano"); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(3 * time.Minute)
	for {
		state := w.Store.Read()
		if state.ModelActivity != nil && state.ModelActivity.Status != "running" {
			if state.ModelActivity.Status != "completed" || len(state.InstrumentSources) != 1 || len(state.InstrumentSources[0].Samples) != 8 {
				log, _ := os.ReadFile(filepath.Join(w.Store.Root, "logs", "service.log"))
				t.Fatalf("立式钢琴音源下载失败: %+v %+v %s", state.ModelActivity, state.InstrumentSources, log)
			}
			if _, err := w.scoreSources(schema.Score{Tracks: []schema.ScoreTrack{{ID: "lead", Name: "立式钢琴", SourceID: state.InstrumentSources[0].ID}}}); err != nil {
				t.Fatal(err)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("立式钢琴音源下载超时")
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// 模拟设置中仍保存旧代理，而 Windows 系统代理已切换到可用端口。
func TestCatalogStaleProxyFallbackLive(t *testing.T) {
	if os.Getenv("YOVOICE_TEST_CATALOG_LIVE") != "1" {
		t.Skip("需要显式启用在线目录下载测试")
	}
	if catalogSystemProxy() == nil {
		t.Skip("当前系统没有可用的手动代理")
	}
	w, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	preferences := w.Store.Read().Preferences
	preferences.ProxyURL = "http://127.0.0.1:1"
	// 旧版本状态没有 proxyEnabled 字段，只有失效的 proxyURL。
	preferences.ProxyEnabled = nil
	if err := w.SavePreferences(preferences); err != nil {
		t.Fatal(err)
	}
	var library []xsxbLibraryEntry
	if err := json.Unmarshal(xsxbLibraryJSON, &library); err != nil {
		t.Fatal(err)
	}
	var sampleURL, wantHash string
	var wantBytes int64
	for _, item := range library {
		if item.ID == "clap" {
			sampleURL = item.Samples[0].DownloadURL
			wantHash = item.Samples[0].SHA256
			wantBytes = item.Samples[0].Bytes
		}
	}
	if sampleURL == "" {
		t.Fatal("测试音源缺失")
	}
	b, err := w.catalogSampleBytes(t.Context(), sampleURL, 20<<20)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(b)
	if int64(len(b)) != wantBytes || hex.EncodeToString(hash[:]) != wantHash {
		t.Fatal("旧代理回退下载的音源内容与目录哈希不一致")
	}
	if !w.catalogSystemProxyPreferred.Load() {
		t.Fatal("成功的系统代理未被复用")
	}
	b, err = w.catalogSampleBytes(t.Context(), sampleURL, 20<<20)
	if err != nil {
		t.Fatal(err)
	}
	hash = sha256.Sum256(b)
	if int64(len(b)) != wantBytes || hex.EncodeToString(hash[:]) != wantHash {
		t.Fatal("复用系统代理后音源内容发生变化")
	}
}

// 新版本状态明确启用旧代理时，也必须跳过失效代理并复用系统代理。
func TestCatalogStaleProxyEnabledFallbackLive(t *testing.T) {
	if os.Getenv("YOVOICE_TEST_CATALOG_LIVE") != "1" {
		t.Skip("需要显式启用在线目录下载测试")
	}
	systemProxy := catalogSystemProxy()
	if systemProxy == nil {
		t.Skip("当前系统没有可用的手动代理")
	}
	w, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	preferences := w.Store.Read().Preferences
	preferences.ProxyURL = "http://127.0.0.1:1"
	enabled := true
	preferences.ProxyEnabled = &enabled
	if err := w.SavePreferences(preferences); err != nil {
		t.Fatal(err)
	}
	var library []xsxbLibraryEntry
	if err := json.Unmarshal(xsxbLibraryJSON, &library); err != nil {
		t.Fatal(err)
	}
	var sampleURL string
	for _, item := range library {
		if item.ID == "clap" && len(item.Samples) > 0 {
			sampleURL = item.Samples[0].DownloadURL
			break
		}
	}
	if sampleURL == "" {
		t.Fatal("测试音源缺失")
	}
	if _, err := w.catalogSampleBytes(t.Context(), sampleURL, 20<<20); err != nil {
		t.Fatal(err)
	}
	if !w.catalogSystemProxyPreferred.Load() {
		t.Fatal("明确启用的失效代理未回退到系统代理")
	}
}

func TestCatalogStaleProxyInstallLive(t *testing.T) {
	if os.Getenv("YOVOICE_TEST_CATALOG_INSTALL_LIVE") != "1" {
		t.Skip("需要显式启用随包转换器的在线安装测试")
	}
	if catalogSystemProxy() == nil {
		t.Skip("当前系统没有可用的手动代理")
	}
	w, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	preferences := w.Store.Read().Preferences
	preferences.ProxyURL = "http://127.0.0.1:1"
	enabled := true
	preferences.ProxyEnabled = &enabled
	if err := w.SavePreferences(preferences); err != nil {
		t.Fatal(err)
	}
	if err := w.DownloadCatalogInstrument("clap"); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(2 * time.Minute)
	for {
		state := w.Store.Read()
		if state.ModelActivity != nil && state.ModelActivity.Status != "running" {
			if state.ModelActivity.Status != "completed" || len(state.InstrumentSources) != 1 || len(state.InstrumentSources[0].Samples) != 1 {
				log, _ := os.ReadFile(filepath.Join(w.Store.Root, "logs", "service.log"))
				t.Fatalf("旧代理回退安装失败: %+v %s", state.ModelActivity, log)
			}
			filename := state.InstrumentSources[0].Samples[0].FileName
			path, err := w.Store.MediaPath("instruments", filename)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatal(err)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("旧代理回退安装超时")
		case <-time.After(100 * time.Millisecond):
		}
	}
}
