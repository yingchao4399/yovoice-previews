package workbench

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"yovoice/internal/audio"
	"yovoice/internal/msg"
	"yovoice/internal/schema"
	"yovoice/internal/score"
)

// 与界面目录同一固定提交；下载地址和哈希只从这份不可变清单读取。
const xsxbLibraryCommit = "4450128bb48888c3247d77ff9ba5ebb13cef186e"
const xsxbLibrarySHA256 = "c1dc1a29002a031bc01a2f4a435b68331dc4c25d7e5dd536cc0030eb53412ca2"

// 已核验目录包含锣等长余音单击，不能套用用户录音的 30 秒上限。
const catalogSampleMaxSeconds = 120

//go:embed xsxb-library.json
var xsxbLibraryJSON []byte

var xsxbCatalogID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
var xsxbSampleSHA = regexp.MustCompile(`^[0-9a-f]{64}$`)

type xsxbLibraryEntry struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	Family              string `json:"family"`
	License             string `json:"license"`
	LicenseURL          string `json:"licenseUrl"`
	SourceURL           string `json:"sourceUrl"`
	DefaultArticulation string `json:"defaultArticulation"`
	Samples             []struct {
		MIDI         int      `json:"midi"`
		Articulation string   `json:"articulation"`
		DownloadURL  string   `json:"downloadUrl"`
		Bytes        int64    `json:"bytes"`
		SHA256       string   `json:"sha256"`
		VelocityMin  *float64 `json:"velocityMin"`
		VelocityMax  *float64 `json:"velocityMax"`
	} `json:"samples"`
}

func (w *Workbench) downloadXSXBLibraryInstrument(id string) error {
	return w.downloadXSXBLibraryInstrumentWithLibrary(id, xsxbLibraryJSON, xsxbLibrarySHA256)
}

