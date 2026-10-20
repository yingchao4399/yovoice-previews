package workbench

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"yovoice/internal/msg"
	"yovoice/internal/platform"
	"yovoice/internal/schema"
	"yovoice/internal/score"
)

type PluginInstrumentInput struct {
	Path         string `json:"path"`
	Editor       bool   `json:"editor,omitempty"`
	PresetPath   string `json:"presetPath,omitempty"`
	Name         string `json:"name"`
	Instrument   string `json:"instrument"`
	Program      string `json:"program,omitempty"`
	Provenance   string `json:"provenance"`
	Articulation string `json:"articulation"`
}

func pluginModule(path string) (string, error) {
	if !slices.ContainsFunc(ScanPlugins(), func(p PluginInfo) bool { return strings.EqualFold(p.Path, path) }) {
		return "", fmt.Errorf("插件不在已扫描的 VST3 目录中")
	}
	info, e := os.Stat(path)
	if e != nil {
		return "", e
	}
	if info.IsDir() {
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		path = filepath.Join(path, "Contents", "x86_64-win", name+".vst3")
	}
	return path, nil
}

func vstHostCommand(ctx context.Context, args ...string) (*exec.Cmd, error) {
	service, e := os.Executable()
	if e != nil {
		return nil, e
	}
	host := filepath.Join(filepath.Dir(filepath.Dir(service)), "tools", "yovoice-vst-host.exe")
	if override := os.Getenv("YOVOICE_VST_HOST"); override != "" {
		host = override
	}
	if _, e = os.Stat(host); e != nil {
		return nil, fmt.Errorf("找不到 VST3 宿主：%w", e)
	}
	cmd := exec.CommandContext(ctx, host, args...)
	platform.ConfigureProcess(cmd)
	return cmd, nil
}

func (w *Workbench) CapturePluginInstrument(in PluginInstrumentInput) (schema.InstrumentSource, error) {
	return w.capturePluginInstrument(context.Background(), in)
}

// 原生编辑器的操作可以超过桥接请求超时，交给可取消的后台任务。
func (w *Workbench) StartPluginInstrument(in PluginInstrumentInput) error {
	_, e := w.start("pluginCapture", msg.ActivityRendering, nil, nil, func(ctx context.Context) error { _, e := w.capturePluginInstrument(ctx, in); return e }, "", "")
	return e
}
func (w *Workbench) capturePluginInstrument(parent context.Context, in PluginInstrumentInput) (schema.InstrumentSource, error) {
	if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Instrument) == "" || len(in.Name) > 120 || len(in.Instrument) > 120 || (in.Provenance != "recorded" && in.Provenance != "synthesized") || len(in.Articulation) > 40 {
		return schema.InstrumentSource{}, fmt.Errorf("请填写名称、乐器及真实录音或合成模拟来源")
	}
	module, e := pluginModule(in.Path)
	if e != nil {
		return schema.InstrumentSource{}, e
	}
	stage, e := os.MkdirTemp(filepath.Join(w.Store.Root, "downloads"), "plugin-preset-")
	if e != nil {
		return schema.InstrumentSource{}, e
	}
	defer os.RemoveAll(stage)
	state := filepath.Join(stage, "preset.vstpreset")
	args := []string{"--plugin", module, "--save-state", state}
	if in.PresetPath != "" {
		info, e := os.Stat(in.PresetPath)
		if e != nil || !info.Mode().IsRegular() || info.Size() > 16<<20 || !strings.EqualFold(filepath.Ext(in.PresetPath), ".vstpreset") {
			return schema.InstrumentSource{}, fmt.Errorf("请选择有效的 VST3 预设文件")
		}
		args = append(args, "--state-in", in.PresetPath)
	}
	if in.Program != "" {
		args = append(args, "--program", in.Program)
	}
	if in.Editor {
		args = append(args, "--editor")
	}
	ctx, cancel := context.WithTimeout(parent, 10*time.Minute)
	defer cancel()
	cmd, e := vstHostCommand(ctx, args...)
	if e != nil {
		return schema.InstrumentSource{}, e
	}
	b, e := cmd.CombinedOutput()
	if e != nil {
		return schema.InstrumentSource{}, fmt.Errorf("预设保存失败：%s", strings.TrimSpace(string(b)))
	}
	data, e := os.ReadFile(state)
	if e != nil || len(data) == 0 || len(data) > 16<<20 {
		return schema.InstrumentSource{}, fmt.Errorf("插件没有返回有效的预设状态")
	}
	moduleData, e := os.ReadFile(module)
	if e != nil {
		return schema.InstrumentSource{}, e
	}
	h := sha256.Sum256(moduleData)
	if in.Articulation == "" {
		in.Articulation = "normal"
	}
	source := schema.InstrumentSource{ID: schema.NewID(), Name: in.Name, Instrument: in.Instrument, Origin: "real", Provenance: in.Provenance, Verification: "user", DefaultArticulation: in.Articulation, Samples: []schema.InstrumentSample{}, Plugin: &schema.PluginInstrument{Path: in.Path, Name: filepath.Base(in.Path), SHA256: hex.EncodeToString(h[:]), StateData: data, Articulations: []string{in.Articulation}}}
	source.Plugin.Dependencies = pluginDependencies(data)
	music := schema.Score{Tempo: 90, TimeSignature: []int{4, 4}, Tracks: []schema.ScoreTrack{{ID: "capture", Name: source.Instrument, Notes: []schema.ScoreNote{{Bar: 1, Beat: 1, Pitch: 62, Length: 2, Velocity: 90}}}}}
	if e = w.renderPluginTrack(ctx, music, source, filepath.Join(stage, "audition.wav")); e != nil {
		return schema.InstrumentSource{}, fmt.Errorf("当前预设未通过实际发声检查，尚未入库；请在原生编辑器加载音色并检查插件兼容性：%w", e)
	}
	source.Plugin.Validated = true
	e = w.Store.Update(func(s *schema.State) { s.InstrumentSources = append(s.InstrumentSources, source) }, true)
	return source, e
}

