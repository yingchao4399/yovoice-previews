package workbench

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"yovoice/internal/audio"
	"yovoice/internal/msg"
	"yovoice/internal/schema"
	"yovoice/internal/score"
)

type InstrumentSampleInput struct {
	SourceID     string `json:"sourceId,omitempty"`
	Name         string `json:"name"`
	Instrument   string `json:"instrument"`
	Origin       string `json:"origin"`
	Verification string `json:"verification,omitempty"`
	Author       string `json:"author,omitempty"`
	License      string `json:"license,omitempty"`
	LicenseURL   string `json:"licenseUrl,omitempty"`
	SourceURL    string `json:"sourceUrl,omitempty"`
	RootPitch    int    `json:"rootPitch"`
	Articulation string `json:"articulation"`
	VelocityLow  int    `json:"velocityLow"`
	VelocityHigh int    `json:"velocityHigh"`
}

func (in InstrumentSampleInput) validate() error {
	if schema.TextLen(strings.TrimSpace(in.Name)) < 1 || schema.TextLen(in.Name) > 120 || schema.TextLen(strings.TrimSpace(in.Instrument)) < 1 || schema.TextLen(in.Instrument) > 80 ||
		(in.Origin != "real" && in.Origin != "ai") || in.RootPitch < 0 || in.RootPitch > 127 ||
		schema.TextLen(strings.TrimSpace(in.Articulation)) < 1 || schema.TextLen(in.Articulation) > 40 ||
		in.VelocityLow < 1 || in.VelocityHigh > 127 || in.VelocityLow > in.VelocityHigh ||
		len(in.Author) > 200 || len(in.License) > 100 || len(in.LicenseURL) > 1000 || len(in.SourceURL) > 1000 ||
		(in.Verification != "" && in.Verification != "user" && in.Verification != "metadata") {
		return msg.Err(msg.ErrRequestInvalid, nil)
	}
	return nil
}

// ImportInstrumentSampleFrom 把上传录音转换成托管 WAV；持久化成功前删除临时文件。
func (w *Workbench) ImportInstrumentSampleFrom(ctx context.Context, reader io.Reader, in InstrumentSampleInput) (schema.InstrumentSource, error) {
	if err := in.validate(); err != nil {
		return schema.InstrumentSource{}, err
	}
	path, err := w.receive(reader)
	if err != nil {
		return schema.InstrumentSource{}, err
	}
	defer os.Remove(path)
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	id := schema.NewID()
	file := "sample-" + id + ".wav"
	dest, err := w.Store.MediaPath("instruments", file)
	if err != nil {
		return schema.InstrumentSource{}, err
	}
	if _, err = score.InspectSample(path); err == nil {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return schema.InstrumentSource{}, readErr
		}
		err = os.WriteFile(dest, data, 0600)
	} else {
		converter, converterErr := audio.Converter()
		if converterErr != nil {
			return schema.InstrumentSource{}, converterErr
		}
		err = audio.ConvertInstrument(ctx, converter, path, dest)
	}
	if err != nil {
		_ = os.Remove(dest)
		return schema.InstrumentSource{}, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(dest)
		}
	}()
	duration, err := audio.Duration(dest)
	if err != nil {
		return schema.InstrumentSource{}, err
	}
	if duration < 0.05 || duration > 30 {
		return schema.InstrumentSource{}, fmt.Errorf("单个采样时长必须在 0.05 至 30 秒之间")
	}
	if _, err = score.InspectSample(dest); err != nil {
		return schema.InstrumentSource{}, err
	}
	b, err := os.ReadFile(dest)
	if err != nil {
		return schema.InstrumentSource{}, err
	}
	hash := sha256.Sum256(b)
	sample := schema.InstrumentSample{ID: id, FileName: file, SHA256: hex.EncodeToString(hash[:]), RootPitch: in.RootPitch, Articulation: strings.TrimSpace(in.Articulation), VelocityLow: in.VelocityLow, VelocityHigh: in.VelocityHigh, Duration: duration}
	w.edit.Lock()
	defer w.edit.Unlock()
	state := w.Store.Read()
	source := schema.InstrumentSource{ID: schema.NewID(), Name: strings.TrimSpace(in.Name), Instrument: strings.TrimSpace(in.Instrument), Origin: in.Origin, Verification: in.Verification, DefaultArticulation: strings.TrimSpace(in.Articulation), Author: strings.TrimSpace(in.Author), License: strings.TrimSpace(in.License), LicenseURL: strings.TrimSpace(in.LicenseURL), SourceURL: strings.TrimSpace(in.SourceURL), Samples: []schema.InstrumentSample{sample}}
	if in.SourceID != "" {
		found := false
		for _, existing := range state.InstrumentSources {
			if existing.ID == in.SourceID {
				if existing.Origin != in.Origin || existing.Instrument != strings.TrimSpace(in.Instrument) {
					return schema.InstrumentSource{}, msg.Err(msg.ErrRequestInvalid, nil)
				}
				source = existing
				source.Samples = append(source.Samples, sample)
				found = true
				break
			}
		}
		if !found {
			return schema.InstrumentSource{}, msg.Err(msg.ErrRequestInvalid, nil)
		}
	}
	if err = w.Store.Update(func(s *schema.State) {
		if in.SourceID == "" {
			s.InstrumentSources = append(s.InstrumentSources, source)
			return
		}
		for i := range s.InstrumentSources {
			if s.InstrumentSources[i].ID == in.SourceID {
				s.InstrumentSources[i] = source
				break
			}
		}
	}, true); err != nil {
		return schema.InstrumentSource{}, err
	}
	keep = true
	return source, nil
}