func (w *Workbench) downloadXSXBLibraryInstrumentWithLibrary(id string, manifest []byte, expectedSHA string) error {
	if !xsxbCatalogID.MatchString(id) {
		return msg.Err(msg.ErrRequestInvalid, nil)
	}
	for _, source := range w.Store.Read().InstrumentSources {
		if source.CatalogID == id && w.sourceFilesIssue(source) == "" {
			return nil
		}
	}
	return w.begin("instrumentDownload", msg.ActivityDownload, msg.Params{"name": id}, nil, func(ctx context.Context) error {
		manifestHash := sha256.Sum256(manifest)
		if hex.EncodeToString(manifestHash[:]) != expectedSHA {
			return fmt.Errorf("内置音源目录校验失败")
		}
		var library []xsxbLibraryEntry
		if err := json.Unmarshal(manifest, &library); err != nil {
			return err
		}
		index := slices.IndexFunc(library, func(entry xsxbLibraryEntry) bool { return entry.ID == id })
		if index < 0 {
			return msg.Err(msg.ErrRequestInvalid, nil)
		}
		entry := library[index]
		if entry.License != "CC0-1.0" || slices.Contains([]string{"amps", "cabinets", "samples"}, entry.Family) ||
			len(entry.Samples) == 0 || len(entry.Samples) > 500 || len(entry.Name) > 120 || len(entry.DefaultArticulation) > 40 {
			return fmt.Errorf("此目录条目不能作为逐音采样乐器安装")
		}
		var expectedBytes int64
		for _, item := range entry.Samples {
			u, e := url.Parse(item.DownloadURL)
			if e != nil || u.Scheme != "https" || !slices.Contains([]string{"raw.githubusercontent.com", "cdn.freesound.org"}, u.Hostname()) ||
				!slices.Contains([]string{".wav", ".flac", ".mp3"}, strings.ToLower(filepath.Ext(u.Path))) ||
				!xsxbSampleSHA.MatchString(item.SHA256) || item.Bytes < 1 || item.Bytes > 20<<20 || item.MIDI < 0 || item.MIDI > 127 ||
				len(item.Articulation) > 40 || (item.VelocityMin != nil && (*item.VelocityMin < 0 || *item.VelocityMin > 1)) ||
				(item.VelocityMax != nil && (*item.VelocityMax < 0 || *item.VelocityMax > 1)) {
				return fmt.Errorf("音源清单含无效采样元数据")
			}
			expectedBytes += item.Bytes
		}
		if expectedBytes > 200<<20 {
			return fmt.Errorf("音源总大小超出限制")
		}
		progressParams := msg.Params{"name": entry.Name, "unit": "bytes"}
		w.progressOf("instrumentDownload", msg.ActivityDownloading, progressParams, 0, expectedBytes)
		stage, err := os.MkdirTemp(filepath.Join(w.Store.Root, "downloads"), "xsxb-library-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(stage)
		articulation := entry.DefaultArticulation
		if articulation == "" {
			articulation = "normal"
		}
		source := schema.InstrumentSource{ID: schema.NewID(), CatalogID: id, Name: entry.Name, Instrument: entry.Name,
			Origin: "real", Verification: "catalog", DefaultArticulation: articulation, License: entry.License, LicenseURL: entry.LicenseURL,
			SourceURL: entry.SourceURL, Samples: make([]schema.InstrumentSample, 0, len(entry.Samples))}
		var convertedBytes, downloadedBytes int64
		for i, item := range entry.Samples {
			if err := ctx.Err(); err != nil {
				return err
			}
			u, _ := url.Parse(item.DownloadURL)
			b, err := w.catalogSampleBytesVerifiedWithProgress(ctx, item.DownloadURL, 20<<20, item.Bytes, item.SHA256, func(received int64) {
				w.progressOf("instrumentDownload", msg.ActivityDownloading, progressParams, downloadedBytes+received, expectedBytes)
			})
			if err != nil {
				return err
			}
			hash := sha256.Sum256(b)
			if int64(len(b)) != item.Bytes || hex.EncodeToString(hash[:]) != item.SHA256 {
				return fmt.Errorf("采样 %d 的大小或 SHA-256 校验失败", i+1)
			}
			// 源文件即使是 WAV，也可能是 24 位或不同采样率；转码输入与输出不能同名。
			original := filepath.Join(stage, fmt.Sprintf("%03d-original%s", i, strings.ToLower(filepath.Ext(u.Path))))
			converted := filepath.Join(stage, fmt.Sprintf("%03d.wav", i))
			if err = os.WriteFile(original, b, 0600); err != nil {
				return err
			}
			if _, err = score.InspectSample(original); err == nil {
				err = os.Rename(original, converted)
			} else {
				converter, converterErr := audio.Converter()
				if converterErr != nil {
					return converterErr
				}
				err = audio.ConvertInstrumentWithLimit(ctx, converter, original, converted, catalogSampleMaxSeconds)
				_ = os.Remove(original)
			}
			if err != nil {
				return err
			}
			info, err := os.Stat(converted)
			if err != nil {
				return err
			}
			convertedBytes += info.Size()
			if convertedBytes > 1<<30 {
				return fmt.Errorf("转换后的音源总大小超出限制")
			}
			duration, err := audio.Duration(converted)
			if err != nil {
				return fmt.Errorf("%s · 采样 %d 无法读取音频时长: %w", entry.Name, i+1, err)
			}
			if duration < .05 || duration > catalogSampleMaxSeconds {
				return fmt.Errorf("%s · 采样 %d 时长 %.3f 秒，目录逐音采样须在 0.05 至 %d 秒之间", entry.Name, i+1, duration, catalogSampleMaxSeconds)
			}
			if _, err := score.InspectSample(converted); err != nil {
				return err
			}
			data, err := os.ReadFile(converted)
			if err != nil {
				return err
			}
			convertedHash := sha256.Sum256(data)
			sampleID := schema.NewID()
			itemArticulation := item.Articulation
			if itemArticulation == "" {
				itemArticulation = articulation
			}
			velocityLow, velocityHigh := 1, 127
			if item.VelocityMin != nil {
				velocityLow = max(1, int(*item.VelocityMin*127)+1)
			}
			if item.VelocityMax != nil {
				velocityHigh = min(127, max(velocityLow, int(*item.VelocityMax*127+.5)))
			}
			source.Samples = append(source.Samples, schema.InstrumentSample{ID: sampleID, FileName: "sample-" + sampleID + ".wav",
				SHA256: hex.EncodeToString(convertedHash[:]), RootPitch: item.MIDI, Articulation: itemArticulation,
				VelocityLow: velocityLow, VelocityHigh: velocityHigh, Duration: duration})
			downloadedBytes += item.Bytes
			w.progressOf("instrumentDownload", msg.ActivityDownloading, progressParams, downloadedBytes, expectedBytes)
		}
		w.edit.Lock()
		defer w.edit.Unlock()
		for _, existing := range w.Store.Read().InstrumentSources {
			if existing.CatalogID == id {
				if w.sourceFilesIssue(existing) == "" {
					return nil
				}
				source.ID = existing.ID
			}
		}
		written := []string{}
		keep := false
		defer func() {
			if !keep {
				for _, path := range written {
					_ = os.Remove(path)
				}
			}
		}()
		for i, sample := range source.Samples {
			dest, err := w.Store.MediaPath("instruments", sample.FileName)
			if err != nil {
				return err
			}
			if err = os.Rename(filepath.Join(stage, fmt.Sprintf("%03d.wav", i)), dest); err != nil {
				return err
			}
			written = append(written, dest)
		}
		if err = w.Store.Update(func(s *schema.State) {
			for i := range s.InstrumentSources {
				if s.InstrumentSources[i].ID == source.ID {
					s.InstrumentSources[i] = source
					return
				}
			}
			s.InstrumentSources = append(s.InstrumentSources, source)
		}, true); err != nil {
			return err
		}
		keep = true
		return nil
	})
}
