package workbench

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
	"yovoice/internal/diag"
	"yovoice/internal/platform"
	"yovoice/internal/schema"
)

// PluginInfo 是本机发现的插件包；发现插件不代表已完成宿主加载。
type PluginInfo struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Format string `json:"format"`
}

type PluginProbeResult struct {
	Name     string  `json:"name"`
	FileName string  `json:"fileName"`
	Peak     float64 `json:"peak"`
}

// OpenPluginEditor 在独立宿主进程中打开插件自己的编辑器窗口。
// VST3 安装后不会自动弹出窗口，只有宿主插入并显式打开编辑器时才会显示。
func (w *Workbench) OpenPluginEditor(path string) error {
	plugins := ScanPlugins()
	found := slices.IndexFunc(plugins, func(p PluginInfo) bool { return strings.EqualFold(p.Path, path) })
	if found < 0 {
		return fmt.Errorf("插件不在已扫描的 VST3 目录中")
	}
	plugin := plugins[found]
	module := plugin.Path
	if info, err := os.Stat(plugin.Path); err != nil {
		return err
	} else if info.IsDir() {
		name := strings.TrimSuffix(filepath.Base(plugin.Path), filepath.Ext(plugin.Path))
		module = filepath.Join(plugin.Path, "Contents", "x86_64-win", name+".vst3")
	}
	if _, err := os.Stat(module); err != nil {
		return fmt.Errorf("找不到插件的 64 位模块：%w", err)
	}
	service, err := os.Executable()
	if err != nil {
		return err
	}
	host := filepath.Join(filepath.Dir(filepath.Dir(service)), "tools", "yovoice-vst-host.exe")
	if _, err := os.Stat(host); err != nil {
		return fmt.Errorf("此安装包未包含 VST3 宿主")
	}
	cmd := exec.Command(host, "--plugin", module, "--editor")
	cmd.Dir = filepath.Dir(host)
	platform.ConfigureProcess(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("插件操作台启动失败：%w", err)
	}
	go func() {
		if err := cmd.Wait(); err != nil {
			diag.Log(w.Store.Root, "plugin.editor_failed", "plugin", plugin.Name, "error", diag.Error(err))
		}
	}()
	return nil
}

