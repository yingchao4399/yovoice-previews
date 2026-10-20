package workbench

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"yovoice/internal/diag"
	"yovoice/internal/msg"
)

// importLegacyPrivate 把旧版单独存放的 local-api.json 与 secrets.json 导入状态库的私有设置，导入成功后删除原文件。
// 这两份文件曾绕过状态库：迁移冻结后仍可写入，写入随旧目录一起丢失。
func (w *Workbench) importLegacyPrivate() error {
	legacy := []struct {
		file string
		key  string
		into func() any
	}{
		{"local-api.json", localAPIPrivate, func() any { return &LocalAPISettings{} }},
		{"secrets.json", aiKeysPrivate, func() any { return &map[string]string{} }},
	}
	for _, item := range legacy {
		path := filepath.Join(w.Store.Root, item.file)
		b, e := os.ReadFile(path)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return e
		}
		value := item.into()
		if e = json.Unmarshal(b, value); e != nil {
			return msg.Err(msg.ErrStateCorrupt, nil)
		}
		// 状态库里已有同名设置说明之前导入过（随后删除原文件前中断），以状态库为准。
		if exists, e := w.Store.Private(item.key, item.into()); e != nil {
			return e
		} else if !exists {
			if e = w.Store.SetPrivate(item.key, value); e != nil {
				return e
			}
		}
		if e = os.Remove(path); e != nil {
			return e
		}
		diag.Log(w.Store.Root, "state.private_imported", "file", item.file)
	}
	return nil
}
