package workbench

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"yovoice/internal/catalog"
	"yovoice/internal/diag"
	"yovoice/internal/download"
	"yovoice/internal/msg"
	"yovoice/internal/platform"
	"yovoice/internal/schema"
	"yovoice/internal/store"
)

func (w *Workbench) register(id, path string, managed bool) error {
	path, e := filepath.Abs(path)
	if e != nil {
		return e
	}
	return w.Store.Update(func(s *schema.State) {
		s.Models = slices.DeleteFunc(s.Models, func(m schema.InstalledModel) bool { return m.ID == id })
		s.Models = append(s.Models, schema.InstalledModel{ID: id, Path: path, Managed: managed})
	}, true)
}
func (w *Workbench) DownloadModel(id string) error {
	m, e := catalog.Lookup(id)
	if e != nil {
		return e
	}
	s := w.Store.Read()
	// 重新下载已安装的模型会替换正在使用的文件，需等生成结束；未安装的模型可与生成同时下载。
	if slices.ContainsFunc(s.Models, func(installed schema.InstalledModel) bool { return installed.ID == id }) {
		w.mu.Lock()
		busy := w.cancel != nil
		w.mu.Unlock()
		if busy {
			return msg.Err(msg.ErrBusy, nil)
		}
	}
	files := m.PackageFiles()
	urls := make([]string, len(files))
	for i, f := range files {
		if urls[i], e = m.FileURL(s.Preferences.DownloadSource, f.Path); e != nil {
			return e
		}
	}
	// Kokoro 的发音库不随安装包分发，随模型一起下载，进度计入同一个模型包。
	var wheels []catalog.KokoroWheel
	if m.Family == "kokoro_tts" {
		if wheels, e = kokoroMissing(w.Store.Root); e != nil {
			return e
		}
	}
	total := m.Size
	for _, wheel := range wheels {
		total += wheel.Size
	}
	return w.begin("download", msg.ActivityDownload, msg.Params{"name": m.Name}, ptr(id), func(ctx context.Context) error {
		dir := value(s.Preferences.ModelDirectory)
		if dir == "" {
			dir = filepath.Join(w.Store.Root, "models")
		}
		// 单文件模型直接放在模型目录；多文件模型放进自己的子目录并保留相对路径。
		root, dest := dir, filepath.Join(dir, filepath.Base(m.RemotePath))
		if len(m.Files) > 0 {
			root = filepath.Join(dir, m.Directory)
			dest = root
		}
		if e := os.MkdirAll(root, 0700); e != nil {
			return e
		}
		target := func(f catalog.ModelFile) string {
			if len(m.Files) > 0 {
				return filepath.Join(root, filepath.FromSlash(f.Path))
			}
			return dest
		}
		available, e := platform.FreeSpace(dir)
		if e != nil {
			return e
		}
		need := int64(100<<20) + total - m.Size
		for _, f := range files {
			need += max(int64(0), f.Size-download.FileSize(target(f)+".part")-download.FileSize(target(f)))
		}
		if available < uint64(need) {
			return msg.Err(msg.ErrModelDirSpace, nil)
		}
		// 进度按整个模型包累计，多个文件依次下载。
		var done int64
		for i, f := range files {
			if e = download.File(ctx, w.client, urls[i], target(f), f.SHA256, f.Size, func(r, _ int64) {
				code, params := msg.ActivityDownloading, msg.Params{"name": m.Name}
				if done+r == total {
					code, params = msg.ActivityVerifying, nil
				}
				w.progressOf("download", code, params, done+r, total)
			}); e != nil {
				return e
			}
			done += f.Size
		}
		if len(wheels) > 0 {
			if e = w.installKokoro(ctx, s.Preferences.DownloadSource, func(r, _ int64) {
				w.progressOf("download", msg.ActivityDownloading, msg.Params{"name": m.Name}, done+r, total)
			}); e != nil {
				return e
			}
		}
		return w.register(id, dest, true)
	})
}

// ForgetModel 移除模型登记。应用自己下载的模型同时删除文件释放空间；模型目录中原有的模型只解除登记，保留本地文件。
// 生成等主操作进行中不能移除（会停止推理引擎）；下载其他模型时可同时移除。
func (w *Workbench) ForgetModel(id string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	state := w.Store.Read()
	activity := state.ModelActivity
	if w.cancel != nil || (w.installCancel != nil && (activity == nil || activity.Kind != "download" || activity.ModelID == nil || *activity.ModelID == id)) {
		return msg.Err(msg.ErrModelForgetBlocked, nil)
	}
	w.engine.Stop()
	if err := w.Store.Update(func(s *schema.State) {
		s.Models = slices.DeleteFunc(s.Models, func(m schema.InstalledModel) bool { return m.ID == id })
	}, true); err != nil {
		return err
	}
	if path := managedModelPath(state, w.Store.Root, id); path != "" {
		if err := os.RemoveAll(path); err != nil {
			diag.Log(w.Store.Root, "model.files_remove_failed", "model_id", id, "error", diag.Error(err))
		}
	}
	return nil
}

// managedModelPath 返回可安全删除的下载模型路径：必须是应用下载的模型，位于模型目录内部（不是目录本身），
// 且没有其他已登记模型使用同一位置；否则返回空串，只解除登记。
func managedModelPath(s schema.State, root, id string) string {
	i := slices.IndexFunc(s.Models, func(m schema.InstalledModel) bool { return m.ID == id })
	if i < 0 || !s.Models[i].Managed || s.Models[i].Path == "" {
		return ""
	}
	path := filepath.Clean(s.Models[i].Path)
	dirs := []string{filepath.Join(root, "models")}
	if dir := value(s.Preferences.ModelDirectory); dir != "" {
		dirs = append(dirs, dir)
	}
	inside := slices.ContainsFunc(dirs, func(dir string) bool { return store.Within(dir, path) && !store.SamePath(dir, path) })
	if !inside {
		return ""
	}
	for j, m := range s.Models {
		if j != i && m.Path != "" && (store.Within(path, m.Path) || store.Within(m.Path, path)) {
			return ""
		}
	}
	return path
}

// packageRoot 根据主模型文件位置推出多文件模型包的目录，文件不全时返回空串。
func packageRoot(m catalog.ModelPackage, main string) string {
	root := filepath.Dir(main)
	for i := 0; i < strings.Count(m.RemotePath, "/"); i++ {
		root = filepath.Dir(root)
	}
	for _, f := range m.Files {
		if download.FileSize(filepath.Join(root, filepath.FromSlash(f.Path))) != f.Size {
			return ""
		}
	}
	return root
}