// InstallPlugin 将用户已下载并解压的 VST3 文件或 bundle 安装到用户级目录。
// 不执行插件或厂商安装器；已有同名插件时拒绝覆盖。
func InstallPlugin(source string) (PluginInfo, error) {
	if runtime.GOOS != "windows" {
		return PluginInfo{}, fmt.Errorf("手动安装 VST3 目前仅支持 Windows")
	}
	source, err := filepath.Abs(filepath.Clean(source))
	if err != nil {
		return PluginInfo{}, err
	}
	if !strings.EqualFold(filepath.Ext(source), ".vst3") {
		return PluginInfo{}, fmt.Errorf("请选择已解压的 .vst3 插件文件或文件夹")
	}
	info, err := os.Lstat(source)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
		return PluginInfo{}, fmt.Errorf("所选路径不是有效的 VST3 插件文件或文件夹")
	}
	name := strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
	module := source
	if info.IsDir() {
		module = filepath.Join(source, "Contents", "x86_64-win", name+".vst3")
	}
	if moduleInfo, moduleErr := os.Stat(module); moduleErr != nil || !moduleInfo.Mode().IsRegular() {
		return PluginInfo{}, fmt.Errorf("插件包缺少 64 位 Windows 模块 Contents/x86_64-win/%s.vst3", name)
	}
	base := filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Common Files", "VST3")
	if os.Getenv("LOCALAPPDATA") == "" {
		return PluginInfo{}, fmt.Errorf("找不到当前用户的本地应用目录")
	}
	if err := os.MkdirAll(base, 0700); err != nil {
		return PluginInfo{}, err
	}
	destination := filepath.Join(base, filepath.Base(source))
	if _, err := os.Lstat(destination); err == nil {
		return PluginInfo{}, fmt.Errorf("用户级 VST3 目录中已存在同名插件；为避免覆盖，请先通过 Windows 设置卸载或移走旧版本")
	} else if !os.IsNotExist(err) {
		return PluginInfo{}, err
	}
	stage, err := os.MkdirTemp(base, ".yovoice-vst3-*")
	if err != nil {
		return PluginInfo{}, err
	}
	defer os.RemoveAll(stage)
	staged := filepath.Join(stage, filepath.Base(source))
	if info.Mode().IsRegular() {
		// DSK 等单文件插件把音色放在同名 .instruments 目录；只复制 DLL 会得到静音插件。
		companion := filepath.Join(filepath.Dir(source), name+".instruments")
		companionTarget := filepath.Join(base, filepath.Base(companion))
		companionStage := filepath.Join(stage, filepath.Base(companion))
		companionInfo, companionErr := os.Lstat(companion)
		if companionErr != nil && !os.IsNotExist(companionErr) {
			return PluginInfo{}, companionErr
		}
		if strings.EqualFold(name, "DSK Asian DreamZ") && os.IsNotExist(companionErr) {
			return PluginInfo{}, fmt.Errorf("DSK Asian DreamZ 缺少配套音色目录 %s，请从完整压缩包安装", filepath.Base(companion))
		}
		if companionErr == nil {
			if companionInfo.Mode()&os.ModeSymlink != 0 || !companionInfo.IsDir() {
				return PluginInfo{}, fmt.Errorf("配套音色目录不是普通文件夹")
			}
			if _, err := os.Lstat(companionTarget); err == nil {
				return PluginInfo{}, fmt.Errorf("用户级 VST3 目录中已存在同名配套音色目录")
			} else if !os.IsNotExist(err) {
				return PluginInfo{}, err
			}
			if err := copyPluginDirectory(companion, companionStage); err != nil {
				return PluginInfo{}, err
			}
		}
		if info.Size() > 2<<30 {
			return PluginInfo{}, fmt.Errorf("插件文件超过 2 GiB")
		}
		in, err := os.Open(source)
		if err != nil {
			return PluginInfo{}, err
		}
		defer in.Close()
		dst, err := os.OpenFile(staged, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return PluginInfo{}, err
		}
		_, copyErr := io.Copy(dst, io.LimitReader(in, (2<<30)+1))
		closeErr := dst.Close()
		if copyErr != nil {
			return PluginInfo{}, copyErr
		}
		if closeErr != nil {
			return PluginInfo{}, closeErr
		}
		if companionErr == nil {
			if err := os.Rename(companionStage, companionTarget); err != nil {
				return PluginInfo{}, err
			}
		}
		if err := os.Rename(staged, destination); err != nil {
			if companionErr == nil {
				_ = os.RemoveAll(companionTarget)
			}
			return PluginInfo{}, err
		}
		return PluginInfo{Name: name, Path: destination, Format: "VST3"}, nil
	}
	var total int64
	err = filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 || entry.Type()&fs.ModeDevice != 0 || entry.Type()&fs.ModeNamedPipe != 0 || entry.Type()&fs.ModeSocket != 0 {
			return fmt.Errorf("插件包包含不支持的链接或特殊文件")
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		out := filepath.Join(staged, relative)
		if entry.IsDir() {
			return os.MkdirAll(out, 0700)
		}
		fileInfo, err := entry.Info()
		if err != nil {
			return err
		}
		if !fileInfo.Mode().IsRegular() || fileInfo.Size() < 0 || fileInfo.Size() > 2<<30 || total > 2<<30-fileInfo.Size() {
			return fmt.Errorf("插件包包含非普通文件或总大小超过 2 GiB")
		}
		total += fileInfo.Size()
		if err := os.MkdirAll(filepath.Dir(out), 0700); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		dst, err := os.OpenFile(out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			in.Close()
			return err
		}
		_, copyErr := io.Copy(dst, in)
		in.Close()
		closeErr := dst.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if err != nil {
		return PluginInfo{}, err
	}
	if err := os.Rename(staged, destination); err != nil {
		return PluginInfo{}, err
	}
	return PluginInfo{Name: name, Path: destination, Format: "VST3"}, nil
}

