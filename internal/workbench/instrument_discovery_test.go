package workbench

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"yovoice/internal/schema"
	"yovoice/internal/score"
)

const candidateID = "12345678-1234-1234-1234-123456789abc"

type onlineRoundTrip func(*http.Request) (*http.Response, error)

func (f onlineRoundTrip) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func onlineResponse(request *http.Request, status int, body []byte) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Request: request, Header: http.Header{}}
}

func candidateFixture() openverseAudio {
	return openverseAudio{ID: candidateID, Title: "pipa C4 pluck", Creator: "musician", Source: "freesound", Provider: "freesound",
		LandingURL: "https://freesound.org/people/musician/sounds/12345/", URL: "https://cdn.freesound.org/previews/123/12345.wav",
		License: "cc0", LicenseVersion: "1.0", LicenseURL: "https://creativecommons.org/publicdomain/zero/1.0/",
		DurationMS: 1000, FileSize: 100000, FileType: "wav"}
}

func TestOnlineCandidateSearchAndVerifiedImport(t *testing.T) {
	w, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	path := filepath.Join(t.TempDir(), "note.wav")
	signal := make([]float32, score.SampleRate)
	for i := range signal {
		signal[i] = float32(.25 * math.Sin(2*math.Pi*261.6256*float64(i)/score.SampleRate))
	}
	if err := score.WriteWAV(path, signal, signal); err != nil {
		t.Fatal(err)
	}
	wav, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	item := candidateFixture()
	w.client = &http.Client{Transport: onlineRoundTrip(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Hostname() {
		case "api.openverse.org":
			if request.URL.Query().Get("q") != "" && request.URL.Query().Get("q") != "pipa" {
				t.Fatalf("未转译民乐搜索词: %s", request.URL)
			}
			var payload any = item
			if request.URL.Query().Get("q") != "" {
				bad := item
				bad.ID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
				bad.License = "by"
				payload = map[string]any{"results": []openverseAudio{item, bad}}
			}
			b, _ := json.Marshal(payload)
			return onlineResponse(request, 200, b), nil
		case "freesound.org":
			return onlineResponse(request, 200, []byte(`<html><a title="Go to the full license text" href='https://creativecommons.org/publicdomain/zero/1.0/'>Creative Commons 0</a></html>`)), nil
		case "cdn.freesound.org":
			return onlineResponse(request, 200, wav), nil
		default:
			t.Fatalf("不应访问未审核主机: %s", request.URL)
			return nil, nil
		}
	})}
	candidates, err := w.SearchInstrumentCandidates(context.Background(), "琵琶")
	if err != nil || len(candidates) != 1 || candidates[0].ID != candidateID {
		t.Fatalf("在线候选过滤失败: %+v, %v", candidates, err)
	}
	source, err := w.ImportOnlineInstrumentSample(context.Background(), OnlineSampleInput{ID: candidateID, Instrument: "琵琶", Articulation: "normal", ConfirmedInstrument: true})
	if err != nil {
		t.Fatal(err)
	}
	if source.Origin != "real" || source.License != "CC0-1.0" || source.Samples[0].RootPitch != 60 || source.SourceURL != item.LandingURL {
		t.Fatalf("在线音源元数据或音高错误: %+v", source)
	}
	if _, err := w.scoreSources(schema.Score{Tracks: []schema.ScoreTrack{{ID: "lead", Name: "琵琶", SourceID: source.ID}}}); err != nil {
		t.Fatal(err)
	}
}

func TestOnlineCandidateRejectsUnverifiedLicenseAndHost(t *testing.T) {
	item := candidateFixture()
	item.URL = "https://127.0.0.1/private.wav"
	if _, ok := item.candidate(); ok {
		t.Fatal("不应接受任意下载主机")
	}
	w, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	item = candidateFixture()
	w.client = &http.Client{Transport: onlineRoundTrip(func(request *http.Request) (*http.Response, error) {
		if request.URL.Hostname() == "api.openverse.org" {
			b, _ := json.Marshal(item)
			return onlineResponse(request, 200, b), nil
		}
		if request.URL.Hostname() == "freesound.org" {
			return onlineResponse(request, 200, []byte(`<html><a href='https://creativecommons.org/licenses/by/4.0/'>CC BY</a></html>`)), nil
		}
		t.Fatalf("许可未核验时不应下载媒体: %s", request.URL)
		return nil, nil
	})}
	_, err = w.ImportOnlineInstrumentSample(context.Background(), OnlineSampleInput{ID: candidateID, Instrument: "琵琶", Articulation: "normal", ConfirmedInstrument: true})
	if err == nil || !strings.Contains(err.Error(), "CC0") || len(w.Store.Read().InstrumentSources) != 0 {
		t.Fatalf("许可不符仍被导入: %v", err)
	}
}

