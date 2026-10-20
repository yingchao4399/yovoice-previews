package workbench

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"yovoice/internal/catalog"
	"yovoice/internal/diag"
	"yovoice/internal/download"
	"yovoice/internal/msg"
	"yovoice/internal/schema"
)

func (w *Workbench) SavePreferences(p schema.Preferences) error {
	w.edit.Lock()
	defer w.edit.Unlock()
	return w.savePreferences(p)
}

func (w *Workbench) savePreferences(p schema.Preferences) error {
	p.ProxyURL = strings.TrimSpace(p.ProxyURL)
	if p.ProxyEnabled != nil && *p.ProxyEnabled && p.ProxyURL == "" {
		return msg.Err(msg.ErrProxyURL, nil)
	}
	if _, err := download.ParseProxy(p.ProxyURL); err != nil {
		return err
	}
	if _, e := catalog.RuntimeArchives(p.Backend); e != nil {
		return e
	}
	if _, e := catalog.Models[0].URL(p.DownloadSource); e != nil {
		return e
	}
	if p.AITimeout != 0 && (p.AITimeout < schema.AITimeoutMin || p.AITimeout > schema.AITimeoutMax) {
		return msg.Err(msg.ErrRequestInvalid, nil)
	}
	if p.ModelDirectory != nil && !filepath.IsAbs(*p.ModelDirectory) {
		return msg.Err(msg.ErrModelDirAbsolute, nil)
	}
	if p.UiLocale == "" {
		p.UiLocale = w.Store.Read().Preferences.UiLocale
		if p.UiLocale == "" {
			p.UiLocale = schema.UiLocaleZhCN
		}
	} else if _, e := schema.ParseUiLocale(string(p.UiLocale)); e != nil {
		return e
	}
	return w.Store.Update(func(s *schema.State) {
		if s.Preferences.Backend != p.Backend {
			diag.Log(w.Store.Root, "preferences.backend", "from", s.Preferences.Backend, "to", p.Backend)
		}
		s.Preferences = p
		if !w.selectRuntime(s) && runtime.GOOS == "darwin" && s.RuntimePath != nil {
			s.RuntimeBackend = ptr(p.Backend)
		}
	}, true)
}

// SetModelDirectory 只更换模型下载目录，其余偏好保持不变。
func (w *Workbench) SetModelDirectory(path string) error {
	w.edit.Lock()
	defer w.edit.Unlock()
	p := w.Store.Read().Preferences
	p.ModelDirectory = ptr(path)
	if err := w.savePreferences(p); err != nil {
		return err
	}
	return w.adoptModels(path)
}

// adoptModels 登记新模型目录中已有的模型：按应用下载时的布局查找（单文件模型在目录根部，
// 多文件模型在各自子目录），所有文件大小与目录清单一致才视为完整。已登记且文件仍在的模型保持不变。
func (w *Workbench) adoptModels(dir string) error {
	found := map[string]string{}
	for _, m := range catalog.Models {
		path := filepath.Join(dir, filepath.Base(m.RemotePath))
		if len(m.Files) > 0 {
			path = filepath.Join(dir, m.Directory)
			if packageRoot(m, filepath.Join(path, filepath.FromSlash(m.RemotePath))) == "" {
				continue
			}
		} else if download.FileSize(path) != m.Size {
			continue
		}
		found[m.ID] = path
	}
	if len(found) == 0 {
		return nil
	}
	return w.Store.Update(func(s *schema.State) {
		for _, m := range s.Models {
			if _, err := os.Stat(m.Path); err == nil {
				delete(found, m.ID)
			}
		}
		for id, path := range found {
			s.Models = slices.DeleteFunc(s.Models, func(m schema.InstalledModel) bool { return m.ID == id })
			s.Models = append(s.Models, schema.InstalledModel{ID: id, Path: path, Managed: true})
		}
		diag.Log(w.Store.Root, "model.directory_adopted", "count", len(found))
	}, true)
}