func pluginFileIssue(source schema.InstrumentSource) string {
	p := source.Plugin
	if p == nil {
		return "SOURCE_FILE_MISSING"
	}
	module, e := pluginModule(p.Path)
	if e != nil || len(p.StateData) == 0 {
		return "PLUGIN_PRESET_MISSING"
	}
	b, e := os.ReadFile(module)
	if e != nil {
		return "PLUGIN_PRESET_MISSING"
	}
	h := sha256.Sum256(b)
	if hex.EncodeToString(h[:]) != p.SHA256 {
		return "PLUGIN_VERSION_CHANGED"
	}
	return ""
}

func (w *Workbench) renderPluginTrack(ctx context.Context, music schema.Score, source schema.InstrumentSource, output string) error {
	if issue := pluginFileIssue(source); issue != "" {
		return fmt.Errorf("%s：%s", source.Name, issue)
	}
	module, e := pluginModule(source.Plugin.Path)
	if e != nil {
		return e
	}
	stage, e := os.MkdirTemp(filepath.Join(w.Store.Root, "downloads"), "plugin-render-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(stage)
	state, midi := filepath.Join(stage, "preset.vstpreset"), filepath.Join(stage, "score.mid")
	if e = os.WriteFile(state, source.Plugin.StateData, 0600); e != nil {
		return e
	}
	bytes, e := score.PerformanceMIDI(music)
	if e != nil {
		return e
	}
	if e = os.WriteFile(midi, bytes, 0600); e != nil {
		return e
	}
	duration := music.Seconds(music.Bars()+1, 1) + 1
	cmd, e := vstHostCommand(ctx, "--plugin", module, "--state-in", state, "--midi", midi, "--validate-notes", "--duration", fmt.Sprintf("%.6f", duration), "--output", output)
	if e != nil {
		return e
	}
	b, e := cmd.CombinedOutput()
	var report struct {
		Peak         float64 `json:"peak"`
		MissingNotes []struct {
			Note     int `json:"note"`
			Velocity int `json:"velocity"`
		} `json:"missingNotes"`
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	parsed := json.Unmarshal([]byte(lines[len(lines)-1]), &report)
	if parsed == nil && len(report.MissingNotes) > 0 {
		missing := report.MissingNotes[0]
		bar := 0
		for _, track := range music.Tracks {
			for _, n := range track.Notes {
				if n.Pitch == missing.Note {
					bar = n.Bar
					break
				}
			}
		}
		return msg.Err(msg.ErrArrangementBlocked, msg.Params{"detail": fmt.Sprintf("%s 的当前预设无法演奏第%d小节 MIDI %d、力度 %d；请更换预设或局部改谱", source.Name, bar, missing.Note, missing.Velocity), "reason": "PLUGIN_NOTE_UNSUPPORTED", "bar": bar, "pitch": missing.Note})
	}
	if e != nil {
		return fmt.Errorf("VST 整轨渲染失败：%s", strings.TrimSpace(string(b)))
	}
	if parsed != nil || report.Peak < .001 {
		return fmt.Errorf("插件当前预设未产生有效音频")
	}
	if len(music.Tracks) == 1 {
		return score.NormalizeStemFile(output, music.Tracks[0])
	}
	return nil

}

// 编辑器关闭后提交状态；并发编辑音源时保留后来的修改。
func (w *Workbench) OpenInstrumentEditor(id string) error {
	sources := w.Store.Read().InstrumentSources
	idx := slices.IndexFunc(sources, func(s schema.InstrumentSource) bool { return s.ID == id })
	if idx < 0 || sources[idx].Plugin == nil {
		return fmt.Errorf("此音源不是已保存的插件预设")
	}
	source := sources[idx]
	module, e := pluginModule(source.Plugin.Path)
	if e != nil {
		return e
	}
	stage, e := os.MkdirTemp(filepath.Join(w.Store.Root, "downloads"), "plugin-editor-")
	if e != nil {
		return e
	}
	state := filepath.Join(stage, "preset.vstpreset")
	if e = os.WriteFile(state, source.Plugin.StateData, 0600); e != nil {
		os.RemoveAll(stage)
		return e
	}
	_, e = w.start("pluginEditor", msg.ActivityRendering, nil, nil, func(ctx context.Context) error {
		defer os.RemoveAll(stage)
		cmd, err := vstHostCommand(ctx, "--plugin", module, "--state-in", state, "--save-state", state, "--editor")
		if err != nil {
			return err
		}
		if err = cmd.Run(); err != nil {
			return err
		}
		b, err := os.ReadFile(state)
		if err != nil || len(b) == 0 || len(b) > 16<<20 {
			return fmt.Errorf("插件没有返回有效预设状态")
		}
		applied := false
		err = w.Store.Update(func(s *schema.State) {
			for i := range s.InstrumentSources {
				p := s.InstrumentSources[i].Plugin
				if s.InstrumentSources[i].ID == id && p != nil && slices.Equal(p.StateData, source.Plugin.StateData) {
					if !slices.Equal(p.StateData, b) {
						p.Validated = false
					}
					p.StateData = b
					p.Dependencies = pluginDependencies(b)
					applied = true
				}
			}
		}, true)
		if err == nil && !applied {
			return fmt.Errorf("编辑器打开期间音源已有修改，未覆盖新预设")
		}
		return err
	}, "", "")
	if e != nil {
		os.RemoveAll(stage)
	}
	return e
}

// AuditionPluginInstrument 使用保存的音色渲染一个短乐句，不注入宿主连通性测试音色。
func (w *Workbench) AuditionPluginInstrument(id string) (map[string]string, error) {
	sources := w.Store.Read().InstrumentSources
	i := slices.IndexFunc(sources, func(s schema.InstrumentSource) bool { return s.ID == id })
	if i < 0 || sources[i].Plugin == nil {
		return nil, fmt.Errorf("插件音色不存在")
	}
	file := "import-" + schema.NewID() + ".wav"
	path, err := w.Store.MediaPath("outputs", file)
	if err != nil {
		return nil, err
	}
	music := schema.Score{Tempo: 90, TimeSignature: []int{4, 4}, Tracks: []schema.ScoreTrack{{ID: "preset-audition", Name: sources[i].Instrument, Notes: []schema.ScoreNote{{Bar: 1, Beat: 1, Pitch: 60, Length: 1, Velocity: 80}, {Bar: 1, Beat: 2, Pitch: 64, Length: 1, Velocity: 95}, {Bar: 1, Beat: 3, Pitch: 67, Length: 2, Velocity: 70}}}}}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err = w.renderPluginTrack(ctx, music, sources[i], path); err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	expectedState := sources[i].Plugin.StateData
	_ = w.Store.Update(func(s *schema.State) {
		for i := range s.InstrumentSources {
			if s.InstrumentSources[i].ID == id && s.InstrumentSources[i].Plugin != nil && slices.Equal(s.InstrumentSources[i].Plugin.StateData, expectedState) {
				s.InstrumentSources[i].Plugin.Validated = true
			}
		}
	}, true)
	return map[string]string{"fileName": file}, nil
}

// 仅登记预设中明确写出的本机音频与音色包依赖；不扫描其他用户文件。
func pluginDependencies(data []byte) []schema.PluginDependency {
	pattern := regexp.MustCompile(`(?i)[A-Z]:[\\/][^\x00\r\n"<>]+?\.(?:mse|dspreset|wav|flac|aiff|aif|mp3|ogg)`)
	var out []schema.PluginDependency
	seen := map[string]bool{}
	for _, match := range pattern.FindAllString(string(data), 4096) {
		path := html.UnescapeString(match)
		if seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, schema.PluginDependency{Path: path, SHA256: fileDigest(path)})
	}
	return out
}