func TestOnlineCandidateRequiresMatchingFreesoundSoundID(t *testing.T) {
	item := candidateFixture()
	item.URL = "https://cdn.freesound.org/previews/123/99999_10194539-hq.mp3"
	if _, ok := item.candidate(); ok {
		t.Fatal("Freesound 页面与媒体文件的声音编号不一致时不能接受候选")
	}
	item = candidateFixture()
	item.URL = "https://cdn.freesound.org/previews/123/12346_10194539-hq.mp3"
	if _, ok := item.candidate(); ok {
		t.Fatal("Freesound CDN 预览编号与落地页不一致时不能接受候选")
	}
	item = candidateFixture()
	if _, ok := item.candidate(); !ok {
		t.Fatal("Freesound 编号匹配的官方预览地址应可接受")
	}
}

func TestOnlineCandidateRequiresInstrumentConfirmation(t *testing.T) {
	w, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.client = &http.Client{Transport: onlineRoundTrip(func(request *http.Request) (*http.Response, error) {
		t.Fatalf("未确认乐器时不应发起网络请求: %s", request.URL)
		return nil, nil
	})}
	if _, err := w.ImportOnlineInstrumentSample(context.Background(), OnlineSampleInput{ID: candidateID, Instrument: "琵琶", Articulation: "normal"}); err == nil {
		t.Fatal("未试听确认乐器的候选不应导入")
	}
}

func TestWikimediaAudioCandidateUsesActualOpenverseSource(t *testing.T) {
	item := candidateFixture()
	item.Source = "wikimedia_audio"
	item.LandingURL = "https://commons.wikimedia.org/w/index.php?curid=12345"
	item.URL = "https://upload.wikimedia.org/wikipedia/commons/a/ab/sample.wav"
	if _, ok := item.candidate(); !ok {
		t.Fatal("Openverse 的 wikimedia_audio 来源应被识别")
	}
	item.Title = "LL-Q5146 (por)-MedK1-pipa"
	if _, ok := item.candidate(); ok {
		t.Fatal("同名外语发音不应作为民乐候选")
	}
}

func TestLongSingleNoteRemainsCandidate(t *testing.T) {
	item := candidateFixture()
	item.Title = "001-Erhu D4.wav"
	item.DurationMS = 10013
	if candidate, ok := item.candidate(); !ok || candidate.Kind != "noteCandidate" {
		t.Fatal("超过五秒的单音录音不应被误归为乐句")
	}
	item.Title = "Erhu melodic loop.wav"
	if candidate, ok := item.candidate(); !ok || candidate.Kind != "phraseCandidate" {
		t.Fatal("标题明确标出的乐句不能作为单音候选")
	}
}

func TestAutoTitleMatchUsesInstrumentNameWithoutExtraSearchWords(t *testing.T) {
	item := candidateFixture()
	item.Title = "Xiao D4 breath note"
	if !autoTitleMatches(item, "箫") {
		t.Fatal("箫单音标题不必重复搜索提示中的 flute")
	}
	item.Title = "Mandolin D4 note"
	if autoTitleMatches(item, "箫") {
		t.Fatal("不能只凭搜索结果就认定乐器匹配")
	}
}