// ConfirmInstrumentSource 在用户试听原站素材后，把自动检索的乐器身份标记为人工确认。
func (w *Workbench) ConfirmInstrumentSource(id string) error {
	if id == "" {
		return msg.Err(msg.ErrRequestInvalid, nil)
	}
	w.edit.Lock()
	defer w.edit.Unlock()
	found := false
	for _, source := range w.Store.Read().InstrumentSources {
		if source.ID == id && source.Verification == "metadata" {
			found = true
			break
		}
	}
	if !found {
		return msg.Err(msg.ErrRequestInvalid, nil)
	}
	return w.Store.Update(func(s *schema.State) {
		for i := range s.InstrumentSources {
			if s.InstrumentSources[i].ID == id {
				s.InstrumentSources[i].Verification = "user"
				break
			}
		}
	}, true)
}

// scoreSources 校验工程实际引用和文件哈希；缺奏法由采样渲染器逐音报告。
func (w *Workbench) scoreSources(s schema.Score) (map[string]score.SampleInstrument, error) {
	state := w.Store.Read()
	result := map[string]score.SampleInstrument{}
	for _, track := range s.Tracks {
		if track.SourceID == "" {
			continue
		}
		if _, ok := result[track.SourceID]; ok {
			continue
		}
		var source *schema.InstrumentSource
		for i := range state.InstrumentSources {
			if state.InstrumentSources[i].ID == track.SourceID {
				source = &state.InstrumentSources[i]
				break
			}
		}
		if source == nil {
			return nil, fmt.Errorf("声部 %s 引用的音源已丢失", track.Name)
		}
		instrument := score.SampleInstrument{DefaultArticulation: source.DefaultArticulation}
		for _, sample := range source.Samples {
			path, err := w.Store.MediaPath("instruments", sample.FileName)
			if err != nil {
				return nil, err
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("音源 %s 的采样缺失: %w", source.Name, err)
			}
			hash := sha256.Sum256(b)
			if hex.EncodeToString(hash[:]) != sample.SHA256 {
				return nil, fmt.Errorf("音源 %s 的采样校验失败", source.Name)
			}
			release := max(0, sample.Duration-sample.SustainSeconds)
			// AI 精确音高采样的释放尾音由生成合同决定，不能被旧的
			// 0.5 秒上限静默截断。真实/旧采样继续沿用原有上限。
			if sample.GenerationID == "" {
				release = min(.5, release)
			}
			instrument.Samples = append(instrument.Samples, score.SampleFile{Path: path, RootPitch: sample.RootPitch, Articulation: sample.Articulation, VelocityLow: sample.VelocityLow, VelocityHigh: sample.VelocityHigh, ExactPitch: sample.GenerationID != "" || sample.ExactPitch, Generated: sample.GenerationID != "", SustainSeconds: sample.SustainSeconds, ReleaseSeconds: release, Duration: sample.Duration})
		}
		result[track.SourceID] = instrument
	}
	return result, nil
}
