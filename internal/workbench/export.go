package workbench

import (
	"io"
	"os"
	"path/filepath"
	"yovoice/internal/msg"
	"yovoice/internal/schema"
)

// 时间线等界面里合成的音频由页面直接上传给本地服务落成临时文件，宿主弹出保存框后复制再删除，
// 不经宿主消息通道传音频字节，长音频不受消息大小限制。同一时间只保留最近一份，启动时回收遗留文件。
const exportPrefix = "export-"

func (w *Workbench) exportFile(id string) string {
	return filepath.Join(w.Store.Root, "downloads", exportPrefix+id+".wav")
}

// SaveExport 校验 WAV 文件头后写入临时文件，返回宿主取文件用的 ID。
func (w *Workbench) SaveExport(r io.Reader) (string, error) {
	head := make([]byte, 12)
	if _, e := io.ReadFull(r, head); e != nil || string(head[:4]) != "RIFF" || string(head[8:]) != "WAVE" {
		return "", msg.Err(msg.ErrAudioFormat, nil)
	}
	dir := filepath.Join(w.Store.Root, "downloads")
	if e := os.MkdirAll(dir, 0700); e != nil {
		return "", e
	}
	old, _ := filepath.Glob(filepath.Join(dir, exportPrefix+"*.wav"))
	for _, f := range old {
		_ = os.Remove(f)
	}
	id := schema.NewID()
	path := w.exportFile(id)
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return "", e
	}
	if _, e = f.Write(head); e == nil {
		_, e = io.Copy(f, r)
	}
	if c := f.Close(); e == nil {
		e = c
	}
	if e != nil {
		_ = os.Remove(path)
		return "", e
	}
	return id, nil
}

// ExportPath 返回 SaveExport 写入的文件路径，由宿主复制到用户选择的位置后删除。
func (w *Workbench) ExportPath(id string) (string, error) {
	if !schema.ValidID(id) {
		return "", msg.Err(msg.ErrAudioIDInvalid, nil)
	}
	path := w.exportFile(id)
	if _, e := os.Stat(path); e != nil {
		return "", msg.Err(msg.ErrAudioMissing, nil)
	}
	return path, nil
}