func TestAutoFindInstrumentDownloadsOnlyExactMetadataCandidate(t *testing.T) {
	w, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	path := filepath.Join(t.TempDir(), "note.wav")
	signal := make([]float32, score.SampleRate)
	for i := range signal {
		signal[i] = float32(.25 * math.Sin(2*math.Pi*261.6256*float64(i)/score.SampleRate))
	}
	if err := score.WriteWAV(path, signal, signal); err != nil {
		t.Fatal(err)
	}
	wav, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	item := candidateFixture()
	bad := item
	bad.ID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	bad.Title = "sfx comer pipas.mp3"
	w.client = &http.Client{Transport: onlineRoundTrip(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Hostname() {
		case "api.openverse.org":
			if request.URL.Query().Get("q") != "" {
				b, _ := json.Marshal(map[string]any{"results": []openverseAudio{bad, item}})
				return onlineResponse(request, 200, b), nil
			}
			b, _ := json.Marshal(item)
			return onlineResponse(request, 200, b), nil
		case "freesound.org":
			return onlineResponse(request, 200, []byte(`<html><a title="Go to the full license text" href="https://creativecommons.org/publicdomain/zero/1.0/">CC0</a></html>`)), nil
		case "cdn.freesound.org":
			return onlineResponse(request, 200, wav), nil
		default:
			t.Fatalf("不应访问未知主机: %s", request.URL)
			return nil, nil
		}
	})}
	if err := w.AutoFindInstrument("琵琶"); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(10 * time.Second)
	for {
		state := w.Store.Read()
		if state.ModelActivity != nil && state.ModelActivity.Status != "running" {
			if state.ModelActivity.Status != "completed" || len(state.InstrumentSources) != 1 || state.InstrumentSources[0].Verification != "metadata" || state.InstrumentSources[0].Samples[0].RootPitch != 60 {
				t.Fatalf("自动检索没有得到可追溯的单音: activity=%+v sources=%+v", state.ModelActivity, state.InstrumentSources)
			}
			if err := w.ConfirmInstrumentSource(state.InstrumentSources[0].ID); err != nil {
				t.Fatal(err)
			}
			if source := w.Store.Read().InstrumentSources[0]; source.Verification != "user" || source.SourceURL != item.LandingURL {
				t.Fatalf("人工确认后来源记录丢失: %+v", source)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("自动检索超时")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestAutoFindInstrumentContinuesPastFirstSearchPage(t *testing.T) {
	w, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	path := filepath.Join(t.TempDir(), "note.wav")
	signal := make([]float32, score.SampleRate)
	for i := range signal {
		signal[i] = float32(.25 * math.Sin(2*math.Pi*261.6256*float64(i)/score.SampleRate))
	}
	if err := score.WriteWAV(path, signal, signal); err != nil {
		t.Fatal(err)
	}
	wav, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	item := candidateFixture()
	w.client = &http.Client{Transport: onlineRoundTrip(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Hostname() {
		case "api.openverse.org":
			if request.URL.Query().Get("page") == "1" {
				if request.URL.Query().Get("source") != "freesound" {
					t.Fatal("自动检索未限定音源站")
				}
				bad := item
				bad.Title = "spoken pipa pronunciation"
				b, _ := json.Marshal(map[string]any{"page_count": 2, "results": []openverseAudio{bad}})
				return onlineResponse(request, 200, b), nil
			}
			if request.URL.Query().Get("page") == "2" {
				b, _ := json.Marshal(map[string]any{"page_count": 2, "results": []openverseAudio{item}})
				return onlineResponse(request, 200, b), nil
			}
			b, _ := json.Marshal(item)
			return onlineResponse(request, 200, b), nil
		case "freesound.org":
			return onlineResponse(request, 200, []byte(`<html><a title="Go to the full license text" href="https://creativecommons.org/publicdomain/zero/1.0/">CC0</a></html>`)), nil
		case "cdn.freesound.org":
			return onlineResponse(request, 200, wav), nil
		}
		t.Fatalf("不应访问未知主机: %s", request.URL)
		return nil, nil
	})}
	if err := w.AutoFindInstrument("琵琶"); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(10 * time.Second)
	for {
		state := w.Store.Read()
		if state.ModelActivity != nil && state.ModelActivity.Status != "running" {
			if state.ModelActivity.Status != "completed" || len(state.InstrumentSources) != 1 {
				t.Fatalf("未从第二页找到采样: activity=%+v sources=%+v", state.ModelActivity, state.InstrumentSources)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("自动检索第二页超时")
		case <-time.After(10 * time.Millisecond):
		}
	}
}
