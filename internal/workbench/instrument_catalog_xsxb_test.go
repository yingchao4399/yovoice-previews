package workbench

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
	"yovoice/internal/score"
)

func TestXSXBCatalogVerifiedDownload(t *testing.T) {
	for _, testcase := range []struct {
		name, license, family          string
		badHash, wantInstalled, mirror bool
		seconds                        int
	}{
		{name: "valid", license: "CC0-1.0", family: "folk", wantInstalled: true},
		{name: "长余音锣采样", license: "CC0-1.0", family: "percussion", wantInstalled: true, seconds: 34},
		{name: "GitHub raw 断线后镜像", license: "CC0-1.0", family: "folk", wantInstalled: true, mirror: true},
		{name: "hash mismatch", license: "CC0-1.0", family: "folk", badHash: true},
		{name: "license mismatch", license: "CC-BY-4.0", family: "folk"},
		{name: "phrase collection", license: "CC0-1.0", family: "samples"},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			w, err := New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer w.Close()
			path := filepath.Join(t.TempDir(), "note.wav")
			seconds := max(1, testcase.seconds)
			signal := make([]float32, score.SampleRate*seconds)
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
			hash := sha256.Sum256(wav)
			digest := hex.EncodeToString(hash[:])
			if testcase.badHash {
				digest = hex.EncodeToString(make([]byte, 32))
			}
			sampleURL := "https://cdn.freesound.org/test.wav"
			if testcase.mirror {
				sampleURL = "https://raw.githubusercontent.com/example/samples/0123456789abcdef0123456789abcdef01234567/test.wav"
			}
			samples := []map[string]any{{"midi": 60, "articulation": "normal", "downloadUrl": sampleURL, "bytes": len(wav), "sha256": digest}}
			if testcase.mirror {
				samples = append(samples, map[string]any{"midi": 62, "articulation": "normal", "downloadUrl": sampleURL, "bytes": len(wav), "sha256": digest})
			}
			manifest, err := json.Marshal([]map[string]any{{"id": "test-flute", "name": "测试笛", "family": testcase.family,
				"license": testcase.license, "licenseUrl": "https://creativecommons.org/publicdomain/zero/1.0/",
				"sourceUrl": "https://github.com/sparklecatta-lang/XSXB-Band", "defaultArticulation": "normal",
				"samples": samples,
			}})
			if err != nil {
				t.Fatal(err)
			}
			var rawCalls atomic.Int32
			w.client = &http.Client{Transport: onlineRoundTrip(func(request *http.Request) (*http.Response, error) {
				switch request.URL.Hostname() {
				case "raw.githubusercontent.com":
					if testcase.mirror {
						rawCalls.Add(1)
						return nil, errors.New("connection reset")
					}
					t.Fatal("内置目录不应再访问 GitHub raw")
					return nil, nil
				case "cdn.jsdelivr.net":
					if !testcase.mirror || request.URL.Path != "/gh/example/samples@0123456789abcdef0123456789abcdef01234567/test.wav" {
						t.Fatalf("镜像地址错误: %s", request.URL)
					}
					return onlineResponse(request, 200, wav), nil
				case "cdn.freesound.org":
					return onlineResponse(request, 200, wav), nil
				default:
					t.Fatalf("意外访问: %s", request.URL)
					return nil, nil
				}
			})}
			manifestHash := sha256.Sum256(manifest)
			if err := w.downloadXSXBLibraryInstrumentWithLibrary("test-flute", manifest, hex.EncodeToString(manifestHash[:])); err != nil {
				t.Fatal(err)
			}
			deadline := time.After(10 * time.Second)
			for {
				state := w.Store.Read()
				if state.ModelActivity != nil && state.ModelActivity.Status != "running" {
					if testcase.wantInstalled {
						wantSamples := 1
						if testcase.mirror {
							wantSamples = 2
						}
						if state.ModelActivity.Status != "completed" || len(state.InstrumentSources) != 1 || len(state.InstrumentSources[0].Samples) != wantSamples || (testcase.mirror && rawCalls.Load() != 1) {
							log, _ := os.ReadFile(filepath.Join(w.Store.Root, "logs", "service.log"))
							t.Fatalf("应安装经核验的单音: activity=%+v sources=%+v log=%s", state.ModelActivity, state.InstrumentSources, log)
						}
						if state.ModelActivity.Total != int64(len(wav)*wantSamples) || state.ModelActivity.Received != state.ModelActivity.Total {
							t.Fatalf("固定目录应报告已核验的真实下载字节进度: %+v", state.ModelActivity)
						}
					} else if state.ModelActivity.Status != "failed" || len(state.InstrumentSources) != 0 {
						t.Fatalf("应拒绝未核验的条目: %s %+v", state.ModelActivity.Status, state.InstrumentSources)
					}
					return
				}
				select {
				case <-deadline:
					t.Fatal("下载超时")
				case <-time.After(10 * time.Millisecond):
				}
			}
		})
	}
}

func TestXSXBCatalogVerifiedDownloadUsesFixedCache(t *testing.T) {
	w, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	path := filepath.Join(t.TempDir(), "cached.wav")
	signal := make([]float32, score.SampleRate)
	for i := range signal {
		signal[i] = float32(.2 * math.Sin(2*math.Pi*261.6256*float64(i)/score.SampleRate))
	}
	if err := score.WriteWAV(path, signal, signal); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(b)
	digest := hex.EncodeToString(hash[:])
	manifest := []byte(fmt.Sprintf(`[{"id":"cache-flute","name":"缓存笛","family":"folk","license":"CC0-1.0","licenseUrl":"https://creativecommons.org/publicdomain/zero/1.0/","sourceUrl":"https://github.com/sparklecatta-lang/XSXB-Band","defaultArticulation":"normal","samples":[{"midi":60,"articulation":"normal","downloadUrl":"https://cdn.freesound.org/cache.wav","bytes":%d,"sha256":"%s"}]}]`, len(b), digest))
	var requests atomic.Int32
	w.client = &http.Client{Transport: onlineRoundTrip(func(request *http.Request) (*http.Response, error) {
		requests.Add(1)
		return onlineResponse(request, 200, b), nil
	})}
	cache := filepath.Join(w.Store.Root, "downloads", "instrument-cache", digest+".bin")
	if err := os.MkdirAll(filepath.Dir(cache), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cache, b, 0600); err != nil {
		t.Fatal(err)
	}
	manifestHash := sha256.Sum256(manifest)
	if err := w.downloadXSXBLibraryInstrumentWithLibrary("cache-flute", manifest, hex.EncodeToString(manifestHash[:])); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(10 * time.Second)
	for {
		state := w.Store.Read()
		if state.ModelActivity != nil && state.ModelActivity.Status != "running" {
			if state.ModelActivity.Status != "completed" || requests.Load() != 0 || len(state.InstrumentSources) != 1 {
				t.Fatalf("固定缓存命中后应完成安装: requests=%d activity=%+v sources=%d", requests.Load(), state.ModelActivity, len(state.InstrumentSources))
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("缓存测试超时")
		case <-time.After(10 * time.Millisecond):
		}
	}
}