func copyPluginDirectory(source, destination string) error {
	var total int64
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 || entry.Type()&fs.ModeDevice != 0 || entry.Type()&fs.ModeNamedPipe != 0 || entry.Type()&fs.ModeSocket != 0 {
			return fmt.Errorf("配套音色目录包含不支持的链接或特殊文件")
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		out := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(out, 0700)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > 2<<30 || total > 2<<30-info.Size() {
			return fmt.Errorf("配套音色目录包含非普通文件或总大小超过 2 GiB")
		}
		total += info.Size()
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		dst, err := os.OpenFile(out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			in.Close()
			return err
		}
		_, copyErr := io.Copy(dst, in)
		in.Close()
		closeErr := dst.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}

// ProbePlugin 在隔离进程中向已扫描的 VST3 发送测试 MIDI，并保存可试听结果。
func (w *Workbench) ProbePlugin(path string) (PluginProbeResult, error) {
	result := PluginProbeResult{}
	if !slices.ContainsFunc(ScanPlugins(), func(p PluginInfo) bool { return strings.EqualFold(p.Path, path) }) {
		return result, fmt.Errorf("插件不在已扫描的 VST3 目录中")
	}
	module := path
	if info, err := os.Stat(path); err != nil {
		return result, err
	} else if info.IsDir() {
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		module = filepath.Join(path, "Contents", "x86_64-win", name+".vst3")
	}
	if _, err := os.Stat(module); err != nil {
		return result, fmt.Errorf("找不到插件的 64 位模块：%w", err)
	}
	service, err := os.Executable()
	if err != nil {
		return result, err
	}
	host := filepath.Join(filepath.Dir(filepath.Dir(service)), "tools", "yovoice-vst-host.exe")
	if _, err := os.Stat(host); err != nil {
		return result, fmt.Errorf("此安装包未包含 VST3 宿主")
	}
	fileName := "vst-test-" + schema.NewID() + ".wav"
	output, err := w.Store.MediaPath("outputs", fileName)
	if err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	data, err := exec.CommandContext(ctx, host, "--plugin", module, "--output", output).CombinedOutput()
	if err != nil {
		_ = os.Remove(output)
		if ctx.Err() != nil {
			return result, fmt.Errorf("插件试音超时")
		}
		return result, fmt.Errorf("插件试音失败：%s", strings.TrimSpace(string(data)))
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &result); err != nil {
		_ = os.Remove(output)
		return PluginProbeResult{}, fmt.Errorf("插件宿主没有返回有效结果：%w", err)
	}
	result.FileName = fileName
	return result, nil
}

// ScanPlugins 只读扫描常见 Windows VST3 目录，不加载不可信插件代码。
func ScanPlugins() []PluginInfo {
	plugins := []PluginInfo{}
	if runtime.GOOS != "windows" {
		return plugins
	}
	roots := []string{}
	for _, base := range []string{os.Getenv("COMMONPROGRAMFILES"), os.Getenv("LOCALAPPDATA"), os.Getenv("APPDATA")} {
		if base != "" {
			roots = append(roots, filepath.Join(base, "VST3"))
		}
	}
	if base := os.Getenv("LOCALAPPDATA"); base != "" {
		roots = append(roots, filepath.Join(base, "Programs", "Common Files", "VST3"))
	}
	if base := os.Getenv("PROGRAMFILES"); base != "" {
		roots = append(roots, filepath.Join(base, "Common Files", "VST3"))
	}
	seen := map[string]bool{}
	for _, root := range roots {
		visited := 0
		_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return filepath.SkipDir
			}
			visited++
			if visited > 5000 || len(plugins) >= 256 {
				return filepath.SkipAll
			}
			if entry.Type()&os.ModeSymlink != 0 {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if path != root && strings.Count(strings.TrimPrefix(path, root), string(filepath.Separator)) > 5 {
				return filepath.SkipDir
			}
			if strings.EqualFold(filepath.Ext(entry.Name()), ".vst3") {
				if !seen[path] {
					plugins = append(plugins, PluginInfo{Name: strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())), Path: path, Format: "VST3"})
					seen[path] = true
				}
				if entry.IsDir() {
					return filepath.SkipDir
				}
			}
			return nil
		})
	}
	slices.SortFunc(plugins, func(a, b PluginInfo) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	return plugins
}
